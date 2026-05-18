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
import { ApiHelper } from '../helpers/api.helper';
import { AuthHelper } from '../helpers/auth.helper';
import { AdminPage } from '../pages/AdminPage';
import { UserManagementPage } from '../pages/UserManagementPage';
import {
    API_URL,
    TEST_USER_PASSWORD,
    makeTestUsername,
} from '../fixtures/test-data';

// ---------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------

const api = new ApiHelper(API_URL);
const auth = new AuthHelper(api);

// ---------------------------------------------------------------
// User Management Tests
// ---------------------------------------------------------------

test.describe('User Management', () => {
    // Apply admin storage state to all browser-based tests in this
    // describe block.  test.use() must be called at describe scope,
    // not inside individual test functions.
    test.use({ storageState: '.auth/admin.json' });

    let adminCookie: string;

    test.beforeAll(async () => {
        adminCookie = await auth.loginAsAdmin();
    });

    // -------------------------------------------------------
    // 1. Create user via API
    // -------------------------------------------------------
    test('create user via API and verify in list', async () => {
        const username = makeTestUsername('api-create');

        await api.createUser(adminCookie, {
            username,
            password: TEST_USER_PASSWORD,
            display_name: `E2E ${username}`,
            email: `${username}@e2e.test`,
        });

        const { users } = await api.listUsers(adminCookie);
        const found = users.find((u) => u.username === username);

        expect(found).toBeDefined();
        expect(found!.username).toBe(username);
        expect(found!.display_name).toBe(`E2E ${username}`);
        expect(found!.enabled).toBe(true);
    });

    // -------------------------------------------------------
    // 2. Create user via UI
    // -------------------------------------------------------
    test('create user via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const userPage = new UserManagementPage(page);
        const username = makeTestUsername('ui-create');

        await test.step('Navigate to Admin > Users', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToUsers();
        });

        await test.step('Create user via dialog', async () => {
            await userPage.createUser(
                username,
                TEST_USER_PASSWORD,
                `E2E ${username}`,
            );
        });

        await test.step('Verify new user in table', async () => {
            await userPage.waitForUsersTable();
            await userPage.expectUserInTable(username);
        });
    });

    // -------------------------------------------------------
    // 3. Update user via API
    // -------------------------------------------------------
    test('update user display name via API', async () => {
        const username = makeTestUsername('api-update');

        await api.createUser(adminCookie, {
            username,
            password: TEST_USER_PASSWORD,
            display_name: 'Before',
            email: `${username}@e2e.test`,
        });

        const { users: before } = await api.listUsers(adminCookie);
        const user = before.find((u) => u.username === username)!;

        await api.updateUser(adminCookie, user.id, {
            display_name: 'After Update',
        });

        const { users: after } = await api.listUsers(adminCookie);
        const updated = after.find((u) => u.id === user.id)!;

        expect(updated.display_name).toBe('After Update');
    });

    // -------------------------------------------------------
    // 4. Update user via UI
    // -------------------------------------------------------
    test('update user via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const userPage = new UserManagementPage(page);
        const username = makeTestUsername('ui-update');

        await test.step('Create test user via API', async () => {
            await api.createUser(adminCookie, {
                username,
                password: TEST_USER_PASSWORD,
                display_name: 'UI Before',
                email: `${username}@e2e.test`,
            });
        });

        await test.step('Navigate to Admin > Users', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToUsers();
            await userPage.waitForUsersTable();
        });

        await test.step('Edit user display name via dialog', async () => {
            await userPage.clickEditUser(username);
            await userPage.updateDisplayName('UI After');
        });

        await test.step('Verify via API', async () => {
            const { users } = await api.listUsers(adminCookie);
            const updated = users.find((u) => u.username === username)!;
            expect(updated.display_name).toBe('UI After');
        });
    });

    // -------------------------------------------------------
    // 5. Delete user via API
    // -------------------------------------------------------
    test('delete user via API returns 204 then 404', async () => {
        const username = makeTestUsername('api-delete');

        await api.createUser(adminCookie, {
            username,
            password: TEST_USER_PASSWORD,
            display_name: 'To Delete',
            email: `${username}@e2e.test`,
        });

        const { users } = await api.listUsers(adminCookie);
        const user = users.find((u) => u.username === username)!;

        // Delete should succeed.
        await api.deleteUser(adminCookie, user.id);

        // User should no longer appear in the list.
        const { users: remaining } = await api.listUsers(adminCookie);
        const found = remaining.find((u) => u.id === user.id);
        expect(found).toBeUndefined();
    });

    // -------------------------------------------------------
    // 6. Delete user via UI
    // -------------------------------------------------------
    test('delete user via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const userPage = new UserManagementPage(page);
        const username = makeTestUsername('ui-delete');

        await test.step('Create test user via API', async () => {
            await api.createUser(adminCookie, {
                username,
                password: TEST_USER_PASSWORD,
                display_name: 'UI Delete',
                email: `${username}@e2e.test`,
            });
        });

        await test.step('Navigate to Admin > Users', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToUsers();
            await userPage.waitForUsersTable();
        });

        await test.step('Delete user via dialog', async () => {
            await userPage.deleteUser(username);
        });

        await test.step('Verify row is gone', async () => {
            await userPage.expectUserNotInTable(username);
        });
    });

    // -------------------------------------------------------
    // 7. Unprivileged user cannot manage users
    // -------------------------------------------------------
    test('user without manage_users gets 403 on create', async () => {
        const username = makeTestUsername('unpriv');
        const { cookie: unprivCookie } = await auth.createAndLoginUser(username);

        const result = await api.rawPost(
            '/api/v1/rbac/users',
            {
                username: makeTestUsername('should-fail'),
                password: TEST_USER_PASSWORD,
            },
            { Cookie: unprivCookie },
        );

        expect(result.status).toBe(403);
    });
});
