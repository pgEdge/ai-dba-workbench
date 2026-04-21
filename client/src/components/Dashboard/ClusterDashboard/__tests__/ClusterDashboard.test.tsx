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
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ClusterDashboard from '../index';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../ReplicationLagSection', () => ({
    default: ({ serverIds }: { serverIds: number[] }) => (
        <div data-testid="replication-lag-section" data-server-ids={serverIds.join(',')}>
            Replication Lag Content
        </div>
    ),
}));

vi.mock('../ComparativeChartsSection', () => ({
    default: ({ serverIds }: { serverIds: number[] }) => (
        <div data-testid="comparative-charts-section" data-server-ids={serverIds.join(',')}>
            Comparative Charts Content
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const createSelection = (serverIds: number[] = [1, 2, 3]) => ({
    name: 'Test Cluster',
    servers: serverIds.map(id => ({ id, name: `Server ${id}` })),
});

const renderClusterDashboard = (selection: Record<string, unknown> = createSelection()) => {
    return render(
        <ThemeProvider theme={theme}>
            <ClusterDashboard selection={selection} />
        </ThemeProvider>,
    );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ClusterDashboard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    it('renders Replication Lag section', () => {
        renderClusterDashboard();

        expect(screen.getByText('Replication Lag')).toBeInTheDocument();
        expect(screen.getByTestId('replication-lag-section')).toBeInTheDocument();
    });

    it('renders Comparative Metrics section', () => {
        renderClusterDashboard();

        expect(screen.getByText('Comparative Metrics')).toBeInTheDocument();
        expect(screen.getByTestId('comparative-charts-section')).toBeInTheDocument();
    });

    it('extracts server IDs from selection', () => {
        const selection = createSelection([10, 20, 30]);
        renderClusterDashboard(selection);

        const replicationSection = screen.getByTestId('replication-lag-section');
        expect(replicationSection).toHaveAttribute('data-server-ids', '10,20,30');

        const chartsSection = screen.getByTestId('comparative-charts-section');
        expect(chartsSection).toHaveAttribute('data-server-ids', '10,20,30');
    });

    it('handles nested server children', () => {
        const selection = {
            name: 'Cluster with nested servers',
            servers: [
                {
                    id: 1,
                    name: 'Primary',
                    children: [
                        { id: 2, name: 'Replica 1' },
                        {
                            id: 3,
                            name: 'Replica 2',
                            children: [{ id: 4, name: 'Cascading Replica' }],
                        },
                    ],
                },
            ],
        };
        renderClusterDashboard(selection);

        const replicationSection = screen.getByTestId('replication-lag-section');
        expect(replicationSection).toHaveAttribute('data-server-ids', '1,2,3,4');
    });

    it('handles empty servers array', () => {
        const selection = { name: 'Empty Cluster', servers: [] };
        renderClusterDashboard(selection);

        const replicationSection = screen.getByTestId('replication-lag-section');
        expect(replicationSection).toHaveAttribute('data-server-ids', '');
    });

    it('handles undefined servers', () => {
        const selection = { name: 'Cluster without servers' };
        renderClusterDashboard(selection);

        const replicationSection = screen.getByTestId('replication-lag-section');
        expect(replicationSection).toHaveAttribute('data-server-ids', '');
    });

    it('deduplicates server IDs', () => {
        // Although unlikely, verify that IDs are unique via Set
        const selection = {
            name: 'Cluster',
            servers: [
                { id: 1, name: 'Server 1' },
                { id: 2, name: 'Server 2' },
                { id: 1, name: 'Duplicate' }, // duplicate ID
            ],
        };
        renderClusterDashboard(selection);

        const replicationSection = screen.getByTestId('replication-lag-section');
        // The implementation uses Set, so duplicates should be removed
        expect(replicationSection).toHaveAttribute('data-server-ids', '1,2');
    });

    it('renders all sections as expanded by default', () => {
        renderClusterDashboard();

        expect(screen.getByTestId('replication-lag-section')).toBeVisible();
        expect(screen.getByTestId('comparative-charts-section')).toBeVisible();
    });
});
