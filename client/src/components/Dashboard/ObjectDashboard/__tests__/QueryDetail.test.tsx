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
import type {
    QueryStatsParams,
    UseQueryStatsReturn,
} from '../../../../hooks/useQueryStats';
import type { UseQueryOverviewReturn } from '../../../../hooks/useQueryOverview';
import type { MetricSeries, MetricQueryParams, TimeRange } from '../../types';

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

let mockTimeRange: TimeRange = '1h';
let mockRefreshTrigger = 0;
vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: mockTimeRange },
        refreshTrigger: mockRefreshTrigger,
        currentOverlay: { connectionName: 'prod-1' },
        setTimeRange: vi.fn(),
    }),
}));

let mockMetricsReturn: UseMetricsReturn = {
    data: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
};
// Captures the params each chart hands to useMetrics so the tests can
// assert the series are scoped to the drilled-into statement.
const mockUseMetrics = vi.fn();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => {
        mockUseMetrics(params);
        return mockMetricsReturn;
    },
}));

let mockQueryStatsReturn: UseQueryStatsReturn = {
    stats: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
};
const mockUseQueryStats = vi.fn();
vi.mock('../../../../hooks/useQueryStats', () => ({
    useQueryStats: (params: QueryStatsParams | null) => {
        mockUseQueryStats(params);
        return mockQueryStatsReturn;
    },
}));

let mockOverviewReturn: UseQueryOverviewReturn = {
    summary: null,
    loading: false,
    error: null,
    generatedAt: null,
    refresh: vi.fn(),
};
vi.mock('../../../../hooks/useQueryOverview', () => ({
    useQueryOverview: () => mockOverviewReturn,
}));

let mockAiEnabled = false;
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: mockAiEnabled }),
}));

// The plan panel auto-fetches EXPLAIN on mount; stub it out.
vi.mock('../QueryPlanPanel', () => ({
    default: () => <div data-testid="plan-panel" />,
}));

vi.mock('../../../QueryAnalysisDialog', () => ({
    QueryAnalysisDialog: (
        { open, onClose }: { open: boolean; onClose: () => void },
    ) => (
        open
            ? (
                <div data-testid="analysis-dialog">
                    <button
                        type="button"
                        data-testid="analysis-dialog-close"
                        onClick={onClose}
                    />
                </div>
            )
            : null
    ),
}));

// Mock the Chart component to avoid ApexCharts dependencies.
vi.mock('../../../Chart', () => ({
    Chart: ({ title }: { title: string }) => (
        <div data-testid="chart">{title}</div>
    ),
}));

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

/** Build a mock query detail row with sensible defaults. */
const makeQueryRow = (
    overrides: Partial<QueryDetailData> = {},
): QueryDetailData => ({
    queryid: '-1234567890',
    query: 'SELECT id FROM users WHERE email = $1',
    calls: 4200,
    total_exec_time: 84000,
    mean_exec_time: 20,
    min_exec_time: 2.5,
    max_exec_time: 512,
    rows: 8400,
    shared_blks_hit: 900,
    shared_blks_read: 100,
    username: 'app_user',
    ...overrides,
});

/** Series covering every metric the two charts request. */
const fullMetricsData = (): MetricSeries[] => {
    const point = { time: '2026-01-01T00:00:00Z', value: 5 };
    return [
        'mean_exec_time',
        'min_exec_time',
        'max_exec_time',
        'calls',
    ].map(metric => ({
        name: metric,
        metric,
        data: [point],
    })) as MetricSeries[];
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

const renderQueryDetail = (
    props: Partial<{
        connectionId: number;
        databaseName: string;
        objectName: string;
    }> = {},
) => render(
    <ThemeProvider theme={theme}>
        <QueryDetail
            connectionId={1}
            databaseName="testdb"
            objectName="-1234567890"
            {...props}
        />
    </ThemeProvider>,
);

/** Return the params captured for the chart requesting `metric`. */
const chartParamsFor = (metric: string): MetricQueryParams | undefined => {
    const calls = mockUseMetrics.mock.calls as [
        MetricQueryParams | null,
    ][];
    return calls
        .map(call => call[0])
        .find(
            (params): params is MetricQueryParams =>
                params !== null
                && (params.metrics?.includes(metric) ?? false),
        );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('QueryDetail', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockTimeRange = '1h';
        mockRefreshTrigger = 0;
        mockAiEnabled = false;
        mockMetricsReturn = {
            data: fullMetricsData(),
            loading: false,
            error: null,
            refetch: vi.fn(),
        };
        mockQueryStatsReturn = {
            stats: {
                queryid: '-1234567890',
                avg_exec_time: 12.5,
                calls: 100,
                total_exec_time: 1250,
            },
            loading: false,
            error: null,
            refetch: vi.fn(),
        };
        mockOverviewReturn = {
            summary: null,
            loading: false,
            error: null,
            generatedAt: null,
            refresh: vi.fn(),
        };
    });

    it('shows the loading spinner while the initial fetch is pending', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderQueryDetail();

        expect(
            screen.getByLabelText('Loading query details'),
        ).toBeInTheDocument();
    });

    it('shows an error message when the fetch fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'Internal server error' }),
        );

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('Internal server error'),
            ).toBeInTheDocument();
        });
    });

    it('falls back to a default error message without an error body', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch query data: 503/),
            ).toBeInTheDocument();
        });
    });

    it('shows an error message when the fetch rejects', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByText('network down')).toBeInTheDocument();
        });
    });

    it('does not fetch when there is no authenticated user', () => {
        mockUser = null;

        renderQueryDetail();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('requests the drilled-into query by id', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('/api/v1/metrics/top-queries');
        expect(url).toContain('queryid=-1234567890');
        expect(url).toContain('limit=1');
    });

    // -- KPI tiles ----------------------------------------------------------

    it('renders the min and max execution time tiles', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('Min Time (All Time)'),
            ).toBeInTheDocument();
        });
        expect(screen.getByText('Max Time (All Time)')).toBeInTheDocument();
        // 2.5 ms and 512 ms via formatTime.
        expect(screen.getByText('2.5 ms')).toBeInTheDocument();
        expect(screen.getByText('512.0 ms')).toBeInTheDocument();
    });

    it('labels the lifetime and period averages distinctly', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('Mean Time (All Time)'),
            ).toBeInTheDocument();
        });
        expect(screen.getByText('Avg Time (Last 1h)')).toBeInTheDocument();
        expect(screen.getByText('20.0 ms')).toBeInTheDocument();
        expect(screen.getByText('12.5 ms')).toBeInTheDocument();
    });

    it('exposes an accessible label for the period average tile', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByLabelText('Avg Time (Last 1h): 12.5 ms'),
            ).toBeInTheDocument();
        });
    });

    it('renders a dash when the period average is null', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
        mockQueryStatsReturn = {
            stats: {
                queryid: '-1234567890',
                avg_exec_time: null,
                calls: 0,
                total_exec_time: 0,
            },
            loading: false,
            error: null,
            refetch: vi.fn(),
        };

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByLabelText('Avg Time (Last 1h): --'),
            ).toBeInTheDocument();
        });
        expect(
            screen.queryByLabelText(/Avg Time \(Last 1h\): (0|NaN)/),
        ).not.toBeInTheDocument();
    });

    it('renders a dash when the stats request returned nothing', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
        mockQueryStatsReturn = {
            stats: null,
            loading: false,
            error: null,
            refetch: vi.fn(),
        };

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByLabelText('Avg Time (Last 1h): --'),
            ).toBeInTheDocument();
        });
    });

    it('renders placeholder dashes when there is no query row', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });
        // With no row the charts have nothing to scope to.
        expect(mockUseQueryStats).toHaveBeenCalledWith(null);
    });

    it('renders the rows-per-call tile', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ calls: 100, rows: 250 })]),
        );

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByText('Avg Rows/Call')).toBeInTheDocument();
        });
        expect(screen.getByText('2.5')).toBeInTheDocument();
    });

    it('renders a dash for rows-per-call when there are no calls', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ calls: 0, rows: 0 })]),
        );

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByLabelText('Avg Rows/Call: --'),
            ).toBeInTheDocument();
        });
    });

    // -- Database user ------------------------------------------------------

    it('shows the database user that ran the statement', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByTestId('query-username')).toHaveTextContent(
                'app_user',
            );
        });
        expect(screen.getByText('Database User')).toBeInTheDocument();
    });

    it('shows Unknown when the role could not be resolved', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ username: '' })]),
        );

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByTestId('query-username')).toHaveTextContent(
                'Unknown',
            );
        });
    });

    // -- Charts -------------------------------------------------------------

    it('scopes both chart requests to the selected queryid', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(chartParamsFor('mean_exec_time')).toBeDefined();
        });
        expect(chartParamsFor('mean_exec_time')).toMatchObject({
            probeName: 'pg_stat_statements',
            connectionId: 1,
            databaseName: 'testdb',
            queryid: '-1234567890',
            timeRange: '1h',
            aggregation: 'avg',
        });
        expect(chartParamsFor('calls')).toMatchObject({
            queryid: '-1234567890',
            aggregation: 'sum',
        });
    });

    it('passes null chart params before the query row arrives', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderQueryDetail();

        expect(mockUseMetrics).toHaveBeenCalledWith(null);
    });

    it('renders both charts once metric data is available', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getAllByTestId('chart').length).toBe(2);
        });
        expect(
            screen.getByText('Execution Time Over Time'),
        ).toBeInTheDocument();
        expect(screen.getByText('Calls Over Time')).toBeInTheDocument();
    });

    it('shows chart loading spinners while metrics load', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
        mockMetricsReturn = {
            data: null,
            loading: true,
            error: null,
            refetch: vi.fn(),
        };

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getAllByLabelText('Loading chart').length).toBe(2);
        });
    });

    it('shows empty-state messages when charts have no data', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
        mockMetricsReturn = {
            data: [],
            loading: false,
            error: null,
            refetch: vi.fn(),
        };

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('No execution time data available'),
            ).toBeInTheDocument();
        });
        expect(
            screen.getByText('No call frequency data available'),
        ).toBeInTheDocument();
    });

    // -- Time range and refresh --------------------------------------------

    it('refetches the period stats when the time range changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        const { rerender } = renderQueryDetail();

        await waitFor(() => {
            expect(mockUseQueryStats).toHaveBeenCalledWith({
                connectionId: 1,
                queryid: '-1234567890',
                timeRange: '1h',
            });
        });

        mockTimeRange = '24h';
        rerender(
            <ThemeProvider theme={theme}>
                <QueryDetail
                    connectionId={1}
                    databaseName="testdb"
                    objectName="-1234567890"
                />
            </ThemeProvider>,
        );

        await waitFor(() => {
            expect(mockUseQueryStats).toHaveBeenCalledWith({
                connectionId: 1,
                queryid: '-1234567890',
                timeRange: '24h',
            });
        });
        expect(screen.getByText('Avg Time (Last 24h)')).toBeInTheDocument();
    });

    it('refetches the query row when the refresh trigger changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        const { rerender } = renderQueryDetail();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        mockRefreshTrigger = 1;
        rerender(
            <ThemeProvider theme={theme}>
                <QueryDetail
                    connectionId={1}
                    databaseName="testdb"
                    objectName="-1234567890"
                />
            </ThemeProvider>,
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    // -- Query text ---------------------------------------------------------

    it('truncates long query text and toggles on click', async () => {
        const longQuery = `SELECT ${'col, '.repeat(60)} FROM users`;
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ query: longQuery })]),
        );

        renderQueryDetail();

        const toggle = await screen.findByLabelText('Expand query text');
        expect(screen.getByText(/\.\.\.$/)).toBeInTheDocument();

        fireEvent.click(toggle);

        expect(
            screen.getByLabelText('Collapse query text'),
        ).toBeInTheDocument();
    });

    it('toggles the query text with the keyboard', async () => {
        const longQuery = `SELECT ${'col, '.repeat(60)} FROM users`;
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ query: longQuery })]),
        );

        renderQueryDetail();

        const toggle = await screen.findByLabelText('Expand query text');

        fireEvent.keyDown(toggle, { key: 'Enter' });
        expect(
            screen.getByLabelText('Collapse query text'),
        ).toBeInTheDocument();

        fireEvent.keyDown(
            screen.getByLabelText('Collapse query text'),
            { key: ' ' },
        );
        expect(
            screen.getByLabelText('Expand query text'),
        ).toBeInTheDocument();

        // Other keys leave the state alone.
        fireEvent.keyDown(
            screen.getByLabelText('Expand query text'),
            { key: 'a' },
        );
        expect(
            screen.getByLabelText('Expand query text'),
        ).toBeInTheDocument();
    });

    it('falls back to the object name when no row is returned', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        renderQueryDetail({ objectName: 'SELECT 1' });

        await waitFor(() => {
            expect(screen.getByText('SELECT 1')).toBeInTheDocument();
        });
    });

    // -- AI overview --------------------------------------------------------

    it('renders the AI overview summary when AI is enabled', async () => {
        mockAiEnabled = true;
        mockOverviewReturn = {
            summary: 'This query looks healthy.',
            loading: false,
            error: null,
            generatedAt: new Date(),
            refresh: vi.fn(),
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('This query looks healthy.'),
            ).toBeInTheDocument();
        });
        expect(screen.getByText('Updated just now')).toBeInTheDocument();
    });

    it.each([
        [5 * 60 * 1000, 'Updated 5 min ago'],
        [60 * 60 * 1000, 'Updated 1 hour ago'],
        [3 * 60 * 60 * 1000, 'Updated 3 hours ago'],
    ])(
        'formats an overview timestamp %i ms old',
        async (ageMs, expected) => {
            mockAiEnabled = true;
            mockOverviewReturn = {
                summary: 'This query looks healthy.',
                loading: false,
                error: null,
                generatedAt: new Date(Date.now() - ageMs),
                refresh: vi.fn(),
            };
            mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

            renderQueryDetail();

            await waitFor(() => {
                expect(screen.getByText(expected)).toBeInTheDocument();
            });
        },
    );

    it('falls back to a date for an old overview timestamp', async () => {
        mockAiEnabled = true;
        const generatedAt = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000);
        mockOverviewReturn = {
            summary: 'This query looks healthy.',
            loading: false,
            error: null,
            generatedAt,
            refresh: vi.fn(),
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText(
                    `Updated ${generatedAt.toLocaleDateString()}`,
                ),
            ).toBeInTheDocument();
        });
    });

    it('closes the full analysis dialog', async () => {
        mockAiEnabled = true;
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        const button = await screen.findByLabelText('Open full analysis');
        fireEvent.click(button);
        expect(screen.getByTestId('analysis-dialog')).toBeInTheDocument();

        fireEvent.click(screen.getByTestId('analysis-dialog-close'));

        expect(
            screen.queryByTestId('analysis-dialog'),
        ).not.toBeInTheDocument();
    });

    it('collapses and expands the AI overview', async () => {
        mockAiEnabled = true;
        mockOverviewReturn = {
            summary: 'This query looks healthy.',
            loading: false,
            error: null,
            generatedAt: null,
            refresh: vi.fn(),
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        const collapse = await screen.findByLabelText('Collapse AI Overview');
        fireEvent.click(collapse);

        expect(
            screen.getByLabelText('Expand AI Overview'),
        ).toBeInTheDocument();
    });

    it('shows skeletons whilst the AI overview loads', async () => {
        mockAiEnabled = true;
        mockOverviewReturn = {
            summary: null,
            loading: true,
            error: null,
            generatedAt: null,
            refresh: vi.fn(),
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByText('AI Overview')).toBeInTheDocument();
        });
        expect(
            screen.queryByText('Generating overview...'),
        ).not.toBeInTheDocument();
    });

    it('shows a placeholder when the AI overview is empty', async () => {
        mockAiEnabled = true;
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(
                screen.getByText('Generating overview...'),
            ).toBeInTheDocument();
        });
    });

    it('hides the AI overview when generation failed', async () => {
        mockAiEnabled = true;
        mockOverviewReturn = {
            summary: null,
            loading: false,
            error: 'model unavailable',
            generatedAt: null,
            refresh: vi.fn(),
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByTestId('plan-panel')).toBeInTheDocument();
        });
        expect(screen.queryByText('AI Overview')).not.toBeInTheDocument();
    });

    it('opens the full analysis dialog', async () => {
        mockAiEnabled = true;
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        const button = await screen.findByLabelText('Open full analysis');
        fireEvent.click(button);

        expect(screen.getByTestId('analysis-dialog')).toBeInTheDocument();
    });

    it('refreshes the AI overview on demand', async () => {
        mockAiEnabled = true;
        const refresh = vi.fn();
        mockOverviewReturn = {
            summary: 'This query looks healthy.',
            loading: false,
            error: null,
            generatedAt: new Date(),
            refresh,
        };
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        const button = await screen.findByLabelText('Refresh overview');
        fireEvent.click(button);

        expect(refresh).toHaveBeenCalled();
    });
});
