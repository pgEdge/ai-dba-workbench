/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import PerformanceSection from '../PerformanceSection';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries } from '../../types';
import type { ChartData } from '../../../Chart/types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type UseMetricsFn = (params: MetricQueryParams | null) => UseMetricsReturn;

const mockUseMetrics = vi.fn<UseMetricsFn>();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => mockUseMetrics(params),
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

vi.mock('../../../Chart', () => ({
    Chart: ({ title, data }: { title: string; data: ChartData }) => (
        <div data-testid="chart" data-title={title}>
            <span>{title}</span>
            {data.series.map((s) => (
                <span
                    key={s.name}
                    data-testid="chart-series"
                    data-chart={title}
                    data-series={s.name}
                    data-values={s.data.join(',')}
                >
                    {s.name}
                </span>
            ))}
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const TXN_TITLE = 'Transactions Over Time';
const CACHE_TITLE = 'Cache Hit Ratio Over Time';

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
}) as MetricSeries;

/** Wrap metric series in a resolved UseMetricsReturn. */
const ready = (data: MetricSeries[] | null): UseMetricsReturn => ({
    data,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** A UseMetricsReturn that is still loading with no data yet. */
const loading = (): UseMetricsReturn => ({
    data: null,
    loading: true,
    error: null,
    refetch: vi.fn(),
});

/** A failed UseMetricsReturn, as an older server would produce. */
const failed = (message: string): UseMetricsReturn => ({
    data: null,
    loading: false,
    error: message,
    refetch: vi.fn(),
});

const TXN_KEY = 'xact_commit_per_sec,xact_rollback_per_sec';

/**
 * Route each useMetrics call by the metrics it requests and the bucket
 * count, since the KPI tiles and the charts share metric names and
 * differ only in resolution.
 */
const routeMetrics = (
    overrides: Partial<Record<string, UseMetricsReturn>> = {},
): void => {
    mockUseMetrics.mockImplementation((params) => {
        const metrics = (params?.metrics ?? []).join(',');
        const key = `${metrics}@${params?.buckets ?? 0}`;
        const override = overrides[key] ?? overrides[metrics];
        if (override) { return override; }

        switch (metrics) {
            case 'database_size_bytes':
                return ready([
                    series('database_size_bytes', [1048576, 2097152]),
                ]);
            case 'blks_hit,blks_read':
                return ready([
                    series('blks_hit', [90, 95]),
                    series('blks_read', [10, 5]),
                ]);
            case TXN_KEY:
                return ready([
                    series('xact_commit_per_sec', [12, 25.5]),
                    series('xact_rollback_per_sec', [1, 2]),
                ]);
            default:
                return ready([
                    series('n_dead_tup', [5, 5]),
                    series('n_live_tup', [95, 95]),
                ]);
        }
    });
};

/** Find the rendered series names for a given chart title. */
const seriesNamesFor = (title: string): string[] =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-chart') === title)
        .map(el => el.getAttribute('data-series') ?? '');

/** Find the rendered series values for a chart/series pair. */
const seriesValuesFor = (title: string, name: string): string =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-chart') === title
            && el.getAttribute('data-series') === name)
        .map(el => el.getAttribute('data-values') ?? '')[0];

const renderSection = () => render(
    <PerformanceSection connectionId={3} databaseName="testdb" />,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PerformanceSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        routeMetrics();
    });

    describe('metric selection', () => {
        it('requests transaction rates rather than raw counters', async () => {
            renderSection();

            await waitFor(() => {
                expect(mockUseMetrics).toHaveBeenCalled();
            });
            const requested = mockUseMetrics.mock.calls
                .map(([params]) => (params?.metrics ?? []).join(','));
            expect(requested).toContain(TXN_KEY);
            expect(requested).not.toContain('xact_commit,xact_rollback');
        });

        it('averages the rate queries and keeps last for the gauges', async () => {
            renderSection();

            await waitFor(() => {
                expect(mockUseMetrics).toHaveBeenCalled();
            });
            const aggregations = new Map<string, string | undefined>();
            for (const [params] of mockUseMetrics.mock.calls) {
                aggregations.set(
                    (params?.metrics ?? []).join(','),
                    params?.aggregation,
                );
            }
            expect(aggregations.get(TXN_KEY)).toBe('avg');
            expect(aggregations.get('database_size_bytes')).toBe('last');
            expect(aggregations.get('blks_hit,blks_read')).toBe('last');
        });
    });

    describe('charts', () => {
        it('plots commits and rollbacks per second', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor(TXN_TITLE))
                    .toEqual(['Commits/s', 'Rollbacks/s']);
            });
            expect(seriesValuesFor(TXN_TITLE, 'Commits/s')).toBe('12,25.5');
        });

        it('plots the computed cache hit ratio', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor(CACHE_TITLE))
                    .toEqual(['Cache Hit Ratio %']);
            });
        });

        it('shows empty messages when the queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No transaction data available'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('No cache hit ratio data available'))
                .toBeInTheDocument();
        });

        it('shows chart loading indicators while queries are in flight', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getAllByLabelText('Loading chart').length)
                .toBe(2);
        });

        it('reports a query error in place of the empty message', async () => {
            routeMetrics({
                [`${TXN_KEY}@150`]: failed('metric not found in probe'),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('metric not found in probe'))
                    .toBeInTheDocument();
            });
            expect(screen.queryByText('No transaction data available'))
                .not.toBeInTheDocument();
        });

        it('renders an empty cache chart when a series is missing', async () => {
            routeMetrics({
                'blks_hit,blks_read': ready([series('blks_hit', [90, 95])]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No cache hit ratio data available'))
                    .toBeInTheDocument();
            });
        });
    });

    describe('KPI tiles', () => {
        it('reports the transaction rate per second', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Transactions')).toBeInTheDocument();
            });
            // 25.5 commits plus 2 rollbacks per second.
            expect(screen.getByText('27.5')).toBeInTheDocument();
            expect(screen.getByText('/s')).toBeInTheDocument();
        });

        it('reports the database size, cache and dead tuple tiles', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Database Size')).toBeInTheDocument();
            });
            expect(screen.getByText('2.0 MB')).toBeInTheDocument();
            expect(screen.getByText('95.0')).toBeInTheDocument();
            expect(screen.getByText('5.0')).toBeInTheDocument();
        });

        it('renders placeholders when the KPI queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Transactions')).toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length)
                .toBeGreaterThanOrEqual(2);
        });

        it('shows the section spinner during the initial KPI load', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getByLabelText('Loading')).toBeInTheDocument();
        });

        it('treats a window with no blocks read as a full cache hit', async () => {
            routeMetrics({
                'blks_hit,blks_read': ready([
                    series('blks_hit', [0, 0]),
                    series('blks_read', [0, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Cache Hit Ratio'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('100.0')).toBeInTheDocument();
        });
    });
});
