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
import WalReplicationSection from '../WalReplicationSection';
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

/*
 * The Chart mock exposes the chart type and every series so the tests
 * can assert which metric lands on which chart, and that the checkpoint
 * counts are drawn as stacked bars rather than a line.
 */
vi.mock('../../../Chart', () => ({
    Chart: ({ title, data, type, stacked }: {
        title: string;
        data: ChartData;
        type: string;
        stacked?: boolean;
    }) => (
        <div
            data-testid="chart"
            data-title={title}
            data-type={type}
            data-stacked={stacked ? 'true' : 'false'}
        >
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

const CHECKPOINTS_TITLE = 'Checkpoints Over Time';
const BUFFERS_TITLE = 'Checkpoint Buffers Written';
const WAL_TITLE = 'WAL Activity Over Time';

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

const CHECKPOINT_KPI_KEY = 'num_timed_delta,num_requested_delta';
const CHECKPOINT_CHART_KEY =
    'num_timed_delta,num_requested_delta,buffers_written_per_sec';
const WAL_KEY = 'wal_bytes_per_sec,wal_records_per_sec';

/**
 * Route each useMetrics call to a response based on the metrics it
 * requests, mirroring how the component issues one query per concern.
 */
const routeMetrics = (
    overrides: Partial<Record<string, UseMetricsReturn>> = {},
): void => {
    mockUseMetrics.mockImplementation((params) => {
        const key = (params?.metrics ?? []).join(',');
        const override = overrides[key];
        if (override) { return override; }

        switch (key) {
            case WAL_KEY:
                return ready([
                    series('wal_bytes_per_sec', [524288, 1048576]),
                    series('wal_records_per_sec', [10, 20]),
                ]);
            case CHECKPOINT_KPI_KEY:
                return ready([
                    series('num_timed_delta', [3, 1]),
                    series('num_requested_delta', [1, 1]),
                ]);
            case CHECKPOINT_CHART_KEY:
                return ready([
                    series('num_timed_delta', [3, 1]),
                    series('num_requested_delta', [1, 1]),
                    series('buffers_written_per_sec', [40, 80]),
                ]);
            default:
                return ready([
                    series('write_lag', [0.1, 0.2]),
                    series('flush_lag', [0.2, 0.3]),
                    series('replay_lag', [0.3, 0.4]),
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

/** Find a rendered chart element by its title. */
const chartFor = (title: string): HTMLElement | undefined =>
    screen.getAllByTestId('chart')
        .find(el => el.getAttribute('data-title') === title);

const renderSection = () => render(
    <WalReplicationSection connectionId={7} connectionName="Test Server" />,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WalReplicationSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        routeMetrics();
    });

    describe('metric selection', () => {
        it('requests rate and delta metrics rather than raw counters', async () => {
            renderSection();

            await waitFor(() => {
                expect(mockUseMetrics).toHaveBeenCalled();
            });
            const requested = mockUseMetrics.mock.calls
                .map(([params]) => (params?.metrics ?? []).join(','));
            expect(requested).toContain(WAL_KEY);
            expect(requested).toContain(CHECKPOINT_KPI_KEY);
            expect(requested).toContain(CHECKPOINT_CHART_KEY);
        });

        it('shares a single checkpoint query between both charts', async () => {
            renderSection();

            await waitFor(() => {
                expect(chartFor(BUFFERS_TITLE)).toBeDefined();
            });
            const checkpointQueries = mockUseMetrics.mock.calls
                .map(([params]) => (params?.metrics ?? []).join(','))
                .filter(key => key === CHECKPOINT_CHART_KEY);
            // React renders more than once, but every render issues one
            // chart query for the checkpointer probe.
            expect(new Set(checkpointQueries).size).toBe(1);
        });
    });

    describe('charts', () => {
        it('plots WAL bytes and records per second', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor(WAL_TITLE))
                    .toEqual(['WAL Bytes/s', 'WAL Records/s']);
            });
            expect(seriesValuesFor(WAL_TITLE, 'WAL Records/s'))
                .toBe('10,20');
        });

        it('draws checkpoint counts as a stacked bar chart', async () => {
            renderSection();

            await waitFor(() => {
                expect(chartFor(CHECKPOINTS_TITLE)).toBeDefined();
            });
            expect(seriesNamesFor(CHECKPOINTS_TITLE))
                .toEqual(['Timed', 'Requested']);
            expect(chartFor(CHECKPOINTS_TITLE)?.getAttribute('data-type'))
                .toBe('bar');
            expect(chartFor(CHECKPOINTS_TITLE)?.getAttribute('data-stacked'))
                .toBe('true');
        });

        it('plots checkpoint buffers written on its own line chart', async () => {
            renderSection();

            await waitFor(() => {
                expect(chartFor(BUFFERS_TITLE)).toBeDefined();
            });
            expect(seriesNamesFor(BUFFERS_TITLE))
                .toEqual(['Buffers Written/s']);
            expect(seriesValuesFor(BUFFERS_TITLE, 'Buffers Written/s'))
                .toBe('40,80');
            expect(chartFor(BUFFERS_TITLE)?.getAttribute('data-type'))
                .toBe('line');
        });

        it('plots each replication lag series', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor('Replication Lag Over Time'))
                    .toEqual(['Write Lag', 'Flush Lag', 'Replay Lag']);
            });
        });

        it('shows empty messages when the queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No WAL data available'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('No checkpoint data available'))
                .toBeInTheDocument();
            expect(screen.getByText('No checkpoint buffer data available'))
                .toBeInTheDocument();
        });

        it('shows chart loading indicators while queries are in flight', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getAllByLabelText('Loading chart').length)
                .toBeGreaterThanOrEqual(4);
        });

        it('reports a query error in place of the empty message', async () => {
            routeMetrics({
                [CHECKPOINT_CHART_KEY]: failed('metric not found in probe'),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getAllByText('metric not found in probe'))
                    .toHaveLength(2);
            });
            expect(screen.queryByText('No checkpoint data available'))
                .not.toBeInTheDocument();
        });
    });

    describe('KPI tiles', () => {
        it('reports WAL bytes and records as per-second rates', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('WAL Bytes')).toBeInTheDocument();
            });
            expect(screen.getByText('1.0 MB')).toBeInTheDocument();
            expect(screen.getByText('20.0')).toBeInTheDocument();
            expect(screen.getAllByText('/s')).toHaveLength(2);
        });

        it('reports the requested share of all checkpoints', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Requested Checkpoints'))
                    .toBeInTheDocument();
            });
            // Two requested out of six checkpoints in the window.
            expect(screen.getByText('33.3')).toBeInTheDocument();
            expect(screen.getByText('%')).toBeInTheDocument();
        });

        it('shows a placeholder when no checkpoints occurred', async () => {
            routeMetrics({
                [CHECKPOINT_KPI_KEY]: ready([
                    series('num_timed_delta', [0, 0]),
                    series('num_requested_delta', [0, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Requested Checkpoints'))
                    .toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length)
                .toBeGreaterThanOrEqual(1);
            expect(screen.queryByText('%')).not.toBeInTheDocument();
        });

        it('handles a window with only requested checkpoints', async () => {
            routeMetrics({
                [CHECKPOINT_KPI_KEY]: ready([
                    series('num_requested_delta', [2, 3]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Requested Checkpoints'))
                    .toBeInTheDocument();
            });
            // Every checkpoint was requested, so the share is 100%.
            expect(screen.getByText('100.0')).toBeInTheDocument();
        });

        it('handles a window with only timed checkpoints', async () => {
            routeMetrics({
                [CHECKPOINT_KPI_KEY]: ready([
                    series('num_timed_delta', [2, 3]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Requested Checkpoints'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('0.0')).toBeInTheDocument();
        });

        it('renders placeholders when the KPI queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('WAL Bytes')).toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length)
                .toBeGreaterThanOrEqual(4);
        });

        it('shows the section spinner during the initial KPI load', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getByLabelText('Loading')).toBeInTheDocument();
        });

        it('flags a critical replication lag', async () => {
            routeMetrics({
                'replay_lag,write_lag,flush_lag': ready([
                    series('replay_lag', [10, 45]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Replication Lag'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('45.0 s')).toBeInTheDocument();
        });

        it('flags a warning replication lag', async () => {
            routeMetrics({
                'replay_lag,write_lag,flush_lag': ready([
                    series('replay_lag', [1, 10]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Replication Lag'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('10.0 s')).toBeInTheDocument();
        });
    });
});
