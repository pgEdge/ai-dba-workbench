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
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ComparativeChartsSection from '../ComparativeChartsSection';

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

interface CapturedChart {
    title: string;
    data: {
        categories: string[];
        series: { name: string; data: number[] }[];
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
    cache_hit_ratio: { current: 0.9876 },
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
                + '?connection_ids=3,4&time_range=24h',
            );
        });
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

        expect(chartByTitle('Cache Hit Ratio (%)').data.series[0].data)
            .toEqual([98.76]);
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
        expect(chartByTitle('Cache Hit Ratio (%)').data.series[0].data)
            .toEqual([0]);
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
