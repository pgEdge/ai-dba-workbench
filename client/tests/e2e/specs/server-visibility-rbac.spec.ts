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
import { ServerNavigatorPage } from '../pages/ServerNavigatorPage';
import { ChatPage } from '../pages/ChatPage';
import {
    ADMIN_USER,
    API_URL,
    BASE_URL,
    TEST_USER_PASSWORD,
    makeTestUsername,
} from '../fixtures/test-data';
import { LLM_CONFIG } from '../fixtures/llm-config';

// ---------------------------------------------------------------
// Helper: inject a fresh admin session cookie into the page
// context without touching .auth/admin.json. Signing out this
// session will not invalidate the shared storage-state used by
// other tests running in parallel.
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
// Uses a one-off admin login to call /api/v1/capabilities so
// the check is independent of any page-level session state.
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

test.describe('Server Visibility and Ask Ellie RBAC', () => {
    // NOTE: Do NOT use storageState here. This test signs out the admin
    // session via the UI, which would invalidate the shared cookie in
    // .auth/admin.json and break every other spec that calls loginAsAdmin().
    // Instead, each admin login phase injects a fresh session via API.

    test('non-privileged user sees only shared server and Ask Ellie denies connections', async ({ page }) => {
        test.setTimeout(180_000);
        await label('package', 'Server Visibility RBAC');

        const SHARED_SERVER  = process.env['E2E_SHARED_SERVER'] ?? 'primary-node';
        const CLUSTER_NAME   = 'Test Binary Cluster';
        const ALL_SERVERS    = ['primary-node', 'standby-node-1', 'standby-node-2'];
        const OTHER_SERVERS  = ALL_SERVERS.filter(s => s !== SHARED_SERVER);
        const testUsername   = makeTestUsername('rbac-vis');

        const api  = new ApiHelper(API_URL);
        const auth = new AuthHelper(api);

        const loginPage  = new LoginPage(page);
        const adminPage  = new AdminPage(page);
        const navigator  = new ServerNavigatorPage(page);
        const chatPage   = new ChatPage(page);

        // Determine whether Ask Ellie (Phase 3) should run.
        //
        // Priority order:
        //   1. E2E_AI_ENABLED env var — explicit runtime override
        //      (resolved via LLM_CONFIG.enabled). Set to "true" or "1"
        //      to force Phase 3 on; "false" or "0" to force it off.
        //   2. /api/v1/capabilities probe — automatic detection when the
        //      env var is not set. Phase 3 runs only if the server reports
        //      ai_enabled: true (i.e. an LLM provider key is configured).
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
                        `[server-visibility-rbac] E2E_AI_ENABLED=${process.env['E2E_AI_ENABLED']} → ` +
                        `Phase 3 ${aiEnabled ? 'ENABLED' : 'DISABLED'} (server ai_enabled=${serverEnabled}).`,
                    );
                }
            } else {
                aiEnabled = serverEnabled;
                console.log(
                    `[server-visibility-rbac] Capabilities probe → ai_enabled=${serverEnabled}. ` +
                    (aiEnabled
                        ? 'Phase 3 will run.'
                        : 'Phase 3 skipped. Set E2E_AI_ENABLED=true to force it on.'),
                );
            }
        });

        // Create non-privileged test user BEFORE admin UI steps
        await test.step('Setup: create non-privileged test user via API', async () => {
            await auth.createAndLoginUser(testUsername, TEST_USER_PASSWORD);
        });

        // -------------------------------------------------------
        // PHASE 1: Admin enables "Share with all users"
        // -------------------------------------------------------
        await test.step('Admin: establish fresh session and load application', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await page.waitForLoadState('load');
        });

        await test.step('Admin: expand cluster to reveal server rows', async () => {
            await navigator.expandCluster(CLUSTER_NAME);
        });

        await test.step('Admin: open edit dialog for shared server', async () => {
            await navigator.openServerEditDialog(SHARED_SERVER);
        });

        await test.step('Admin: enable Share with all users and save', async () => {
            await navigator.enableSharedForAllUsers();
        });

        await test.step('Admin: close edit dialog', async () => {
            await navigator.closeEditDialog();
        });

        await test.step('Admin: sign out', async () => {
            await adminPage.signOut();
        });

        // -------------------------------------------------------
        // PHASE 2: Non-privileged user validation
        // -------------------------------------------------------
        await test.step('User: log in as non-privileged user', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, TEST_USER_PASSWORD);
        });

        await test.step('User: expand cluster in navigator', async () => {
            await page.waitForLoadState('load');
            await navigator.expandCluster(CLUSTER_NAME);
        });

        await test.step('User: only shared server is visible', async () => {
            // Shared server must be visible
            await expect(
                navigator.getServerRow(SHARED_SERVER).first(),
                `Shared server "${SHARED_SERVER}" should be visible`,
            ).toBeVisible({ timeout: 15_000 });

            // Other cluster servers must NOT be visible
            for (const name of OTHER_SERVERS) {
                await expect(
                    navigator.getServerRow(name),
                    `Non-shared server "${name}" should not be visible`,
                ).not.toBeVisible({ timeout: 5_000 });
            }

            // Exactly one server row total
            await page.waitForLoadState('load');
            const count = await navigator.serverRows.count();
            expect(count, 'Only one server row should be visible').toBe(1);
        });

        await test.step('User: can click the shared server', async () => {
            await navigator.selectServer(SHARED_SERVER);
            await expect(
                navigator.getServerRow(SHARED_SERVER).first(),
            ).toBeVisible({ timeout: 5_000 });
            // Wait for the server detail panel to finish loading so
            // the chat FAB (which only renders in the server context)
            // has time to appear.
            await page.waitForLoadState('load');
        });

        // -------------------------------------------------------
        // PHASE 3: Ask Ellie RBAC validation
        // Skipped when AI is not configured in this environment.
        // -------------------------------------------------------
        if (aiEnabled) {
            await test.step('Ellie: open chat panel', async () => {
                await chatPage.openChat();
            });

            await test.step('Ellie: send "List all connections" prompt', async () => {
                await chatPage.sendMessage('List all connections');
            });

            await test.step('Ellie: verify authorization error or empty result', async () => {
                // Do NOT call waitForResponse here: when Ellie responds
                // with an error the chat panel may unmount the input
                // element, causing waitForResponse ("element not found")
                // to fail in Firefox and WebKit. Poll body text directly.
                await chatPage.expectErrorResponse(75_000);
            });
        } else {
            await test.step('Ellie: skipped — AI not configured in this environment', async () => {
                // No-op: the FAB gated by ai_enabled would never render.
                // Run this test in an environment with a configured LLM
                // provider to exercise Phase 3.
            });
        }

        // -------------------------------------------------------
        // PHASE 4: Admin disables share → user sees empty tree
        // -------------------------------------------------------
        await test.step('Admin: sign out user and establish fresh admin session', async () => {
            // Sign out the non-privileged user first.
            await adminPage.signOut();
            // Inject a new admin session — this is independent of the
            // shared .auth/admin.json so other tests are unaffected.
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await page.waitForLoadState('load');
        });

        await test.step('Admin: expand cluster and re-open server edit dialog', async () => {
            await navigator.expandCluster(CLUSTER_NAME);
            await navigator.openServerEditDialog(SHARED_SERVER);
        });

        await test.step('Admin: disable Share with all users and save', async () => {
            const dialog = page.locator('.MuiDialog-paperFullScreen');
            await expect(dialog).toBeVisible({ timeout: 5_000 });
            const checkbox = dialog.getByLabel('Share with all users');
            await expect(checkbox).toBeVisible({ timeout: 5_000 });
            const isChecked = await checkbox.isChecked();
            if (isChecked) {
                await checkbox.uncheck();
            }
            await dialog.getByRole('button', { name: /save/i }).click();
            await expect(
                dialog.getByText('Server settings saved successfully'),
            ).toBeVisible({ timeout: 10_000 });
            await page.waitForLoadState('load');
        });

        await test.step('Admin: close edit dialog and sign out', async () => {
            await navigator.closeEditDialog();
            await adminPage.signOut();
        });

        await test.step('User: log in again after share is disabled', async () => {
            await loginPage.goto();
            await loginPage.loginAndWaitForApp(testUsername, TEST_USER_PASSWORD);
        });

        await test.step('User: server tree is empty — shared server no longer visible', async () => {
            await page.waitForLoadState('load');
            // The shared server should no longer be visible
            await expect(
                navigator.getServerRow(SHARED_SERVER),
                `Server "${SHARED_SERVER}" should not be visible after share is disabled`,
            ).not.toBeVisible({ timeout: 10_000 });
            // The empty-state message should appear
            await navigator.expectEmptyServerTree();
        });
    });
});
