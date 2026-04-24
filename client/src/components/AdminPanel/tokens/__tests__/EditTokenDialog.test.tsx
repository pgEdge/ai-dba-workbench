/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import EditTokenDialog from '../EditTokenDialog';
import type { Token, Connection, McpPrivilege, AdminPermissionEntry } from '../tokenTypes';

const theme = createTheme();

const TOKEN: Token = {
    id: 1,
    name: 'Test Token',
    username: 'alice',
    user_id: 42,
    scope: { scoped: true },
};

const CONNECTIONS: Connection[] = [
    { id: 1, name: 'Primary DB' },
    { id: 2, name: 'Secondary DB' },
];

const MCP_PRIVILEGES: McpPrivilege[] = [
    { id: 1, identifier: 'query_read' },
    { id: 2, identifier: 'query_write' },
];

const ADMIN_PERMISSIONS: AdminPermissionEntry[] = [
    { id: 'manage_users', label: 'Manage Users' },
    { id: 'manage_groups', label: 'Manage Groups' },
];

const renderComponent = (props: Partial<React.ComponentProps<typeof EditTokenDialog>> = {}) => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onSubmit: vi.fn(),
        loading: false,
        error: null,
        token: TOKEN,
        availableConnections: CONNECTIONS,
        scopedConnections: [],
        onScopedConnectionsChange: vi.fn(),
        ownerConnectionLevels: { 1: 'read_write', 2: 'read_write' },
        ownerIsSuperuser: false,
        availableMcpPrivileges: MCP_PRIVILEGES,
        selectedMcpPrivileges: [],
        onMcpPrivilegesChange: vi.fn(),
        availableAdminPermissions: ADMIN_PERMISSIONS,
        selectedAdminPermissions: [],
        onAdminPermissionsChange: vi.fn(),
    };
    return render(
        <ThemeProvider theme={theme}>
            <EditTokenDialog {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

describe('EditTokenDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders dialog title with token name', () => {
        renderComponent();
        expect(screen.getByText('Edit token: Test Token')).toBeInTheDocument();
    });

    it('renders dialog title with token prefix when no name', () => {
        renderComponent({ token: { ...TOKEN, name: undefined, token_prefix: 'pgedge_abc' } });
        expect(screen.getByText('Edit token: pgedge_abc')).toBeInTheDocument();
    });

    it('renders dialog title with Token fallback when no name or prefix', () => {
        renderComponent({ token: { ...TOKEN, name: undefined, token_prefix: undefined } });
        expect(screen.getByText('Edit token: Token')).toBeInTheDocument();
    });

    it('renders close button in title', () => {
        renderComponent();
        expect(screen.getByRole('button', { name: 'close' })).toBeInTheDocument();
    });

    it('renders Connections section', () => {
        renderComponent();
        expect(screen.getByText('Connections')).toBeInTheDocument();
    });

    it('renders MCP Privileges section', () => {
        renderComponent();
        expect(screen.getByText('MCP Privileges')).toBeInTheDocument();
    });

    it('renders Admin Permissions section', () => {
        renderComponent();
        expect(screen.getByText('Admin Permissions')).toBeInTheDocument();
    });

    it('renders Add Connection autocomplete', () => {
        renderComponent();
        expect(screen.getByLabelText(/Add Connection/)).toBeInTheDocument();
    });

    it('renders MCP Privileges autocomplete', () => {
        renderComponent();
        expect(screen.getByLabelText(/Allowed MCP Privileges/)).toBeInTheDocument();
    });

    it('renders Admin Permissions autocomplete', () => {
        renderComponent();
        expect(screen.getByLabelText(/Allowed Admin Permissions/)).toBeInTheDocument();
    });

    it('renders Cancel and Save buttons', () => {
        renderComponent();
        expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
    });

    it('calls onClose when Cancel is clicked', () => {
        const onClose = vi.fn();
        renderComponent({ onClose });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        expect(onClose).toHaveBeenCalled();
    });

    it('calls onClose when close button in title is clicked', () => {
        const onClose = vi.fn();
        renderComponent({ onClose });

        fireEvent.click(screen.getByRole('button', { name: 'close' }));
        expect(onClose).toHaveBeenCalled();
    });

    it('calls onSubmit when Save is clicked', () => {
        const onSubmit = vi.fn();
        renderComponent({ onSubmit });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        expect(onSubmit).toHaveBeenCalled();
    });

    it('displays error when error prop is set', () => {
        renderComponent({ error: 'Failed to save scope' });
        expect(screen.getByText('Failed to save scope')).toBeInTheDocument();
    });

    it('shows loading spinner when loading', () => {
        renderComponent({ loading: true });
        expect(screen.getByRole('progressbar', { name: 'Saving' })).toBeInTheDocument();
    });

    it('disables Cancel button when loading', () => {
        renderComponent({ loading: true });
        expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    });

    it('disables close button when loading', () => {
        renderComponent({ loading: true });
        expect(screen.getByRole('button', { name: 'close' })).toBeDisabled();
    });

    it('does not render when open is false', () => {
        renderComponent({ open: false });
        expect(screen.queryByText('Edit token: Test Token')).not.toBeInTheDocument();
    });

    it('shows connection options in autocomplete', async () => {
        renderComponent();
        const addConnInput = screen.getByLabelText(/Add Connection/);
        fireEvent.focus(addConnInput);
        fireEvent.keyDown(addConnInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.getByRole('option', { name: 'Primary DB' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'Secondary DB' })).toBeInTheDocument();
        });
    });

    it('adds connection to scope when selected', async () => {
        const onScopedConnectionsChange = vi.fn();
        renderComponent({ onScopedConnectionsChange });

        const addConnInput = screen.getByLabelText(/Add Connection/);
        fireEvent.focus(addConnInput);
        fireEvent.keyDown(addConnInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.getByRole('option', { name: 'Primary DB' })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('option', { name: 'Primary DB' }));
        expect(onScopedConnectionsChange).toHaveBeenCalledWith(
            expect.arrayContaining([
                expect.objectContaining({ id: 1, name: 'Primary DB' }),
            ]),
        );
    });

    it('filters out already selected connections from dropdown', async () => {
        renderComponent({
            scopedConnections: [{ id: 1, name: 'Primary DB', access_level: 'read' }],
        });

        const addConnInput = screen.getByLabelText(/Add Connection/);
        fireEvent.focus(addConnInput);
        fireEvent.keyDown(addConnInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.queryByRole('option', { name: 'Primary DB' })).not.toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'Secondary DB' })).toBeInTheDocument();
        });
    });

    it('shows MCP privilege options', async () => {
        renderComponent();
        const mcpInput = screen.getByLabelText(/Allowed MCP Privileges/);
        fireEvent.focus(mcpInput);
        fireEvent.keyDown(mcpInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.getByRole('option', { name: 'All MCP Privileges' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'query_read' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'query_write' })).toBeInTheDocument();
        });
    });

    it('shows admin permission options', async () => {
        renderComponent();
        const adminInput = screen.getByLabelText(/Allowed Admin Permissions/);
        fireEvent.focus(adminInput);
        fireEvent.keyDown(adminInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.getByRole('option', { name: 'All Admin Permissions' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'Manage Users' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'Manage Groups' })).toBeInTheDocument();
        });
    });

    it('handles null token gracefully', () => {
        renderComponent({ token: null });
        expect(screen.getByText('Edit token: Token')).toBeInTheDocument();
    });
});
