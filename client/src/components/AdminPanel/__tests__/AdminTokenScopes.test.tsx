/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';

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

import AdminTokenScopes from '../AdminTokenScopes';

const MCP_PRIVILEGES = [
    { id: 1, identifier: 'query_read' },
    { id: 2, identifier: 'query_write' },
    { id: 3, identifier: 'schema_read' },
];

const USERS = [
    { id: 42, username: 'alice' },
];

const CONNECTIONS = [
    { id: 100, name: 'primary-db' },
];

const setupApiGetMock = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens: [] });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: CONNECTIONS });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve(MCP_PRIVILEGES);
        }
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({ users: USERS });
        }
        if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
            return Promise.resolve({
                is_superuser: false,
                connection_privileges: {},
                mcp_privileges: ['*'],
                admin_permissions: ['*'],
            });
        }
        return Promise.reject(new Error(`Unexpected URL: ${url}`));
    });
};

describe('AdminTokenScopes', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('exposes all MCP and admin options when owner has wildcard privileges', async () => {
        setupApiGetMock();

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create token/i }))
                .toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /create token/i }));

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const ownerOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(ownerOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        const mcpCombo = screen.getByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);

        const mcpListbox = await screen.findByRole('listbox');
        expect(
            within(mcpListbox).getByRole('option', { name: 'All MCP Privileges' }),
        ).toBeInTheDocument();
        for (const priv of MCP_PRIVILEGES) {
            expect(
                within(mcpListbox).getByRole('option', { name: priv.identifier }),
            ).toBeInTheDocument();
        }

        await user.keyboard('{Escape}');

        const adminCombo = screen.getByRole('combobox', {
            name: /allowed admin permissions/i,
        });
        await user.click(adminCombo);

        const adminListbox = await screen.findByRole('listbox');
        expect(
            within(adminListbox).getByRole('option', { name: 'All Admin Permissions' }),
        ).toBeInTheDocument();
        const expectedAdminLabels = [
            'Manage Connections',
            'Manage Groups',
            'Manage Permissions',
            'Manage Users',
            'Manage Token Scopes',
            'Manage Blackouts',
            'Manage Probes',
            'Manage Alert Rules',
            'Manage Notification Channels',
        ];
        for (const label of expectedAdminLabels) {
            expect(
                within(adminListbox).getByRole('option', { name: label }),
            ).toBeInTheDocument();
        }
    });
});
