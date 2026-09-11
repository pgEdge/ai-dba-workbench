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
import PostgresOverviewSection from '../PostgresOverviewSection';
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

const mockApiGet = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiGet: (url: string, options?: { signal?: AbortSignal }) =>
        mockApiGet(url, options) as unknown,
}));

const mockLoggerError = vi.fn();
vi.mock('../../../../utils/logger', () => ({
    logger: {
        error: (...args: unknown[]) => mockLoggerError(...args),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

/*
 * The Chart mock renders the title plus a serialised description of
 * every series so the tests can assert exactly which metric ends up on
 * which chart, which is the whole point of the split.
 */
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

const CONNECTIONS_TITLE = 'Connections (Monitored Database)';
const SESSIONS_TITLE = 'Sessions Established (Monitored Database)';

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2024-01-01T00:0${idx}:00Z`,
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
            case 'numbackends':
                return ready([series('numbackends', [10, 12])]);
            case 'numbackends,sessions':
                return ready([
                    series('numbackends', [10, 12]),
                    series('sessions', [8000, 8100]),
                ]);
            case 'xact_commit,xact_rollback':
                return ready([
                    series('xact_commit', [100, 200]),
                    series('xact_rollback', [1, 2]),
                ]);
            case 'blks_hit,blks_read':
                return ready([
                    series('blks_hit', [90, 95]),
                    series('blks_read', [10, 5]),
                ]);
            case 'temp_bytes':
                return ready([series('temp_bytes', [1024, 2048])]);
            default:
                return ready([
                    series('tup_fetched', [1, 2]),
                    series('tup_inserted', [3, 4]),
                    series('tup_updated', [5, 6]),
                    series('tup_deleted', [7, 8]),
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

const renderSection = (connectionId = 7) => render(
    <PostgresOverviewSection
        connectionId={connectionId}
        connectionName="Test Server"
    />,
);

/** Deferred settlement handles for each queued apiGet call. */
interface Deferred {
    resolve: (value: unknown) => void;
    reject: (reason: unknown) => void;
}

/**
 * Make apiGet return promises that the test settles by hand, so that
 * every assertion about the reference series follows an observed state
 * transition rather than the initial pre-fetch render.
 */
const deferApiGet = (): Deferred[] => {
    const deferrals: Deferred[] = [];
    mockApiGet.mockImplementation(() => new Promise((resolve, reject) => {
        deferrals.push({ resolve, reject });
    }));
    return deferrals;
};

/**
 * Render, settle the first lookup with a usable limit, and wait for the
 * reference series to appear. Returns the rerender helper and the
 * deferred queue so the caller can drive a connection change.
 */
const renderWithLimit = async (deferrals: Deferred[]) => {
    const view = renderSection(7);
    await waitFor(() => { expect(deferrals).toHaveLength(1); });
    deferrals[0].resolve([{ max_connections: 100 }]);
    await waitFor(() => {
        expect(seriesNamesFor(CONNECTIONS_TITLE))
            .toEqual(['Backends', 'Max Connections']);
    });
    return view;
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PostgresOverviewSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        routeMetrics();
        mockApiGet.mockResolvedValue([{ max_connections: 100 }]);
    });

    describe('connection charts', () => {
        it('renders backends and sessions as two separate charts', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText(CONNECTIONS_TITLE)).toBeInTheDocument();
            });
            expect(screen.getByText(SESSIONS_TITLE)).toBeInTheDocument();

            // The gauge and the counter must never share a chart.
            expect(seriesNamesFor(CONNECTIONS_TITLE))
                .not.toContain('Cumulative Sessions');
            expect(seriesNamesFor(SESSIONS_TITLE))
                .toEqual(['Cumulative Sessions']);
        });

        it('plots numbackends with a max_connections reference series', async () => {
            renderSection();

            await waitFor(() => {
                expect(seriesNamesFor(CONNECTIONS_TITLE))
                    .toEqual(['Backends', 'Max Connections']);
            });

            expect(seriesValuesFor(CONNECTIONS_TITLE, 'Backends'))
                .toBe('10,12');
            // One reference point per category, at the configured limit.
            expect(seriesValuesFor(CONNECTIONS_TITLE, 'Max Connections'))
                .toBe('100,100');
        });

        it('queries the latest pg_server_info row for max_connections', async () => {
            renderSection();

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalled();
            });
            const url = mockApiGet.mock.calls[0][0] as string;
            expect(url).toContain('probe_name=pg_server_info');
            expect(url).toContain('connection_id=7');
            expect(url).toContain('limit=1');
            expect(url).toContain('order_by=collected_at');
        });

        it('drops the previous limit as soon as the connection changes', async () => {
            const deferrals = deferApiGet();
            const { rerender } = await renderWithLimit(deferrals);

            // Switching connection must not leave the old server's limit
            // drawn over the new server's backend count whilst the
            // second lookup is still in flight.
            rerender(
                <PostgresOverviewSection
                    connectionId={8}
                    connectionName="Other Server"
                />,
            );
            expect(seriesNamesFor(CONNECTIONS_TITLE)).toEqual(['Backends']);

            await waitFor(() => { expect(deferrals).toHaveLength(2); });
            deferrals[1].resolve([{ max_connections: 250 }]);
            await waitFor(() => {
                expect(seriesValuesFor(CONNECTIONS_TITLE, 'Max Connections'))
                    .toBe('250,250');
            });
        });

        /**
         * Drive a connection change whose lookup settles with an
         * unusable response, and assert the reference series is dropped
         * rather than merely absent from the outset.
         */
        const expectOmittedAfter = async (
            settle: (deferred: Deferred) => void,
        ) => {
            const deferrals = deferApiGet();
            const { rerender } = await renderWithLimit(deferrals);

            rerender(
                <PostgresOverviewSection
                    connectionId={8}
                    connectionName="Other Server"
                />,
            );
            await waitFor(() => { expect(deferrals).toHaveLength(2); });
            settle(deferrals[1]);

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledTimes(2);
            });
            expect(seriesNamesFor(CONNECTIONS_TITLE)).toEqual(['Backends']);
        };

        it('omits the reference series when max_connections is unavailable', async () => {
            await expectOmittedAfter(d => { d.resolve([]); });
        });

        it('omits the reference series when max_connections is not positive', async () => {
            await expectOmittedAfter(d => { d.resolve([{ max_connections: 0 }]); });
        });

        it('omits the reference series when the response is not an array', async () => {
            await expectOmittedAfter(d => { d.resolve(null); });
        });

        it('logs and degrades gracefully when the lookup fails', async () => {
            const deferrals = deferApiGet();
            const { rerender } = await renderWithLimit(deferrals);

            rerender(
                <PostgresOverviewSection
                    connectionId={8}
                    connectionName="Other Server"
                />,
            );
            await waitFor(() => { expect(deferrals).toHaveLength(2); });
            deferrals[1].reject(new Error('boom'));

            await waitFor(() => {
                expect(mockLoggerError).toHaveBeenCalledWith(
                    'Error fetching max_connections:',
                    expect.any(Error),
                );
            });
            expect(seriesNamesFor(CONNECTIONS_TITLE)).toEqual(['Backends']);
        });

        it('aborts the in-flight lookup and ignores a late resolution', async () => {
            const deferrals = deferApiGet();
            const { unmount } = renderSection();
            await waitFor(() => { expect(deferrals).toHaveLength(1); });

            const signal = (mockApiGet.mock.calls[0][1] as {
                signal: AbortSignal;
            }).signal;
            expect(signal.aborted).toBe(false);

            unmount();
            expect(signal.aborted).toBe(true);

            // A late success is discarded by the aborted guard, so no
            // state update is attempted after teardown.
            deferrals[0].resolve([{ max_connections: 100 }]);
            await waitFor(() => {
                expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
            });
            expect(mockLoggerError).not.toHaveBeenCalled();
        });

        it('aborts the in-flight lookup and ignores a late rejection', async () => {
            const deferrals = deferApiGet();
            const { unmount } = renderSection();
            await waitFor(() => { expect(deferrals).toHaveLength(1); });

            const signal = (mockApiGet.mock.calls[0][1] as {
                signal: AbortSignal;
            }).signal;

            unmount();
            expect(signal.aborted).toBe(true);

            // A late failure is swallowed rather than logged, because
            // the abort is the cause rather than a real lookup error.
            deferrals[0].reject(new Error('aborted'));
            await waitFor(() => {
                expect(mockLoggerError).not.toHaveBeenCalled();
            });
        });

        it('shows empty messages when no connection data is returned', async () => {
            routeMetrics({ 'numbackends,sessions': ready([]) });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('No connection data available'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('No session data available'))
                .toBeInTheDocument();
        });

        it('shows chart loading indicators while the query is in flight', async () => {
            routeMetrics({ 'numbackends,sessions': loading() });
            renderSection();

            await waitFor(() => {
                expect(screen.getAllByLabelText('Loading chart').length)
                    .toBeGreaterThanOrEqual(2);
            });
        });
    });

    describe('other charts', () => {
        it('renders the transaction, block I/O and tuple charts', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Transactions')).toBeInTheDocument();
            });
            expect(seriesNamesFor('Transactions'))
                .toEqual(['Commits', 'Rollbacks']);
            expect(seriesNamesFor('Block I/O'))
                .toEqual(['Blocks Hit', 'Blocks Read']);
            expect(seriesNamesFor('Tuple Operations'))
                .toEqual(['Fetched', 'Inserted', 'Updated', 'Deleted']);
        });
    });

    describe('KPI tiles', () => {
        it('renders the KPI values derived from the metric series', async () => {
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Cache Hit Ratio')).toBeInTheDocument();
            });
            // 'Backends' and 'Commits' are both KPI labels and chart
            // series names, so each appears more than once.
            expect(screen.getAllByText('Backends').length)
                .toBeGreaterThanOrEqual(2);
            expect(screen.getAllByText('Commits').length)
                .toBeGreaterThanOrEqual(2);
            expect(screen.getByText('12')).toBeInTheDocument();
            // 95 hits against 5 reads gives a 95.0% ratio.
            expect(screen.getByText('95.0')).toBeInTheDocument();
            expect(screen.getByText('Temp Bytes')).toBeInTheDocument();
        });

        it('renders placeholders when the KPI queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Backends')).toBeInTheDocument();
            });
            expect(screen.getByText('Temp Bytes')).toBeInTheDocument();
            expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(3);
        });

        it('shows the section spinner during the initial KPI load', () => {
            mockUseMetrics.mockImplementation(() => loading());
            renderSection();

            expect(screen.getByLabelText('Loading')).toBeInTheDocument();
        });

        it('handles a cache hit ratio with no blocks accounted for', async () => {
            routeMetrics({
                'blks_hit,blks_read': ready([
                    series('blks_hit', [0, 0]),
                    series('blks_read', [0, 0]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Cache Hit Ratio')).toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(1);
        });

        it('pads the cache ratio sparkline when the series lengths differ', async () => {
            routeMetrics({
                'blks_hit,blks_read': ready([
                    series('blks_hit', [90, 95, 99]),
                    series('blks_read', [10]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Cache Hit Ratio')).toBeInTheDocument();
            });
            // The sparkline pads the shorter read series with zeros; the
            // tile itself still reports 99 hits against 10 reads.
            expect(screen.getByText('90.8')).toBeInTheDocument();
        });

        it('pads the cache ratio sparkline when hits are missing entirely', async () => {
            routeMetrics({
                'blks_hit,blks_read': ready([
                    series('blks_read', [10, 20]),
                ]),
            });
            renderSection();

            await waitFor(() => {
                expect(screen.getByText('Cache Hit Ratio')).toBeInTheDocument();
            });
            // With no hit series at all the ratio is unknown, so the
            // tile falls back to the placeholder.
            expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(1);
        });
    });
});
