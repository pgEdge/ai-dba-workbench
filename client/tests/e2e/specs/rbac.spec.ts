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
// RBAC Enforcement Tests
// ---------------------------------------------------------------

test.describe('RBAC Enforcement', () => {
    let adminCookie: string;

    test.beforeEach(async () => {
        await label('package', 'RBAC Enforcement');
    });

    test.beforeAll(async () => {
        adminCookie = await auth.loginAsAdmin();
    });

    // -------------------------------------------------------
    // 1. Unauthenticated request -> 401
    // -------------------------------------------------------
    test('unauthenticated request returns 401', async () => {
        const result = await api.rawGet('/api/v1/rbac/users');
        expect(result.status).toBe(401);
    });

    // -------------------------------------------------------
    // 2. Invalid / fabricated cookie -> 401
    // -------------------------------------------------------
    test('invalid cookie returns 401', async () => {
        const result = await api.rawGet('/api/v1/rbac/users', {
            Cookie: 'session_token=fabricated-invalid-token-value',
        });
        expect(result.status).toBe(401);
    });

    // -------------------------------------------------------
    // 3. Token scope blocks out-of-scope connections
    // -------------------------------------------------------
    test('scoped token restricts connection visibility', async () => {
        const username = makeTestUsername('scope-conn');

        await test.step('Create user and scoped token', async () => {
            await api.createUser(adminCookie, {
                username,
                password: TEST_USER_PASSWORD,
                display_name: `Scope ${username}`,
                email: `${username}@e2e.test`,
            });
        });

        const { tokenId, rawToken } = await auth.createBearerToken(
            adminCookie,
            username,
        );

        await test.step('Scope token to non-existent connection', async () => {
            await api.setTokenScope(adminCookie, tokenId, {
                connections: [{ connection_id: 99999, access_level: 'read' }],
            });
        });

        await test.step('Verify scoped response', async () => {
            const result = await api.rawGet('/api/v1/connections', {
                Authorization: `Bearer ${rawToken}`,
            });

            expect(result.status).toBe(200);
            const body = result.body as { connections?: unknown[] } | null;
            if (body && body.connections) {
                expect(body.connections.length).toBe(0);
            }
        });
    });

    // -------------------------------------------------------
    // 4. User without manage_users -> 403
    // -------------------------------------------------------
    test('user without manage_users gets 403', async () => {
        const username = makeTestUsername('no-perm');
        const { cookie: userCookie } = await auth.createAndLoginUser(username);

        const result = await api.rawPost(
            '/api/v1/rbac/users',
            {
                username: makeTestUsername('forbidden'),
                password: TEST_USER_PASSWORD,
            },
            { Cookie: userCookie },
        );

        expect(result.status).toBe(403);
    });

    // -------------------------------------------------------
    // 5. Superuser can access all endpoints
    // -------------------------------------------------------
    test('superuser can access all endpoints', async () => {
        await test.step('Access users endpoint', async () => {
            const result = await api.rawGet('/api/v1/rbac/users', {
                Cookie: adminCookie,
            });
            expect(result.status).toBe(200);
        });

        await test.step('Access tokens endpoint', async () => {
            const tokenResult = await api.rawGet('/api/v1/rbac/tokens', {
                Cookie: adminCookie,
            });
            expect(tokenResult.status).toBe(200);
        });
    });

    // -------------------------------------------------------
    // 6. Cookie lifecycle: login -> use -> logout -> reuse -> 401
    // -------------------------------------------------------
    test('session cookie is invalidated after logout', async () => {
        const username = makeTestUsername('cookie-lifecycle');
        const { cookie } = await auth.createAndLoginUser(username);

        await test.step('Cookie works before logout', async () => {
            const before = await api.rawGet('/api/v1/connections', {
                Cookie: cookie,
            });
            expect(before.status).toBe(200);
        });

        await test.step('Logout', async () => {
            await api.logout(cookie);
        });

        await test.step('Cookie fails after logout', async () => {
            const after = await api.rawGet('/api/v1/connections', {
                Cookie: cookie,
            });
            expect(after.status).toBe(401);
        });
    });

    // -------------------------------------------------------
    // 7. Bearer token from revoked token -> 401
    // -------------------------------------------------------
    test('revoked bearer token returns 401', async () => {
        const username = makeTestUsername('revoked-bearer');

        await test.step('Create user', async () => {
            await api.createUser(adminCookie, {
                username,
                password: TEST_USER_PASSWORD,
                display_name: `Revoked ${username}`,
                email: `${username}@e2e.test`,
            });
        });

        const { tokenId, rawToken } = await auth.createBearerToken(
            adminCookie,
            username,
        );

        await test.step('Verify token works', async () => {
            const before = await api.rawGet('/api/v1/connections', {
                Authorization: `Bearer ${rawToken}`,
            });
            expect(before.status).toBe(200);
        });

        await test.step('Revoke token', async () => {
            await auth.revokeToken(adminCookie, tokenId);
        });

        await test.step('Token fails after revocation', async () => {
            const after = await api.rawGet('/api/v1/connections', {
                Authorization: `Bearer ${rawToken}`,
            });
            expect(after.status).toBe(401);
        });
    });
});
