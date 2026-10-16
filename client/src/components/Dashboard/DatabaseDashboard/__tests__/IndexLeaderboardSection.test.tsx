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
import IndexLeaderboardSection from '../IndexLeaderboardSection';
import type { IndexLeaderboardRow } from '../types';

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

const ORDERS_PKEY: IndexLeaderboardRow = {
    schemaname: 'public',
    relname: 'orders',
    indexrelname: 'orders_pkey',
    idx_scan: 40,
    idx_tup_read: 300,
    idx_tup_fetch: 250,
    index_size: 16384,
};

const CUSTOMERS_EMAIL: IndexLeaderboardRow = {
    schemaname: 'sales',
    relname: undefined as unknown as string,
    indexrelname: 'customers_email_idx',
    idx_scan: 0,
    idx_tup_read: 0,
    idx_tup_fetch: 0,
    index_size: 8192,
};

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
    <IndexLeaderboardSection connectionId={7} databaseName="appdb" />
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('IndexLeaderboardSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ username: 'test.user' });
        mockAIEnabled.mockReturnValue(false);
        mockHasCachedAnalysis.mockReturnValue(false);
        mockApiFetch.mockResolvedValue(
            okResponse({ rows: [ORDERS_PKEY, CUSTOMERS_EMAIL] })
        );
    });

    it('asks the server to leave out system schemas (issue #499)', async () => {
        renderSection();

        await waitFor(() => { expect(mockApiFetch).toHaveBeenCalled(); });
        const params = fetchParams();
        expect(params.get('probe_name')).toBe('pg_stat_all_indexes');
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('database_name')).toBe('appdb');
        expect(params.get('limit')).toBe('10');
        expect(params.get('order_by')).toBe('idx_tup_read');
        expect(params.get('order')).toBe('desc');
        expect(params.get('exclude_system_schemas')).toBe('true');
    });

    it('renders the rows ranked by reads', async () => {
        renderSection();

        expect(await screen.findByText('300 reads')).toBeInTheDocument();
        expect(screen.getByText('40 scans')).toBeInTheDocument();
        expect(screen.getByText('on orders')).toBeInTheDocument();
        expect(screen.getByText('on --')).toBeInTheDocument();
        expect(
            screen.getByTitle('sales.customers_email_idx')
        ).toBeInTheDocument();
    });

    it.each([
        ['Scans', 'idx_scan', 'desc'],
        ['Unused', 'idx_scan', 'asc'],
    ])('switches to the %s tab', async (label, orderBy, order) => {
        renderSection();
        await screen.findByText('300 reads');

        fireEvent.click(
            screen.getByRole('tab', { name: `Sort indexes by ${label}` })
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
        expect(fetchParams(1).get('order_by')).toBe(orderBy);
        expect(fetchParams(1).get('order')).toBe(order);
        expect(fetchParams(1).get('exclude_system_schemas')).toBe('true');
        expect(await screen.findByText('40')).toBeInTheDocument();
        expect(screen.getByText('300 reads')).toBeInTheDocument();
    });

    it('changes tab from the keyboard', async () => {
        renderSection();
        await screen.findByText('300 reads');
        const tab = screen.getByRole('tab', { name: 'Sort indexes by Scans' });

        fireEvent.keyDown(tab, { key: 'Tab' });
        expect(tab).toHaveAttribute('aria-selected', 'false');

        fireEvent.keyDown(tab, { key: 'Enter' });
        expect(tab).toHaveAttribute('aria-selected', 'true');

        const unusedTab = screen.getByRole('tab', {
            name: 'Sort indexes by Unused',
        });
        fireEvent.keyDown(unusedTab, { key: ' ' });
        expect(unusedTab).toHaveAttribute('aria-selected', 'true');
    });

    it('opens the index detail overlay on click and keypress', async () => {
        renderSection();
        const row = await screen.findByRole('button', {
            name: 'View details for index public.orders_pkey',
        });

        fireEvent.click(row);
        fireEvent.keyDown(row, { key: 'Enter' });
        fireEvent.keyDown(row, { key: ' ' });
        fireEvent.keyDown(row, { key: 'Escape' });

        expect(mockPushOverlay).toHaveBeenCalledTimes(3);
        expect(mockPushOverlay).toHaveBeenCalledWith({
            level: 'object',
            title: 'public.orders_pkey',
            entityId: 'public.orders_pkey',
            entityName: 'orders_pkey',
            objectType: 'index',
            connectionId: 7,
            databaseName: 'appdb',
            schemaName: 'public',
            objectName: 'orders_pkey',
        });
    });

    it('accepts a bare array response', async () => {
        mockApiFetch.mockResolvedValue(okResponse([ORDERS_PKEY]));
        renderSection();

        expect(await screen.findByText('on orders')).toBeInTheDocument();
        expect(screen.queryByText('on --')).not.toBeInTheDocument();
    });

    it('shows the empty state when no rows come back', async () => {
        mockApiFetch.mockResolvedValue(okResponse({}));
        renderSection();

        expect(
            await screen.findByText('No index data available')
        ).toBeInTheDocument();
    });

    it('shows the server error message', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(403, { error: 'Permission denied' })
        );
        renderSection();

        expect(await screen.findByText('Permission denied')).toBeInTheDocument();
    });

    it('falls back to the status when the error body is unreadable', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(500));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch index data: 500')
        ).toBeInTheDocument();
    });

    it('uses a generic message for an error without one', async () => {
        mockApiFetch.mockRejectedValue(new Error(''));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch index data')
        ).toBeInTheDocument();
    });

    it('does not fetch without a signed-in user', () => {
        mockUser.mockReturnValue(null);
        renderSection();

        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('opens and closes the AI analysis dialog', async () => {
        mockAIEnabled.mockReturnValue(true);
        mockHasCachedAnalysis.mockReturnValue(true);
        renderSection();
        await screen.findByText('300 reads');

        fireEvent.click(screen.getByRole('button', { name: 'AI Analysis' }));

        const dialog = screen.getByTestId('analysis-dialog');
        expect(dialog).toHaveTextContent('Index Leaderboard — Reads');
        expect(dialog).toHaveTextContent(
            'Index Scans,Tuples Read,Tuples Fetched'
        );

        fireEvent.click(screen.getByText('Close analysis'));
        expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();
    });
});
