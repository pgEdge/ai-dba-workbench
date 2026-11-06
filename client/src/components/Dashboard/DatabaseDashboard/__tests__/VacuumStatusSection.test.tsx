/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createTheme } from '@mui/material/styles';
import VacuumStatusSection from '../VacuumStatusSection';
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

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({ refreshTrigger: 0 }),
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
        chartData: { categories?: string[] };
    }) => (
        open ? (
            <div data-testid="analysis-dialog">
                <span>{analysisContext.metricDescription}</span>
                <span>{(chartData.categories ?? []).join(',')}</span>
                <button onClick={onClose}>Close analysis</button>
            </div>
        ) : null
    ),
}));

// The section starts collapsed and unmounts its content; render the
// content directly so these tests exercise the section itself.
vi.mock('../../CollapsibleSection', () => ({
    default: ({ children }: { children: React.ReactNode }) => (
        <div>{children}</div>
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

/** The default MUI palette, which the section uses without a provider. */
const palette = createTheme().palette;

const hoursAgo = (hours: number): string => new Date(
    Date.now() - hours * 3600_000
).toISOString();

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

// A high ratio on a table autovacuumed an hour ago: the age is fresh, but
// the ratio is critical (issue #806).
const BLOATED = tableRow({
    relname: 'pgbench_accounts',
    n_live_tup: 64,
    n_dead_tup: 936,
    last_autovacuum: hoursAgo(1),
});

// A low ratio on a table last vacuumed a month ago.
const TIDY = tableRow({
    relname: 'pgbench_branches',
    n_live_tup: 990,
    n_dead_tup: 10,
    last_vacuum: hoursAgo(24 * 30),
});

const MIDDLING = tableRow({
    schemaname: 'sales',
    relname: 'customers',
    n_live_tup: 90,
    n_dead_tup: 10,
    last_vacuum: hoursAgo(48),
    last_autovacuum: hoursAgo(3),
});

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

/** The query parameters of the first apiFetch call. */
const fetchParams = (): URLSearchParams => {
    const url = mockApiFetch.mock.calls[0][0];
    expect(url.startsWith('/api/v1/metrics/latest?')).toBe(true);
    return new URLSearchParams(url.split('?')[1]);
};

const renderSection = () => render(
    <VacuumStatusSection connectionId={7} databaseName="appdb" />
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('VacuumStatusSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ username: 'test.user' });
        mockAIEnabled.mockReturnValue(false);
        mockHasCachedAnalysis.mockReturnValue(false);
        mockApiFetch.mockResolvedValue(
            okResponse({ rows: [TIDY, MIDDLING, BLOATED] })
        );
    });

    it('asks the server to leave out system schemas (issue #806)', async () => {
        renderSection();

        await waitFor(() => { expect(mockApiFetch).toHaveBeenCalled(); });
        const params = fetchParams();
        expect(params.get('probe_name')).toBe('pg_stat_all_tables');
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('database_name')).toBe('appdb');
        expect(params.get('limit')).toBe('20');
        expect(params.get('order_by')).toBe('n_dead_tup');
        expect(params.get('order')).toBe('desc');
        expect(params.get('exclude_system_schemas')).toBe('true');
    });

    it('lists the tables by dead ratio, highest first', async () => {
        renderSection();

        await screen.findByText('93.6%');
        const names = screen.getAllByText(/^(public|sales)\./)
            .map(el => el.textContent);
        expect(names).toEqual([
            'public.pgbench_accounts',
            'sales.customers',
            'public.pgbench_branches',
        ]);
    });

    it('colors the dead ratio by the ratio, not the vacuum age', async () => {
        renderSection();

        expect(await screen.findByText('93.6%'))
            .toHaveStyle({ color: palette.error.main });
        expect(screen.getByText('10.0%'))
            .toHaveStyle({ color: palette.warning.main });
        expect(screen.getByText('1.0%'))
            .toHaveStyle({ color: palette.success.main });
    });

    it('colors each vacuum timestamp by its age', async () => {
        renderSection();
        await screen.findByText('93.6%');

        const never = screen.getAllByText('Never');
        expect(never).toHaveLength(2);
        never.forEach((el) => {
            expect(el).toHaveStyle({ color: palette.error.main });
        });
    });

    it('shows a zero ratio for an empty table', async () => {
        mockApiFetch.mockResolvedValue(okResponse([tableRow({})]));
        renderSection();

        expect(await screen.findByText('0.0%'))
            .toHaveStyle({ color: palette.success.main });
        expect(screen.getByText('public.orders')).toBeInTheDocument();
    });

    it('tolerates a row with missing names and counters', async () => {
        // The server returns whatever columns the probe stored, so a
        // row can arrive without some of the fields the type promises.
        const sparse = {
            last_autovacuum: hoursAgo(1),
        } as unknown as TableLeaderboardRow;
        mockApiFetch.mockResolvedValue(okResponse([sparse]));
        mockAIEnabled.mockReturnValue(true);
        renderSection();

        expect(await screen.findByText('public.unknown'))
            .toHaveAttribute('title', 'public.unknown');
        expect(screen.getByText('0.0%')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: 'AI Analysis' }));
        expect(screen.getByTestId('analysis-dialog'))
            .toHaveTextContent('Vacuum Status');
    });

    it('shows the empty state when no rows come back', async () => {
        mockApiFetch.mockResolvedValue(okResponse({}));
        renderSection();

        expect(
            await screen.findByText('No vacuum status data available')
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
    });

    it('falls back to the status when the error body is unreadable', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(502));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch vacuum status: 502')
        ).toBeInTheDocument();
    });

    it('uses a generic message for an error without one', async () => {
        mockApiFetch.mockRejectedValue(new Error(''));
        renderSection();

        expect(
            await screen.findByText('Failed to fetch vacuum status')
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
        await screen.findByText('93.6%');

        fireEvent.click(screen.getByRole('button', { name: 'AI Analysis' }));

        const dialog = screen.getByTestId('analysis-dialog');
        expect(dialog).toHaveTextContent('Vacuum Status');
        expect(dialog).toHaveTextContent(
            'public.pgbench_accounts,sales.customers,public.pgbench_branches'
        );

        fireEvent.click(screen.getByText('Close analysis'));
        expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();
    });
});
