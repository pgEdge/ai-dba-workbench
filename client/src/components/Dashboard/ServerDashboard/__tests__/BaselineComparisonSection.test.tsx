/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { fireEvent, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderWithTheme } from '../../../../test/renderWithTheme';
import BaselineComparisonSection from '../BaselineComparisonSection';
import type {
    AnomalyComparisonQuery,
    UseAnomalyComparisonReturn,
    UseComparisonMetricsReturn,
} from '../../../../hooks/useAnomalyComparison';
import type { UseDatabaseSummariesResult } from '../../../../hooks/useDatabaseSummaries';
import type { AnomalyComparisonResponse, ComparisonPoint } from '../baselineComparison';
import { ENGINE_ANOMALY_LABEL, OUTSIDE_LABEL } from '../baselineComparison';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

let metricsResult: UseComparisonMetricsReturn;
let comparisonResult: UseAnomalyComparisonReturn;
let databasesResult: UseDatabaseSummariesResult;
const queries: (AnomalyComparisonQuery | null)[] = [];
const databaseCalls: boolean[] = [];

vi.mock('../../../../hooks/useAnomalyComparison', () => ({
    useComparisonMetrics: () => metricsResult,
    useAnomalyComparison: (query: AnomalyComparisonQuery | null) => {
        queries.push(query);
        return comparisonResult;
    },
}));

vi.mock('../../../../hooks/useDatabaseSummaries', () => ({
    useDatabaseSummaries: (_id: number, _key: number, _range: unknown, enabled: boolean) => {
        databaseCalls.push(enabled);
        return databasesResult;
    },
}));

interface ChartProps {
    title: string;
    data: { categories: string[]; series: { name: string }[] };
    echartsOptions: {
        legend: { data: (string | { name: string })[] };
        series: { name: string }[];
    };
}

vi.mock('../../../Chart', () => ({
    Chart: ({ title, data, echartsOptions }: ChartProps) => (
        <div
            data-testid="chart"
            data-categories={data.categories.length}
            data-legend={echartsOptions.legend.data
                .map((item) => (typeof item === 'string' ? item : item.name))
                .join('|')}
        >
            {title}
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const CPU = 'pg_sys_cpu_usage_info.processor_time_percent';
const CACHE = 'pg_stat_database.cache_hit_ratio';

const METRICS = [
    { name: CACHE, probe_name: 'pg_stat_database', per_database: true },
    { name: CPU, probe_name: 'pg_sys_cpu_usage_info', per_database: false },
];

const point = (time: string, overrides: Partial<ComparisonPoint> = {}): ComparisonPoint => ({
    time,
    value: 40,
    period_type: 'hourly',
    mean: 30,
    stddev: 4,
    cold: false,
    z_score: 2.5,
    outside: false,
    ...overrides,
});

const response = (
    overrides: Partial<AnomalyComparisonResponse> = {}
): AnomalyComparisonResponse => ({
    metric: CPU,
    connection_id: 1,
    database_name: null,
    model: 'as_scored',
    sensitivity: 3,
    default_sensitivity: 3,
    scoring_source: 'alerter',
    baselines_as_of: '2026-10-28T12:00:00Z',
    points: [
        // Not next to the alerted point, whose marker also covers the
        // point before it, so it keeps its hollow marker.
        point('2026-10-28T11:00:00Z', { value: 60, z_score: 7.5, outside: true }),
        point('2026-10-28T11:05:00Z'),
        point('2026-10-28T11:10:00Z', { value: 70, z_score: 10, outside: true }),
    ],
    ...overrides,
});

const loadedComparison = (
    overrides: Partial<UseAnomalyComparisonReturn> = {}
): UseAnomalyComparisonReturn => ({
    data: response(),
    alerts: [
        {
            id: 1,
            metric_name: CPU,
            metric_value: 70,
            anomaly_score: 10,
            anomaly_details: '{"baseline_context":{"period_type":"hourly"}}',
            triggered_at: '2026-10-28T11:10:00Z',
            status: 'cleared',
            cleared_at: '2026-10-28T11:10:00Z',
        },
    ],
    window: {
        startMs: Date.parse('2026-10-28T11:00:00Z'),
        endMs: Date.parse('2026-10-28T12:00:00Z'),
        buckets: 300,
    },
    loading: false,
    error: null,
    alertsError: null,
    alertsCompleteFromMs: null,
    ...overrides,
});

const lastQuery = (): AnomalyComparisonQuery | null => queries[queries.length - 1];

/** Open an MUI select by its accessible name and pick an option. */
const choose = (selectName: string, optionName: string): void => {
    fireEvent.mouseDown(screen.getByRole('combobox', { name: selectName }));
    const listbox = screen.getByRole('listbox');
    fireEvent.click(within(listbox).getByRole('option', { name: optionName }));
};

const renderSection = () => renderWithTheme(<BaselineComparisonSection connectionId={1} />);

beforeEach(() => {
    queries.length = 0;
    databaseCalls.length = 0;
    metricsResult = { metrics: METRICS, loading: false, error: null };
    comparisonResult = loadedComparison();
    databasesResult = {
        databases: [
            { database_name: 'zeta' },
            { database_name: 'app' },
        ] as UseDatabaseSummariesResult['databases'],
        loading: false,
        error: null,
    };
    localStorage.clear();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('BaselineComparisonSection', () => {
    it('defaults to CPU usage, the as-scored model and no sensitivity', () => {
        renderSection();
        expect(screen.getByText('Baseline comparison')).toBeInTheDocument();
        expect(lastQuery()).toEqual({
            connectionId: 1,
            metric: CPU,
            databaseName: null,
            model: 'as_scored',
            sensitivity: null,
        });
        expect(screen.queryByRole('combobox', { name: 'Database' })).not.toBeInTheDocument();
        expect(databaseCalls.every((enabled) => !enabled)).toBe(true);
    });

    it('charts the series with both deviation kinds in the legend', () => {
        renderSection();
        const chart = screen.getByTestId('chart');
        expect(chart).toHaveTextContent('CPU usage (%) against its baseline');
        expect(chart.dataset.categories).toBe('3');
        const legend = chart.dataset.legend?.split('|') ?? [];
        expect(legend).toContain(ENGINE_ANOMALY_LABEL);
        expect(legend).toContain(OUTSIDE_LABEL);
        expect(
            screen.getByText(/Bands are the current baselines applied across the window\./)
        ).toBeInTheDocument();
    });

    it('names the chart for assistive technology', () => {
        renderSection();
        const image = screen.getByRole('img', {
            name: /^CPU usage \(%\) against its baseline from /,
        });
        expect(image).toContainElement(screen.getByTestId('chart'));
        expect(image.getAttribute('aria-label')).toContain(
            '1 engine anomaly alert; 1 point outside the band with no engine alert.'
        );
    });

    it('says when no baselines exist yet for the metric', () => {
        const bare = { mean: null, stddev: null, period_type: null, z_score: null };
        comparisonResult = loadedComparison({
            data: response({
                model: 'daily',
                points: [point('2026-10-28T11:00:00Z', bare), point('2026-10-28T11:05:00Z', bare)],
            }),
        });
        renderSection();
        expect(
            screen.getByText(
                'No day-of-week baselines exist yet for this metric, so only its value is shown.'
            )
        ).toBeInTheDocument();
        expect(screen.queryByText(/Bands are the current baselines/)).not.toBeInTheDocument();
        expect(screen.getByRole('img').getAttribute('aria-label')).toContain(
            'no baseline applies'
        );
    });

    it('explains that some unmarked points beyond the band are below the value floor', () => {
        comparisonResult = loadedComparison({
            data: response({
                points: [
                    // z beyond the band, yet not outside: below the floor.
                    point('2026-10-28T11:00:00Z', { value: 5, z_score: -6.25 }),
                    point('2026-10-28T11:05:00Z'),
                ],
            }),
            alerts: [],
        });
        renderSection();
        expect(screen.getByText(/Some unmarked points beyond the band are below the metric's value floor, where the engine does not alert\./)).toBeInTheDocument();
        expect(screen.getByRole('img').getAttribute('aria-label')).toContain(
            '1 point below the value floor'
        );
    });

    it('does not mention the value floor when no point is below it', () => {
        renderSection();
        expect(screen.queryByText(/value floor/)).not.toBeInTheDocument();
    });

    it('shows the database selector for a per-database metric', () => {
        renderSection();
        choose('Metric', 'Cache hit ratio (%)');
        expect(databaseCalls[databaseCalls.length - 1]).toBe(true);
        expect(lastQuery()).toMatchObject({ metric: CACHE, databaseName: 'app' });

        choose('Database', 'zeta');
        expect(lastQuery()).toMatchObject({ databaseName: 'zeta' });
    });

    it('waits for a database before querying a per-database metric', () => {
        databasesResult = { databases: [], loading: false, error: null };
        renderSection();
        choose('Metric', 'Cache hit ratio (%)');
        expect(lastQuery()).toBeNull();
        expect(screen.getByText('No databases are known for this server.')).toBeInTheDocument();
    });

    it('changes the model and sensitivity', () => {
        renderSection();
        choose('Baseline model', 'Day of week');
        expect(lastQuery()).toMatchObject({ model: 'daily' });
        choose('Sensitivity', '4σ');
        expect(lastQuery()).toMatchObject({ sensitivity: 4 });
    });

    it('rounds an unoffered default sensitivity and asks for it', () => {
        comparisonResult = loadedComparison({
            data: response({ sensitivity: 2.7, default_sensitivity: 2.7 }),
        });
        renderSection();
        expect(lastQuery()).toMatchObject({ sensitivity: 3 });
    });

    it('shows the offered default without refetching', () => {
        renderSection();
        expect(screen.getByRole('combobox', { name: 'Sensitivity' })).toHaveTextContent('3σ');
        expect(lastQuery()?.sensitivity).toBeNull();
    });

    it('warns when scoring settings are the defaults', () => {
        comparisonResult = loadedComparison({
            data: response({ scoring_source: 'defaults' }),
        });
        renderSection();
        expect(screen.getByText(/Scoring settings: defaults/)).toBeInTheDocument();
    });

    it('notes when the anomaly alerts could not be loaded', () => {
        comparisonResult = loadedComparison({ alertsError: 'nope' });
        renderSection();
        expect(screen.getByText(/anomaly alerts could not be loaded/)).toBeInTheDocument();
    });

    it('notes when not every engine alert was loaded', () => {
        comparisonResult = loadedComparison({
            alertsCompleteFromMs: Date.parse('2026-10-28T11:05:00Z'),
        });
        renderSection();
        expect(screen.getByText(/Not every engine alert could be loaded/)).toBeInTheDocument();
        expect(screen.getByTestId('chart').dataset.legend?.split('|')).not.toContain(OUTSIDE_LABEL);
    });

    it('shows the empty state when there are no points', () => {
        comparisonResult = loadedComparison({
            data: response({ points: [], baselines_as_of: null }),
        });
        renderSection();
        expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
        expect(
            screen.getByText('No samples of this metric were collected in the selected period.')
        ).toBeInTheDocument();
    });

    it('treats null points as empty', () => {
        comparisonResult = loadedComparison({
            data: response({ points: null }),
        });
        renderSection();
        expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
    });

    it('shows the loading state', () => {
        comparisonResult = loadedComparison({
            data: null,
            window: null,
            loading: true,
        });
        renderSection();
        expect(screen.getByLabelText('Loading chart')).toBeInTheDocument();
    });

    it('reports a failed comparison', () => {
        comparisonResult = loadedComparison({
            data: null,
            error: 'unknown metric',
        });
        renderSection();
        expect(screen.getByText('unknown metric')).toBeInTheDocument();
    });

    it('reports the metric list loading, failing and being empty', () => {
        metricsResult = { metrics: [], loading: true, error: null };
        const { unmount } = renderSection();
        expect(screen.getByLabelText('Loading baselined metrics')).toBeInTheDocument();
        expect(lastQuery()).toBeNull();
        unmount();

        metricsResult = { metrics: [], loading: false, error: 'denied' };
        const second = renderSection();
        expect(screen.getByText('denied')).toBeInTheDocument();
        second.unmount();

        metricsResult = { metrics: [], loading: false, error: null };
        renderSection();
        expect(screen.getByText('No baselined metrics are available.')).toBeInTheDocument();
    });

    it('falls back to the first metric when CPU usage is not offered', () => {
        metricsResult = {
            metrics: [{ name: 'custom.metric', probe_name: 'custom', per_database: false }],
            loading: false,
            error: null,
        };
        renderSection();
        expect(lastQuery()).toMatchObject({ metric: 'custom.metric' });
        expect(screen.getByRole('combobox', { name: 'Metric' })).toHaveTextContent('custom.metric');
    });
});
