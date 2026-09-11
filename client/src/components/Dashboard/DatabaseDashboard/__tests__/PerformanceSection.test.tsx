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
    useAICapabilities: () => ({ aiEnabled: false }),
}));

/*
 * KpiTile is mocked so the sparkline points reach the DOM verbatim; the
 * tests care that an idle bucket arrives as null, not as 0.
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

const CACHE_KEY = 'blks_hit_per_sec,blks_read_per_sec';
const CACHE_CHART_TITLE = 'Cache Hit Ratio Over Time';

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2024-01-01T00:0${idx}:00Z`,
        value,
    })),
});

const ready = (data: MetricSeries[] | null): UseMetricsReturn => ({
    data,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

const loading = (): UseMetricsReturn => ({
    data: null,
    loading: true,
    error: null,
    refetch: vi.fn(),
});

/** Route each useMetrics call by the metrics it requests. */
const routeMetrics = (
    overrides: Partial<Record<string, UseMetricsReturn>> = {},
): void => {
    mockUseMetrics.mockImplementation((params) => {
        const key = (params?.metrics ?? []).join(',');
        const override = overrides[key];
        if (override) { return override; }

        switch (key) {
            case 'database_size_bytes':
                return ready([series('database_size_bytes', [1024, 2048])]);
            case CACHE_KEY:
                return ready([
                    series('blks_hit_per_sec', [90, 95]),
                    series('blks_read_per_sec', [10, 5]),
                ]);
            case 'xact_commit,xact_rollback':
                return ready([
                    series('xact_commit', [100, 200]),
                    series('xact_rollback', [1, 2]),
                ]);
            case 'n_dead_tup,n_live_tup':
                return ready([
                    series('n_dead_tup', [5, 10]),
                    series('n_live_tup', [95, 90]),
                ]);
            default:
                return ready([]);
        }
    });
};

const kpi = (label: string): HTMLElement =>
    screen.getAllByTestId('kpi')
        .find(el => el.getAttribute('data-label') === label) as HTMLElement;

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
        routeMetrics();
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

    it('derives the cache hit ratio per bucket from the rates', async () => {
        renderSection();

        await waitFor(() => {
            expect(kpi('Cache Hit Ratio')).toBeInTheDocument();
        });
        const tile = kpi('Cache Hit Ratio');
        expect(tile.getAttribute('data-value')).toBe('95.0');
        expect(tile.getAttribute('data-unit')).toBe('%');
        expect(tile.getAttribute('data-sparkline')).toBe('[90,95]');
        expect(chartValues(CACHE_CHART_TITLE, 'Cache Hit Ratio %'))
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
        expect(kpi('Cache Hit Ratio').getAttribute('data-value')).toBe('50.0');
        expect(chartValues(CACHE_CHART_TITLE, 'Cache Hit Ratio %'))
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
        expect(kpi('Cache Hit Ratio').getAttribute('data-value')).toBe('80.0');
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
        expect(kpi('Cache Hit Ratio').getAttribute('data-value')).toBe('0.0');
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
        expect(kpi('Cache Hit Ratio').getAttribute('data-value')).toBe('--');
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
        expect(kpi('Cache Hit Ratio').getAttribute('data-value')).toBe('--');
    });

    it('shows chart loading indicators while the queries are in flight', async () => {
        routeMetrics({
            [CACHE_KEY]: loading(),
            'xact_commit,xact_rollback': loading(),
        });
        renderSection();

        await waitFor(() => {
            expect(screen.getAllByLabelText('Loading chart').length).toBe(2);
        });
    });

    it('shows the section spinner during the initial KPI load', () => {
        mockUseMetrics.mockImplementation(() => loading());
        renderSection();

        expect(screen.getByLabelText('Loading')).toBeInTheDocument();
    });

    it('renders the other KPIs and the transaction chart', async () => {
        renderSection();

        await waitFor(() => {
            expect(kpi('Database Size')).toBeInTheDocument();
        });
        expect(kpi('Database Size').getAttribute('data-value')).toBe('2.0 KB');
        expect(kpi('Transactions').getAttribute('data-value')).toBe('202');
        // 10 dead against 90 live tuples is a 10.0% ratio.
        expect(kpi('Dead Tuple Ratio').getAttribute('data-value')).toBe('10.0');
        expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
            .toBe('[5,10]');
        expect(chartValues('Transactions Over Time', 'Commits'))
            .toBe('[100,200]');
    });

    it('renders placeholders when the other KPI queries return nothing', async () => {
        routeMetrics({
            'database_size_bytes': ready([]),
            'xact_commit,xact_rollback': ready([]),
            'n_dead_tup,n_live_tup': ready([]),
        });
        renderSection();

        await waitFor(() => {
            expect(kpi('Database Size')).toBeInTheDocument();
        });
        expect(kpi('Transactions').getAttribute('data-value')).toBe('--');
        expect(kpi('Dead Tuple Ratio').getAttribute('data-value')).toBe('--');
        expect(screen.getByText('No transaction data available'))
            .toBeInTheDocument();
    });

    it('handles dead tuple series with all zero and mismatched lengths', async () => {
        routeMetrics({
            'n_dead_tup,n_live_tup': ready([
                series('n_dead_tup', [0, 0, 0]),
                series('n_live_tup', [0]),
            ]),
        });
        renderSection();

        await waitFor(() => {
            expect(kpi('Dead Tuple Ratio')).toBeInTheDocument();
        });
        expect(kpi('Dead Tuple Ratio').getAttribute('data-value')).toBe('0.0');
        expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
            .toBe('[0]');
    });

    it('returns an empty dead tuple sparkline when a series is missing', async () => {
        routeMetrics({
            'n_dead_tup,n_live_tup': ready([series('n_dead_tup', [3])]),
        });
        renderSection();

        await waitFor(() => {
            expect(kpi('Dead Tuple Ratio')).toBeInTheDocument();
        });
        expect(kpi('Dead Tuple Ratio').getAttribute('data-sparkline'))
            .toBe('[]');
    });
});
