/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import QueryDetail from '../QueryDetail';
import type { QueryDetailData } from '../types';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries } from '../../types';
import type { ChartData } from '../../../Chart/types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

let mockUser: { id: number; username: string } | null = {
    id: 1,
    username: 'testuser',
};
vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: '1h' },
        refreshTrigger: 0,
        setTimeRange: vi.fn(),
        currentOverlay: { connectionName: 'Test Server' },
    }),
}));

let mockAiEnabled = false;
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: mockAiEnabled }),
}));

const mockRefreshOverview = vi.fn();
let mockOverview: {
    summary: string | null;
    loading: boolean;
    error: string | null;
    generatedAt: Date | null;
} = {
    summary: null,
    loading: false,
    error: null,
    generatedAt: null,
};
vi.mock('../../../../hooks/useQueryOverview', () => ({
    useQueryOverview: () => ({
        ...mockOverview,
        refresh: mockRefreshOverview,
    }),
}));

type UseMetricsFn = (params: MetricQueryParams | null) => UseMetricsReturn;

const mockUseMetrics = vi.fn<UseMetricsFn>();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => mockUseMetrics(params),
}));

vi.mock('../QueryPlanPanel', () => ({
    default: () => <div data-testid="query-plan-panel" />,
}));

vi.mock('../../../QueryAnalysisDialog', () => ({
    QueryAnalysisDialog: ({ open, onClose }: {
        open: boolean;
        onClose: () => void;
    }) => (
        <div
            data-testid="query-analysis-dialog"
            data-open={open ? 'true' : 'false'}
        >
            <button type="button" onClick={onClose}>Close analysis</button>
        </div>
    ),
}));

vi.mock('../../TimeRangeSelector', () => ({
    default: () => <div data-testid="time-range-selector" />,
}));

/*
 * The Chart mock exposes the chart type and series so the tests can
 * assert that calls are drawn as a rate line rather than a bar chart
 * of raw counter values.
 */
vi.mock('../../../Chart', () => ({
    Chart: ({ title, data, type, smooth }: {
        title: string;
        data: ChartData;
        type: string;
        smooth?: boolean;
    }) => (
        <div
            data-testid="chart"
            data-title={title}
            data-type={type}
            data-smooth={smooth ? 'true' : 'false'}
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

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const CALLS_TITLE = 'Calls Over Time';
const EXEC_TITLE = 'Execution Time Over Time';
const EXEC_KEY = 'mean_exec_time,min_exec_time,max_exec_time';
const QUERY_ID = '12345';

/** Build a mock query row with sensible defaults. */
const makeQueryRow = (
    overrides: Partial<QueryDetailData> = {},
): QueryDetailData => ({
    queryid: QUERY_ID,
    query: 'SELECT * FROM users WHERE id = $1',
    calls: 500,
    total_exec_time: 1000,
    mean_exec_time: 2,
    rows: 1000,
    shared_blks_hit: 900,
    shared_blks_read: 100,
    ...overrides,
});

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
const loadingMetrics = (): UseMetricsReturn => ({
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

/** Route each useMetrics call by the metrics it requests. */
const routeMetrics = (
    overrides: Partial<Record<string, UseMetricsReturn>> = {},
): void => {
    mockUseMetrics.mockImplementation((params) => {
        const key = (params?.metrics ?? []).join(',');
        const override = overrides[key];
        if (override) { return override; }

        if (key === 'calls_per_sec') {
            return ready([series('calls_per_sec', [1.5, 3])]);
        }
        return ready([
            series('mean_exec_time', [2, 3]),
            series('min_exec_time', [1, 1]),
            series('max_exec_time', [9, 10]),
        ]);
    });
};

/** Create a successful Response-like object. */
const okResponse = (data: unknown): Partial<Response> => ({
    ok: true,
    json: () => Promise.resolve(data),
});

/** Create a failed Response-like object. */
const errorResponse = (
    status: number,
    body: Record<string, string> = {},
): Partial<Response> => ({
    ok: false,
    status,
    json: () => Promise.resolve(body),
});

/** Find the rendered series names for a given chart title. */
const seriesNamesFor = (title: string): string[] =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-chart') === title)
        .map(el => el.getAttribute('data-series') ?? '');

/** Find a rendered chart element by its title. */
const chartFor = (title: string): HTMLElement | undefined =>
    screen.getAllByTestId('chart')
        .find(el => el.getAttribute('data-title') === title);

/** The parameters of the most recent useMetrics call for a metric set. */
const paramsFor = (key: string): MetricQueryParams | undefined => {
    const matches = mockUseMetrics.mock.calls
        .map(([params]) => params)
        .filter((p): p is MetricQueryParams =>
            p !== null && (p.metrics ?? []).join(',') === key);
    return matches[matches.length - 1];
};

const renderDetail = () => render(
    <ThemeProvider theme={theme}>
        <QueryDetail
            connectionId={4}
            databaseName="testdb"
            objectName={QUERY_ID}
        />
    </ThemeProvider>,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('QueryDetail', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        mockUser = { id: 1, username: 'testuser' };
        mockAiEnabled = false;
        mockOverview = {
            summary: null,
            loading: false,
            error: null,
            generatedAt: null,
        };
        routeMetrics();
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
    });

    describe('query fetch', () => {
        it('shows the loading spinner while the initial fetch is pending',
            () => {
                mockApiFetch.mockReturnValue(new Promise(() => {}));

                renderDetail();

                expect(
                    screen.getByLabelText('Loading query details'),
                ).toBeInTheDocument();
            });

        it('requests the selected query from the top-queries endpoint',
            async () => {
                renderDetail();

                await waitFor(() => {
                    expect(mockApiFetch).toHaveBeenCalledWith(
                        '/api/v1/metrics/top-queries?connection_id=4'
                        + `&queryid=${QUERY_ID}&limit=1`,
                    );
                });
            });

        it('does not query metrics before the query row arrives', () => {
            mockApiFetch.mockReturnValue(new Promise(() => {}));

            renderDetail();

            expect(
                mockUseMetrics.mock.calls.every(([p]) => p === null),
            ).toBe(true);
        });
    });

    describe('calls chart', () => {
        it('requests the per-second call rate with an average', async () => {
            renderDetail();

            await waitFor(() => {
                expect(chartFor(CALLS_TITLE)).toBeDefined();
            });
            const callsQuery = paramsFor('calls_per_sec');
            expect(callsQuery?.metrics).toEqual(['calls_per_sec']);
            expect(callsQuery?.aggregation).toBe('avg');
        });

        it('scopes both charts to the selected query id', async () => {
            renderDetail();

            await waitFor(() => {
                expect(paramsFor(EXEC_KEY)?.queryId).toBe(QUERY_ID);
            });

            const execParams = paramsFor(EXEC_KEY);
            expect(execParams?.probeName).toBe('pg_stat_statements');
            expect(execParams?.databaseName).toBe('testdb');
            expect(execParams?.timeRange).toBe('1h');
            expect(execParams?.metrics).toEqual([
                'mean_exec_time', 'min_exec_time', 'max_exec_time',
            ]);

            const callsParams = paramsFor('calls_per_sec');
            expect(callsParams?.queryId).toBe(QUERY_ID);
            expect(callsParams?.probeName).toBe('pg_stat_statements');
            expect(callsParams?.timeRange).toBe('1h');
        });

        it('draws the call rate as a smooth line', async () => {
            renderDetail();

            await waitFor(() => {
                expect(chartFor(CALLS_TITLE)).toBeDefined();
            });
            expect(seriesNamesFor(CALLS_TITLE)).toEqual(['Calls/s']);
            expect(chartFor(CALLS_TITLE)?.getAttribute('data-type'))
                .toBe('line');
            expect(chartFor(CALLS_TITLE)?.getAttribute('data-smooth'))
                .toBe('true');
        });

        it('plots the execution time series alongside it', async () => {
            renderDetail();

            await waitFor(() => {
                expect(chartFor(EXEC_TITLE)).toBeDefined();
            });
            expect(seriesNamesFor(EXEC_TITLE)).toEqual([
                'Mean Time (ms)', 'Min Time (ms)', 'Max Time (ms)',
            ]);
        });

        it('shows empty messages when the queries return nothing', async () => {
            mockUseMetrics.mockImplementation(() => ready([]));
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('No call frequency data available'))
                    .toBeInTheDocument();
            });
            expect(screen.getByText('No execution time data available'))
                .toBeInTheDocument();
        });

        it('shows chart loading indicators while queries are in flight',
            async () => {
                mockUseMetrics.mockImplementation(() => loadingMetrics());
                renderDetail();

                await waitFor(() => {
                    expect(screen.getAllByLabelText('Loading chart'))
                        .toHaveLength(2);
                });
            });

        it('reports a query error in place of the empty message', async () => {
            routeMetrics({
                calls_per_sec: failed('metric not found in probe'),
            });
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('metric not found in probe'))
                    .toBeInTheDocument();
            });
            expect(screen.queryByText('No call frequency data available'))
                .not.toBeInTheDocument();
        });

        it('reports an execution time query error', async () => {
            routeMetrics({
                [EXEC_KEY]: failed('probe unavailable'),
            });
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('probe unavailable'))
                    .toBeInTheDocument();
            });
        });
    });

    describe('query statistics', () => {
        it('renders the KPI tiles from the fetched row', async () => {
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(screen.getByText('500')).toBeInTheDocument();
            expect(screen.getByText('Avg Rows/Call')).toBeInTheDocument();
            expect(screen.getByText('2.0')).toBeInTheDocument();
        });

        it('reports a failed detail fetch', async () => {
            mockApiFetch.mockResolvedValue(
                errorResponse(400, { error: 'query not found' }),
            );
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('query not found'))
                    .toBeInTheDocument();
            });
        });

        it('falls back to a status error message without an error body',
            async () => {
                mockApiFetch.mockResolvedValue(errorResponse(503));
                renderDetail();

                await waitFor(() => {
                    expect(
                        screen.getByText(/Failed to fetch query data: 503/),
                    ).toBeInTheDocument();
                });
            });

        it('shows an error message when the fetch rejects', async () => {
            mockApiFetch.mockRejectedValue(new Error('network down'));
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('network down'))
                    .toBeInTheDocument();
            });
        });

        it('renders placeholders when no row is returned', async () => {
            mockApiFetch.mockResolvedValue(okResponse([]));
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length)
                .toBeGreaterThanOrEqual(4);
        });

        it('renders a placeholder for a query with no calls', async () => {
            mockApiFetch.mockResolvedValue(
                okResponse([makeQueryRow({ calls: 0 })]),
            );
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });

        it('skips the fetch when no user is signed in', async () => {
            mockUser = null;
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(mockApiFetch).not.toHaveBeenCalled();
        });
    });

    describe('query text', () => {
        const LONG_QUERY = `SELECT ${'column_name, '.repeat(20)}1`;

        it('expands and collapses a long query on click', async () => {
            mockApiFetch.mockResolvedValue(
                okResponse([makeQueryRow({ query: LONG_QUERY })]),
            );
            renderDetail();

            const toggle = await screen.findByLabelText(
                'Expand query text'
            );
            expect(screen.getByText(/\.\.\.$/)).toBeInTheDocument();

            fireEvent.click(toggle);
            expect(await screen.findByLabelText('Collapse query text'))
                .toBeInTheDocument();
        });

        it('expands a long query from the keyboard', async () => {
            mockApiFetch.mockResolvedValue(
                okResponse([makeQueryRow({ query: LONG_QUERY })]),
            );
            renderDetail();

            const toggle = await screen.findByLabelText(
                'Expand query text'
            );
            fireEvent.keyDown(toggle, { key: 'Enter' });
            expect(await screen.findByLabelText('Collapse query text'))
                .toBeInTheDocument();

            fireEvent.keyDown(
                screen.getByLabelText('Collapse query text'),
                { key: ' ' },
            );
            expect(await screen.findByLabelText('Expand query text'))
                .toBeInTheDocument();
        });

        it('ignores unrelated keys on the query text', async () => {
            mockApiFetch.mockResolvedValue(
                okResponse([makeQueryRow({ query: LONG_QUERY })]),
            );
            renderDetail();

            const toggle = await screen.findByLabelText(
                'Expand query text'
            );
            fireEvent.keyDown(toggle, { key: 'Escape' });
            expect(screen.getByLabelText('Expand query text'))
                .toBeInTheDocument();
        });

        it('reports a failed fetch with an unreadable body', async () => {
            mockApiFetch.mockResolvedValue({
                ok: false,
                status: 500,
                json: () => Promise.reject(new Error('not json')),
            });
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Failed to fetch query data: 500'))
                    .toBeInTheDocument();
            });
        });
    });

    describe('AI overview panel', () => {
        beforeEach(() => {
            mockAiEnabled = true;
        });

        it('opens and closes the full analysis dialog', async () => {
            renderDetail();

            const open = await screen.findByLabelText('Open full analysis');
            fireEvent.click(open);
            await waitFor(() => {
                expect(
                    screen.getByTestId('query-analysis-dialog')
                        .getAttribute('data-open'),
                ).toBe('true');
            });

            fireEvent.click(screen.getByText('Close analysis'));
            await waitFor(() => {
                expect(
                    screen.getByTestId('query-analysis-dialog')
                        .getAttribute('data-open'),
                ).toBe('false');
            });
        });

        it('collapses and expands the overview panel', async () => {
            renderDetail();

            const collapse = await screen.findByLabelText(
                'Collapse AI Overview'
            );
            fireEvent.click(collapse);
            expect(await screen.findByLabelText('Expand AI Overview'))
                .toBeInTheDocument();

            fireEvent.click(screen.getByLabelText('Expand AI Overview'));
            expect(await screen.findByLabelText('Collapse AI Overview'))
                .toBeInTheDocument();
        });

        it('shows placeholders whilst the overview is generating', async () => {
            renderDetail();

            expect(await screen.findByText('Generating overview...'))
                .toBeInTheDocument();
        });

        it('shows skeletons whilst the overview is loading', async () => {
            mockOverview = {
                summary: null,
                loading: true,
                error: null,
                generatedAt: null,
            };
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });
            expect(screen.queryByText('Generating overview...'))
                .not.toBeInTheDocument();
        });

        it('refreshes the overview on request', async () => {
            mockOverview = {
                summary: 'This query looks healthy.',
                loading: false,
                error: null,
                generatedAt: new Date(),
            };
            renderDetail();

            const refresh = await screen.findByLabelText('Refresh overview');
            fireEvent.click(refresh);
            expect(mockRefreshOverview).toHaveBeenCalled();
            expect(screen.getByText('This query looks healthy.'))
                .toBeInTheDocument();
            expect(screen.getByText('Updated just now')).toBeInTheDocument();
        });

        it.each([
            [5 * 60 * 1000, 'Updated 5 min ago'],
            [2 * 60 * 60 * 1000, 'Updated 2 hours ago'],
            [60 * 60 * 1000, 'Updated 1 hour ago'],
        ])('reports an overview generated %i ms ago', async (ago, label) => {
            mockOverview = {
                summary: 'This query looks healthy.',
                loading: false,
                error: null,
                generatedAt: new Date(Date.now() - ago),
            };
            renderDetail();

            expect(await screen.findByText(label)).toBeInTheDocument();
        });

        it('falls back to a date for an old overview', async () => {
            const generatedAt = new Date(Date.now() - 3 * 86400 * 1000);
            mockOverview = {
                summary: 'This query looks healthy.',
                loading: false,
                error: null,
                generatedAt,
            };
            renderDetail();

            expect(await screen.findByText(
                `Updated ${generatedAt.toLocaleDateString()}`,
            )).toBeInTheDocument();
        });

        it('hides the overview panel when the overview errors', async () => {
            mockOverview = {
                summary: null,
                loading: false,
                error: 'model unavailable',
                generatedAt: null,
            };
            renderDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(screen.queryByText('AI Overview'))
                .not.toBeInTheDocument();
        });
    });
});
