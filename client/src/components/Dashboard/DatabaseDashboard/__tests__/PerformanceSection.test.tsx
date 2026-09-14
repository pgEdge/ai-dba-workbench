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
import type { MetricQueryParams, MetricSeries, SparklinePoint } from '../../types';
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

/*
 * KpiTile is mocked so the value, unit and sparkline points reach the
 * DOM verbatim; the cache tests care that an idle bucket arrives as
 * null, not as 0.
 */
vi.mock('../../KpiTile', () => ({
    default: ({ label, value, unit, sparklineData }: {
        label: string;
        value: string | number;
        unit?: string;
        sparklineData?: SparklinePoint[];
    }) => (
        <div
            data-testid="kpi"
            data-label={label}
            data-value={String(value)}
            data-unit={unit ?? ''}
            data-sparkline={JSON.stringify(
                (sparklineData ?? []).map(p => p.value)
            )}
        >
            {label}
        </div>
    ),
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
                    data-values={JSON.stringify(s.data)}
                />
            ))}
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const TXN_TITLE = 'Transactions Over Time';
const CACHE_TITLE = 'Cache Hit Ratio Over Time';

const TXN_KEY = 'xact_commit_per_sec,xact_rollback_per_sec';
const CACHE_KEY = 'blks_hit_per_sec,blks_read_per_sec';
const DEAD_TUPLE_KEY = 'n_dead_tup,n_live_tup';

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
});

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
            case CACHE_KEY:
                return ready([
                    series('blks_hit_per_sec', [90, 95]),
                    series('blks_read_per_sec', [10, 5]),
                ]);
            case TXN_KEY:
                return ready([
                    series('xact_commit_per_sec', [12, 25.5]),
                    series('xact_rollback_per_sec', [1, 2]),
                ]);
            case DEAD_TUPLE_KEY:
                return ready([
                    series('n_dead_tup', [5, 10]),
                    series('n_live_tup', [95, 90]),
                ]);
            default:
                return ready([]);
        }
    });
};

/** Find the mocked KPI tile with the given label. */
const kpi = (label: string): HTMLElement =>
    screen.getAllByTestId('kpi')
        .find(el => el.getAttribute('data-label') === label) as HTMLElement;

/** Find the rendered series names for a given chart title. */
const seriesNamesFor = (title: string): string[] =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-chart') === title)
        .map(el => el.getAttribute('data-series') ?? '');

/** Find the rendered series values for a chart/series pair. */
const chartValues = (title: string, name: string): string =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-chart') === title
            && el.getAttribute('data-series') === name)
        .map(el => el.getAttribute('data-values') ?? '')[0];

const renderSection = () => render(
    <PerformanceSection connectionId={7} databaseName="appdb" />,
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
            expect(aggregations.get(CACHE_KEY)).toBe('last');
        });

        it('requests the per-second block metrics for the cache KPI and chart', async () => {
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });

            const cacheCalls = mockUseMetrics.mock.calls
                .map(([params]) => params)
                .filter(p => (p?.metrics ?? []).join(',') === CACHE_KEY);
            const buckets = new Set(cacheCalls.map(p => p?.buckets));
            expect(buckets).toEqual(new Set([30, 150]));
            cacheCalls.forEach(p => {
                expect(p?.probeName).toBe('pg_stat_database');
                expect(p?.databaseName).toBe('appdb');
                expect(p?.aggregation).toBe('last');
            });
            // The raw counters are no longer requested anywhere.
            expect(mockUseMetrics.mock.calls.some(([p]) =>
                (p?.metrics ?? []).includes('blks_hit'))).toBe(false);
        });
    });

    describe('cache hit ratio', () => {
        it('derives the cache hit ratio per bucket from the rates', async () => {
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });
            const tile = kpi('Cache Hit Ratio');
            expect(tile.getAttribute('data-value')).toBe('95.0');
            expect(tile.getAttribute('data-unit')).toBe('%');
            expect(tile.getAttribute('data-sparkline')).toBe('[90,95]');
            expect(chartValues(CACHE_TITLE, 'Cache Hit Ratio %'))
                .toBe('[90,95]');
        });

        it('turns an idle bucket into a gap and headlines the latest ratio', async () => {
            routeMetrics({
                [CACHE_KEY]: ready([
                    series('blks_hit_per_sec', [90, 0, 50]),
                    series('blks_read_per_sec', [10, 0, 50]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });
            expect(kpi('Cache Hit Ratio').getAttribute('data-sparkline'))
                .toBe('[90,null,50]');
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('50.0');
            expect(chartValues(CACHE_TITLE, 'Cache Hit Ratio %'))
                .toBe('[90,null,50]');
        });

        it('uses the last bucket with a ratio when the newest bucket is idle', async () => {
            routeMetrics({
                [CACHE_KEY]: ready([
                    series('blks_hit_per_sec', [80, 0]),
                    series('blks_read_per_sec', [20, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('80.0');
        });

        it('reports a genuine 0% rather than skipping it', async () => {
            routeMetrics({
                [CACHE_KEY]: ready([
                    series('blks_hit_per_sec', [90, 0]),
                    series('blks_read_per_sec', [10, 40]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('0.0');
        });

        it('shows the placeholder when every bucket is idle', async () => {
            routeMetrics({
                [CACHE_KEY]: ready([
                    series('blks_hit_per_sec', [0, 0]),
                    series('blks_read_per_sec', [0, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
            });
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('--');
            // No unit beside the placeholder, matching the server dashboard.
            expect(kpi('Cache Hit Ratio').getAttribute('data-unit')).toBe('');
            expect(kpi('Cache Hit Ratio').getAttribute('data-sparkline'))
                .toBe('[null,null]');
        });

        it('shows the empty chart message when the cache series is missing', async () => {
            routeMetrics({ [CACHE_KEY]: ready([]) });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No cache hit ratio data available'))
                    .toBeInTheDocument();
            });
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('--');
        });

        it('renders an empty cache chart when one series is missing', async () => {
            routeMetrics({
                [CACHE_KEY]: ready([series('blks_hit_per_sec', [90, 95])]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No cache hit ratio data available'))
                    .toBeInTheDocument();
            });
        });
    });

    describe('charts', () => {
        it('plots commits and rollbacks per second', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor(TXN_TITLE))
                    .toEqual(['Commits/s', 'Rollbacks/s']);
            });
            expect(chartValues(TXN_TITLE, 'Commits/s')).toBe('[12,25.5]');
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
    });

    describe('KPI tiles', () => {
        it('reports the transaction rate per second', async () => {
            renderSection();

            await waitFor(() => {
                expect(kpi('Transactions')).toBeInTheDocument();
            });
            // 25.5 commits plus 2 rollbacks per second.
            expect(kpi('Transactions').getAttribute('data-value'))
                .toBe('27.5');
            expect(kpi('Transactions').getAttribute('data-unit')).toBe('/s');
        });

        it('reports an idle transaction rate as zero', async () => {
            routeMetrics({
                [TXN_KEY]: ready([
                    series('xact_commit_per_sec', [12, 0]),
                    series('xact_rollback_per_sec', [1, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Transactions')).toBeInTheDocument();
            });
            expect(kpi('Transactions').getAttribute('data-value'))
                .toBe('0.0');
        });

        it('sums commits and rollbacks in the transaction sparkline', async () => {
            renderSection();

            await waitFor(() => {
                expect(kpi('Transactions')).toBeInTheDocument();
            });
            // 12 + 1 and 25.5 + 2, matching the 27.5/s on the tile.
            expect(kpi('Transactions').getAttribute('data-sparkline'))
                .toBe('[13,27.5]');
        });

        it('reports the database size and dead tuple tiles', async () => {
            renderSection();

            await waitFor(() => {
                expect(kpi('Database Size')).toBeInTheDocument();
            });
            expect(kpi('Database Size').getAttribute('data-value'))
                .toBe('2.0 MB');
            // 10 dead against 90 live tuples is a 10.0% ratio.
            expect(kpi('Dead Tuple Ratio').getAttribute('data-value'))
                .toBe('10.0');
            expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
                .toBe('[5,10]');
        });

        it('renders placeholders when the KPI queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(kpi('Transactions')).toBeInTheDocument();
            });
            expect(kpi('Transactions').getAttribute('data-value')).toBe('--');
            expect(kpi('Transactions').getAttribute('data-unit')).toBe('');
            expect(kpi('Dead Tuple Ratio').getAttribute('data-value'))
                .toBe('--');
            expect(kpi('Cache Hit Ratio').getAttribute('data-value'))
                .toBe('--');
        });

        it('shows the section spinner during the initial KPI load', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getByLabelText('Loading')).toBeInTheDocument();
        });

        it('handles dead tuple series with all zero and mismatched lengths', async () => {
            routeMetrics({
                [DEAD_TUPLE_KEY]: ready([
                    series('n_dead_tup', [0, 0, 0]),
                    series('n_live_tup', [0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Dead Tuple Ratio')).toBeInTheDocument();
            });
            expect(kpi('Dead Tuple Ratio').getAttribute('data-value'))
                .toBe('0.0');
            expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
                .toBe('[0]');
        });

        it('returns an empty dead tuple sparkline when a series is missing', async () => {
            routeMetrics({
                [DEAD_TUPLE_KEY]: ready([series('n_dead_tup', [3])]),
            });
            renderSection();

            await waitFor(() => {
                expect(kpi('Dead Tuple Ratio')).toBeInTheDocument();
            });
            expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
                .toBe('[]');
        });
    });
});
