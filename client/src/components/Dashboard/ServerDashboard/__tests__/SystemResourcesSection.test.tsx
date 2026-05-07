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
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import SystemResourcesSection from '../SystemResourcesSection';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricSeries } from '../../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockUseMetrics = vi.fn<[], UseMetricsReturn>();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: () => mockUseMetrics(),
}));

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: '1h' },
        refreshTrigger: 0,
    }),
}));

vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({
        capabilities: null,
        isLoading: false,
        error: null,
        refetch: vi.fn(),
        isAIEnabled: false,
    }),
}));

// Mock the Chart component to avoid ApexCharts dependencies
vi.mock('../../../Chart', () => ({
    Chart: ({ title }: { title: string }) => (
        <div data-testid="chart">{title}</div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mock UseMetricsReturn with loading state */
const loadingMetrics = (): UseMetricsReturn => ({
    data: null,
    loading: true,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with empty data (no system_stats) */
const emptyMetrics = (): UseMetricsReturn => ({
    data: [],
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with data that has all zeros (no system_stats) */
const zeroMetrics = (): UseMetricsReturn => ({
    data: [
        {
            name: 'idle_mode_percent',
            metric: 'idle_mode_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 0 },
                { time: '2024-01-01T00:01:00Z', value: 0 },
            ],
        },
    ] as MetricSeries[],
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with real system stats data */
const realMetrics = (): UseMetricsReturn => ({
    data: [
        {
            name: 'idle_mode_percent',
            metric: 'idle_mode_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 85.5 },
                { time: '2024-01-01T00:01:00Z', value: 90.2 },
            ],
        },
        {
            name: 'usermode_normal_process_percent',
            metric: 'usermode_normal_process_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 10.2 },
                { time: '2024-01-01T00:01:00Z', value: 8.5 },
            ],
        },
    ] as MetricSeries[],
    loading: false,
    error: null,
    refetch: vi.fn(),
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SystemResourcesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        vi.mocked(localStorage.setItem).mockClear();
    });

    describe('force collapse behavior', () => {
        it('shows expanded section with loading spinner during initial load', () => {
            mockUseMetrics.mockReturnValue(loadingMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Section should be visible with title
            expect(screen.getByText('System Resources')).toBeInTheDocument();

            // Should show loading indicator
            expect(screen.getByLabelText('Loading')).toBeInTheDocument();

            // Anti-flicker contract: while loading the section MUST stay
            // expanded and MUST NOT display the force-collapsed message.
            // This guards against forceCollapsed being applied before
            // the initial fetch resolves.
            const header = screen.getByRole('button', {
                name: /collapse system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'true');
            expect(
                screen.queryByText(/No data available/),
            ).not.toBeInTheDocument();
        });

        it('forces collapse when system_stats extension is not installed (empty data)', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for render to stabilize
            await waitFor(() => {
                // Section title should still be visible
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should show the force collapsed message
            expect(
                screen.getByText(/No data available.*system_stats/i),
            ).toBeInTheDocument();

            // Header should indicate collapsed state
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // Charts should NOT be rendered (collapsed)
            expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
        });

        it('forces collapse when all metric values are zero', async () => {
            mockUseMetrics.mockReturnValue(zeroMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should show the force collapsed message
            expect(
                screen.getByText(/No data available.*system_stats/i),
            ).toBeInTheDocument();

            // Header should indicate collapsed state
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'false');
        });

        it('stays expanded when system_stats data is available', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should NOT show the force collapsed message
            expect(
                screen.queryByText(/No data available.*system_stats/i),
            ).not.toBeInTheDocument();

            // Header should indicate expanded state
            const header = screen.getByRole('button', {
                name: /collapse system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // KPI tiles should be visible
            expect(screen.getByText('CPU Usage')).toBeInTheDocument();
            expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            expect(screen.getByText('Disk Usage')).toBeInTheDocument();
            expect(screen.getByText('Load Average')).toBeInTheDocument();
        });

        it('does not modify localStorage when force collapsed', async () => {
            // Start with stored expanded state
            vi.mocked(localStorage.getItem).mockReturnValue('true');
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // localStorage should NOT have been modified
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('allows user to manually expand a force-collapsed section', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for force-collapse to apply after initial load
            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // Section starts collapsed, KPI tiles hidden
            expect(screen.queryByText('CPU Usage')).not.toBeInTheDocument();

            // User clicks to expand
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            fireEvent.click(header);

            // After expand: aria-expanded flips, KPI tiles render, and
            // the force-collapsed message disappears from the header.
            expect(header).toHaveAttribute('aria-expanded', 'true');
            expect(screen.getByText('CPU Usage')).toBeInTheDocument();
            expect(
                screen.queryByText(/No data available.*system_stats/i),
            ).not.toBeInTheDocument();

            // localStorage MUST NOT be touched - the override is in-memory.
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });
    });

    describe('section title and icon', () => {
        it('renders section with Computer icon', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // MUI icons render as SVGs with data-testid
            expect(screen.getByTestId('ComputerIcon')).toBeInTheDocument();
        });
    });

    describe('KPI tiles', () => {
        it('renders the exact force-collapsed message wired by the section', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Must use the precise message the production component
            // passes to CollapsibleSection. A regression that swaps
            // the wording would make this assertion fail.
            await waitFor(() => {
                expect(
                    screen.getByText(
                        'No data available. Is the system_stats extension installed?',
                    ),
                ).toBeInTheDocument();
            });
        });

        it('shows placeholder dashes for KPIs when forced to expand on empty data', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for initial load + force-collapse to settle.
            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // Manually expand to surface the KPI tiles.
            fireEvent.click(
                screen.getByRole('button', {
                    name: /expand system resources section/i,
                }),
            );

            // With no system_stats data, Memory/Disk/Load all render '--'.
            expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            expect(screen.getByText('Disk Usage')).toBeInTheDocument();
            expect(screen.getByText('Load Average')).toBeInTheDocument();
            // Three placeholders: Memory, Disk, Load all show '--'.
            expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(3);
        });
    });
});
