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
vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockPushOverlay = vi.fn();
let mockTimeRange = '1h';
let mockCustomStart: string | undefined;
let mockCustomEnd: string | undefined;
vi.mock('../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: 0,
        pushOverlay: mockPushOverlay,
        timeRange: {
            range: mockTimeRange,
            customStart: mockCustomStart,
            customEnd: mockCustomEnd,
        },
    }),
}));

vi.mock('../../../contexts/useAuth', () => ({
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

/** Render the section pinned to a single database. */
const renderScopedSection = (databaseName = 'analytics') => render(
    <TopQueriesSection
        connectionId={1}
        connectionName="Test Server"
        databaseName={databaseName}
    />,
);

/** All database-summaries request URLs seen so far. */
const summariesUrls = (): string[] => mockApiFetch.mock.calls
    .map(call => call[0] as string)
    .filter(url => url.includes('database-summaries'));

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TopQueriesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        mockTimeRange = '1h';
        mockCustomStart = undefined;
        mockCustomEnd = undefined;
    });

    describe('time window', () => {
        it.each(['1h', '6h', '24h', '7d', '30d'])(
            'sends the %s preset as time_range',
            async (range) => {
                mockTimeRange = range;
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await waitFor(() => {
                    expect(topQueryUrls()).not.toHaveLength(0);
                });
                const params = new URLSearchParams(
                    lastTopQueryUrl().split('?')[1],
                );
                expect(params.get('time_range')).toBe(range);
                expect(params.get('time_start')).toBeNull();
                expect(params.get('time_end')).toBeNull();
            });

        it('sends both bounds for a custom range', async () => {
            mockTimeRange = 'custom';
            mockCustomStart = '2026-09-01T00:00:00Z';
            mockCustomEnd = '2026-09-02T00:00:00Z';
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await waitFor(() => {
                expect(topQueryUrls()).not.toHaveLength(0);
            });
            const params = new URLSearchParams(
                lastTopQueryUrl().split('?')[1],
            );
            expect(params.get('time_range')).toBe('custom');
            expect(params.get('time_start'))
                .toBe('2026-09-01T00:00:00Z');
            expect(params.get('time_end'))
                .toBe('2026-09-02T00:00:00Z');
        });

        it('makes no request whilst a custom range lacks a bound',
            async () => {
                mockTimeRange = 'custom';
                mockCustomEnd = '2026-09-02T00:00:00Z';
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await waitFor(() => {
                    expect(mockApiFetch).toHaveBeenCalled();
                });
                expect(topQueryUrls()).toHaveLength(0);
            });

        it('returns to the first page when the range changes', async () => {
            mockTimeRange = '30d';
            setupFetch({
                pages: {
                    '0': makeRows(20, 'a'),
                    '20': makeRows(20, 'b'),
                },
                totalCount: '45',
            });

            const { rerender } = renderSection();

            await waitFor(() => {
                expect(
                    screen.getByText('Showing 1–20 of 45'),
                ).toBeInTheDocument();
            });

            fireEvent.click(
                screen.getByRole('button', { name: 'Next page of queries' }),
            );

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
            });

            mockTimeRange = '1h';
            rerender(
                <TopQueriesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'time_range')).toBe('1h');
            });
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });

        it('returns to the first page when a custom bound narrows',
            async () => {
                mockTimeRange = 'custom';
                mockCustomStart = '2026-09-01T00:00:00Z';
                mockCustomEnd = '2026-09-30T00:00:00Z';
                setupFetch({
                    pages: {
                        '0': makeRows(20, 'a'),
                        '20': makeRows(20, 'b'),
                    },
                    totalCount: '45',
                });

                const { rerender } = renderSection();

                await waitFor(() => {
                    expect(
                        screen.getByText('Showing 1–20 of 45'),
                    ).toBeInTheDocument();
                });

                fireEvent.click(
                    screen.getByRole('button', {
                        name: 'Next page of queries',
                    }),
                );

                await waitFor(() => {
                    expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
                });

                mockCustomEnd = '2026-09-02T00:00:00Z';
                rerender(
                    <TopQueriesSection
                        connectionId={1}
                        connectionName="Test Server"
                    />,
                );

                await waitFor(() => {
                    expect(paramOf(lastTopQueryUrl(), 'time_end'))
                        .toBe('2026-09-02T00:00:00Z');
                });
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
            });

        it('refetches when the selected range changes', async () => {
            setupFetch({ rows: makeRows(1) });

            const { rerender } = renderSection();

            await waitFor(() => {
                expect(topQueryUrls()).toHaveLength(1);
            });

            mockTimeRange = '7d';
            rerender(
                <TopQueriesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(topQueryUrls()).toHaveLength(2);
            });
            const params = new URLSearchParams(
                lastTopQueryUrl().split('?')[1],
            );
            expect(params.get('time_range')).toBe('7d');
        });
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

        const rowButton = screen.getByRole('row', {
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

        const rowButton = await screen.findByRole('row', {
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

        const rowButton = await screen.findByRole('row', {
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

        fireEvent.click(screen.getByRole('row', {
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

    it('resets to the first page when the database filter changes',
        async () => {
            setupFetch({
                pages: {
                    '0': makeRows(20, 'a'),
                    '20': makeRows(20, 'b'),
                },
                totalCount: '90',
                databases: ['analytics', 'reporting'],
            });

            renderSection();

            await waitFor(() => {
                expect(
                    screen.getByRole('button', {
                        name: 'Next page of queries',
                    }),
                ).toBeEnabled();
            });
            fireEvent.click(
                screen.getByRole('button', { name: 'Next page of queries' }),
            );
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
            });

            fireEvent.mouseDown(screen.getByRole('combobox'));
            fireEvent.click(
                screen.getByRole('option', { name: 'reporting' }),
            );

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'database_name')).toBe(
                    'reporting',
                );
            });
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });

    it('clears the stale total whilst a reset request is in flight',
        async () => {
            // The pager keeps the previous page's rows on screen whilst
            // the next response is outstanding, so a total left over
            // from the old filter would be shown against the new one.
            let resolveSecond: (() => void) | undefined;
            let seenTopQueries = 0;
            mockApiFetch.mockImplementation((url: string) => {
                if (url.includes('database-summaries')) {
                    return Promise.resolve(okResponse({ databases: [] }));
                }
                seenTopQueries += 1;
                if (seenTopQueries === 1) {
                    return Promise.resolve(okResponse(makeRows(20), '90'));
                }
                return new Promise(resolve => {
                    resolveSecond = () =>
                        resolve(okResponse(makeRows(20), '90'));
                });
            });

            renderSection();
            await waitFor(() => {
                expect(
                    screen.getByText('Showing 1–20 of 90'),
                ).toBeInTheDocument();
            });

            fireEvent.click(
                screen.getByRole('button', { name: 'Show 50 rows per page' }),
            );

            await waitFor(() => {
                expect(screen.getByText('Showing 1–20')).toBeInTheDocument();
            });
            expect(screen.queryByText('Showing 1–20 of 90')).toBeNull();

            resolveSecond?.();
            await waitFor(() => {
                expect(
                    screen.getByText('Showing 1–20 of 90'),
                ).toBeInTheDocument();
            });
        });

    // -----------------------------------------------------------------
    it('ignores a stale response that resolves after a newer one', async () => {
        // Hold each offset's response open so they can be resolved out
        // of order, which is what happens when a user clicks Next twice
        // in quick succession on a slow link.
        const resolvers: Record<string, (rows: TopQueryRow[]) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('database-summaries')) {
                return Promise.resolve(okResponse({ databases: [] }));
            }
            const offset = paramOf(url, 'offset') ?? '0';
            if (offset === '0') {
                return Promise.resolve(okResponse(makeRows(20, 'a'), '60'));
            }
            return new Promise(resolve => {
                resolvers[offset] = (rows: TopQueryRow[]) =>
                    resolve(okResponse(rows, '60'));
            });
        });

        renderSection();
        await waitFor(() => {
            expect(screen.getByText('Showing 1–20 of 60')).toBeInTheDocument();
        });

        const next = screen.getByRole('button', {
            name: 'Next page of queries',
        });
        fireEvent.click(next);
        await waitFor(() => {
            expect(resolvers['20']).toBeDefined();
        });

        fireEvent.click(next);
        await waitFor(() => {
            expect(resolvers['40']).toBeDefined();
        });

        // Distinct query text per page, so the rows on screen identify
        // which response was applied.
        const pageRows = (label: string): TopQueryRow[] =>
            Array.from({ length: 20 }, (_, i) => makeQueryRow({
                queryid: `${label}${i}`,
                query: `SELECT '${label}' AS page, ${i}`,
            }));

        // The newer request (offset 40) lands first, then the older one
        // (offset 20) arrives late. The late response must be dropped.
        resolvers['40'](pageRows('third'));
        await waitFor(() => {
            expect(screen.getByText('Showing 41–60 of 60')).toBeInTheDocument();
        });
        expect(
            screen.getByText("SELECT 'third' AS page, 0"),
        ).toBeInTheDocument();

        resolvers['20'](pageRows('second'));

        // Give the stale response a chance to be applied before
        // asserting that it was not.
        await waitFor(() => {
            expect(screen.getByText('Showing 41–60 of 60')).toBeInTheDocument();
        });
        expect(screen.queryByText('Showing 21–40 of 60')).toBeNull();
        expect(
            screen.getByText("SELECT 'third' AS page, 0"),
        ).toBeInTheDocument();
        expect(screen.queryByText("SELECT 'second' AS page, 0")).toBeNull();
    });

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

    // -----------------------------------------------------------------
    // Pinned to a single database
    // -----------------------------------------------------------------

    describe('when pinned to a database', () => {
        it('sends the pinned database as the database_name filter',
            async () => {
                setupFetch({ rows: makeRows(1) });

                renderScopedSection('analytics');

                await waitFor(() => {
                    expect(paramOf(lastTopQueryUrl(), 'database_name'))
                        .toBe('analytics');
                });
            });

        it('does not render the database filter control', async () => {
            setupFetch({
                rows: makeRows(1),
                databases: ['analytics', 'reporting', 'staging'],
            });

            renderScopedSection('analytics');

            await waitFor(() => {
                expect(screen.getByText('SELECT 0')).toBeInTheDocument();
            });
            expect(screen.queryByLabelText('Database')).not.toBeInTheDocument();
        });

        it('does not fetch the database list', async () => {
            setupFetch({ rows: makeRows(1), databases: ['analytics'] });

            renderScopedSection('analytics');

            await waitFor(() => {
                expect(screen.getByText('SELECT 0')).toBeInTheDocument();
            });
            expect(summariesUrls()).toHaveLength(0);
        });

        it('drops the Database column', async () => {
            setupFetch({ rows: makeRows(1) });

            renderScopedSection('analytics');

            await waitFor(() => {
                expect(screen.getByText('SELECT 0')).toBeInTheDocument();
            });
            expect(screen.queryByText('Database')).not.toBeInTheDocument();
            expect(screen.getByText('Query')).toBeInTheDocument();
        });

        it('names the database in the empty state', async () => {
            setupFetch({ rows: [] });

            renderScopedSection('analytics');

            await waitFor(() => {
                expect(
                    screen.getByText(
                        'No query statistics available for analytics.',
                    ),
                ).toBeInTheDocument();
            });
        });

        it('resets paging when the pinned database changes', async () => {
            setupFetch({
                pages: {
                    '0': makeRows(20, 'a'),
                    '20': makeRows(20, 'b'),
                },
                totalCount: '90',
            });

            const { rerender } = renderScopedSection('analytics');

            await waitFor(() => {
                expect(
                    screen.getByText('Showing 1–20 of 90'),
                ).toBeInTheDocument();
            });
            fireEvent.click(
                screen.getByRole('button', { name: 'Next page of queries' }),
            );
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
            });

            rerender(
                <TopQueriesSection
                    connectionId={1}
                    connectionName="Test Server"
                    databaseName="reporting"
                />,
            );

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'database_name'))
                    .toBe('reporting');
            });
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });
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

    describe('alongside another instance', () => {
        it('gives each instance its own labelled controls', async () => {
            setupFetch({ rows: makeRows(1), databases: ['app', 'analytics'] });

            render(
                <>
                    <TopQueriesSection
                        connectionId={1}
                        connectionName="Test Server"
                    />
                    <TopQueriesSection
                        connectionId={2}
                        connectionName="Other Server"
                    />
                </>,
            );

            const filters = await screen.findAllByLabelText('Filter queries');
            expect(filters).toHaveLength(2);
            expect(filters[0].id).not.toBe(filters[1].id);
            for (const filter of filters) {
                expect(filter.tagName).toBe('INPUT');
                expect(
                    document.querySelectorAll(`[id="${filter.id}"]`),
                ).toHaveLength(1);
            }

            const databases = await screen.findAllByRole(
                'combobox', { name: 'Database' },
            );
            expect(databases).toHaveLength(2);
            expect(databases[0].id).not.toBe(databases[1].id);
        });
    });

    describe('column sorting', () => {
        /** The header cell holding the named sort control. */
        const headerFor = (label: string): HTMLElement =>
            screen.getByRole('columnheader', { name: label });

        /** The sort control inside the named header. */
        const sortControl = (label: string): HTMLElement =>
            screen.getByRole('button', { name: label });

        it('orders by total time, largest first, by default', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe(
                'total_exec_time',
            );
            expect(paramOf(lastTopQueryUrl(), 'order')).toBe('desc');
            expect(headerFor('Total Time')).toHaveAttribute(
                'aria-sort', 'descending',
            );
            for (const label of ['Calls', 'Mean Time', 'Rows']) {
                expect(headerFor(label)).toHaveAttribute('aria-sort', 'none');
            }
        });

        it('leaves the Database and Query headers unsortable', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            expect(headerFor('Database')).not.toHaveAttribute('aria-sort');
            expect(headerFor('Query')).not.toHaveAttribute('aria-sort');
            expect(
                screen.queryByRole('button', { name: 'Query' }),
            ).not.toBeInTheDocument();
        });

        it.each([
            ['Calls', 'calls'],
            ['Mean Time', 'mean_exec_time'],
            ['Rows', 'rows'],
        ])('sorts by %s descending on first click', async (label, key) => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            fireEvent.click(sortControl(label));

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe(key);
            });
            expect(paramOf(lastTopQueryUrl(), 'order')).toBe('desc');
            expect(headerFor(label)).toHaveAttribute(
                'aria-sort', 'descending',
            );
            expect(headerFor('Total Time')).toHaveAttribute(
                'aria-sort', 'none',
            );
        });

        it('toggles direction when the active column is clicked', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            fireEvent.click(sortControl('Total Time'));

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'order')).toBe('asc');
            });
            expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe(
                'total_exec_time',
            );
            expect(headerFor('Total Time')).toHaveAttribute(
                'aria-sort', 'ascending',
            );
            // The visible arrow must follow the order too, not only the
            // aria-sort attribute on the header cell.
            expect(
                sortControl('Total Time').querySelector(
                    '.MuiTableSortLabel-iconDirectionAsc',
                ),
            ).not.toBeNull();

            fireEvent.click(sortControl('Total Time'));

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'order')).toBe('desc');
            });
            expect(headerFor('Total Time')).toHaveAttribute(
                'aria-sort', 'descending',
            );
            expect(
                sortControl('Total Time').querySelector(
                    '.MuiTableSortLabel-iconDirectionDesc',
                ),
            ).not.toBeNull();
        });

        it('starts a newly chosen column descending after an ascending sort',
            async () => {
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await screen.findByRole('table', { name: 'Top queries' });
                fireEvent.click(sortControl('Total Time'));
                await waitFor(() => {
                    expect(paramOf(lastTopQueryUrl(), 'order')).toBe('asc');
                });

                fireEvent.click(sortControl('Calls'));

                await waitFor(() => {
                    expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe(
                        'calls',
                    );
                });
                expect(paramOf(lastTopQueryUrl(), 'order')).toBe('desc');
            });

        it('can be operated from the keyboard', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            const control = sortControl('Mean Time');
            expect(control).toHaveAttribute('tabindex', '0');
            control.focus();
            fireEvent.keyDown(control, { key: 'Enter' });

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe(
                    'mean_exec_time',
                );
            });
        });

        it('resets to the first page when the sort changes', async () => {
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
                    screen.getByRole('button', {
                        name: 'Next page of queries',
                    }),
                ).toBeEnabled();
            });
            fireEvent.click(
                screen.getByRole('button', { name: 'Next page of queries' }),
            );
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
            });

            fireEvent.click(sortControl('Rows'));

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'order_by')).toBe('rows');
            });
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
        });
    });

    describe('query text filter', () => {
        /** The text filter input. */
        const filterInput = (): HTMLInputElement =>
            screen.getByRole('textbox', { name: 'Filter queries' }) as
                HTMLInputElement;

        /** Type a term into the filter as a single change. */
        const typeFilter = (value: string): void => {
            fireEvent.change(filterInput(), { target: { value } });
        };

        /** Top-queries URLs that carried a query_text parameter. */
        const filteredUrls = (): string[] => topQueryUrls().filter(
            url => paramOf(url, 'query_text') !== null,
        );

        /** Wait comfortably past the debounce delay. */
        const waitPastDebounce = (): Promise<void> =>
            new Promise(resolve => { setTimeout(resolve, 400); });

        it('omits query_text when no term is entered', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            expect(filterInput()).toHaveValue('');
            expect(paramOf(lastTopQueryUrl(), 'query_text')).toBeNull();
        });

        it('caps the term at the server limit of 1000 characters',
            async () => {
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await screen.findByRole('table', { name: 'Top queries' });
                expect(filterInput()).toHaveAttribute('maxlength', '1000');
            });

        it('sends one debounced request for a burst of keystrokes',
            async () => {
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await screen.findByRole('table', { name: 'Top queries' });
                typeFilter('s');
                typeFilter('se');
                typeFilter('sel');
                typeFilter('select');

                // Nothing has been sent yet: the debounce is still
                // waiting for the user to pause.
                expect(filteredUrls()).toHaveLength(0);

                await waitFor(() => {
                    expect(filteredUrls()).toHaveLength(1);
                });
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBe(
                    'select',
                );

                await waitPastDebounce();
                expect(filteredUrls()).toHaveLength(1);
            });

        it('sends nothing until the debounce delay has elapsed',
            async () => {
                setupFetch({ rows: makeRows(1) });

                renderSection();

                await screen.findByRole('table', { name: 'Top queries' });
                typeFilter('users');

                // Well inside the 300 ms delay, the term must still be
                // held back; an immediate request would show up here.
                await new Promise(resolve => { setTimeout(resolve, 150); });
                expect(filteredUrls()).toHaveLength(0);

                await waitFor(() => {
                    expect(filteredUrls()).toHaveLength(1);
                });
            });

        it('trims surrounding whitespace from the term', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            typeFilter('  users  ');

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBe(
                    'users',
                );
            });
        });

        it('sends no filter for a whitespace-only term', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            const before = topQueryUrls().length;
            typeFilter('   ');

            await waitPastDebounce();
            expect(filteredUrls()).toHaveLength(0);
            expect(topQueryUrls()).toHaveLength(before);
        });

        it('clears the term with the clear button', async () => {
            setupFetch({ rows: makeRows(1) });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            expect(
                screen.queryByRole('button', { name: 'Clear query filter' }),
            ).not.toBeInTheDocument();

            typeFilter('users');
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBe(
                    'users',
                );
            });

            fireEvent.click(
                screen.getByRole('button', { name: 'Clear query filter' }),
            );

            expect(filterInput()).toHaveValue('');
            expect(
                screen.queryByRole('button', { name: 'Clear query filter' }),
            ).not.toBeInTheDocument();
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBeNull();
            });
        });

        it('resets to the first page when the filter changes', async () => {
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
                    screen.getByRole('button', {
                        name: 'Next page of queries',
                    }),
                ).toBeEnabled();
            });
            fireEvent.click(
                screen.getByRole('button', { name: 'Next page of queries' }),
            );
            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('20');
            });

            typeFilter('users');

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBe(
                    'users',
                );
            });
            expect(paramOf(lastTopQueryUrl(), 'offset')).toBe('0');
            // The reset lands before the fetch, so no request ever
            // pairs the new term with the old offset.
            for (const url of filteredUrls()) {
                expect(paramOf(url, 'offset')).toBe('0');
            }
        });

        it('says so when the term matches nothing', async () => {
            mockApiFetch.mockImplementation((url: string) => {
                if (url.includes('database-summaries')) {
                    return Promise.resolve(okResponse({ databases: [] }));
                }
                const rows = paramOf(url, 'query_text') === null
                    ? makeRows(1)
                    : [];
                return Promise.resolve(okResponse(rows, String(rows.length)));
            });

            renderSection();

            await screen.findByRole('table', { name: 'Top queries' });
            typeFilter('zzz');

            expect(
                await screen.findByText("No queries match 'zzz'."),
            ).toBeInTheDocument();
            expect(
                screen.queryByText(/pg_stat_statements/),
            ).not.toBeInTheDocument();
            // The field stays available so the user can amend the term.
            expect(filterInput()).toHaveValue('zzz');
        });

        it('names the selected database when the term matches nothing',
            async () => {
                mockApiFetch.mockImplementation((url: string) => {
                    if (url.includes('database-summaries')) {
                        return Promise.resolve(okResponse({
                            databases: [
                                { database_name: 'analytics' },
                                { database_name: 'reporting' },
                            ],
                        }));
                    }
                    const rows = paramOf(url, 'query_text') === null
                        ? makeRows(1)
                        : [];
                    return Promise.resolve(okResponse(rows));
                });

                renderSection();

                await screen.findByRole('combobox');
                fireEvent.mouseDown(screen.getByRole('combobox'));
                fireEvent.click(
                    screen.getByRole('option', { name: 'reporting' }),
                );
                typeFilter('zzz');

                expect(
                    await screen.findByText(
                        "No queries in reporting match 'zzz'.",
                    ),
                ).toBeInTheDocument();
            });
    });

    describe('sorting and filtering when pinned to a database', () => {
        it('offers the filter and sort controls and sends both', async () => {
            setupFetch({ rows: makeRows(1) });

            renderScopedSection('analytics');

            await screen.findByRole('table', { name: 'Top queries' });
            fireEvent.click(screen.getByRole('button', { name: 'Calls' }));
            fireEvent.change(
                screen.getByRole('textbox', { name: 'Filter queries' }),
                { target: { value: 'orders' } },
            );

            await waitFor(() => {
                expect(paramOf(lastTopQueryUrl(), 'query_text')).toBe(
                    'orders',
                );
            });
            const url = lastTopQueryUrl();
            expect(paramOf(url, 'order_by')).toBe('calls');
            expect(paramOf(url, 'order')).toBe('desc');
            expect(paramOf(url, 'database_name')).toBe('analytics');
            expect(
                screen.getByRole('columnheader', { name: 'Calls' }),
            ).toHaveAttribute('aria-sort', 'descending');
        });

        it('names the pinned database when the term matches nothing',
            async () => {
                mockApiFetch.mockImplementation((url: string) => {
                    const rows = paramOf(url, 'query_text') === null
                        ? makeRows(1)
                        : [];
                    return Promise.resolve(okResponse(rows));
                });

                renderScopedSection('analytics');

                await screen.findByRole('table', { name: 'Top queries' });
                fireEvent.change(
                    screen.getByRole('textbox', { name: 'Filter queries' }),
                    { target: { value: 'zzz' } },
                );

                expect(
                    await screen.findByText(
                        "No queries in analytics match 'zzz'.",
                    ),
                ).toBeInTheDocument();
            });
    });
});
