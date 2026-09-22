/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ComparativeChartsSection from '../ComparativeChartsSection';
import type { TimeRangeState } from '../../types';

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

vi.mock('../../../../contexts/useClusterData', () => ({
    useClusterData: () => ({ lastRefresh: 0 }),
}));

let mockTimeRange: TimeRangeState = { range: '24h' };
vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({ timeRange: mockTimeRange }),
}));

interface CapturedChart {
    title: string;
    data: {
        categories: string[];
        series: { name: string; data: (number | null)[] }[];
    };
}

const capturedCharts: CapturedChart[] = [];

vi.mock('../../../Chart', () => ({
    Chart: (props: CapturedChart) => {
        capturedCharts.push({ title: props.title, data: props.data });
        return <div data-testid="chart">{props.title}</div>;
    },
}));

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

/** Build a performance-summary connection payload. */
const makeConnection = (
    overrides: Record<string, unknown> = {},
): Record<string, unknown> => ({
    connection_id: 1,
    connection_name: 'node-1',
    cache_hit_ratio: { current: 98.7654 },
    transactions: { commits_per_sec: 12.345, rollback_percent: 1.234 },
    active_connections: 17,
    ...overrides,
});

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

const renderSection = (serverIds: number[] = [1, 2]) => render(
    <ThemeProvider theme={theme}>
        <ComparativeChartsSection serverIds={serverIds} />
    </ThemeProvider>,
);

/** Find the most recent render of a chart by its title. */
const chartByTitle = (title: string): CapturedChart => {
    const matches = capturedCharts.filter(c => c.title === title);
    return matches[matches.length - 1];
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ComparativeChartsSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        capturedCharts.length = 0;
        mockUser = { id: 1, username: 'testuser' };
        mockTimeRange = { range: '24h' };
    });

    it('shows the loading spinner while the initial fetch is pending', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderSection();

        expect(screen.getByLabelText('Loading charts')).toBeInTheDocument();
    });

    it('requests the performance summary for every server', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ connections: [makeConnection()] }),
        );

        renderSection([3, 4]);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                '/api/v1/metrics/performance-summary'
                + '?connection_ids=3%2C4&time_range=24h',
            );
        });
    });

    it('follows the selected preset window', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ connections: [makeConnection()] }),
        );
        mockTimeRange = { range: '6h' };

        renderSection([3]);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                '/api/v1/metrics/performance-summary'
                + '?connection_ids=3&time_range=6h',
            );
        });
    });

    it('sends both bounds for a custom window', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ connections: [makeConnection()] }),
        );
        mockTimeRange = {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        };

        renderSection([3]);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('time_range=custom');
        expect(url).toContain('time_start=');
        expect(url).toContain('time_end=');
    });

    it('makes no request when a custom bound is missing', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ connections: [makeConnection()] }),
        );
        mockTimeRange = {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
        };

        renderSection([3]);

        await waitFor(() => {
            expect(mockApiFetch).not.toHaveBeenCalled();
        });
    });

    it('discards a response for a superseded time range', async () => {
        // Hold each request open so that the window can be changed
        // whilst the first one is still in flight.
        const resolvers: Record<string, (body: unknown) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            const range = new URLSearchParams(url.split('?')[1] ?? '')
                .get('time_range') ?? '';
            return new Promise(resolve => {
                resolvers[range] = (body: unknown) => resolve(
                    okResponse(body) as Response,
                );
            });
        });

        mockTimeRange = { range: '24h' };
        const { rerender } = renderSection([3]);
        await waitFor(() => {
            expect(resolvers['24h']).toBeDefined();
        });

        mockTimeRange = { range: '1h' };
        rerender(
            <ThemeProvider theme={theme}>
                <ComparativeChartsSection serverIds={[3]} />
            </ThemeProvider>,
        );
        await waitFor(() => {
            expect(resolvers['1h']).toBeDefined();
        });

        // The newly selected window answers first.
        await act(async () => {
            resolvers['1h']({
                connections: [makeConnection({ connection_name: 'hour' })],
            });
        });
        await waitFor(() => {
            expect(chartByTitle('Connection Count').data.categories)
                .toEqual(['hour']);
        });

        // The superseded request then arrives late and must not
        // overwrite the metrics belonging to the current window.
        await act(async () => {
            resolvers['24h']({
                connections: [makeConnection({ connection_name: 'day' })],
            });
        });
        expect(chartByTitle('Connection Count').data.categories)
            .toEqual(['hour']);
    });

    it('discards a pending response when a custom bound is cleared',
        async () => {
            let resolvePending: ((body: unknown) => void) | null = null;
            mockApiFetch.mockImplementation(() => new Promise(resolve => {
                resolvePending = (body: unknown) => resolve(
                    okResponse(body) as Response,
                );
            }));

            mockTimeRange = { range: '24h' };
            const { rerender } = renderSection([3]);
            await waitFor(() => {
                expect(resolvePending).not.toBeNull();
            });

            // A half-entered custom range makes no request of its own,
            // so the in-flight one has to be abandoned explicitly.
            mockTimeRange = {
                range: 'custom',
                customStart: '2026-09-01T00:00:00Z',
            };
            rerender(
                <ThemeProvider theme={theme}>
                    <ComparativeChartsSection serverIds={[3]} />
                </ThemeProvider>,
            );
            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledTimes(1);
            });

            await act(async () => {
                resolvePending!({
                    connections: [makeConnection({ connection_name: 'day' })],
                });
            });

            expect(screen.getByText(
                'No performance data available for comparison.',
            )).toBeInTheDocument();
        });

    it('plots the reported connection count for each server', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            connections: [
                makeConnection(),
                makeConnection({
                    connection_id: 2,
                    connection_name: 'node-2',
                    active_connections: 4,
                }),
            ],
        }));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Connection Count')).toBeInTheDocument();
        });

        const chart = chartByTitle('Connection Count');
        expect(chart.data.categories).toEqual(['node-1', 'node-2']);
        expect(chart.data.series[0].data).toEqual([17, 4]);
    });

    it('falls back to zero when the connection count is missing', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            connections: [makeConnection({ active_connections: undefined })],
        }));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Connection Count')).toBeInTheDocument();
        });

        expect(chartByTitle('Connection Count').data.series[0].data)
            .toEqual([0]);
    });

    it('rounds the other comparative series', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ connections: [makeConnection()] }),
        );

        renderSection();

        await waitFor(() => {
            expect(screen.getAllByTestId('chart').length).toBe(4);
        });

        // The server sends a percentage, not a fraction, so it is only
        // rounded, never rescaled.
        expect(chartByTitle('Cache Hit Ratio (%)').data.series[0].data)
            .toEqual([98.77]);
        expect(
            chartByTitle('Transaction Rate (commits/sec)').data.series[0].data,
        ).toEqual([12.35]);
        expect(chartByTitle('Rollback Rate (%)').data.series[0].data)
            .toEqual([1.23]);
    });

    it('defaults missing metrics and names', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            connections: [{ connection_id: 9 }],
        }));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Connection Count')).toBeInTheDocument();
        });

        const chart = chartByTitle('Connection Count');
        expect(chart.data.categories).toEqual(['Server 9']);
        expect(chart.data.series[0].data).toEqual([0]);
        // A missing cache hit ratio is unknown, not 0%, so the bar is
        // left empty.
        expect(chartByTitle('Cache Hit Ratio (%)').data.series[0].data)
            .toEqual([null]);
    });

    it('keeps a null cache hit ratio null rather than drawing it at 0', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            connections: [
                makeConnection({
                    connection_id: 1,
                    cache_hit_ratio: { current: null, time_series: [] },
                }),
                makeConnection({
                    connection_id: 2,
                    connection_name: 'node-2',
                    cache_hit_ratio: { current: 95, time_series: [] },
                }),
            ],
        }));

        renderSection();

        await waitFor(() => {
            expect(screen.getAllByTestId('chart').length).toBe(4);
        });

        expect(chartByTitle('Cache Hit Ratio (%)').data.series[0].data)
            .toEqual([null, 95]);
    });

    it('shows the empty state when no connections are returned', async () => {
        mockApiFetch.mockResolvedValue(okResponse({}));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(
                    'No performance data available for comparison.',
                ),
            ).toBeInTheDocument();
        });
    });

    it('shows the server error message when the fetch fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'Internal server error' }),
        );

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText('Internal server error'),
            ).toBeInTheDocument();
        });
    });

    it('falls back to a status error message without an error body', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch data: 503/),
            ).toBeInTheDocument();
        });
    });

    it('shows an error message when the fetch rejects', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('network down')).toBeInTheDocument();
        });
    });

    it('does not fetch without an authenticated user', () => {
        mockUser = null;

        renderSection();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('does not fetch when no servers are selected', () => {
        renderSection([]);

        expect(mockApiFetch).not.toHaveBeenCalled();
    });
});

describe('ComparativeChartsSection connection_ids batching', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        capturedCharts.length = 0;
        mockUser = { id: 1, username: 'testuser' };
    });

    const ids = (count: number): number[] =>
        Array.from({ length: count }, (_, i) => i + 1);

    const requestedIds = (url: string): number[] =>
        (new URLSearchParams(url.split('?')[1]).get('connection_ids') ?? '')
            .split(',')
            .map(Number);

    it('batches an over-cap cluster and merges the members', async () => {
        mockApiFetch.mockImplementation((url: string) => Promise.resolve(
            okResponse({
                connections: requestedIds(url).map(id => makeConnection({
                    connection_id: id,
                    connection_name: `node-${id}`,
                })),
            }),
        ));

        renderSection(ids(250));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(3);
        });

        const urls = mockApiFetch.mock.calls.map(call => call[0] as string);
        expect(requestedIds(urls[0])).toEqual(ids(250).slice(0, 100));
        expect(requestedIds(urls[1])).toEqual(ids(250).slice(100, 200));
        expect(requestedIds(urls[2])).toEqual(ids(250).slice(200));

        await waitFor(() => {
            expect(chartByTitle('Transaction Rate (commits/sec)').data.categories)
                .toHaveLength(250);
        });
        expect(chartByTitle('Transaction Rate (commits/sec)').data.categories[0])
            .toBe('node-1');
        expect(chartByTitle('Transaction Rate (commits/sec)').data.categories[249])
            .toBe('node-250');
    });

    it('shows an error when one batch fails', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            Promise.resolve(requestedIds(url)[0] === 101
                ? errorResponse(500, { error: 'batch failed' })
                : okResponse({ connections: [makeConnection()] })),
        );

        renderSection(ids(250));

        await waitFor(() => {
            expect(screen.getByText('batch failed')).toBeInTheDocument();
        });
        expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
    });
});
