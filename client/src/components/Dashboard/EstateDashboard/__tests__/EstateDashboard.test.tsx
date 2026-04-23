/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import EstateDashboard from '../index';
import type { EstateSelection } from '../../../../types/selection';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../HealthOverviewSection', () => ({
    default: () => <div data-testid="health-overview-section">Health Overview Content</div>,
}));

vi.mock('../KpiTilesSection', () => ({
    default: ({ serverIds }: { serverIds: number[] }) => (
        <div data-testid="kpi-tiles-section" data-server-ids={serverIds.join(',')}>
            KPI Tiles Content
        </div>
    ),
}));

vi.mock('../ClusterCardsSection', () => ({
    default: () => <div data-testid="cluster-cards-section">Cluster Cards Content</div>,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const createSelection = (serverIds: number[] = [1, 2, 3]): EstateSelection => ({
    type: 'estate',
    name: 'Estate',
    status: 'online',
    groups: [
        {
            id: 'group-1',
            name: 'Production',
            clusters: [
                {
                    id: 'cluster-1',
                    name: 'Cluster 1',
                    servers: serverIds.map(id => ({ id, name: `Server ${id}` })),
                },
            ],
        },
    ],
});

const renderEstateDashboard = (selection: EstateSelection = createSelection()) => {
    return render(
        <ThemeProvider theme={theme}>
            <EstateDashboard selection={selection} />
        </ThemeProvider>,
    );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('EstateDashboard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders Health Overview section', () => {
        renderEstateDashboard();

        expect(screen.getByText('Health Overview')).toBeInTheDocument();
        expect(screen.getByTestId('health-overview-section')).toBeInTheDocument();
    });

    it('renders Key Performance Indicators section', () => {
        renderEstateDashboard();

        expect(screen.getByText('Key Performance Indicators')).toBeInTheDocument();
        expect(screen.getByTestId('kpi-tiles-section')).toBeInTheDocument();
    });

    it('renders Clusters section', () => {
        renderEstateDashboard();

        expect(screen.getByText('Clusters')).toBeInTheDocument();
        expect(screen.getByTestId('cluster-cards-section')).toBeInTheDocument();
    });

    it('extracts server IDs from selection and passes to KpiTilesSection', () => {
        const selection = createSelection([10, 20, 30]);
        renderEstateDashboard(selection);

        const kpiSection = screen.getByTestId('kpi-tiles-section');
        expect(kpiSection).toHaveAttribute('data-server-ids', '10,20,30');
    });

    it('handles nested server children', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'Estate',
            status: 'online',
            groups: [
                {
                    id: 'group-1',
                    name: 'Group 1',
                    clusters: [
                        {
                            id: 'cluster-1',
                            name: 'Cluster 1',
                            servers: [
                                {
                                    id: 1,
                                    name: 'Primary',
                                    children: [
                                        { id: 2, name: 'Replica 1' },
                                        { id: 3, name: 'Replica 2' },
                                    ],
                                },
                            ],
                        },
                    ],
                },
            ],
        };
        renderEstateDashboard(selection);

        const kpiSection = screen.getByTestId('kpi-tiles-section');
        expect(kpiSection).toHaveAttribute('data-server-ids', '1,2,3');
    });

    it('handles empty selection gracefully', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'Estate',
            status: 'online',
            groups: [],
        };
        renderEstateDashboard(selection);

        const kpiSection = screen.getByTestId('kpi-tiles-section');
        expect(kpiSection).toHaveAttribute('data-server-ids', '');
    });

    it('handles multiple groups and clusters', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'Estate',
            status: 'online',
            groups: [
                {
                    id: 'group-1',
                    name: 'Group 1',
                    clusters: [
                        {
                            id: 'cluster-1',
                            name: 'Cluster 1',
                            servers: [{ id: 1, name: 'S1' }, { id: 2, name: 'S2' }],
                        },
                    ],
                },
                {
                    id: 'group-2',
                    name: 'Group 2',
                    clusters: [
                        {
                            id: 'cluster-2',
                            name: 'Cluster 2',
                            servers: [{ id: 3, name: 'S3' }],
                        },
                        {
                            id: 'cluster-3',
                            name: 'Cluster 3',
                            servers: [{ id: 4, name: 'S4' }, { id: 5, name: 'S5' }],
                        },
                    ],
                },
            ],
        };
        renderEstateDashboard(selection);

        const kpiSection = screen.getByTestId('kpi-tiles-section');
        expect(kpiSection).toHaveAttribute('data-server-ids', '1,2,3,4,5');
    });

    it('renders all sections as expanded by default', () => {
        renderEstateDashboard();

        // All sections should be expanded and visible
        expect(screen.getByTestId('health-overview-section')).toBeVisible();
        expect(screen.getByTestId('kpi-tiles-section')).toBeVisible();
        expect(screen.getByTestId('cluster-cards-section')).toBeVisible();
    });
});
