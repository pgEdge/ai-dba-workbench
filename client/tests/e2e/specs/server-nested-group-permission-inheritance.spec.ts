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

test.describe('Nested Group Permission Inheritance', () => {
    // Do NOT use storageState — inject admin sessions fresh to
    // avoid invalidating the shared cookie used by other specs.

    const CLUSTER_NAME = 'Test Binary Cluster';
    const testUsername = makeTestUsername('nested-grp');
    const ts = Date.now();
    const parentGroupName = `e2e-parent-${ts}`;
    const childGroupName  = `e2e-child-${ts}`;

    const api  = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);

    let userId: number;
    let parentGroupId: number;
    let childGroupId: number;
    let primaryConnId: number;

    test.afterAll(async () => {
        try {
            const cleanupCookie = await auth.loginAsAdmin();
            await api.deleteUser(cleanupCookie, userId).catch(() => {});
            await api.deleteGroup(cleanupCookie, childGroupId).catch(() => {});
            await api.deleteGroup(cleanupCookie, parentGroupId).catch(() => {});
        } catch {
            // Best-effort cleanup
        }
    });

    test('user inherits group privileges transitively via nested group membership and loses them on removal', async ({ page }) => {
        test.setTimeout(300_000);
        await label('package', 'Nested Group Permission Inheritance');

        const loginPage  = new LoginPage(page);
        const adminPage  = new AdminPage(page);
        const groupPage  = new GroupManagementPage(page);
        const navigator  = new ServerNavigatorPage(page);
        const chatPage   = new ChatPage(page);

        let adminCookie: string;

        // Determine AI availability
        let aiEnabled = false;
        await test.step('Setup: determine AI availability', async () => {
            const serverEnabled = await checkAiEnabled(api, auth);
            if (process.env['E2E_AI_ENABLED'] !== undefined) {
                const requested = LLM_CONFIG.enabled;
                aiEnabled = requested && serverEnabled ? requested : false;
                console.log(
                    `[nested-group] E2E_AI_ENABLED=${process.env['E2E_AI_ENABLED']} → ` +
                    `AI phases ${aiEnabled ? 'ENABLED' : 'DISABLED'} (server ai_enabled=${serverEnabled}).`,
                );
            } else {
                aiEnabled = serverEnabled;
                console.log(
                    `[nested-group] Capabilities probe → ai_enabled=${serverEnabled}. ` +
                    (aiEnabled ? 'AI phases will run.' : 'AI phases skipped.'),
                );
            }
        });

        // ---------------------------------------------------------
        // Setup — all via API (test data only, not under test)
        // ---------------------------------------------------------
        await test.step('Setup: create test user via API', async () => {
            const result = await auth.createAndLoginUser(testUsername, TEST_USER_PASSWORD);
            userId = result.userId;
        });

        await test.step('Setup: create parent and child groups via API', async () => {
            adminCookie = await auth.loginAsAdmin();
            const parent = await api.createGroup(adminCookie, { name: parentGroupName });
            parentGroupId = parent.id;
            const child = await api.createGroup(adminCookie, { name: childGroupName });
            childGroupId = child.id;
        });

        await test.step('Setup: find primary connection via API', async () => {
            const connections = await api.listConnections({ cookie: adminCookie });
            const primary = connections.find((c) => /primary/i.test(c.name));
            if (!primary) {
                throw new Error(
                    'No connection matching /primary/i found. ' +
                    `Available: ${connections.map((c) => c.name).join(', ')}`,
                );
            }
            primaryConnId = primary.id;
        });

        await test.step('Setup: grant privileges to parent group via API', async () => {
            await api.grantGroupConnectionPrivilege(
                adminCookie, parentGroupId, primaryConnId, 'read',
            );
            await api.grantGroupAdminPermission(
                adminCookie, parentGroupId, PERMISSIONS.MANAGE_USERS,
            );
            await api.grantGroupMcpPrivilege(adminCookie, parentGroupId, 'list_connections');
            await api.grantGroupMcpPrivilege(adminCookie, parentGroupId, 'count_rows');
            await api.grantGroupMcpPrivilege(adminCookie, parentGroupId, 'query_database');
        });

        await test.step('Setup: add child group as member of parent group via API', async () => {
            await api.addGroupMemberGroup(adminCookie, parentGroupId, childGroupId);
        });

        await test.step('Setup: add test user as member of child group via API', async () => {
            await api.addGroupMember(adminCookie, childGroupId, userId);
        });

        // ---------------------------------------------------------
        // Phase 1: Verify Groups UI (admin session)
        // ---------------------------------------------------------
        await test.step('Phase 1: admin establishes fresh session', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await page.waitForLoadState('load');
        });

        await test.step('Phase 1: navigate to Groups and expand parent group row', async () => {
            await adminPage.navigateToGroups();
            await groupPage.expandGroupRow(parentGroupName);
        });

        await test.step('Phase 1: verify admin permission chip visible', async () => {
            await groupPage.expectAdminPermissionVisible('Manage Users');
        });

        await test.step('Phase 1: verify MCP privilege chips visible', async () => {
            await groupPage.expectMcpPrivilegeVisible('count rows');
            await groupPage.expectMcpPrivilegeVisible('list connections');
            await groupPage.expectMcpPrivilegeVisible('query database');
        });

        await test.step('Phase 1: verify connection privilege chip visible', async () => {
            await groupPage.expectConnectionPrivilegeVisible(
                new RegExp(`connection ${primaryConnId}`, 'i'),
            );
        });

        await test.step('Phase 1: verify child group is listed as member of parent', async () => {
            await groupPage.expectMemberInList(childGroupName);
        });

        await test.step('Phase 1: close admin panel and sign out', async () => {
            await adminPage.closeAdminPanel();
            await adminPage.signOut();
        });

        // ---------------------------------------------------------
        // Phase 2: Login as test user — verify inherited admin section
        // ---------------------------------------------------------
        await test.step('Phase 2: login as test user', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, TEST_USER_PASSWORD);
        });

        await test.step('Phase 2: admin panel shows Users but not Groups or Permissions', async () => {
            await page.getByTestId('admin-panel-trigger').click();
            await expect(
                page.getByText('Administration', { exact: true }),
            ).toBeVisible({ timeout: 5_000 });

            // Users visible — manage_users inherited via parent → child → user
            await expect(
                page.getByTestId('admin-panel-item-users'),
            ).toBeVisible({ timeout: 5_000 });

            // Groups and Permissions NOT visible
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

        // ---------------------------------------------------------
        // Phase 3: Connection visibility
        // ---------------------------------------------------------
        await test.step('Phase 3: primary node visible; standbys hidden', async () => {
            await page.waitForLoadState('load');
            await navigator.expandCluster(CLUSTER_NAME);

            await expect(
                navigator.getServerRow('primary-node').first(),
                'Primary node should be visible via transitive group privilege',
            ).toBeVisible({ timeout: 15_000 });

            await expect(
                navigator.getServerRow('standby-node-1'),
            ).not.toBeVisible({ timeout: 5_000 });
            await expect(
                navigator.getServerRow('standby-node-2'),
            ).not.toBeVisible({ timeout: 5_000 });
        });

        await test.step('Phase 3: select primary node', async () => {
            await navigator.selectServer('primary-node');
            await page.waitForLoadState('load');
        });

        // ---------------------------------------------------------
        // Phase 4: Ellie (conditional)
        // ---------------------------------------------------------
        if (aiEnabled) {
            await test.step('Phase 4 (AI): open chat panel', async () => {
                await chatPage.openChat();
            });

            await test.step('Phase 4 (AI): list connections — primary visible', async () => {
                await chatPage.sendMessage('List the available connections.');
                await chatPage.waitForResponse(60_000);
                await expect(async () => {
                    const text: string = await page.evaluate(
                        () => document.body.innerText,
                    );
                    expect(text.toLowerCase()).toContain('primary');
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
                // No-op
            });
        }

        await test.step('Phase 4: sign out test user', async () => {
            await adminPage.signOut();
        });

        // ---------------------------------------------------------
        // Phase 5: Revocation via GUI — admin removes child group
        // from parent group's member list
        // ---------------------------------------------------------
        await test.step('Phase 5: admin establishes fresh session', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await page.waitForLoadState('load');
        });

        await test.step('Phase 5: navigate to Groups and expand parent group row', async () => {
            await adminPage.navigateToGroups();
            await groupPage.expandGroupRow(parentGroupName);
        });

        await test.step('Phase 5: remove child group from parent via GUI', async () => {
            // Wait for the DELETE /members API response before navigating
            // away; without this the browser cancels the in-flight request
            // and the membership remains intact in the backend.
            const responsePromise = page.waitForResponse(
                (resp) =>
                    resp.url().includes('/members') &&
                    resp.request().method() === 'DELETE',
                { timeout: 10_000 },
            );
            await groupPage.removeMember(childGroupName);
            await responsePromise;
            await groupPage.expectMemberNotInList(childGroupName);
        });

        await test.step('Phase 5: close admin panel and sign out', async () => {
            await adminPage.closeAdminPanel();
            await adminPage.signOut();
        });

        // ---------------------------------------------------------
        // Phase 6: Verify revocation via GUI
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
            const triggerVisible = await page
                .getByTestId('admin-panel-trigger')
                .isVisible()
                .catch(() => false);

            if (triggerVisible) {
                await page.getByTestId('admin-panel-trigger').click();
                await expect(
                    page.getByText('Administration', { exact: true }),
                ).toBeVisible({ timeout: 5_000 });

                await expect(
                    page.getByTestId('admin-panel-item-users'),
                ).not.toBeVisible({ timeout: 3_000 });
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

        if (aiEnabled) {
            await test.step('Phase 6 (AI): Ellie denies connection tools after revocation', async () => {
                // Do NOT call waitForResponse: after revocation with no
                // server selected, the chat panel may unmount the input
                // element once Ellie responds, causing waitForResponse
                // ("element not found") to fail in WebKit. Poll body text.
                await chatPage.openChat();
                await chatPage.sendMessage('List the available connections.');
                await chatPage.expectErrorResponse(75_000);
            });
        }
    });
});
