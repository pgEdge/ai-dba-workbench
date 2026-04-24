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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TopQueriesSection from '../TopQueriesSection';
import { TopQueryRow } from '../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockPushOverlay = vi.fn();
vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: 0,
        pushOverlay: mockPushOverlay,
    }),
}));

vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({
        user: { id: 1, username: 'testuser' },
    }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mock TopQueryRow with sensible defaults. */
const makeQueryRow = (overrides: Partial<TopQueryRow> = {}): TopQueryRow => ({
    query: 'SELECT * FROM users WHERE id = 1',
    queryid: 'abc123',
    calls: 100,
    total_exec_time: 5000,
    mean_exec_time: 50,
    rows: 200,
    shared_blks_hit: 1000,
    shared_blks_read: 50,
    database_name: 'mydb',
    ...overrides,
});

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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TopQueriesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    it('renders "Top Queries" section title', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        expect(screen.getByText('Top Queries')).toBeInTheDocument();
    });

    it('shows loading spinner when fetching', () => {
        // Never resolve the fetch so we stay in loading state
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        expect(
            screen.getByLabelText('Loading queries'),
        ).toBeInTheDocument();
    });

    it('shows error message on fetch failure', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'Internal server error' }),
        );

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Internal server error'),
            ).toBeInTheDocument();
        });
    });

    it('shows "No query statistics available" when data is empty', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText(/No query statistics available/),
            ).toBeInTheDocument();
        });
    });

    it('renders the Database column header', async () => {
        const rows = [makeQueryRow()];
        mockApiFetch.mockResolvedValue(okResponse(rows));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Database')).toBeInTheDocument();
        });
    });

    it('renders database_name in each row', async () => {
        const rows = [
            makeQueryRow({ queryid: 'q1', database_name: 'appdb' }),
            makeQueryRow({ queryid: 'q2', database_name: 'analytics' }),
        ];
        mockApiFetch.mockResolvedValue(okResponse(rows));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('appdb')).toBeInTheDocument();
            expect(screen.getByText('analytics')).toBeInTheDocument();
        });
    });

    it('calls pushOverlay with databaseName when a row is clicked', async () => {
        const row = makeQueryRow({
            queryid: 'q1',
            database_name: 'proddb',
            query: 'SELECT 1',
        });
        mockApiFetch.mockResolvedValue(okResponse([row]));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('proddb')).toBeInTheDocument();
        });

        const rowButton = screen.getByRole('button', {
            name: /View details for query/,
        });
        fireEvent.click(rowButton);

        expect(mockPushOverlay).toHaveBeenCalledTimes(1);
        expect(mockPushOverlay).toHaveBeenCalledWith(
            expect.objectContaining({
                databaseName: 'proddb',
                connectionId: 1,
                objectType: 'query',
            }),
        );
    });

    it('shows database name in overlay title', async () => {
        const row = makeQueryRow({
            queryid: 'q1',
            database_name: 'proddb',
            query: 'SELECT 1',
        });
        mockApiFetch.mockResolvedValue(okResponse([row]));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('proddb')).toBeInTheDocument();
        });

        const rowButton = screen.getByRole('button', {
            name: /View details for query/,
        });
        fireEvent.click(rowButton);

        expect(mockPushOverlay).toHaveBeenCalledWith(
            expect.objectContaining({
                title: expect.stringContaining('proddb'),
            }),
        );
    });

    it('has the "Hide monitoring queries" toggle on by default', async () => {
        mockApiFetch.mockResolvedValue(okResponse([]));

        render(
            <TopQueriesSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        const toggle = screen.getByRole('checkbox');
        expect(toggle).toBeChecked();
    });
});
