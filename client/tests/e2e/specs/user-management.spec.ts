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
import {
    loginViaUI,
    navigateToAdminUsers,
    waitForUsersTable,
    clickAddUser,
    fillUserForm,
    submitUserForm,
    clickEditUser,
    clickDeleteUser,
    confirmDelete,
} from '../helpers/browser.helper';
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
        test.use({ storageState: '.auth/admin.json' });

        const username = makeTestUsername('ui-create');

        await page.goto('/');
        await loginViaUI(page, 'admin', process.env.E2E_ADMIN_PASS || 'E2ETestPass123!');
        await navigateToAdminUsers(page);

        await clickAddUser(page);
        await fillUserForm(page, username, TEST_USER_PASSWORD, `E2E ${username}`);
        await submitUserForm(page);

        // Wait for table to refresh and verify the new user row.
        await waitForUsersTable(page);
        await expect(page.getByRole('row', { name: new RegExp(username) })).toBeVisible({
            timeout: 10_000,
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
        const username = makeTestUsername('ui-update');

        await api.createUser(adminCookie, {
            username,
            password: TEST_USER_PASSWORD,
            display_name: 'UI Before',
            email: `${username}@e2e.test`,
        });

        await page.goto('/');
        await loginViaUI(page, 'admin', process.env.E2E_ADMIN_PASS || 'E2ETestPass123!');
        await navigateToAdminUsers(page);
        await waitForUsersTable(page);

        await clickEditUser(page, username);

        // Update display name in the edit dialog.
        const dialog = page.getByRole('dialog');
        await expect(dialog).toBeVisible();
        const nameField = dialog.getByLabel('Display Name');
        await nameField.clear();
        await nameField.fill('UI After');
        await dialog.getByRole('button', { name: /save|update/i }).click();
        await expect(dialog).toBeHidden({ timeout: 5_000 });

        // Verify via API.
        const { users } = await api.listUsers(adminCookie);
        const updated = users.find((u) => u.username === username)!;
        expect(updated.display_name).toBe('UI After');
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
        const username = makeTestUsername('ui-delete');

        await api.createUser(adminCookie, {
            username,
            password: TEST_USER_PASSWORD,
            display_name: 'UI Delete',
            email: `${username}@e2e.test`,
        });

        await page.goto('/');
        await loginViaUI(page, 'admin', process.env.E2E_ADMIN_PASS || 'E2ETestPass123!');
        await navigateToAdminUsers(page);
        await waitForUsersTable(page);

        await clickDeleteUser(page, username);
        await confirmDelete(page);

        // Verify the row is gone.
        await expect(
            page.getByRole('row', { name: new RegExp(username) }),
        ).toBeHidden({ timeout: 10_000 });
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
