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
import { TokenManagementPage } from '../pages/TokenManagementPage';
import {
    ADMIN_USER,
    API_URL,
    TEST_USER_PREFIX,
    TEST_USER_PASSWORD,
    PERMISSIONS,
    makeTestUsername,
} from '../fixtures/test-data';

// ---------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------

const api = new ApiHelper(API_URL);
const auth = new AuthHelper(api);

// ---------------------------------------------------------------
// Token Management Tests
// ---------------------------------------------------------------

test.describe('Token Management', () => {
    // Apply admin storage state to all browser-based tests in this
    // describe block so the UI tests skip the manual login flow.
    test.use({ storageState: '.auth/admin.json' });

    let adminCookie: string;

    test.beforeAll(async () => {
        adminCookie = await auth.loginAsAdmin();
    });

    // -------------------------------------------------------
    // 1. Create token returns { id, token }
    // -------------------------------------------------------
    test('create token returns id and raw token', async () => {
        const annotation = `${TEST_USER_PREFIX}token-create-${Date.now()}`;
        const result = await api.createToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        expect(result.id).toBeGreaterThan(0);
        expect(typeof result.token).toBe('string');
        expect(result.token.length).toBeGreaterThan(0);
        expect(result.owner).toBe(ADMIN_USER.username);
        expect(result.annotation).toBe(annotation);
    });

    // -------------------------------------------------------
    // 2. Token appears in list
    // -------------------------------------------------------
    test('created token appears in list', async () => {
        const annotation = `${TEST_USER_PREFIX}token-list-${Date.now()}`;
        const created = await api.createToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        const { tokens } = await api.listTokens(adminCookie);
        const found = tokens.find((t) => t.id === created.id);

        expect(found).toBeDefined();
        expect(found!.name).toBe(annotation);
        expect(found!.username).toBe(ADMIN_USER.username);
    });

    // -------------------------------------------------------
    // 3. Raw token authenticates as Bearer
    // -------------------------------------------------------
    test('raw token authenticates as Bearer', async () => {
        const annotation = `${TEST_USER_PREFIX}token-bearer-${Date.now()}`;
        const { rawToken } = await auth.createBearerToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        // Use the bearer token to call a protected endpoint.
        // The /api/v1/rbac/users endpoint requires manage_users
        // which the admin superuser has by default.
        const result = await api.rawGet('/api/v1/rbac/users', {
            Authorization: `Bearer ${rawToken}`,
        });

        expect(result.status).toBe(200);
    });

    // -------------------------------------------------------
    // 4. Set token scope -> scoped: true
    // -------------------------------------------------------
    test('set token scope marks token as scoped', async () => {
        const annotation = `${TEST_USER_PREFIX}token-scope-${Date.now()}`;
        const { tokenId } = await auth.createBearerToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        // Set a scope with admin permissions.
        await api.setTokenScope(adminCookie, tokenId, {
            admin_permissions: [PERMISSIONS.MANAGE_USERS],
        });

        const scope = await api.getTokenScope(adminCookie, tokenId);

        expect(scope.scoped).toBe(true);
        expect(scope.admin_permissions).toContain(PERMISSIONS.MANAGE_USERS);
    });

    // -------------------------------------------------------
    // 5. Clear token scope -> scoped: false
    // -------------------------------------------------------
    test('clear token scope marks token as unscoped', async () => {
        const annotation = `${TEST_USER_PREFIX}token-unscope-${Date.now()}`;
        const { tokenId } = await auth.createBearerToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        // Set then clear.
        await api.setTokenScope(adminCookie, tokenId, {
            admin_permissions: [PERMISSIONS.MANAGE_CONNECTIONS],
        });
        await api.clearTokenScope(adminCookie, tokenId);

        const scope = await api.getTokenScope(adminCookie, tokenId);

        expect(scope.scoped).toBe(false);
    });

    // -------------------------------------------------------
    // 6. Revoke token -> Bearer fails with 401
    // -------------------------------------------------------
    test('revoked token returns 401 on bearer request', async () => {
        const annotation = `${TEST_USER_PREFIX}token-revoke-${Date.now()}`;
        const { tokenId, rawToken } = await auth.createBearerToken(
            adminCookie,
            ADMIN_USER.username,
            annotation,
        );

        // Revoke the token.
        await auth.revokeToken(adminCookie, tokenId);

        // Attempt to use the revoked token.
        const result = await api.rawGet('/api/v1/rbac/users', {
            Authorization: `Bearer ${rawToken}`,
        });

        expect(result.status).toBe(401);
    });

    // -------------------------------------------------------
    // 7. Token UI creation
    // -------------------------------------------------------
    test('create token via UI', async ({ page }) => {
        const adminPage = new AdminPage(page);
        const tokenPage = new TokenManagementPage(page);
        const username = makeTestUsername('token-ui');

        await test.step('Create service account via API', async () => {
            await api.createUser(adminCookie, {
                username,
                password: TEST_USER_PASSWORD,
                display_name: `Token UI ${username}`,
                email: `${username}@e2e.test`,
                is_service_account: true,
            });
        });

        await test.step('Navigate to Admin > Tokens', async () => {
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToTokens();
        });

        await test.step('Create token via dialog', async () => {
            await tokenPage.createToken(
                username,
                `${TEST_USER_PREFIX}ui-token`,
            );
        });

        await test.step('Verify token creation', async () => {
            await tokenPage.expectTokenCreated(
                new RegExp(TEST_USER_PREFIX),
            );
        });
    });
});
