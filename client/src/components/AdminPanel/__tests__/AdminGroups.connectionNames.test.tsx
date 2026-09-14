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

// Regression coverage for issue #309: expanding a group row showed
// connection-privilege chips as "Connection 3 (read)" (the numeric DB
// id) instead of the resolved connection name. The root cause was in
// AdminGroups' fetch of GET /api/v1/connections, which decoded the
// endpoint's bare JSON array as a wrapped `{ connections: [...] }`
// object and always read `undefined`. The real EffectivePermissionsPanel
// is intentionally NOT mocked here so the rendered chip label is
// asserted end-to-end.

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
    useAuth: () => ({ user: { isSuperuser: false } }),
}));

import AdminGroups from '../AdminGroups';

const GROUPS = [{ id: 1, name: 'eng', description: 'Engineering', member_count: 1 }];

// The connections endpoint returns a bare array (see
// connection_handlers.go RespondJSON). A wrapped object here would mask
// the regression, so the fixture uses the real shape.
const CONNECTIONS = [{ id: 3, name: 'primary-node' }];

function setupRouter() {
    const routes: Record<string, unknown> = {
        '/api/v1/rbac/groups': { groups: GROUPS },
        '/api/v1/connections': CONNECTIONS,
        '/api/v1/rbac/groups/1': { user_members: ['alice'], group_members: [] },
        '/api/v1/rbac/groups/1/effective-privileges': {
            connection_privileges: [{ connection_id: 3, access_level: 'read' }],
            admin_permissions: [],
            mcp_privileges: [],
        },
    };
    mockApiGet.mockImplementation((url: string) => {
        const keys = Object.keys(routes).sort((a, b) => b.length - a.length);
        const matchKey = keys.find((k) => url === k || url.startsWith(`${k}/`));
        if (!matchKey) {
            return Promise.reject(new Error(`No fixture for ${url}`));
        }
        return Promise.resolve(routes[matchKey]);
    });
}

describe('AdminGroups connection-name resolution (issue #309)', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('resolves the connection id to its name in the effective-permissions chip', async () => {
        setupRouter();
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminGroups />);
        await waitFor(() => {
            expect(screen.getByText('eng')).toBeInTheDocument();
        });

        await user.click(screen.getByText('eng'));

        // The chip should show the resolved connection name, not the id.
        await waitFor(() => {
            expect(
                screen.getByText('primary-node (read)'),
            ).toBeInTheDocument();
        });
        expect(screen.queryByText('Connection 3 (read)')).not.toBeInTheDocument();
    });
});
