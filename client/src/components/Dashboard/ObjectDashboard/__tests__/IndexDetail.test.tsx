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
import IndexDetail from '../IndexDetail';
import type { IndexDetailData } from '../types';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricSeries, MetricQueryParams } from '../../types';

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
    }),
}));

let mockMetricsReturn: UseMetricsReturn = {
    data: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
};
// Spy that captures the params IndexDetail hands to useMetrics for the
// Scan Activity chart, so the routing tests can assert the query is scoped
// to the index (indexName) rather than misrouted into tableName.
const mockUseMetrics = vi.fn();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => {
        mockUseMetrics(params);
        return mockMetricsReturn;
    },
}));

vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: false }),
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

/** Build a mock index detail row with sensible defaults. */
const makeIndexRow = (
    overrides: Partial<IndexDetailData> = {},
): IndexDetailData => ({
    schemaname: 'public',
    relname: 'users',
    indexrelname: 'users_pkey',
    idx_scan: 500,
    idx_tup_read: 4000,
    idx_tup_fetch: 3500,
    index_size: 65536,
    ...overrides,
});

/** Series covering the single metric the scan chart requests. */
const scanMetricsData = (): MetricSeries[] => [
    {
        name: 'idx_scan_per_sec',
        metric: 'idx_scan_per_sec',
        data: [{ time: '2026-01-01T00:00:00Z', value: 3 }],
    },
] as MetricSeries[];

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

const renderIndexDetail = (
    props: Partial<{
        connectionId: number;
        databaseName: string;
        schemaName?: string;
        objectName: string;
    }> = {},
) => render(
    <ThemeProvider theme={theme}>
        <IndexDetail
            connectionId={1}
            databaseName="testdb"
            schemaName="public"
            objectName="users_pkey"
            {...props}
        />
    </ThemeProvider>,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('IndexDetail', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockMetricsReturn = {
            data: scanMetricsData(),
            loading: false,
            error: null,
            refetch: vi.fn(),
        };
    });

    it('shows the loading spinner while the initial fetch is pending', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderIndexDetail();

        expect(
            screen.getByLabelText('Loading index details'),
        ).toBeInTheDocument();
    });

    it('shows an error message when the fetch fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'boom from server' }),
        );

        renderIndexDetail();

        await waitFor(() => {
            expect(screen.getByText('boom from server')).toBeInTheDocument();
        });
    });

    it('falls back to a default error message without an error body', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(404));

        renderIndexDetail();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch index data: 404/),
            ).toBeInTheDocument();
        });
    });

    it('shows an error message when the fetch rejects', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));

        renderIndexDetail();

        await waitFor(() => {
            expect(screen.getByText('network down')).toBeInTheDocument();
        });
    });

    it('renders the raw index size formatted from bytes', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeIndexRow()]));

        renderIndexDetail();

        // 65536 bytes -> "64.0 KB" via formatBytes.
        await waitFor(() => {
            expect(screen.getByText('64.0 KB')).toBeInTheDocument();
        });
        expect(screen.getByText('Index Size')).toBeInTheDocument();
        // Scans and tuple counts are formatted numbers.
        expect(screen.getByText('500')).toBeInTheDocument();
        expect(screen.getByText('4,000')).toBeInTheDocument();
        expect(screen.getByText('3,500')).toBeInTheDocument();
    });

    it('accepts a rows-wrapped response payload', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ rows: [makeIndexRow({ index_size: 2048 })] }),
        );

        renderIndexDetail();

        await waitFor(() => {
            expect(screen.getByText('2.0 KB')).toBeInTheDocument();
        });
    });

    it('renders placeholder dashes when there is no data', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        renderIndexDetail();

        // Index size falls back to formatBytes(null) === "--".
        await waitFor(() => {
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });
    });

    it('renders the parent table name and chart', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeIndexRow()]));

        renderIndexDetail();

        await waitFor(() => {
            expect(screen.getByText('public.users_pkey')).toBeInTheDocument();
        });
        expect(screen.getByText(/on table users/)).toBeInTheDocument();
        expect(
            screen.getByText('Index Scan Activity Over Time'),
        ).toBeInTheDocument();
    });

    it('renders the bare object name when no schema is supplied', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeIndexRow()]));

        renderIndexDetail({ schemaName: undefined });

        await waitFor(() => {
            expect(screen.getByText('users_pkey')).toBeInTheDocument();
        });
    });

    it('shows the chart loading spinner while metrics load', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeIndexRow()]));
        mockMetricsReturn = {
            data: null,
            loading: true,
            error: null,
            refetch: vi.fn(),
        };

        renderIndexDetail();

        await waitFor(() => {
            expect(screen.getByLabelText('Loading chart')).toBeInTheDocument();
        });
    });

    it('shows an empty-state message when the chart has no data', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeIndexRow()]));
        mockMetricsReturn = {
            data: [],
            loading: false,
            error: null,
            refetch: vi.fn(),
        };

        renderIndexDetail();

        await waitFor(() => {
            expect(
                screen.getByText('No index scan data available'),
            ).toBeInTheDocument();
        });
    });

    it('does not fetch when there is no authenticated user', () => {
        mockUser = null;

        renderIndexDetail();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    // -----------------------------------------------------------------------
    // Scan Activity chart query routing (guards PR #353's fix)
    //
    // The regression these guard: the index name was previously sent to the
    // chart query as tableName, which filtered relname = the index name and
    // returned no rows. The fix scopes the query via indexName instead.
    // -----------------------------------------------------------------------

    it('scopes the chart query to the index via indexName', () => {
        // A never-resolving latest-row fetch keeps the component in its
        // pending state so no async updates fire; useMetrics is still
        // invoked with the chart params during the initial render.
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderIndexDetail();

        const params = mockUseMetrics.mock.calls[0][0] as MetricQueryParams;
        expect(params.probeName).toBe('pg_stat_all_indexes');
        expect(params.indexName).toBe('users_pkey');
        expect(params.metrics).toEqual(['idx_scan_per_sec']);
        expect(params.schemaName).toBe('public');
    });

    it('does not misroute the index name into tableName', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderIndexDetail();

        const params = mockUseMetrics.mock.calls[0][0] as MetricQueryParams;
        expect(params.tableName).toBeUndefined();
    });
});
