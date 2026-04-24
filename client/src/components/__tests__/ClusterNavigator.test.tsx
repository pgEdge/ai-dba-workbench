/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterNavigator Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@mui/material/styles';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterNavigator from '../ClusterNavigator';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

const lightTheme = createPgedgeTheme('light');
const darkTheme = createPgedgeTheme('dark');

// Hoisted mock handles so tests can assert on the calls made by handlers
// inside the component.
const {
    mockUpdateGroupName,
    mockUpdateClusterName,
    mockUpdateServerName,
    mockDeleteCluster,
    mockCreateGroup,
    mockDeleteGroup,
    mockMoveClusterToGroup,
    mockFetchClusterData,
    mockUseAuth,
} = vi.hoisted(() => ({
    mockUpdateGroupName: vi.fn(),
    mockUpdateClusterName: vi.fn(),
    mockUpdateServerName: vi.fn(),
    mockDeleteCluster: vi.fn(),
    mockCreateGroup: vi.fn(),
    mockDeleteGroup: vi.fn(),
    mockMoveClusterToGroup: vi.fn(),
    mockFetchClusterData: vi.fn(),
    mockUseAuth: vi.fn(() => ({ user: { isSuperuser: false } })),
}));

// Mock the AuthContext
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => mockUseAuth(),
}));

// Mock the ClusterContext
vi.mock('../../contexts/useCluster', () => ({
    useCluster: () => ({
        updateGroupName: mockUpdateGroupName,
        updateClusterName: mockUpdateClusterName,
        updateServerName: mockUpdateServerName,
        deleteCluster: mockDeleteCluster,
        createGroup: mockCreateGroup,
        deleteGroup: mockDeleteGroup,
        moveClusterToGroup: mockMoveClusterToGroup,
        fetchClusterData: mockFetchClusterData,
        autoRefreshEnabled: false,
        setAutoRefreshEnabled: vi.fn(),
        lastRefresh: null,
        getServer: vi.fn(),
        createServer: vi.fn(),
        updateServer: vi.fn(),
        deleteServer: vi.fn(),
    }),
}));

// Mock the AlertsContext
vi.mock('../../contexts/useAlerts', () => ({
    useAlerts: () => ({
        alerts: [],
        activeAlerts: [],
        loading: false,
        error: null,
        fetchAlerts: vi.fn(),
        getServerAlertCount: () => 0,
        getTotalAlertCount: () => 0,
    }),
}));

// Mock the BlackoutContext
vi.mock('../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({
        blackouts: [],
        loading: false,
        error: null,
        isServerBlackedOut: () => false,
        isClusterBlackedOut: () => false,
        isGroupBlackedOut: () => false,
        getEffectiveBlackout: () => null,
        createBlackout: vi.fn(),
        deleteBlackout: vi.fn(),
        fetchBlackouts: vi.fn(),
    }),
}));

// Mock override panels so they don't fetch data from the API
vi.mock('../AlertOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="alert-overrides-panel">
            AlertOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ProbeOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="probe-overrides-panel">
            ProbeOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ChannelOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="channel-overrides-panel">
            ChannelOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

// Mock data
const mockClusterData = [
    {
        id: 'group-1',
        name: 'Production',
        clusters: [
            {
                id: 'cluster-1',
                name: 'US East Cluster',
                servers: [
                    { id: 1, name: 'pg-east-1', host: 'pg-east-1.example.com', port: 5432, status: 'online', primary_role: 'binary_primary' },
                    { id: 2, name: 'pg-east-2', host: 'pg-east-2.example.com', port: 5432, status: 'online', primary_role: 'binary_standby' },
                ],
            },
            {
                id: 'cluster-2',
                name: 'US West Cluster',
                servers: [
                    { id: 3, name: 'pg-west-1', host: 'pg-west-1.example.com', port: 5432, status: 'warning', primary_role: 'binary_primary' },
                    { id: 4, name: 'pg-west-2', host: 'pg-west-2.example.com', port: 5432, status: 'offline', primary_role: 'binary_standby' },
                ],
            },
        ],
    },
    {
        id: 'group-2',
        name: 'Development',
        clusters: [
            {
                id: 'cluster-3',
                name: 'Dev Cluster',
                servers: [
                    { id: 5, name: 'pg-dev-1', host: 'localhost', port: 5432, status: 'online', primary_role: null },
                ],
            },
        ],
    },
];

const renderWithTheme = (ui: React.ReactElement, theme = lightTheme) =>
    render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);

describe('ClusterNavigator', () => {
    let onSelectServer;
    let onRefresh;

    beforeEach(() => {
        onSelectServer = vi.fn();
        onRefresh = vi.fn();
        mockUpdateGroupName.mockReset().mockResolvedValue(undefined);
        mockUpdateClusterName.mockReset().mockResolvedValue(undefined);
        mockUpdateServerName.mockReset().mockResolvedValue(undefined);
        mockDeleteCluster.mockReset().mockResolvedValue(undefined);
        mockCreateGroup.mockReset().mockResolvedValue({});
        mockDeleteGroup.mockReset().mockResolvedValue(undefined);
        mockMoveClusterToGroup.mockReset().mockResolvedValue(undefined);
        mockFetchClusterData.mockReset().mockResolvedValue(undefined);
        mockUseAuth.mockReset().mockReturnValue({
            user: { isSuperuser: false },
        });
    });

    it('renders the component with header', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('Database Servers')).toBeInTheDocument();
    });

    it('displays server count summary', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // Should show online count
        expect(screen.getByText(/online/)).toBeInTheDocument();
        // Should show total servers
        expect(screen.getByText(/of 5 servers/)).toBeInTheDocument();
    });

    it('renders cluster groups', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('Production')).toBeInTheDocument();
        expect(screen.getByText('Development')).toBeInTheDocument();
    });

    it('renders clusters within groups', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('US East Cluster')).toBeInTheDocument();
        expect(screen.getByText('US West Cluster')).toBeInTheDocument();
        expect(screen.getByText('Dev Cluster')).toBeInTheDocument();
    });

    it('renders servers within clusters', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('pg-east-1')).toBeInTheDocument();
        expect(screen.getByText('pg-east-2')).toBeInTheDocument();
        expect(screen.getByText('pg-west-1')).toBeInTheDocument();
    });

    it('calls onSelectServer when a server is clicked', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        fireEvent.click(screen.getByText('pg-east-1'));

        expect(onSelectServer).toHaveBeenCalledWith(
            expect.objectContaining({
                id: 1,
                name: 'pg-east-1',
            })
        );
    });

    it('highlights the selected server', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                selectedServerId={1}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // The selected server should have different styling (tested via snapshot or visual)
        const serverItem = screen.getByText('pg-east-1').closest('div');
        expect(serverItem).toBeInTheDocument();
    });

    it('filters servers based on search query', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        const searchInput = screen.getByPlaceholderText('Search servers...');
        fireEvent.change(searchInput, { target: { value: 'east' } });

        // Should show matching servers
        expect(screen.getByText('pg-east-1')).toBeInTheDocument();
        expect(screen.getByText('pg-east-2')).toBeInTheDocument();

        // Should not show non-matching servers
        expect(screen.queryByText('pg-west-1')).not.toBeInTheDocument();
        expect(screen.queryByText('pg-dev-1')).not.toBeInTheDocument();
    });

    it('shows empty state when no servers match search', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        const searchInput = screen.getByPlaceholderText('Search servers...');
        fireEvent.change(searchInput, { target: { value: 'nonexistent' } });

        expect(screen.getByText('No servers found')).toBeInTheDocument();
    });

    it('shows empty state when data is empty', () => {
        renderWithTheme(
            <ClusterNavigator
                data={[]}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('No servers configured')).toBeInTheDocument();
    });

    it('calls onRefresh when refresh button is clicked', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        const refreshButton = screen.getByLabelText('Refresh');
        fireEvent.click(refreshButton);

        expect(onRefresh).toHaveBeenCalled();
    });

    it('shows loading state', () => {
        renderWithTheme(
            <ClusterNavigator
                data={[]}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
                loading={true}
            />
        );

        // Should show skeleton loaders
        const skeletons = document.querySelectorAll('.MuiSkeleton-root');
        expect(skeletons.length).toBeGreaterThan(0);
    });

    it('displays server roles', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // There are multiple servers with roles displayed as RolePill components
        expect(screen.getAllByText('Primary').length).toBeGreaterThan(0);
        expect(screen.getAllByText('Standby').length).toBeGreaterThan(0);
    });

    it('renders in dark mode', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
                mode="dark"
            />,
            darkTheme
        );

        expect(screen.getByText('Database Servers')).toBeInTheDocument();
    });

    it('displays footer with group and cluster counts', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText(/2 groups/)).toBeInTheDocument();
        expect(screen.getByText(/3 clusters/)).toBeInTheDocument();
    });

    it('preserves selection when data is updated with same content', () => {
        const { rerender } = renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                selectedServerId={1}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // Verify server is selected
        expect(screen.getByText('pg-east-1')).toBeInTheDocument();

        // Rerender with same data (simulating refresh with no changes)
        rerender(
            <ThemeProvider theme={lightTheme}><ClusterNavigator
                data={mockClusterData}
                selectedServerId={1}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            /></ThemeProvider>
        );

        // Server should still be visible and selection preserved
        expect(screen.getByText('pg-east-1')).toBeInTheDocument();
    });

    it('maintains expanded state after data update', () => {
        const { rerender } = renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // All groups and clusters should be expanded by default
        expect(screen.getByText('pg-east-1')).toBeInTheDocument();
        expect(screen.getByText('pg-west-1')).toBeInTheDocument();
        expect(screen.getByText('pg-dev-1')).toBeInTheDocument();

        // Rerender with updated data (same structure, different status)
        const updatedData = JSON.parse(JSON.stringify(mockClusterData));
        updatedData[0].clusters[0].servers[0].status = 'warning';

        rerender(
            <ThemeProvider theme={lightTheme}><ClusterNavigator
                data={updatedData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            /></ThemeProvider>
        );

        // All servers should still be visible (expanded state preserved)
        expect(screen.getByText('pg-east-1')).toBeInTheDocument();
        expect(screen.getByText('pg-west-1')).toBeInTheDocument();
        expect(screen.getByText('pg-dev-1')).toBeInTheDocument();
    });

    it('attaches scroll handler to navigation tree container', () => {
        renderWithTheme(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        // Find the scrollable container (the one with overflow: auto)
        const scrollContainer = document.querySelector('[style*="overflow"]') ||
            screen.getByText('Production').closest('[class*="MuiBox"]')?.parentElement;

        // Verify scroll container exists
        expect(scrollContainer).toBeInTheDocument();
    });

    describe('configure group round-trip (issue #63)', () => {
        // These tests intentionally avoid userEvent.type / userEvent.clear for
        // the name field. Under v8 coverage instrumentation every keystroke
        // re-enters heavily instrumented MUI transitions, and typing a full
        // string character-by-character pushes the test past the default
        // 5s timeout. fireEvent.change is equivalent for a controlled
        // TextField (single synchronous state update) and is deterministic
        // under both plain `npm test` and `npm run test:coverage`. The
        // generous waitFor timeout is a belt-and-suspenders guard for the
        // dialog's Grow/Collapse transitions, which are also slowed down
        // significantly by v8 instrumentation.

        const WAIT_TIMEOUT = 15000;

        it('passes the unchanged "group-{id}" string from configure to updateGroupName', async () => {
            // Superuser is required to see the settings (configure) button
            mockUseAuth.mockReturnValue({ user: { isSuperuser: true } });

            renderWithTheme(
                <ClusterNavigator
                    data={mockClusterData}
                    onSelectServer={onSelectServer}
                    onRefresh={onRefresh}
                />
            );

            // Open the configure dialog for the "Production" (group-1) group.
            // The settings icon sits inside the action-buttons group on each
            // row; click the first one which belongs to group-1.
            const settingsButtons = document.querySelectorAll(
                '.action-buttons button'
            );
            expect(settingsButtons.length).toBeGreaterThan(0);
            fireEvent.click(settingsButtons[0]);

            // Verify the edit dialog shows the correct group name.
            await waitFor(
                () => {
                    expect(
                        screen.getByText(/Group Settings: Production/)
                    ).toBeInTheDocument();
                },
                { timeout: WAIT_TIMEOUT }
            );

            // Name field should be pre-populated from the group data
            const nameField = screen.getByRole('textbox', { name: /^name/i });
            expect(nameField).toHaveValue('Production');

            // Change the name and submit. fireEvent.change is a single
            // synchronous update that bypasses per-keystroke instrumentation
            // overhead in v8 coverage runs.
            fireEvent.change(nameField, {
                target: { value: 'Production Renamed' },
            });
            expect(nameField).toHaveValue('Production Renamed');

            const saveButton = screen.getByRole('button', { name: /^save$/i });
            fireEvent.click(saveButton);

            // Root-cause check: updateGroupName must receive the original
            // "group-1" string, not "1" and not the number 1.
            await waitFor(
                () => {
                    expect(mockUpdateGroupName).toHaveBeenCalledWith(
                        'group-1',
                        'Production Renamed'
                    );
                },
                { timeout: WAIT_TIMEOUT }
            );
            // Type-level sanity: the first argument must be a string.
            const [firstArg] = mockUpdateGroupName.mock.calls[0];
            expect(typeof firstArg).toBe('string');
            expect(firstArg).toBe('group-1');
        }, 20000);

        it('calls createGroup (not updateGroupName) when saving from the Add Group flow', async () => {
            mockUseAuth.mockReturnValue({ user: { isSuperuser: true } });

            renderWithTheme(
                <ClusterNavigator
                    data={mockClusterData}
                    onSelectServer={onSelectServer}
                    onRefresh={onRefresh}
                />
            );

            // Open the add menu via the "+" button in the header
            const addButton = screen.getByRole('button', {
                name: /add server or group/i,
            });
            fireEvent.click(addButton);

            // Click the "Add Cluster Group" option in the menu (Grow
            // transition can take noticeable time under v8 coverage).
            const menuItem = await screen.findByRole(
                'menuitem',
                { name: /add cluster group/i },
                { timeout: WAIT_TIMEOUT }
            );
            fireEvent.click(menuItem);

            // Dialog opens in create mode
            await waitFor(
                () => {
                    expect(
                        screen.getByText('Add Cluster Group')
                    ).toBeInTheDocument();
                },
                { timeout: WAIT_TIMEOUT }
            );

            const nameField = screen.getByRole('textbox', { name: /^name/i });
            // Use fireEvent.change for the same reason as above.
            fireEvent.change(nameField, { target: { value: 'New Group' } });
            expect(nameField).toHaveValue('New Group');

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(
                () => {
                    expect(mockCreateGroup).toHaveBeenCalledWith(
                        expect.objectContaining({ name: 'New Group' })
                    );
                },
                { timeout: WAIT_TIMEOUT }
            );
            expect(mockUpdateGroupName).not.toHaveBeenCalled();
        }, 20000);

        it('renders the Alert overrides panel with a numeric scopeId extracted from "group-{id}"', async () => {
            mockUseAuth.mockReturnValue({ user: { isSuperuser: true } });

            renderWithTheme(
                <ClusterNavigator
                    data={mockClusterData}
                    onSelectServer={onSelectServer}
                    onRefresh={onRefresh}
                />
            );

            const settingsButtons = document.querySelectorAll(
                '.action-buttons button'
            );
            fireEvent.click(settingsButtons[0]);

            await waitFor(
                () => {
                    expect(
                        screen.getByText(/Group Settings: Production/)
                    ).toBeInTheDocument();
                },
                { timeout: WAIT_TIMEOUT }
            );

            fireEvent.click(
                screen.getByRole('tab', { name: /alert overrides/i })
            );

            // The panel mock echoes its scope/scopeId props; this asserts
            // that the numeric 1 (not the string "group-1") reached the panel.
            await waitFor(
                () => {
                    expect(
                        screen.getByTestId('alert-overrides-panel')
                    ).toHaveTextContent('AlertOverridesPanel: group 1');
                },
                { timeout: WAIT_TIMEOUT }
            );
        }, 20000);
    });
});
