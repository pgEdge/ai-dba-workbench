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
import { AdminPage } from '../pages/AdminPage';
import { TokenManagementPage } from '../pages/TokenManagementPage';
import {
    ADMIN_USER,
    API_URL,
    BASE_URL,
    PERMISSIONS,
    TEST_USER_PASSWORD,
    TEST_USER_PREFIX,
    makeTestUsername,
} from '../fixtures/test-data';

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
// Constants: MCP tools the group will NOT be granted.
// Used in negative tests to confirm 403 enforcement.
// ---------------------------------------------------------------
const DENIED_MCP_TOOLS = [
    'generate_embedding',
    'get_schema_info',
];

// ---------------------------------------------------------------
// All known MCP tool names for exhaustive TC2 verification.
// ---------------------------------------------------------------
const ALL_MCP_TOOLS = [
    'list_connections',
    'count_rows',
    'query_database',
    'execute_explain',
    'get_schema_info',
    'query_metrics',
    'generate_embedding',
    'similarity_search',
    'store_memory',
    'delete_memory',
    'recall_memories',
    'get_alert_history',
    'get_alert_rules',
    'get_blackouts',
    'get_timeline_events',
    'search_knowledgebase',
    'list_probes',
    'describe_probe',
    'query_datastore',
    'read_resource',
    'get_metric_baselines',
];

// ---------------------------------------------------------------
// Admin permission → testable API endpoint mapping.
// Only include permissions whose GET endpoint actually enforces
// the corresponding permission check.  Several "manage_*" guards
// apply only to write operations (POST/PUT/DELETE) while the GET
// returns 200 for any authenticated user; those are omitted to
// avoid false negatives in the denied-access assertion.
//
// Omitted (GET is public for all authenticated users):
//   manage_connections  → GET /api/v1/connections
//   manage_blackouts    → GET /api/v1/blackouts
//   manage_probes       → GET /api/v1/probe-configs
//   manage_alert_rules  → GET /api/v1/alert-rules
// ---------------------------------------------------------------
const ADMIN_PERMISSION_ENDPOINTS: Record<string, string> = {
    [PERMISSIONS.MANAGE_USERS]: '/api/v1/rbac/users',
    [PERMISSIONS.MANAGE_GROUPS]: '/api/v1/rbac/groups',
    [PERMISSIONS.MANAGE_PERMISSIONS]: '/api/v1/rbac/privileges/mcp',
    [PERMISSIONS.MANAGE_TOKEN_SCOPES]: '/api/v1/rbac/tokens',
    [PERMISSIONS.MANAGE_NOTIFICATION_CHANNELS]: '/api/v1/notification-channels',
};

// ---------------------------------------------------------------
// Build MCP tool arguments based on tool name.
// Tools that need a connection_id receive primaryConnId;
// others get empty or minimal arguments.
// ---------------------------------------------------------------
function buildToolArgs(
    toolName: string,
    primaryConnId: number,
): Record<string, unknown> {
    switch (toolName) {
        case 'count_rows':
            return {
                connection_id: primaryConnId,
                table_name: 'documents',
                schema_name: 'public',
            };
        case 'query_database':
            return {
                connection_id: primaryConnId,
                query: 'SELECT 1',
            };
        case 'execute_explain':
            return {
                connection_id: primaryConnId,
                query: 'SELECT 1',
            };
        case 'get_schema_info':
            return { connection_id: primaryConnId };
        case 'query_metrics':
            return { connection_id: primaryConnId };
        case 'generate_embedding':
            return { text: 'test' };
        case 'similarity_search':
            return {
                connection_id: primaryConnId,
                query: 'test',
            };
        case 'store_memory':
            return { content: 'test', tags: [] };
        case 'delete_memory':
            return { memory_id: 0 };
        case 'recall_memories':
            return { query: 'test' };
        case 'get_alert_history':
            return {};
        case 'get_alert_rules':
            return {};
        case 'get_blackouts':
            return {};
        case 'get_timeline_events':
            return {};
        case 'search_knowledgebase':
            return { query: 'test' };
        case 'list_probes':
            return {};
        case 'describe_probe':
            return { probe_name: 'cpu' };
        case 'query_datastore':
            return { connection_id: primaryConnId, query: 'SELECT 1' };
        case 'read_resource':
            return { uri: 'test://resource' };
        case 'get_metric_baselines':
            return { connection_id: primaryConnId };
        default:
            return {};
    }
}

// ---------------------------------------------------------------
// Token Scope RBAC Validation
// ---------------------------------------------------------------

test.describe('Token Scope RBAC Validation', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const testUsername = makeTestUsername('scope-rbac');
    const groupName = `e2e-scope-grp-${Date.now()}`;

    // MCP privileges and admin permissions granted to the group
    const GRANTED_MCP_TOOLS = ['list_connections', 'count_rows', 'query_database'];
    const GRANTED_ADMIN_PERMISSIONS = [
        PERMISSIONS.MANAGE_USERS,
        PERMISSIONS.MANAGE_GROUPS,
    ];

    let userId: number;
    let groupId: number;
    let primaryConnId: number;

    // Track token IDs for cleanup
    const createdTokenIds: number[] = [];

    test.beforeAll(async () => {
        // Step 1: create user
        const result = await auth.createAndLoginUser(testUsername, TEST_USER_PASSWORD);
        userId = result.userId;

        // Step 2: create group
        const freshCookie = await auth.loginAsAdmin();
        const group = await api.createGroup(freshCookie, {
            name: groupName,
            description: 'E2E token scope RBAC test group',
        });
        groupId = group.id;

        // Step 3a: find primary connection
        const connections = await api.listConnections({ cookie: freshCookie });
        const primary = connections.find((c) => /primary/i.test(c.name));
        if (!primary) {
            throw new Error(
                'No connection matching /primary/i found. This spec requires ' +
                'connections set up by replication-cluster.spec.ts. ' +
                `Available: ${connections.map((c) => c.name).join(', ')}`,
            );
        }
        primaryConnId = primary.id;

        // Step 3b: grant admin permissions to group
        for (const perm of GRANTED_ADMIN_PERMISSIONS) {
            await api.grantGroupAdminPermission(freshCookie, groupId, perm);
        }

        // Step 3c: grant MCP privileges to group
        for (const tool of GRANTED_MCP_TOOLS) {
            await api.grantGroupMcpPrivilege(freshCookie, groupId, tool);
        }

        // Step 3d: grant connection privilege (read) to group
        await api.grantGroupConnectionPrivilege(
            freshCookie, groupId, primaryConnId, 'read',
        );

        // Step 4: add user to group
        await api.addGroupMember(freshCookie, groupId, userId);
    });

    test.afterAll(async () => {
        try {
            const cleanupCookie = await auth.loginAsAdmin();
            // Clean up tokens
            for (const tokenId of createdTokenIds) {
                await api.deleteToken(cleanupCookie, tokenId).catch(() => {});
            }
            // Clean up user and group
            await api.deleteUser(cleanupCookie, userId).catch(() => {});
            await api.deleteGroup(cleanupCookie, groupId).catch(() => {});
        } catch {
            // Best-effort cleanup
        }
    });

    // -----------------------------------------------------------
    // TC1: scoped token permits only the group-granted privileges
    // -----------------------------------------------------------
    test('TC1: scoped token permits only the group-granted privileges', async ({ page }) => {
        test.setTimeout(300_000);
        await label('package', 'Token Scope RBAC');

        const adminPage = new AdminPage(page);
        const tokenPage = new TokenManagementPage(page);
        const tokenAnnotation = `${TEST_USER_PREFIX}scope-tc1-${Date.now()}`;

        let rawToken = '';
        let tokenId: number;

        // ---------------------------------------------------------
        // GUI: create token for testUsername
        // ---------------------------------------------------------
        await test.step('Navigate to Admin > Tokens', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToTokens();
        });

        await test.step('Open create dialog and fill form', async () => {
            await tokenPage.openCreateDialog();
            await tokenPage.selectOwner(testUsername);
            await tokenPage.fillAnnotation(tokenAnnotation);
        });

        await test.step('Install clipboard interceptor', async () => {
            await page.evaluate(() => {
                (window as unknown as Record<string, unknown>)['_capturedToken'] = '';
                const orig = navigator.clipboard.writeText.bind(navigator.clipboard);
                navigator.clipboard.writeText = async (text: string) => {
                    (window as unknown as Record<string, unknown>)['_capturedToken'] = text;
                    return orig(text).catch(() => undefined);
                };
            });
        });

        await test.step('Submit form and capture raw token', async () => {
            await tokenPage.submitCreateForm();
            await expect(
                page.getByRole('heading', { name: 'Token created' }),
            ).toBeVisible({ timeout: 10_000 });

            await page.getByRole('button', { name: /copy token/i }).click();

            rawToken = await page.evaluate(
                () => (window as unknown as Record<string, unknown>)['_capturedToken'] as string,
            );
            expect(rawToken, 'Raw token should be non-empty').toBeTruthy();

            await tokenPage.closeCreatedTokenDialog();
        });

        // ---------------------------------------------------------
        // API: find token ID and set scope
        // ---------------------------------------------------------
        await test.step('Find token ID from API', async () => {
            const adminCookie = await auth.loginAsAdmin();
            const { tokens } = await api.listTokens(adminCookie);
            const found = tokens.find((t) => t.name === tokenAnnotation);
            expect(found, `Token "${tokenAnnotation}" should exist in list`).toBeDefined();
            tokenId = found!.id;
            createdTokenIds.push(tokenId);
        });

        await test.step('Set token scope to group-granted privileges', async () => {
            const adminCookie = await auth.loginAsAdmin();
            await api.setTokenScope(adminCookie, tokenId, {
                connections: [{ connection_id: primaryConnId, access_level: 'read' }],
                mcp_privileges: GRANTED_MCP_TOOLS,
                admin_permissions: GRANTED_ADMIN_PERMISSIONS,
            });
        });

        // ---------------------------------------------------------
        // Verify: granted MCP tools return 200 and are not denied.
        // The endpoint always returns HTTP 200; denial is signaled
        // via isError: true with an "Access denied" message. A
        // transient error (e.g. DatabaseNotReadyError) produces
        // isError: true with a different message and must not be
        // treated as a permission failure.
        // ---------------------------------------------------------
        await test.step('Verify granted MCP tools are not denied', async () => {
            for (const toolName of GRANTED_MCP_TOOLS) {
                const args = buildToolArgs(toolName, primaryConnId);
                const r = await api.rawPost(
                    '/api/v1/mcp/tools/call',
                    { name: toolName, arguments: args },
                    { Authorization: `Bearer ${rawToken}` },
                );
                expect(
                    r.status,
                    `MCP tool ${toolName} should return HTTP 200`,
                ).toBe(200);
                const body = r.body as {
                    isError?: boolean;
                    content?: Array<{ type?: string; text?: string }>;
                };
                if (body.isError) {
                    const text = body.content?.[0]?.text ?? '';
                    expect(
                        text,
                        `MCP tool ${toolName} should not be permission-denied`,
                    ).not.toMatch(/access denied|do not have permission/i);
                }
            }
        });

        // ---------------------------------------------------------
        // Verify: granted admin permissions return 200
        // ---------------------------------------------------------
        await test.step('Verify granted admin permissions succeed (200)', async () => {
            for (const perm of GRANTED_ADMIN_PERMISSIONS) {
                const endpoint = ADMIN_PERMISSION_ENDPOINTS[perm];
                if (!endpoint) continue;
                const r = await api.rawGet(endpoint, {
                    Authorization: `Bearer ${rawToken}`,
                });
                expect(
                    r.status,
                    `Admin permission ${perm} via ${endpoint} should succeed (200)`,
                ).toBe(200);
            }
        });

        // ---------------------------------------------------------
        // Verify: denied MCP tools return HTTP 200 with isError: true
        // The /api/v1/mcp/tools/call endpoint always returns HTTP 200;
        // privilege denial is signaled via isError: true in the body.
        // ---------------------------------------------------------
        await test.step('Verify denied MCP tools return isError: true', async () => {
            for (const toolName of DENIED_MCP_TOOLS) {
                const r = await api.rawPost(
                    '/api/v1/mcp/tools/call',
                    { name: toolName, arguments: {} },
                    { Authorization: `Bearer ${rawToken}` },
                );
                expect(r.status, `MCP tool ${toolName} denial should return HTTP 200`).toBe(200);
                const body = r.body as { isError?: boolean };
                expect(
                    body.isError,
                    `MCP tool ${toolName} should be denied (isError: true in body)`,
                ).toBe(true);
            }
        });

        // ---------------------------------------------------------
        // Verify: denied admin permission returns 403
        // ---------------------------------------------------------
        await test.step('Verify denied admin permission is forbidden (403)', async () => {
            const endpoint = ADMIN_PERMISSION_ENDPOINTS[PERMISSIONS.MANAGE_PERMISSIONS];
            if (endpoint) {
                const r = await api.rawGet(endpoint, {
                    Authorization: `Bearer ${rawToken}`,
                });
                expect(
                    r.status,
                    `Admin permission ${PERMISSIONS.MANAGE_PERMISSIONS} should be forbidden (403)`,
                ).toBe(403);
            }
        });

        // ---------------------------------------------------------
        // Cleanup: delete the token
        // ---------------------------------------------------------
        await test.step('Cleanup: delete token', async () => {
            const adminCookie = await auth.loginAsAdmin();
            await api.deleteToken(adminCookie, tokenId).catch(() => {});
        });
    });

    // -----------------------------------------------------------
    // TC2: scoped token covering all effective permissions passes;
    //      token scoped to subset denies the rest
    // -----------------------------------------------------------
    test('TC2: scoped token covering all effective permissions passes; subset denies the rest', async ({ page }) => {
        test.setTimeout(300_000);
        await label('package', 'Token Scope RBAC');

        const adminPage = new AdminPage(page);
        const tokenPage = new TokenManagementPage(page);
        const tokenAnnotation = `${TEST_USER_PREFIX}scope-tc2-${Date.now()}`;

        let rawToken = '';
        let tokenId: number;

        // ---------------------------------------------------------
        // GUI: create token for testUsername
        // ---------------------------------------------------------
        await test.step('Navigate to Admin > Tokens', async () => {
            await injectFreshAdminSession(page, api);
            await page.goto('/');
            await adminPage.waitForAppLoad();
            await adminPage.navigateToTokens();
        });

        await test.step('Open create dialog and fill form', async () => {
            await tokenPage.openCreateDialog();
            await tokenPage.selectOwner(testUsername);
            await tokenPage.fillAnnotation(tokenAnnotation);
        });

        await test.step('Install clipboard interceptor', async () => {
            await page.evaluate(() => {
                (window as unknown as Record<string, unknown>)['_capturedToken'] = '';
                const orig = navigator.clipboard.writeText.bind(navigator.clipboard);
                navigator.clipboard.writeText = async (text: string) => {
                    (window as unknown as Record<string, unknown>)['_capturedToken'] = text;
                    return orig(text).catch(() => undefined);
                };
            });
        });

        await test.step('Submit form and capture raw token', async () => {
            await tokenPage.submitCreateForm();
            await expect(
                page.getByRole('heading', { name: 'Token created' }),
            ).toBeVisible({ timeout: 10_000 });

            await page.getByRole('button', { name: /copy token/i }).click();

            rawToken = await page.evaluate(
                () => (window as unknown as Record<string, unknown>)['_capturedToken'] as string,
            );
            expect(rawToken, 'Raw token should be non-empty').toBeTruthy();

            await tokenPage.closeCreatedTokenDialog();
        });

        // ---------------------------------------------------------
        // API: find token ID
        // ---------------------------------------------------------
        await test.step('Find token ID from API', async () => {
            const adminCookie = await auth.loginAsAdmin();
            const { tokens } = await api.listTokens(adminCookie);
            const found = tokens.find((t) => t.name === tokenAnnotation);
            expect(found, `Token "${tokenAnnotation}" should exist in list`).toBeDefined();
            tokenId = found!.id;
            createdTokenIds.push(tokenId);
        });

        // ---------------------------------------------------------
        // API: enumerate effective privileges
        // ---------------------------------------------------------
        let effectiveMcpPrivileges: string[] = [];
        let effectiveAdminPermissions: string[] = [];
        let effectiveConnectionPrivileges: Array<{
            connection_id: number;
            access_level: string;
        }> = [];

        await test.step('Enumerate effective user privileges', async () => {
            const adminCookie = await auth.loginAsAdmin();
            const privileges = await api.getUserPrivileges(adminCookie, userId);

            effectiveMcpPrivileges = privileges.mcp_privileges ?? [];
            effectiveAdminPermissions = privileges.admin_permissions ?? [];
            effectiveConnectionPrivileges = Object.entries(
                privileges.connection_privileges ?? {},
            ).map(([id, level]) => ({
                connection_id: parseInt(id, 10),
                access_level: level,
            }));

            expect(
                effectiveMcpPrivileges.length,
                'User should have at least one MCP privilege from group',
            ).toBeGreaterThan(0);
        });

        // ---------------------------------------------------------
        // API: set token scope = all effective privileges
        // ---------------------------------------------------------
        await test.step('Set token scope to all effective privileges', async () => {
            const adminCookie = await auth.loginAsAdmin();
            await api.setTokenScope(adminCookie, tokenId, {
                connections: effectiveConnectionPrivileges,
                mcp_privileges: effectiveMcpPrivileges,
                admin_permissions: effectiveAdminPermissions,
            });
        });

        // ---------------------------------------------------------
        // Verify: each effective MCP privilege is not permission-denied.
        // HTTP 200 is always returned. isError: true may occur for
        // transient reasons (e.g. DatabaseNotReadyError); only an
        // "Access denied" message indicates a permission failure.
        // ---------------------------------------------------------
        await test.step('Verify each effective MCP privilege is not denied', async () => {
            for (const toolName of effectiveMcpPrivileges) {
                const args = buildToolArgs(toolName, primaryConnId);
                const r = await api.rawPost(
                    '/api/v1/mcp/tools/call',
                    { name: toolName, arguments: args },
                    { Authorization: `Bearer ${rawToken}` },
                );
                expect(r.status, `Tool ${toolName} should return HTTP 200`).toBe(200);
                const body = r.body as {
                    isError?: boolean;
                    content?: Array<{ type?: string; text?: string }>;
                };
                if (body.isError) {
                    const text = body.content?.[0]?.text ?? '';
                    expect(
                        text,
                        `Tool ${toolName} should not be permission-denied`,
                    ).not.toMatch(/access denied|do not have permission/i);
                }
            }
        });

        // ---------------------------------------------------------
        // Verify: non-granted MCP tools return HTTP 200 with isError: true
        // The /api/v1/mcp/tools/call endpoint always returns HTTP 200;
        // privilege denial is signaled via isError: true in the body.
        // ---------------------------------------------------------
        await test.step('Verify non-granted MCP tools return isError: true', async () => {
            const nonGrantedTools = ALL_MCP_TOOLS.filter(
                (t) => !effectiveMcpPrivileges.includes(t),
            );
            for (const toolName of nonGrantedTools) {
                const r = await api.rawPost(
                    '/api/v1/mcp/tools/call',
                    { name: toolName, arguments: {} },
                    { Authorization: `Bearer ${rawToken}` },
                );
                expect(r.status, `Tool ${toolName} denial should return HTTP 200`).toBe(200);
                const body = r.body as { isError?: boolean };
                expect(
                    body.isError,
                    `Tool ${toolName} should be denied (isError: true in body)`,
                ).toBe(true);
            }
        });

        // ---------------------------------------------------------
        // Verify: each effective admin permission endpoint passes
        // ---------------------------------------------------------
        await test.step('Verify each effective admin permission endpoint succeeds', async () => {
            for (const perm of effectiveAdminPermissions) {
                const endpoint = ADMIN_PERMISSION_ENDPOINTS[perm];
                if (!endpoint) {
                    // No testable endpoint for this permission; skip
                    continue;
                }
                const r = await api.rawGet(endpoint, {
                    Authorization: `Bearer ${rawToken}`,
                });
                expect(
                    r.status,
                    `Admin permission ${perm} via ${endpoint} should not be forbidden`,
                ).not.toBe(403);
            }
        });

        // ---------------------------------------------------------
        // Verify: non-effective admin permission endpoint returns 403
        // ---------------------------------------------------------
        await test.step('Verify non-effective admin permission endpoint is forbidden (403)', async () => {
            const nonEffectivePerms = Object.values(PERMISSIONS).filter(
                (p) => !effectiveAdminPermissions.includes(p),
            );
            for (const perm of nonEffectivePerms) {
                const endpoint = ADMIN_PERMISSION_ENDPOINTS[perm];
                if (!endpoint) {
                    // No testable endpoint for this permission; skip
                    continue;
                }
                const r = await api.rawGet(endpoint, {
                    Authorization: `Bearer ${rawToken}`,
                });
                expect(
                    r.status,
                    `Admin permission ${perm} via ${endpoint} should be forbidden`,
                ).toBe(403);
            }
        });

        // ---------------------------------------------------------
        // Cleanup: delete the token
        // ---------------------------------------------------------
        await test.step('Cleanup: delete token', async () => {
            const adminCookie = await auth.loginAsAdmin();
            await api.deleteToken(adminCookie, tokenId).catch(() => {});
        });
    });
});
