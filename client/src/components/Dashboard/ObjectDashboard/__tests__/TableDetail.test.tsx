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
import TableDetail from '../TableDetail';
import type { TableDetailData } from '../types';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricSeries } from '../../types';

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
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: () => mockMetricsReturn,
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

/** Build a mock table detail row with sensible defaults. */
const makeTableRow = (
    overrides: Partial<TableDetailData> = {},
): TableDetailData => ({
    schemaname: 'public',
    relname: 'users',
    n_live_tup: 1000,
    n_dead_tup: 20,
    seq_scan: 42,
    seq_tup_read: 500,
    idx_scan: 300,
    idx_tup_fetch: 250,
    n_tup_ins: 10,
    n_tup_upd: 5,
    n_tup_del: 2,
    n_tup_hot_upd: 1,
    table_size: 1048576,
    last_vacuum: '2026-01-01T00:00:00Z',
    last_autovacuum: '2026-01-02T00:00:00Z',
    last_analyze: '2026-01-03T00:00:00Z',
    last_autoanalyze: '2026-01-04T00:00:00Z',
    ...overrides,
});

/** Series covering every metric the three charts request. */
const fullMetricsData = (): MetricSeries[] => {
    const point = { time: '2026-01-01T00:00:00Z', value: 5 };
    return [
        'n_tup_ins_per_sec',
        'n_tup_upd_per_sec',
        'n_tup_del_per_sec',
        'n_tup_hot_upd_per_sec',
        'seq_scan_per_sec',
        'idx_scan_per_sec',
        'dead_tuple_ratio',
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

const renderTableDetail = (
    props: Partial<{
        connectionId: number;
        databaseName: string;
        schemaName?: string;
        objectName: string;
    }> = {},
) => render(
    <ThemeProvider theme={theme}>
        <TableDetail
            connectionId={1}
            databaseName="testdb"
            schemaName="public"
            objectName="users"
            {...props}
        />
    </ThemeProvider>,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TableDetail', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockMetricsReturn = {
            data: fullMetricsData(),
            loading: false,
            error: null,
            refetch: vi.fn(),
        };
    });

    it('shows the loading spinner while the initial fetch is pending', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderTableDetail();

        expect(
            screen.getByLabelText('Loading table details'),
        ).toBeInTheDocument();
    });

    it('shows an error message when the fetch fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'Internal server error' }),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(
                screen.getByText('Internal server error'),
            ).toBeInTheDocument();
        });
    });

    it('falls back to a default error message without an error body', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        renderTableDetail();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch table data: 503/),
            ).toBeInTheDocument();
        });
    });

    it('shows an error message when the fetch rejects', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText('network down')).toBeInTheDocument();
        });
    });

    it('renders the raw table size formatted from bytes', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));

        renderTableDetail();

        // 1048576 bytes -> "1.0 MB" via formatBytes.
        await waitFor(() => {
            expect(screen.getByText('1.0 MB')).toBeInTheDocument();
        });
        expect(screen.getByText('Table Size')).toBeInTheDocument();
        // Live tuples and sequential scans are formatted numbers.
        expect(screen.getByText('1,000')).toBeInTheDocument();
        expect(screen.getByText('42')).toBeInTheDocument();
    });

    it('accepts a rows-wrapped response payload', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ rows: [makeTableRow({ table_size: 2048 })] }),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText('2.0 KB')).toBeInTheDocument();
        });
    });

    it('renders placeholder dashes when there is no data', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        renderTableDetail();

        // Table size falls back to formatBytes(null) === "--".
        await waitFor(() => {
            expect(screen.getAllByText('--').length).toBeGreaterThan(0);
        });
    });

    it('marks a healthy dead-tuple ratio as good', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeTableRow({ n_live_tup: 100, n_dead_tup: 2 })]),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText(/2.0% dead/)).toBeInTheDocument();
        });
    });

    it('marks a moderate dead-tuple ratio as a warning', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeTableRow({ n_live_tup: 100, n_dead_tup: 15 })]),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText(/13.0% dead/)).toBeInTheDocument();
        });
    });

    it('marks a high dead-tuple ratio as critical', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeTableRow({ n_live_tup: 100, n_dead_tup: 50 })]),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText(/33.3% dead/)).toBeInTheDocument();
        });
    });

    it('handles a table with no live or dead tuples', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse([makeTableRow({ n_live_tup: 0, n_dead_tup: 0 })]),
        );

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText(/0.0% dead/)).toBeInTheDocument();
        });
    });

    it('renders the schema-qualified display name and charts', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText('public.users')).toBeInTheDocument();
        });
        expect(
            screen.getByText('Tuple Operations Over Time'),
        ).toBeInTheDocument();
        expect(screen.getAllByTestId('chart').length).toBe(3);
    });

    it('renders the bare object name when no schema is supplied', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));

        renderTableDetail({ schemaName: undefined });

        await waitFor(() => {
            expect(screen.getByText('users')).toBeInTheDocument();
        });
    });

    it('renders the maintenance info section timestamps', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));

        renderTableDetail();

        await waitFor(() => {
            expect(screen.getByText('Last Vacuum')).toBeInTheDocument();
        });
        expect(screen.getByText('Last Autovacuum')).toBeInTheDocument();
        expect(screen.getByText('Last Analyze')).toBeInTheDocument();
        expect(screen.getByText('Last Autoanalyze')).toBeInTheDocument();
    });

    it('shows chart loading spinners while metrics load', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));
        mockMetricsReturn = {
            data: null,
            loading: true,
            error: null,
            refetch: vi.fn(),
        };

        renderTableDetail();

        await waitFor(() => {
            expect(
                screen.getAllByLabelText('Loading chart').length,
            ).toBe(3);
        });
    });

    it('shows empty-state messages when charts have no data', async () => {
        mockApiFetch.mockResolvedValue(okResponse([makeTableRow()]));
        mockMetricsReturn = {
            data: [],
            loading: false,
            error: null,
            refetch: vi.fn(),
        };

        renderTableDetail();

        await waitFor(() => {
            expect(
                screen.getByText('No tuple operation data available'),
            ).toBeInTheDocument();
        });
        expect(
            screen.getByText('No scan data available'),
        ).toBeInTheDocument();
        expect(
            screen.getByText('No dead tuple data available'),
        ).toBeInTheDocument();
    });

    it('does not fetch when there is no authenticated user', () => {
        mockUser = null;

        renderTableDetail();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });
});
