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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import TokensTable from '../TokensTable';
import type { Token, Connection } from '../tokenTypes';

// Mock EffectivePermissionsPanel to simplify tests
vi.mock('../../EffectivePermissionsPanel', () => ({
    default: () => <div data-testid="effective-permissions-panel">Permissions Panel</div>,
}));

const theme = createTheme();

const CONNECTIONS: Connection[] = [
    { id: 1, name: 'Primary DB' },
    { id: 2, name: 'Secondary DB' },
];

const TOKENS: Token[] = [
    {
        id: 1,
        name: 'API Token',
        username: 'alice',
        user_id: 42,
        expires_at: '2025-12-31T23:59:59Z',
        scope: {
            scoped: true,
            connections: [{ connection_id: 1, access_level: 'read_write' }],
            mcp_privileges: [1, 2],
            admin_permissions: ['manage_users'],
        },
    },
    {
        id: 2,
        token_prefix: 'pgedge_abc',
        username: 'bob',
        is_service_account: true,
        expires_at: null,
        scope: { scoped: false },
    },
    {
        id: 3,
        name: 'Superuser Token',
        username: 'admin',
        is_superuser: true,
        expires_at: '2024-06-15T00:00:00Z',
        scope: { scoped: false },
    },
];

const renderComponent = (props: Partial<React.ComponentProps<typeof TokensTable>> = {}) => {
    const defaultProps = {
        tokens: TOKENS,
        connections: CONNECTIONS,
        expandedToken: null,
        onRowClick: vi.fn(),
        onEdit: vi.fn(),
        onDelete: vi.fn(),
        getMcpPrivilegeName: (id: number) => id === -1 ? 'All MCP Privileges' : `Privilege ${id}`,
    };
    return render(
        <ThemeProvider theme={theme}>
            <TokensTable {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

describe('TokensTable', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders table headers', () => {
        renderComponent();
        expect(screen.getByText('Name')).toBeInTheDocument();
        expect(screen.getByText('Owner')).toBeInTheDocument();
        expect(screen.getByText('Expires')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();
    });

    it('renders empty state when no tokens', () => {
        renderComponent({ tokens: [] });
        expect(screen.getByText('No tokens found.')).toBeInTheDocument();
    });

    it('renders token names', () => {
        renderComponent();
        expect(screen.getByText('API Token')).toBeInTheDocument();
        expect(screen.getByText('pgedge_abc')).toBeInTheDocument();
        expect(screen.getByText('Superuser Token')).toBeInTheDocument();
    });

    it('renders token usernames', () => {
        renderComponent();
        expect(screen.getByText('alice')).toBeInTheDocument();
        expect(screen.getByText('bob')).toBeInTheDocument();
        expect(screen.getByText('admin')).toBeInTheDocument();
    });

    it('renders Service Account chip', () => {
        renderComponent();
        expect(screen.getByText('Service Account')).toBeInTheDocument();
    });

    it('renders Superuser chip', () => {
        renderComponent();
        expect(screen.getByText('Superuser')).toBeInTheDocument();
    });

    it('renders formatted expiry dates', () => {
        renderComponent();
        // The actual formatted date depends on locale, so check for "Never" for null expiry
        expect(screen.getByText('Never')).toBeInTheDocument();
    });

    it('renders edit and delete buttons for each token', () => {
        renderComponent();
        const editButtons = screen.getAllByRole('button', { name: /edit token/i });
        const deleteButtons = screen.getAllByRole('button', { name: /delete token/i });

        expect(editButtons).toHaveLength(3);
        expect(deleteButtons).toHaveLength(3);
    });

    it('calls onRowClick when a row is clicked', () => {
        const onRowClick = vi.fn();
        renderComponent({ onRowClick });

        // Find the row containing "API Token" and click it
        const row = screen.getByText('API Token').closest('tr');
        if (row) {
            fireEvent.click(row);
        }

        expect(onRowClick).toHaveBeenCalledWith(TOKENS[0]);
    });

    it('calls onEdit when edit button is clicked', () => {
        const onEdit = vi.fn();
        renderComponent({ onEdit });

        const editButtons = screen.getAllByRole('button', { name: /edit token/i });
        fireEvent.click(editButtons[0]);

        expect(onEdit).toHaveBeenCalledWith(TOKENS[0]);
    });

    it('calls onDelete when delete button is clicked', () => {
        const onDelete = vi.fn();
        renderComponent({ onDelete });

        const deleteButtons = screen.getAllByRole('button', { name: /delete token/i });
        fireEvent.click(deleteButtons[0]);

        expect(onDelete).toHaveBeenCalledWith(TOKENS[0]);
    });

    it('stops propagation when edit button is clicked', () => {
        const onRowClick = vi.fn();
        const onEdit = vi.fn();
        renderComponent({ onRowClick, onEdit });

        const editButtons = screen.getAllByRole('button', { name: /edit token/i });
        fireEvent.click(editButtons[0]);

        expect(onEdit).toHaveBeenCalled();
        expect(onRowClick).not.toHaveBeenCalled();
    });

    it('stops propagation when delete button is clicked', () => {
        const onRowClick = vi.fn();
        const onDelete = vi.fn();
        renderComponent({ onRowClick, onDelete });

        const deleteButtons = screen.getAllByRole('button', { name: /delete token/i });
        fireEvent.click(deleteButtons[0]);

        expect(onDelete).toHaveBeenCalled();
        expect(onRowClick).not.toHaveBeenCalled();
    });

    it('shows expand icon when token is not expanded', () => {
        renderComponent({ expandedToken: null });
        const expandIcons = screen.getAllByTestId('ExpandMoreIcon');
        expect(expandIcons.length).toBeGreaterThan(0);
    });

    it('shows collapse icon when token is expanded', () => {
        renderComponent({ expandedToken: 1 });
        expect(screen.getByTestId('ExpandLessIcon')).toBeInTheDocument();
    });

    it('shows EffectivePermissionsPanel when token is expanded and has scope', () => {
        renderComponent({ expandedToken: 1 });
        expect(screen.getByTestId('effective-permissions-panel')).toBeInTheDocument();
    });

    it('shows unrestricted message when expanded token has no scope', () => {
        renderComponent({ expandedToken: 2 });
        expect(
            screen.getByText(/Unrestricted - this token has access to all permissions/)
        ).toBeInTheDocument();
    });

    it('displays Token Scope heading when expanded', () => {
        renderComponent({ expandedToken: 1 });
        expect(screen.getByText('Token Scope')).toBeInTheDocument();
    });

    it('falls back to Token #id when no name or prefix', () => {
        const tokensWithoutName: Token[] = [
            { id: 99, username: 'test', scope: { scoped: false } },
        ];
        renderComponent({ tokens: tokensWithoutName });
        expect(screen.getByText('Token #99')).toBeInTheDocument();
    });

    it('shows dash when no username', () => {
        const tokensWithoutUsername: Token[] = [
            { id: 99, name: 'Test Token', scope: { scoped: false } },
        ];
        renderComponent({ tokens: tokensWithoutUsername });
        expect(screen.getByText('-')).toBeInTheDocument();
    });
});
