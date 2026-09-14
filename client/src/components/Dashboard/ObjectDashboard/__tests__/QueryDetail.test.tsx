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
import { ThemeProvider, createTheme } from '@mui/material/styles';
import QueryDetail from '../QueryDetail';
import type { QueryDetailData } from '../types';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries } from '../../types';
import type { UseQueryOverviewReturn } from '../../../../hooks/useQueryOverview';

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
        currentOverlay: { connectionName: 'node-1' },
    }),
}));

let mockAiEnabled = false;
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: mockAiEnabled }),
}));

// Capture the parameters every useMetrics call receives so the tests can
// assert that the charts are scoped to the selected query.
const metricsParams: (MetricQueryParams | null)[] = [];
let mockMetricsReturn: UseMetricsReturn = {
    data: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
};
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => {
        metricsParams.push(params);
        return mockMetricsReturn;
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

vi.mock('../../../Chart', () => ({
    Chart: ({ title }: { title: string }) => (
        <div data-testid="chart">{title}</div>
    ),
}));

vi.mock('../../../QueryAnalysisDialog', () => ({
    QueryAnalysisDialog: ({ open }: { open: boolean }) => (
        open ? <div data-testid="analysis-dialog" /> : null
    ),
}));

vi.mock('../QueryPlanPanel', () => ({
    default: () => <div data-testid="plan-panel" />,
}));

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const makeQueryRow = (
    overrides: Partial<QueryDetailData> = {},
): QueryDetailData => ({
    queryid: '-1234567890123456789',
    query: 'SELECT 1',
    calls: 250,
    total_exec_time: 5000,
    mean_exec_time: 20,
    rows: 500,
    shared_blks_hit: 90,
    shared_blks_read: 10,
    ...overrides,
});

/** Series covering both charts on the page. */
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

const okResponse = (data: unknown): Partial<Response> => ({
    ok: true,
    json: () => Promise.resolve(data),
});

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
            objectName="-1234567890123456789"
            {...props}
        />
    </ThemeProvider>,
);

/** The parameters of the most recent non-null useMetrics call. */
const latestParams = (aggregation: string): MetricQueryParams | undefined => {
    const matches = metricsParams.filter(
        (p): p is MetricQueryParams => p !== null
            && p.aggregation === aggregation,
    );
    return matches[matches.length - 1];
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('QueryDetail', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        metricsParams.length = 0;
        mockUser = { id: 1, username: 'testuser' };
        mockAiEnabled = false;
        mockMetricsReturn = {
            data: fullMetricsData(),
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

    it('requests the selected query from the top-queries endpoint', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                '/api/v1/metrics/top-queries?connection_id=1'
                + '&queryid=-1234567890123456789&limit=1',
            );
        });
    });

    it('scopes both charts to the selected query id', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(latestParams('avg')?.queryId)
                .toBe('-1234567890123456789');
        });

        const execTimeParams = latestParams('avg');
        expect(execTimeParams?.probeName).toBe('pg_stat_statements');
        expect(execTimeParams?.databaseName).toBe('testdb');
        expect(execTimeParams?.metrics).toEqual([
            'mean_exec_time', 'min_exec_time', 'max_exec_time',
        ]);

        const callsParams = latestParams('sum');
        expect(callsParams?.queryId).toBe('-1234567890123456789');
        expect(callsParams?.metrics).toEqual(['calls']);
    });

    it('does not query metrics before the query row arrives', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderQueryDetail();

        expect(metricsParams.every(p => p === null)).toBe(true);
    });

    it('renders the statistics tiles and both charts', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getByText('250')).toBeInTheDocument();
        });
        expect(screen.getByText('Total Calls')).toBeInTheDocument();
        expect(screen.getByText('Avg Rows/Call')).toBeInTheDocument();
        expect(
            screen.getByText('Execution Time Over Time'),
        ).toBeInTheDocument();
        expect(screen.getByText('Calls Over Time')).toBeInTheDocument();
    });

    it('renders placeholder tiles when the query is not found', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });
    });

    it('renders a placeholder for a query with no calls', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeQueryRow({ calls: 0 })]),
        );

        renderQueryDetail();

        await waitFor(() => {
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });
    });

    it('toggles long query text between collapsed and expanded', async () => {
        const longQuery = `SELECT ${'column_name, '.repeat(20)} FROM t`;
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

        fireEvent.keyDown(
            screen.getByLabelText('Collapse query text'),
            { key: 'Enter' },
        );
        expect(
            screen.getByLabelText('Expand query text'),
        ).toBeInTheDocument();

        fireEvent.keyDown(
            screen.getByLabelText('Expand query text'),
            { key: ' ' },
        );
        expect(
            screen.getByLabelText('Collapse query text'),
        ).toBeInTheDocument();

        fireEvent.keyDown(
            screen.getByLabelText('Collapse query text'),
            { key: 'Escape' },
        );
        expect(
            screen.getByLabelText('Collapse query text'),
        ).toBeInTheDocument();
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

    it('shows empty-state messages when the charts have no data', async () => {
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

    it('shows the server error message when the fetch fails', async () => {
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

    it('falls back to a status error message without an error body', async () => {
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

    it('does not fetch without an authenticated user', () => {
        mockUser = null;

        renderQueryDetail();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    describe('with AI enabled', () => {
        beforeEach(() => {
            mockAiEnabled = true;
            mockApiFetch.mockResolvedValue(okResponse([makeQueryRow()]));
        });

        it('shows the generating placeholder before a summary arrives',
            async () => {
                renderQueryDetail();

                await waitFor(() => {
                    expect(
                        screen.getByText('Generating overview...'),
                    ).toBeInTheDocument();
                });
            });

        it('shows skeletons whilst the overview loads', async () => {
            mockOverviewReturn = {
                ...mockOverviewReturn,
                loading: true,
            };

            renderQueryDetail();

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });
            expect(
                screen.queryByText('Generating overview...'),
            ).not.toBeInTheDocument();
        });

        it('renders the summary and its relative timestamp', async () => {
            mockOverviewReturn = {
                ...mockOverviewReturn,
                summary: 'The query looks healthy.',
                generatedAt: new Date(),
            };

            renderQueryDetail();

            await waitFor(() => {
                expect(
                    screen.getByText('The query looks healthy.'),
                ).toBeInTheDocument();
            });
            expect(
                screen.getByText('Updated just now'),
            ).toBeInTheDocument();
        });

        it.each([
            [5 * 60 * 1000, 'Updated 5 min ago'],
            [3 * 60 * 60 * 1000, 'Updated 3 hours ago'],
            [61 * 60 * 1000, 'Updated 1 hour ago'],
        ])('formats an overview age of %i ms', async (ageMs, expected) => {
            mockOverviewReturn = {
                ...mockOverviewReturn,
                summary: 'Summary text.',
                generatedAt: new Date(Date.now() - ageMs),
            };

            renderQueryDetail();

            await waitFor(() => {
                expect(screen.getByText(expected)).toBeInTheDocument();
            });
        });

        it('formats an overview generated days ago as a date', async () => {
            const old = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000);
            mockOverviewReturn = {
                ...mockOverviewReturn,
                summary: 'Summary text.',
                generatedAt: old,
            };

            renderQueryDetail();

            await waitFor(() => {
                expect(
                    screen.getByText(
                        `Updated ${old.toLocaleDateString()}`,
                    ),
                ).toBeInTheDocument();
            });
        });

        it('refreshes the overview on demand', async () => {
            const refresh = vi.fn();
            mockOverviewReturn = {
                ...mockOverviewReturn,
                summary: 'Summary text.',
                generatedAt: new Date(),
                refresh,
            };

            renderQueryDetail();

            const button = await screen.findByLabelText('Refresh overview');
            fireEvent.click(button);

            expect(refresh).toHaveBeenCalled();
        });

        it('collapses and expands the overview panel', async () => {
            renderQueryDetail();

            const collapse = await screen.findByLabelText(
                'Collapse AI Overview',
            );
            fireEvent.click(collapse);

            const expand = await screen.findByLabelText('Expand AI Overview');
            fireEvent.click(expand);

            expect(
                await screen.findByLabelText('Collapse AI Overview'),
            ).toBeInTheDocument();
        });

        it('opens the full analysis dialog', async () => {
            renderQueryDetail();

            const button = await screen.findByLabelText('Open full analysis');
            fireEvent.click(button);

            expect(
                await screen.findByTestId('analysis-dialog'),
            ).toBeInTheDocument();
        });

        it('hides the overview panel when the overview errors', async () => {
            mockOverviewReturn = {
                ...mockOverviewReturn,
                error: 'model unavailable',
            };

            renderQueryDetail();

            await waitFor(() => {
                expect(screen.getByText('Total Calls')).toBeInTheDocument();
            });
            expect(
                screen.queryByText('AI Overview'),
            ).not.toBeInTheDocument();
        });
    });
});
