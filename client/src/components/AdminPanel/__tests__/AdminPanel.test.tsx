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
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import AdminPanel from '../index';

// Mock the child admin components to isolate AdminPanel testing
vi.mock('../AdminUsers', () => ({
    default: () => <div data-testid="admin-users">Admin Users Component</div>,
}));
vi.mock('../AdminGroups', () => ({
    default: () => <div data-testid="admin-groups">Admin Groups Component</div>,
}));
vi.mock('../AdminPermissions', () => ({
    default: () => <div data-testid="admin-permissions">Admin Permissions Component</div>,
}));
vi.mock('../AdminTokenScopes', () => ({
    default: () => <div data-testid="admin-token-scopes">Admin Token Scopes Component</div>,
}));
vi.mock('../AdminProbes', () => ({
    default: () => <div data-testid="admin-probes">Admin Probes Component</div>,
}));
vi.mock('../AdminAlertRules', () => ({
    default: () => <div data-testid="admin-alert-rules">Admin Alert Rules Component</div>,
}));
vi.mock('../AdminEmailChannels', () => ({
    default: () => <div data-testid="admin-email-channels">Admin Email Channels Component</div>,
}));
vi.mock('../AdminSlackChannels', () => ({
    default: () => <div data-testid="admin-slack-channels">Admin Slack Channels Component</div>,
}));
vi.mock('../AdminMattermostChannels', () => ({
    default: () => <div data-testid="admin-mattermost-channels">Admin Mattermost Channels Component</div>,
}));
vi.mock('../AdminWebhookChannels', () => ({
    default: () => <div data-testid="admin-webhook-channels">Admin Webhook Channels Component</div>,
}));
vi.mock('../AdminMemories', () => ({
    default: () => <div data-testid="admin-memories">Admin Memories Component</div>,
}));

// Mock context hooks
const mockUser = {
    authenticated: true,
    username: 'testuser',
    isSuperuser: true,
};

const mockHasPermission = vi.fn(() => true);

vi.mock('../../../contexts/AuthContext', () => ({
    useAuth: () => ({
        user: mockUser,
        hasPermission: mockHasPermission,
    }),
}));

vi.mock('../../../contexts/AICapabilitiesContext', () => ({
    useAICapabilities: () => ({
        aiEnabled: true,
        maxIterations: 50,
        loading: false,
    }),
}));

describe('AdminPanel', () => {
    const mockOnClose = vi.fn();

    // Small helper to reduce repetition of the renderWithTheme(<AdminPanel ... />)
    // call. `open` defaults to true; pass `{ open: false }` to test the
    // closed state.
    const renderAdminPanel = ({ open = true }: { open?: boolean } = {}) =>
        renderWithTheme(<AdminPanel open={open} onClose={mockOnClose} />);

    beforeEach(() => {
        vi.clearAllMocks();
        mockHasPermission.mockReturnValue(true);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders the panel when open is true', () => {
        renderAdminPanel();

        expect(screen.getByText('Administration')).toBeInTheDocument();
    });

    it('does not render content when open is false', () => {
        renderAdminPanel({ open: false });

        expect(screen.queryByText('Administration')).not.toBeInTheDocument();
    });

    it('renders the close button', () => {
        renderAdminPanel();

        const closeButton = screen.getByRole('button', { name: /close administration/i });
        expect(closeButton).toBeInTheDocument();
    });

    it('calls onClose when the close button is clicked', () => {
        renderAdminPanel();

        const closeButton = screen.getByRole('button', { name: /close administration/i });
        fireEvent.click(closeButton);

        expect(mockOnClose).toHaveBeenCalledTimes(1);
    });

    it('renders Security section with navigation items', () => {
        renderAdminPanel();

        expect(screen.getByText('Security')).toBeInTheDocument();
        expect(screen.getByText('Users')).toBeInTheDocument();
        expect(screen.getByText('Groups')).toBeInTheDocument();
        expect(screen.getByText('Permissions')).toBeInTheDocument();
        expect(screen.getByText('Tokens')).toBeInTheDocument();
    });

    it('renders Monitoring section with navigation items', () => {
        renderAdminPanel();

        expect(screen.getByText('Monitoring')).toBeInTheDocument();
        expect(screen.getByText('Probe Defaults')).toBeInTheDocument();
        expect(screen.getByText('Alert Defaults')).toBeInTheDocument();
    });

    it('renders Notifications section with navigation items', () => {
        renderAdminPanel();

        expect(screen.getByText('Notifications')).toBeInTheDocument();
        expect(screen.getByText('Email Channels')).toBeInTheDocument();
        expect(screen.getByText('Slack Channels')).toBeInTheDocument();
        expect(screen.getByText('Mattermost Channels')).toBeInTheDocument();
        expect(screen.getByText('Webhook Channels')).toBeInTheDocument();
    });

    it('renders AI section when aiEnabled is true', () => {
        renderAdminPanel();

        expect(screen.getByText('AI')).toBeInTheDocument();
        expect(screen.getByText('Memories')).toBeInTheDocument();
    });

    it('shows the first component when dialog opens', async () => {
        renderAdminPanel();

        // Wait for the first component (Users) to be selected and rendered
        await waitFor(() => {
            expect(screen.getByTestId('admin-users')).toBeInTheDocument();
        });
    });

    it('navigates to Groups when the Groups button is clicked', async () => {
        renderAdminPanel();

        // Wait for initial render - Users should be selected
        await waitFor(() => {
            expect(screen.getByTestId('admin-users')).toBeInTheDocument();
        });

        // Click the Groups nav item. MUI's ListItemButton renders as a div
        // with role="button" and accepts regular DOM click events.
        fireEvent.click(screen.getByText('Groups'));

        // After clicking Groups, the Groups component should render and the
        // Users component should no longer be in the document.
        await waitFor(() => {
            expect(screen.getByTestId('admin-groups')).toBeInTheDocument();
        });
        expect(screen.queryByTestId('admin-users')).not.toBeInTheDocument();
    });

    it('nav buttons have correct role and are accessible', async () => {
        renderAdminPanel();

        await waitFor(() => {
            expect(screen.getByTestId('admin-users')).toBeInTheDocument();
        });

        // All nav items should be buttons
        expect(screen.getByRole('button', { name: 'Users' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Groups' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Permissions' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Tokens' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Probe Defaults' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Alert Defaults' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Memories' })).toBeInTheDocument();
    });
});

describe('AdminPanel - Permission filtering', () => {
    const mockOnClose = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('filters navigation items based on permissions', () => {
        // Only allow manage_users permission
        mockHasPermission.mockImplementation((perm: string) => {
            return perm === 'manage_users';
        });

        renderWithTheme(
            <AdminPanel open={true} onClose={mockOnClose} />
        );

        expect(screen.getByText('Users')).toBeInTheDocument();
        // Groups requires manage_groups permission which is not granted
        expect(screen.queryByText('Groups')).not.toBeInTheDocument();
    });
});
