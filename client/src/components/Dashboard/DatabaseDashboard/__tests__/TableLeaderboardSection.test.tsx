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
import TableLeaderboardSection from '../TableLeaderboardSection';
import type { TableLeaderboardRow } from '../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockUser = vi.fn<() => { username: string } | null>(
    () => ({ username: 'test.user' })
);
vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser() }),
}));

const mockPushOverlay = vi.fn();
vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: 0,
        pushOverlay: mockPushOverlay,
    }),
}));

const mockAIEnabled = vi.fn(() => false);
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: mockAIEnabled() }),
}));

const mockHasCachedAnalysis = vi.fn(() => false);
vi.mock('../../../../hooks/useChartAnalysis', () => ({
    hasCachedAnalysis: () => mockHasCachedAnalysis(),
}));

vi.mock('../../../ChartAnalysisDialog', () => ({
    ChartAnalysisDialog: ({
        open,
        onClose,
        analysisContext,
        chartData,
    }: {
        open: boolean;
        onClose: () => void;
        analysisContext: { metricDescription: string };
        chartData: { series: Array<{ name: string }> };
    }) => (
        open ? (
            <div data-testid="analysis-dialog">
                <span>{analysisContext.metricDescription}</span>
                <span>{chartData.series.map(s => s.name).join(',')}</span>
                <button onClick={onClose}>Close analysis</button>
            </div>
        ) : null
    ),
}));

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn() },
}));

const mockApiFetch = vi.fn<(url: string) => Promise<Response>>();
vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: (url: string) => mockApiFetch(url),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a table row with every counter set to zero unless overridden. */
const tableRow = (
    overrides: Partial<TableLeaderboardRow>,
): TableLeaderboardRow => ({
    schemaname: 'public',
    relname: 'orders',
    n_live_tup: 0,
    n_dead_tup: 0,
    seq_scan: 0,
    seq_tup_read: 0,
    idx_scan: 0,
    idx_tup_fetch: 0,
    n_tup_ins: 0,
    n_tup_upd: 0,
    n_tup_del: 0,
    n_tup_hot_upd: 0,
    table_size: 8192,
    ...overrides,
});

const ORDERS = tableRow({
    relname: 'orders',
    n_live_tup: 500,
    n_dead_tup: 25,
    seq_scan: 12,
    idx_scan: 40,
    n_tup_ins: 100,
    n_tup_upd: 20,
    n_tup_del: 5,
});

const CUSTOMERS = tableRow({ schemaname: 'sales', relname: 'customers' });

/** A successful JSON response carrying the given body. */
const okResponse = (body: unknown): Response => ({
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
}) as unknown as Response;

/** A failed response whose body is the given JSON, or unparseable. */
const errorResponse = (status: number, body?: unknown): Response => ({
    ok: false,
    status,
    json: () => (body === undefined
        ? Promise.reject(new Error('not JSON'))
        : Promise.resolve(body)),
}) as unknown as Response;

/** The query parameters of the n-th apiFetch call. */
const fetchParams = (call = 0): URLSearchParams => {
    const url = mockApiFetch.mock.calls[call][0];
    expect(url.startsWith('/api/v1/metrics/latest?')).toBe(true);
    return new URLSearchParams(url.split('?')[1]);
};

const renderSection = () => render(
    <TableLeaderboardSection connectionId={7} databaseName="appdb" />
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TableLeaderboardSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ username: 'test.user' });
        mockAIEnabled.mockReturnValue(false);
        mockHasCachedAnalysis.mockReturnValue(false);
        mockApiFetch.mockResolvedValue(
            okResponse({ rows: [ORDERS, CUSTOMERS], total_count: 2 })
        );
    });

    it('asks the server to leave out system schemas (issue #499)', async () => {
        renderSection();

        await waitFor(() => { expect(mockApiFetch).toHaveBeenCalled(); });
        const params = fetchParams();
        expect(params.get('probe_name')).toBe('pg_stat_all_tables');
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('database_name')).toBe('appdb');
        expect(params.get('limit')).toBe('10');
        expect(params.get('order_by')).toBe('n_live_tup');
        expect(params.get('order')).toBe('desc');
        expect(params.get('exclude_system_schemas')).toBe('true');
    });

    it('renders the rows ranked by live rows', async () => {
        renderSection();

        expect(await screen.findByText('500 rows')).toBeInTheDocument();
        expect(screen.getByText('public.orders')).toBeInTheDocument();
        expect(screen.getByText('sales.customers')).toBeInTheDocument();
        expect(screen.getByText('25 dead')).toBeInTheDocument();
        expect(screen.getByText('0 rows')).toBeInTheDocument();
    });

    it.each([
        ['Seq Scans', 'seq_scan', '12', '40 idx scans'],
        ['Dead Tuples', 'n_dead_tup', '25', '4.8% dead'],
        ['Modifications', 'n_tup_ins', '125', '500 live'],
    ])('switches to the %s tab', async (label, orderBy, primary, secondary) => {
        renderSection();
        await screen.findByText('500 rows');

        fireEvent.click(
            screen.getByRole('tab', { name: `Sort tables by ${label}` })
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
        expect(fetchParams(1).get('order_by')).toBe(orderBy);
        expect(fetchParams(1).get('exclude_system_schemas')).toBe('true');
        expect(await screen.findByText(primary)).toBeInTheDocument();
        expect(screen.getByText(secondary)).toBeInTheDocument();
        expect(
            screen.getByRole('tab', { name: `Sort tables by ${label}` })
        ).toHaveAttribute('aria-selected', 'true');
    });

    it('shows a zero dead ratio for an empty table', async () => {
        renderSection();
        await screen.findByText('500 rows');

        fireEvent.click(
            screen.getByRole('tab', { name: 'Sort tables by Dead Tuples' })
        );

        expect(await screen.findByText('0.0% dead')).toBeInTheDocument();
    });

    it('changes tab from the keyboard', async () => {
        renderSection();
        await screen.findByText('500 rows');
        const tab = screen.getByRole('tab', {
            name: 'Sort tables by Seq Scans',
        });

        fireEvent.keyDown(tab, { key: 'a' });
        expect(tab).toHaveAttribute('aria-selected', 'false');

        fireEvent.keyDown(tab, { key: 'Enter' });
        expect(tab).toHaveAttribute('aria-selected', 'true');

        const modsTab = screen.getByRole('tab', {
            name: 'Sort tables by Modifications',
        });
        fireEvent.keyDown(modsTab, { key: ' ' });
        expect(modsTab).toHaveAttribute('aria-selected', 'true');
    });

    it('opens the table detail overlay on click and keypress', async () => {
        renderSection();
        const row = await screen.findByRole('button', {
            name: 'View details for table public.orders',
        });

        fireEvent.click(row);
        fireEvent.keyDown(row, { key: 'Enter' });
        fireEvent.keyDown(row, { key: ' ' });
        fireEvent.keyDown(row, { key: 'Escape' });

        expect(mockPushOverlay).toHaveBeenCalledTimes(3);
        expect(mockPushOverlay).toHaveBeenCalledWith({
            level: 'object',
            title: 'public.orders',
            entityId: 'public.orders',
            entityName: 'orders',
            objectType: 'table',
            connectionId: 7,
            databaseName: 'appdb',
            schemaName: 'public',
            objectName: 'orders',
        });
    });

    it('accepts a bare array response', async () => {
        mockApiFetch.mockResolvedValue(okResponse([CUSTOMERS]));
        renderSection();

        expect(await screen.findByText('sales.customers')).toBeInTheDocument();
        expect(screen.queryByText('public.orders')).not.toBeInTheDocument();
    });

    it('shows the empty state when no rows come back', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ total_count: 0 }));
        renderSection();

        expect(
            await screen.findByText('No table data available')
        ).toBeInTheDocument();
    });

    it('shows the server error message', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(400, { error: 'Probe "x" not found' })
        );
        renderSection();

        expect(
            await screen.findByText('Probe "x" not found')
        ).toBeInTheDocument();
        expect(
            screen.queryByText('No table data available')
        ).not.toBeInTheDocument();
    });

    it('falls back to the status when the error body is unreadable', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(502));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch table data: 502')
        ).toBeInTheDocument();
    });

    it('uses a generic message for an error without one', async () => {
        mockApiFetch.mockRejectedValue(new Error(''));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch table data')
        ).toBeInTheDocument();
    });

    it('does not fetch without a signed-in user', () => {
        mockUser.mockReturnValue(null);
        renderSection();

        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(screen.getByText('No table data available')).toBeInTheDocument();
    });

    it('opens and closes the AI analysis dialog', async () => {
        mockAIEnabled.mockReturnValue(true);
        mockHasCachedAnalysis.mockReturnValue(true);
        renderSection();
        await screen.findByText('500 rows');

        fireEvent.click(screen.getByRole('button', { name: 'AI Analysis' }));

        const dialog = screen.getByTestId('analysis-dialog');
        expect(dialog).toHaveTextContent('Table Leaderboard — Rows');
        expect(dialog).toHaveTextContent(
            'Live Rows,Dead Tuples,Sequential Scans,Index Scans,'
            + 'Inserts,Updates,Deletes'
        );

        fireEvent.click(screen.getByText('Close analysis'));
        expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();
    });

    it('hides the AI button when there is nothing to analyse', async () => {
        mockAIEnabled.mockReturnValue(true);
        mockApiFetch.mockResolvedValue(okResponse({ rows: [] }));
        renderSection();

        await screen.findByText('No table data available');
        expect(
            screen.queryByRole('button', { name: 'AI Analysis' })
        ).not.toBeInTheDocument();
    });
});
