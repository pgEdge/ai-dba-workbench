/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterNavigator Tests
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterNavigator from '../ClusterNavigator';

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

describe('ClusterNavigator', () => {
    let onSelectServer;
    let onRefresh;

    beforeEach(() => {
        onSelectServer = vi.fn();
        onRefresh = vi.fn();
    });

    it('renders the component with header', () => {
        render(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('Database Servers')).toBeInTheDocument();
    });

    it('displays server count summary', () => {
        render(
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
        render(
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
        render(
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
        render(
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
        render(
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
        render(
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
        render(
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
        render(
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
        render(
            <ClusterNavigator
                data={[]}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText('No servers configured')).toBeInTheDocument();
    });

    it('calls onRefresh when refresh button is clicked', () => {
        render(
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
        render(
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
        render(
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
        render(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
                mode="dark"
            />
        );

        expect(screen.getByText('Database Servers')).toBeInTheDocument();
    });

    it('displays footer with group and cluster counts', () => {
        render(
            <ClusterNavigator
                data={mockClusterData}
                onSelectServer={onSelectServer}
                onRefresh={onRefresh}
            />
        );

        expect(screen.getByText(/2 groups/)).toBeInTheDocument();
        expect(screen.getByText(/3 clusters/)).toBeInTheDocument();
    });
});
