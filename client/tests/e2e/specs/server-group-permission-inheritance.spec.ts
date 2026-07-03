/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper } from '../helpers/api.helper';
import { AuthHelper } from '../helpers/auth.helper';
import { LoginPage } from '../pages/LoginPage';
import { AdminPage } from '../pages/AdminPage';
import { GroupManagementPage } from '../pages/GroupManagementPage';
import { ServerNavigatorPage } from '../pages/ServerNavigatorPage';
import { ChatPage } from '../pages/ChatPage';
import {
    ADMIN_USER,
    API_URL,
    BASE_URL,
    PERMISSIONS,
    TEST_USER_PASSWORD,
    makeTestUsername,
} from '../fixtures/test-data';
import { LLM_CONFIG } from '../fixtures/llm-config';

// ---------------------------------------------------------------
// Helper: inject a fresh admin session cookie into the page
// context without touching .auth/admin.json.
// ---------------------------------------------------------------
async function injectFreshAdminSession(
    page: import('@playwright/test').Page,
    api: ApiHelper,
): Promise<void> {
    const { cookie } = await api.login(ADMIN_USER.username, ADMIN_USER.password);
    const sessionValue = cookie.split('=').slice(1).join('=');
    const { hostname } = new URL(BASE_URL);
    await page.context().addCookies([{
        name: 'session_token',
        value: sessionValue,
        domain: hostname,
        path: '/',
        httpOnly: true,
        sameSite: 'Lax',
    }]);
}

// ---------------------------------------------------------------
// Helper: check whether the server has AI features enabled.
// ---------------------------------------------------------------
async function checkAiEnabled(api: ApiHelper, auth: AuthHelper): Promise<boolean> {
    try {
        const adminCookie = await auth.loginAsAdmin();
        const result = await api.rawGet('/api/v1/capabilities', {
            Cookie: adminCookie,
        });
        const body = result.body as { ai_enabled?: boolean } | null;
        return body?.ai_enabled === true;
    } catch {
        return false;
    }
}

test.describe('Group Permission Inheritance', () => {
    // Do NOT use storageState — inject admin sessions fresh to
    // avoid invalidating the shared cookie used by other specs.

    const CLUSTER_NAME = 'Test Binary Cluster';
    const testUsername = makeTestUsername('grp-perm');
    const groupName = `e2e-grp-${Date.now()}`;

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);

    // Track IDs for cleanup (set inside test, read in afterAll)
    let userId: number;
    let groupId: number;

    test.afterAll(async () => {
        try {
            const cleanupCookie = await auth.loginAsAdmin();
            await api.deleteUser(cleanupCookie, userId).catch(() => {});
            await api.deleteGroup(cleanupCookie, groupId).catch(() => {});
        } catch {
            // Best-effort cleanup
        }
    });

    test('group privileges propagate to members and are revoked on removal', async ({ page }) => {
        test.setTimeout(300_000);
        await label('package', 'Group Permission Inheritance');

        const loginPage = new LoginPage(page);
        const adminPage = new AdminPage(page);
        const groupPage = new GroupManagementPage(page);
        const navigator = new ServerNavigatorPage(page);
        const chatPage = new ChatPage(page);

        let testUserCookie: string;
        let adminCookie: string;
        let primaryConnId: number;

        // Determine AI availability
        let aiEnabled = false;
        await test.step('Setup: determine AI availability', async () => {
            const serverEnabled = await checkAiEnabled(api, auth);
            if (process.env['E2E_AI_ENABLED'] !== undefined) {
                const requested = LLM_CONFIG.enabled;
                if (requested && !serverEnabled) {
                    aiEnabled = false;
                } else {
                    aiEnabled = requested;
                    console.log(
                        `[group-permission-inheritance] E2E_AI_ENABLED=${process.env['E2E_AI_ENABLED']} → ` +
                        `AI phases ${aiEnabled ? 'ENABLED' : 'DISABLED'} (server ai_enabled=${serverEnabled}).`,
                    );
                }
            } else {
                aiEnabled = serverEnabled;
                console.log(
                    `[group-permission-inheritance] Capabilities probe → ai_enabled=${serverEnabled}. ` +
                    (aiEnabled
                        ? 'AI phases will run.'
                        : 'AI phases skipped. Set E2E_AI_ENABLED=true to force.'),
                );
            }
        });

        // ---------------------------------------------------------
        // Setup: create user, group, and grant all privileges via API
        // ---------------------------------------------------------
        await test.step('Setup: create test user via API', async () => {
            const result = await auth.createAndLoginUser(testUsername, TEST_USER_PASSWORD);
            userId = result.userId;
            testUserCookie = result.cookie;
        });

        await test.step('Setup: create group via API', async () => {
            adminCookie = await auth.loginAsAdmin();
            const result = await api.createGroup(adminCookie, { name: groupName });
            groupId = result.id;
        });

        await test.step('Setup: find primary connection', async () => {
            const connections = await api.listConnections({ cookie: adminCookie });
            const primary = connections.find(
                (c) => /primary/i.test(c.name),
            );
            if (!primary) {
                throw new Error(
                    'No connection matching /primary/i found. ' +
                    `Available: ${connections.map((c) => c.name).join(', ')}`,
                );
            }
            primaryConnId = primary.id;
        });

        await test.step('Setup: grant connection privilege (read) to group', async () => {
            await api.grantGroupConnectionPrivilege(
                adminCookie, groupId, primaryConnId, 'read',
            );
        });

        await test.step('Setup: grant admin permission (manage_users) to group', async () => {
            await api.grantGroupAdminPermission(
                adminCookie, groupId, PERMISSIONS.MANAGE_USERS,
            );
        });

        await test.step('Setup: grant MCP privileges to group', async () => {
            await api.grantGroupMcpPrivilege(adminCookie, groupId, 'list_connections');
            await api.grantGroupMcpPrivilege(adminCookie, groupId, 'count_rows');
            await api.grantGroupMcpPrivilege(adminCookie, groupId, 'query_database');
        });

        await test.step('Setup: add user as member of group', async () => {
            await api.addGroupMember(adminCookie, groupId, userId);
        });

        await test.step('Setup: verify grants via API round-trip', async () => {
            const { permissions } = await api.listGroupAdminPermissions(
                adminCookie, groupId,
            );
            expect(permissions).toContain(PERMISSIONS.MANAGE_USERS);

            const { connection_privileges } = await api.listGroupConnectionPrivileges(
                adminCookie, groupId,
            );
            const connPriv = connection_privileges.find(
                (cp) => cp.connection_id === primaryConnId,
            );
            expect(connPriv, 'Connection privilege should exist').toBeTruthy();
            expect(connPriv!.access_level).toBe('read');
        });

        // ---------------------------------------------------------
        // Phase 1: Verify permissions in Groups UI (admin session)
        // ---------------------------------------------------------
        await test.step('Phase 1: admin establishes fresh session', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await page.waitForLoadState('load');
        });

        await test.step('Phase 1: navigate to Groups and expand group row', async () => {
            await adminPage.navigateToGroups();
            await groupPage.expandGroupRow(groupName);
        });

        await test.step('Phase 1: verify admin permission visible in expanded row', async () => {
            await groupPage.expectAdminPermissionVisible('Manage Users');
        });

        await test.step('Phase 1: verify MCP privileges visible in expanded row', async () => {
            await groupPage.expectMcpPrivilegeVisible('count rows');
            await groupPage.expectMcpPrivilegeVisible('list connections');
            await groupPage.expectMcpPrivilegeVisible('query database');
        });

        await test.step('Phase 1: verify connection privilege visible in expanded row', async () => {
            await groupPage.expectConnectionPrivilegeVisible(
                new RegExp(`connection ${primaryConnId}`, 'i'),
            );
        });

        await test.step('Phase 1: verify member is listed', async () => {
            await groupPage.expectMemberInList(testUsername);
        });

        await test.step('Phase 1: close admin panel and sign out', async () => {
            await adminPage.closeAdminPanel();
            await adminPage.signOut();
        });

        // ---------------------------------------------------------
        // Phase 2: Login as test user, verify admin section
        // ---------------------------------------------------------
        await test.step('Phase 2: login as test user', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, TEST_USER_PASSWORD);
        });

        await test.step('Phase 2: verify admin panel shows Users but not Groups or Permissions', async () => {
            await page.getByTestId('admin-panel-trigger').click();
            await expect(
                page.getByText('Administration', { exact: true }),
            ).toBeVisible({ timeout: 5_000 });

            // Users should be visible (manage_users granted via group)
            await expect(
                page.getByTestId('admin-panel-item-users'),
            ).toBeVisible({ timeout: 5_000 });

            // Groups and Permissions should NOT be visible
            await expect(
                page.getByTestId('admin-panel-item-groups'),
            ).not.toBeVisible({ timeout: 3_000 });
            await expect(
                page.getByTestId('admin-panel-item-permissions'),
            ).not.toBeVisible({ timeout: 3_000 });

            await page
                .getByRole('button', { name: /close administration/i })
                .click();
        });

        await test.step('Phase 2: verify user can create a user via API (manage_users)', async () => {
            // Re-login to get a fresh cookie for the test user
            const { cookie: freshCookie } = await api.login(testUsername, TEST_USER_PASSWORD);
            testUserCookie = freshCookie;

            const probeUsername = makeTestUsername('grp-probe');
            // Should succeed with 200-level response (no throw)
            await api.createUser(testUserCookie, {
                username: probeUsername,
                password: TEST_USER_PASSWORD,
                display_name: 'Probe User',
            });

            // Clean up the probe user — refresh admin cookie in case it expired
            const freshAdminCookie = await auth.loginAsAdmin();
            const { users } = await api.listUsers(freshAdminCookie);
            const probeUser = users.find((u) => u.username === probeUsername);
            if (probeUser) {
                await api.deleteUser(freshAdminCookie, probeUser.id);
            }
        });

        // ---------------------------------------------------------
        // Phase 3: Connection visibility
        // ---------------------------------------------------------
        await test.step('Phase 3: verify primary node visible in navigator', async () => {
            await page.waitForLoadState('load');
            await navigator.expandCluster(CLUSTER_NAME);

            // Primary node should be visible
            await expect(
                navigator.getServerRow('primary-node').first(),
                'Primary node should be visible via group connection privilege',
            ).toBeVisible({ timeout: 15_000 });

            // Standbys should NOT be visible
            await expect(
                navigator.getServerRow('standby-node-1'),
                'standby-node-1 should not be visible',
            ).not.toBeVisible({ timeout: 5_000 });
            await expect(
                navigator.getServerRow('standby-node-2'),
                'standby-node-2 should not be visible',
            ).not.toBeVisible({ timeout: 5_000 });
        });

        await test.step('Phase 3: select primary node to load server context', async () => {
            await navigator.selectServer('primary-node');
            await page.waitForLoadState('load');
        });

        // ---------------------------------------------------------
        // Phase 4: Ellie (conditional on aiEnabled)
        // ---------------------------------------------------------
        if (aiEnabled) {
            await test.step('Phase 4 (AI): open chat panel', async () => {
                await chatPage.openChat();
            });

            await test.step('Phase 4 (AI): list connections — primary visible', async () => {
                await chatPage.sendMessage('List the available connections.');
                await chatPage.waitForResponse(60_000);
                // The response should mention the primary connection
                await expect(async () => {
                    const text: string = await page.evaluate(
                        () => document.body.innerText,
                    );
                    expect(text.toLowerCase()).toContain('primary');
                }).toPass({ timeout: 15_000, intervals: [500] });
            });

            await test.step('Phase 4 (AI): list standby databases — none accessible', async () => {
                await chatPage.sendMessage('List the standby databases.');
                await chatPage.waitForResponse(60_000);
                // The test user only has access to primary-node via RBAC.
                // Ellie may respond with an explicit error/denial, or may
                // answer from context that no standbys are visible. Either
                // outcome is correct — the RBAC invariant is that specific
                // standby node names must NOT appear in the response.
                await expect(async () => {
                    const text: string = await page.evaluate(
                        () => document.body.innerText,
                    );
                    expect(
                        text,
                        'Ellie should not reveal standby-node-1 to a restricted user',
                    ).not.toMatch(/standby-node-1/i);
                    expect(
                        text,
                        'Ellie should not reveal standby-node-2 to a restricted user',
                    ).not.toMatch(/standby-node-2/i);
                }).toPass({ timeout: 15_000, intervals: [500] });
            });

            await test.step('Phase 4 (AI): count rows — succeeds with a number', async () => {
                await chatPage.sendMessage('Count the rows in the documents table.');
                await chatPage.waitForResponse(60_000);
                await expect(async () => {
                    const text: string = await page.evaluate(
                        () => document.body.innerText,
                    );
                    expect(text).toMatch(/\d+/);
                }).toPass({ timeout: 15_000, intervals: [500] });
            });

            await test.step('Phase 4 (AI): generate embedding — denied', async () => {
                await chatPage.sendMessage('Generate an embedding for "test".');
                await chatPage.waitForResponse(60_000);
                await chatPage.expectErrorResponse(15_000);
            });
        } else {
            await test.step('Phase 4 (AI): skipped — AI not configured', async () => {
                // No-op: the FAB gated by ai_enabled would never render.
            });
        }

        await test.step('Phase 4: sign out test user', async () => {
            await adminPage.signOut();
        });

        // ---------------------------------------------------------
        // Phase 5: Revocation — remove user from group via API
        // ---------------------------------------------------------
        await test.step('Phase 5: remove user from group via API', async () => {
            const freshAdminCookie = await auth.loginAsAdmin();
            await api.removeGroupMember(freshAdminCookie, groupId, userId);
        });

        // ---------------------------------------------------------
        // Phase 6: Verify revocation
        // ---------------------------------------------------------
        await test.step('Phase 6: login as test user after revocation', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, TEST_USER_PASSWORD);
        });

        await test.step('Phase 6: server tree is empty', async () => {
            // Navigate explicitly to '/' before checking. In Phase 3 the
            // test selected a server (changing the URL to a server-specific
            // path); Firefox may resume at that URL after re-login rather
            // than '/', which can delay or prevent the "no servers" empty
            // state from rendering. goto('/') forces a fresh root load.
            await page.goto('/');
            await page.waitForLoadState('load');
            await navigator.expectEmptyServerTree(30_000);
        });

        await test.step('Phase 6: admin panel shows no admin sections', async () => {
            // The admin trigger may or may not be visible depending on
            // whether AI is enabled (Memories section is visibleToAll).
            const triggerVisible = await page
                .getByTestId('admin-panel-trigger')
                .isVisible()
                .catch(() => false);

            if (triggerVisible) {
                await page.getByTestId('admin-panel-trigger').click();
                await expect(
                    page.getByText('Administration', { exact: true }),
                ).toBeVisible({ timeout: 5_000 });

                // Users should no longer be visible
                await expect(
                    page.getByTestId('admin-panel-item-users'),
                ).not.toBeVisible({ timeout: 3_000 });

                // Groups and Permissions still not visible
                await expect(
                    page.getByTestId('admin-panel-item-groups'),
                ).not.toBeVisible({ timeout: 3_000 });
                await expect(
                    page.getByTestId('admin-panel-item-permissions'),
                ).not.toBeVisible({ timeout: 3_000 });

                await page
                    .getByRole('button', { name: /close administration/i })
                    .click();
            }
        });

        await test.step('Phase 6: verify user cannot create a user via API (403)', async () => {
            const { cookie: freshCookie } = await api.login(testUsername, TEST_USER_PASSWORD);
            const probeUsername = makeTestUsername('grp-denied');
            let gotForbidden = false;
            try {
                await api.createUser(freshCookie, {
                    username: probeUsername,
                    password: TEST_USER_PASSWORD,
                    display_name: 'Should Fail',
                });
            } catch (err: unknown) {
                const message = err instanceof Error ? err.message : String(err);
                // Expect 403 Forbidden
                expect(message).toMatch(/403|forbidden/i);
                gotForbidden = true;
            }
            expect(
                gotForbidden,
                'createUser should have been denied with 403 after group removal',
            ).toBe(true);
        });

        if (aiEnabled) {
            await test.step('Phase 6 (AI): Ellie denies connection tools after revocation', async () => {
                // The chat FAB is visible whenever AI is globally enabled,
                // regardless of connection access. Open it and attempt a
                // tool call that requires a connection privilege — Ellie
                // should respond with an error because no connections are
                // accessible to this user anymore.
                //
                // Do NOT call waitForResponse here: after revocation the
                // user has no server selected, and the chat panel may
                // unmount the input element when the LLM responds, causing
                // waitForResponse ("element not found") to time out.
                // Poll body text directly instead — expectErrorResponse
                // uses document.body.innerText which works regardless of
                // whether the chat input remains in the DOM.
                await chatPage.openChat();
                await chatPage.sendMessage('List the available connections.');
                await chatPage.expectErrorResponse(75_000);
            });
        }
    });
});
