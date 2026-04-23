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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CreateTokenDialog from '../CreateTokenDialog';
import type { User, Connection, McpPrivilege, AdminPermissionEntry } from '../tokenTypes';

const theme = createTheme();

const USERS: User[] = [
    { id: 1, username: 'alice' },
    { id: 2, username: 'bob' },
];

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

const renderComponent = (props: Partial<React.ComponentProps<typeof CreateTokenDialog>> = {}) => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onSubmit: vi.fn(),
        loading: false,
        error: null,
        annotation: '',
        onAnnotationChange: vi.fn(),
        owner: null,
        onOwnerChange: vi.fn(),
        users: USERS,
        expiry: '90d',
        onExpiryChange: vi.fn(),
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
            <CreateTokenDialog {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

describe('CreateTokenDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders dialog title', () => {
        renderComponent();
        expect(screen.getByText('Create token')).toBeInTheDocument();
    });

    it('renders Name field', () => {
        renderComponent();
        expect(screen.getByLabelText(/^Name/)).toBeInTheDocument();
    });

    it('renders Owner autocomplete', () => {
        renderComponent();
        expect(screen.getByLabelText(/^Owner/)).toBeInTheDocument();
    });

    it('renders Expiry select', () => {
        renderComponent();
        expect(screen.getByLabelText(/^Expiry/)).toBeInTheDocument();
    });

    it('renders Scope section', () => {
        renderComponent();
        expect(screen.getByText('Scope (Optional)')).toBeInTheDocument();
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

    it('renders Cancel and Create buttons', () => {
        renderComponent();
        expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Create' })).toBeInTheDocument();
    });

    it('disables Create button when no owner or annotation', () => {
        renderComponent({ owner: null, annotation: '' });
        expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
    });

    it('enables Create button when owner and annotation are provided', () => {
        renderComponent({
            owner: USERS[0],
            annotation: 'Test Token',
        });
        expect(screen.getByRole('button', { name: 'Create' })).not.toBeDisabled();
    });

    it('calls onAnnotationChange when name field changes', () => {
        const onAnnotationChange = vi.fn();
        renderComponent({ onAnnotationChange });

        const input = screen.getByLabelText(/^Name/);
        fireEvent.change(input, { target: { value: 'New Token' } });

        expect(onAnnotationChange).toHaveBeenCalledWith('New Token');
    });

    it('calls onClose when Cancel is clicked', () => {
        const onClose = vi.fn();
        renderComponent({ onClose });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        expect(onClose).toHaveBeenCalled();
    });

    it('calls onSubmit when Create is clicked', () => {
        const onSubmit = vi.fn();
        renderComponent({
            onSubmit,
            owner: USERS[0],
            annotation: 'Test Token',
        });

        fireEvent.click(screen.getByRole('button', { name: 'Create' }));
        expect(onSubmit).toHaveBeenCalled();
    });

    it('displays error when error prop is set', () => {
        renderComponent({ error: 'Failed to create token' });
        expect(screen.getByText('Failed to create token')).toBeInTheDocument();
    });

    it('shows loading spinner when loading', () => {
        renderComponent({
            loading: true,
            owner: USERS[0],
            annotation: 'Test Token',
        });
        expect(screen.getByRole('progressbar', { name: 'Creating' })).toBeInTheDocument();
    });

    it('disables fields when loading', () => {
        renderComponent({ loading: true });
        expect(screen.getByLabelText(/^Name/)).toBeDisabled();
    });

    it('does not render when open is false', () => {
        renderComponent({ open: false });
        expect(screen.queryByText('Create token')).not.toBeInTheDocument();
    });

    it('shows expiry options', async () => {
        renderComponent();
        const expirySelect = screen.getByLabelText(/^Expiry/);
        fireEvent.mouseDown(expirySelect);

        await waitFor(() => {
            expect(screen.getByRole('option', { name: '30 days' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: '90 days' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: '1 year' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'Never' })).toBeInTheDocument();
        });
    });

    it('calls onExpiryChange when expiry is changed', async () => {
        const onExpiryChange = vi.fn();
        renderComponent({ onExpiryChange });

        const expirySelect = screen.getByLabelText(/^Expiry/);
        fireEvent.mouseDown(expirySelect);

        await waitFor(() => {
            expect(screen.getByRole('option', { name: '30 days' })).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('option', { name: '30 days' }));
        expect(onExpiryChange).toHaveBeenCalledWith('30d');
    });

    it('shows owner options in autocomplete', async () => {
        renderComponent();
        const ownerInput = screen.getByLabelText(/^Owner/);
        fireEvent.focus(ownerInput);
        fireEvent.keyDown(ownerInput, { key: 'ArrowDown' });

        await waitFor(() => {
            expect(screen.getByRole('option', { name: 'alice' })).toBeInTheDocument();
            expect(screen.getByRole('option', { name: 'bob' })).toBeInTheDocument();
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
        expect(onScopedConnectionsChange).toHaveBeenCalled();
    });
});
