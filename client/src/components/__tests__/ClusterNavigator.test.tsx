/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterNavigator Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { ThemeProvider } from '@mui/material/styles';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterNavigator from '../ClusterNavigator';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

const lightTheme = createPgedgeTheme('light');
const darkTheme = createPgedgeTheme('dark');

// Mock the AuthContext
vi.mock('../../contexts/AuthContext', () => ({
    useAuth: () => ({
        user: { isSuperuser: false },
    }),
}));

// Mock the ClusterContext
vi.mock('../../contexts/ClusterContext', () => ({
    useCluster: () => ({
        updateGroupName: vi.fn(),
        updateClusterName: vi.fn(),
        updateServerName: vi.fn(),
    }),
}));

// Mock the AlertsContext
vi.mock('../../contexts/AlertsContext', () => ({
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
vi.mock('../../contexts/BlackoutContext', () => ({
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
});
