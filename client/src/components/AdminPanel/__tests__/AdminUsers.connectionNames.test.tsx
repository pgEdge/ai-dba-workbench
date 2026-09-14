/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

// Regression coverage for issue #309 in the users panel: the
// connection-privilege chip must resolve the numeric connection id to
// its name when GET /api/v1/connections returns a bare array. The real
// EffectivePermissionsPanel is intentionally NOT mocked here.

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiPut = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: { username: 'alice', isSuperuser: true } }),
}));

import AdminUsers from '../AdminUsers';

const USERS = [
    {
        id: 2,
        username: 'bob',
        display_name: 'Bob',
        email: 'bob@example.com',
        annotation: '',
        is_service_account: false,
        is_superuser: false,
        enabled: true,
    },
];

// The connections endpoint returns a bare array (see
// connection_handlers.go). A wrapped object here would mask the bug.
const CONNECTIONS = [{ id: 3, name: 'primary-node' }];

function setupRouter() {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({ users: USERS });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve(CONNECTIONS);
        }
        if (/^\/api\/v1\/rbac\/users\/\d+\/privileges$/.test(url)) {
            return Promise.resolve({
                connection_privileges: [
                    { connection_id: 3, access_level: 'read' },
                ],
                admin_permissions: [],
                mcp_privileges: [],
                groups: [],
            });
        }
        return Promise.resolve({});
    });
}

describe('AdminUsers connection-name resolution (issue #309)', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('resolves the connection id to its name in the effective-permissions chip', async () => {
        setupRouter();
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminUsers />);
        await waitFor(() => {
            expect(screen.getByText('bob')).toBeInTheDocument();
        });

        await user.click(screen.getByText('bob'));

        await waitFor(() => {
            expect(
                screen.getByText('primary-node (read)'),
            ).toBeInTheDocument();
        });
        expect(screen.queryByText('Connection 3 (read)')).not.toBeInTheDocument();
    });
});
