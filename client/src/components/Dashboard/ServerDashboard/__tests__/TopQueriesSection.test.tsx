/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TopQueriesSection from '../TopQueriesSection';
import type { TopQueryRow } from '../types';

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

/** Build a Headers-like object carrying an X-Total-Count value. */
const totalCountHeaders = (value: string): Headers => ({
    get: (name: string) => (
        name.toLowerCase() === 'x-total-count' ? value : null
    ),
} as unknown as Headers);

/**
 * Create a successful Response-like object. When `totalCount` is
 * omitted the response carries no headers at all, mimicking a server
 * that does not advertise X-Total-Count.
 */
const okResponse = (
    data: unknown,
    totalCount?: string,
): Partial<Response> => ({
    ok: true,
    headers: totalCount === undefined
        ? undefined
        : totalCountHeaders(totalCount),
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

/** Generate `count` distinct query rows. */
const makeRows = (count: number, prefix = 'q'): TopQueryRow[] =>
    Array.from({ length: count }, (_, i) => makeQueryRow({
        queryid: `${prefix}${i}`,
        query: `SELECT ${i}`,
    }));

interface FetchSetup {
    /** Rows returned for each top-queries request, keyed by offset. */
    pages?: Record<string, TopQueryRow[]>;
    /** Rows returned when no offset-specific entry matches. */
    rows?: TopQueryRow[];
    /** Value advertised in X-Total-Count; omit to send no header. */
    totalCount?: string;
    /** Database names reported by the database-summaries endpoint. */
    databases?: string[];
}

/** Extract a query-string parameter from a request URL. */
const paramOf = (url: string, name: string): string | null =>
    new URLSearchParams(url.split('?')[1] ?? '').get(name);

/** Route apiFetch calls to the top-queries or summaries responses. */
const setupFetch = (setup: FetchSetup = {}): void => {
    mockApiFetch.mockImplementation((url: string) => {
        if (url.includes('database-summaries')) {
            return Promise.resolve(okResponse({
                databases: (setup.databases ?? []).map(name => ({
                    database_name: name,
                })),
            }));
        }
        const offset = paramOf(url, 'offset') ?? '0';
        const rows = setup.pages?.[offset] ?? setup.rows ?? [];
        return Promise.resolve(okResponse(rows, setup.totalCount));
    });
};

/** All top-queries request URLs seen so far, in call order. */
const topQueryUrls = (): string[] => mockApiFetch.mock.calls
    .map(call => call[0] as string)
    .filter(url => url.includes('top-queries'));

/** The most recent top-queries request URL. */
const lastTopQueryUrl = (): string => {
    const urls = topQueryUrls();
    return urls[urls.length - 1];
};

const renderSection = () => render(
    <TopQueriesSection
        connectionId={1}
        connectionName="Test Server"
    />,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TopQueriesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    it('renders "Top Queries" section title', async () => {
        setupFetch();

        renderSection();

        expect(screen.getByText('Top Queries')).toBeInTheDocument();
    });

    it('shows loading spinner when fetching', () => {
        // Never resolve the fetch so we stay in loading state
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderSection();

        expect(
            screen.getByLabelText('Loading queries'),
        ).toBeInTheDocument();
    });

    it('shows error message on fetch failure', async () => {
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

    it('shows a generic error when the failure carries no message', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch top queries: 503/),
            ).toBeInTheDocument();
        });
    });

    it('handles an error body that cannot be parsed', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 502,
            json: () => Promise.reject(new Error('bad json')),
        } as Partial<Response>);

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch top queries: 502/),
            ).toBeInTheDocument();
        });
    });

    it('shows "No query statistics available" when data is empty', async () => {
        setupFetch();

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/No query statistics available/),
            ).toBeInTheDocument();
        });
    });

    it('renders the Database column header', async () => {
        setupFetch({ rows: [makeQueryRow()] });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Database')).toBeInTheDocument();
        });
    });

    it('renders database_name in each row', async () => {
        setupFetch({
            rows: [
                makeQueryRow({ queryid: 'q1', database_name: 'appdb' }),
                makeQueryRow({ queryid: 'q2', database_name: 'analytics' }),
            ],
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('appdb')).toBeInTheDocument();
            expect(screen.getByText('analytics')).toBeInTheDocument();
        });
    });

    it('truncates long query text at 80 characters', async () => {
        const longQuery = `SELECT ${'a'.repeat(200)}`;
        setupFetch({ rows: [makeQueryRow({ query: longQuery })] });

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(`${longQuery.substring(0, 80)}...`),
            ).toBeInTheDocument();
        });
    });

    it('calls pushOverlay with databaseName when a row is clicked', async () => {
        setupFetch({
            rows: [makeQueryRow({
                queryid: 'q1',
                database_name: 'proddb',
                query: 'SELECT 1',
            })],
        });

        renderSection();

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

    it('opens the overlay when a row is activated with Enter', async () => {
        setupFetch({
            rows: [makeQueryRow({ queryid: 'q1', query: 'SELECT 1' })],
        });

        renderSection();

        const rowButton = await screen.findByRole('button', {
            name: /View details for query/,
        });
        fireEvent.keyDown(rowButton, { key: 'Enter' });

        expect(mockPushOverlay).toHaveBeenCalledTimes(1);
    });

    it('ignores unrelated keys on a row', async () => {
        setupFetch({
            rows: [makeQueryRow({ queryid: 'q1', query: 'SELECT 1' })],
        });

        renderSection();

        const rowButton = await screen.findByRole('button', {
            name: /View details for query/,
        });
        fireEvent.keyDown(rowButton, { key: 'Escape' });

        expect(mockPushOverlay).not.toHaveBeenCalled();
    });

    it('shows database name in overlay title', async () => {
        setupFetch({
            rows: [makeQueryRow({
                queryid: 'q1',
                database_name: 'proddb',
                query: 'SELECT 1',
            })],
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('proddb')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', {
            name: /View details for query/,
        }));

        expect(mockPushOverlay).toHaveBeenCalledWith(
            expect.objectContaining({
                title: expect.stringContaining('proddb'),
            }),
        );
    });

    it('has the "Hide monitoring queries" toggle on by default', async () => {
        setupFetch();

        renderSection();

        const toggle = screen.getByRole('checkbox');
        expect(toggle).toBeChecked();
    });

    it('drops exclude_collector when the toggle is switched off', async () => {
        setupFetch({ rows: makeRows(2) });

        renderSection();

        await waitFor(() => {
            expect(lastTopQueryUrl()).toContain('exclude_collector=true');
        });

        fireEvent.click(screen.getByRole('checkbox'));

        await waitFor(() => {
            expect(lastTopQueryUrl()).not.toContain('exclude_collector');
        });
    });

    // -----------------------------------------------------------------
    // Paging
    // -----------------------------------------------------------------

    it('requests 20 rows from offset 0 by default', async () => {
        setupFetch({ rows: makeRows(20) });

        renderSection();

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'limit')).toBe('20');
        });
        expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
    });

    it('pages forward and back with the pager controls', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
            totalCount: '45',
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 45')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
        });
        expect(
            await screen.findByText('Showing 21–40 of 45'),
        ).toBeInTheDocument();

        fireEvent.click(
            screen.getByRole('button', { name: 'Previous page of queries' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });
        expect(
            await screen.findByText('Showing 1–20 of 45'),
        ).toBeInTheDocument();
    });

    it('disables the previous control on the first page', async () => {
        setupFetch({ rows: makeRows(20), totalCount: '45' });

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByRole('button', {
                    name: 'Previous page of queries',
                }),
            ).toBeDisabled();
        });
    });

    it('disables the next control on the final page', async () => {
        setupFetch({ rows: makeRows(5), totalCount: '5' });

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Next page of queries' }),
            ).toBeDisabled();
        });
    });

    it('resets to the first page when the page size changes', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
            totalCount: '90',
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 90')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );
        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
        });

        fireEvent.click(
            screen.getByRole('button', { name: 'Show 50 rows per page' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'limit')).toBe('50');
        });
        expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
    });

    it('resets to the first page when the collector toggle changes', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
            totalCount: '90',
        });

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Next page of queries' }),
            ).toBeEnabled();
        });
        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );
        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
        });

        fireEvent.click(screen.getByRole('checkbox'));

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });
    });

    // -----------------------------------------------------------------
    // X-Total-Count handling
    // -----------------------------------------------------------------

    it('falls back to short-page detection when the header is absent', async () => {
        setupFetch({ rows: makeRows(20) });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
        });
        expect(
            screen.getByRole('button', { name: 'Next page of queries' }),
        ).toBeEnabled();
    });

    it('disables next on a short page when the header is absent', async () => {
        setupFetch({ rows: makeRows(7) });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–7')).toBeInTheDocument();
        });
        expect(
            screen.getByRole('button', { name: 'Next page of queries' }),
        ).toBeDisabled();
    });

    it('ignores an unparseable X-Total-Count header', async () => {
        setupFetch({ rows: makeRows(20), totalCount: 'not-a-number' });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
        });
    });

    it('ignores a negative X-Total-Count header', async () => {
        setupFetch({ rows: makeRows(20), totalCount: '-5' });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
        });
    });

    it('tolerates a headers object that throws on get', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('database-summaries')) {
                return Promise.resolve(okResponse({ databases: [] }));
            }
            return Promise.resolve({
                ok: true,
                headers: {
                    get: () => { throw new Error('no headers here'); },
                } as unknown as Headers,
                json: () => Promise.resolve(makeRows(3)),
            } as Partial<Response>);
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–3')).toBeInTheDocument();
        });
    });

    it('steps back when a refresh strands the user past the end', async () => {
        // Page 2 is empty and the server reports only 20 rows in
        // total, so the panel must fall back to the last real page.
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': [],
            },
            totalCount: '20',
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 20')).toBeInTheDocument();
        });

        // The next control is disabled at the end of the data, so drive
        // the stranded state through a page-size change instead.
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('database-summaries')) {
                return Promise.resolve(okResponse({ databases: [] }));
            }
            const offset = paramOf(url, 'offset') ?? '0';
            return Promise.resolve(okResponse(
                offset === '0' ? makeRows(10, 'a') : [],
                '10',
            ));
        });

        fireEvent.click(
            screen.getByRole('button', { name: 'Show 10 rows per page' }),
        );

        await waitFor(() => {
            expect(screen.getByText('Showing 1–10 of 10')).toBeInTheDocument();
        });
        expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
    });

    it('steps back one page when stranded without a total header', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );
        await waitFor(() => {
            expect(screen.getByText('Showing 21–40')).toBeInTheDocument();
        });

        // The second page disappears; the panel walks back to page one.
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': [],
            },
        });
        fireEvent.click(
            screen.getByRole('button', { name: 'Show 20 rows per page' }),
        );

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------
    // Database filter
    // -----------------------------------------------------------------

    it('hides the database filter for a single-database connection', async () => {
        setupFetch({ rows: makeRows(3), databases: ['onlydb'] });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–3')).toBeInTheDocument();
        });
        expect(screen.queryByLabelText('Database')).not.toBeInTheDocument();
    });

    it('shows the database filter when several databases exist', async () => {
        setupFetch({
            rows: makeRows(3),
            databases: ['appdb', 'analytics'],
        });

        renderSection();

        expect(
            await screen.findByLabelText('Database'),
        ).toBeInTheDocument();
    });

    it('filters the request by the selected database', async () => {
        setupFetch({
            rows: makeRows(20),
            databases: ['appdb', 'analytics'],
        });

        renderSection();

        const select = await screen.findByLabelText('Database');
        fireEvent.mouseDown(select);

        fireEvent.click(
            await screen.findByRole('option', { name: 'analytics' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'database_name'))
                .toBe('analytics');
        });
        expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
    });

    it('resets to the first page when the database filter changes', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
            totalCount: '90',
            databases: ['appdb', 'analytics'],
        });

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 90')).toBeInTheDocument();
        });
        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );
        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
        });

        fireEvent.mouseDown(screen.getByLabelText('Database'));
        fireEvent.click(
            await screen.findByRole('option', { name: 'appdb' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });
        expect(paramOf(lastTopQueryUrl(), 'database_name')).toBe('appdb');
    });

    it('clears the filter when "All databases" is selected', async () => {
        setupFetch({
            rows: makeRows(5),
            databases: ['appdb', 'analytics'],
        });

        renderSection();

        fireEvent.mouseDown(await screen.findByLabelText('Database'));
        fireEvent.click(
            await screen.findByRole('option', { name: 'analytics' }),
        );
        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'database_name'))
                .toBe('analytics');
        });

        fireEvent.mouseDown(screen.getByLabelText('Database'));
        fireEvent.click(
            await screen.findByRole('option', { name: 'All databases' }),
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'database_name')).toBeNull();
        });
    });

    it('shows a filter-aware empty message', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('database-summaries')) {
                return Promise.resolve(okResponse({
                    databases: [
                        { database_name: 'appdb' },
                        { database_name: 'analytics' },
                    ],
                }));
            }
            const db = paramOf(url, 'database_name');
            return Promise.resolve(okResponse(db ? [] : makeRows(3)));
        });

        renderSection();

        fireEvent.mouseDown(await screen.findByLabelText('Database'));
        fireEvent.click(
            await screen.findByRole('option', { name: 'analytics' }),
        );

        expect(
            await screen.findByText(
                'No query statistics available for analytics.',
            ),
        ).toBeInTheDocument();
    });

    // -----------------------------------------------------------------
    // Connection changes
    // -----------------------------------------------------------------

    it('resets paging when the connection changes', async () => {
        setupFetch({
            pages: {
                '0': makeRows(20, 'a'),
                '20': makeRows(20, 'b'),
            },
            totalCount: '90',
        });

        const { rerender } = renderSection();

        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 90')).toBeInTheDocument();
        });
        fireEvent.click(
            screen.getByRole('button', { name: 'Next page of queries' }),
        );
        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
        });

        rerender(
            <TopQueriesSection
                connectionId={2}
                connectionName="Other Server"
            />,
        );

        await waitFor(() => {
            expect(paramOf(lastTopQueryUrl(), 'connection_id')).toBe('2');
        });
        expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
    });

    it('renders no rows when the response is not an array', async () => {
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('database-summaries')) {
                return Promise.resolve(okResponse({ databases: [] }));
            }
            return Promise.resolve(okResponse({ unexpected: true }));
        });

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/No query statistics available/),
            ).toBeInTheDocument();
        });
    });
});
