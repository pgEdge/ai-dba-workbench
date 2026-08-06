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
import { ThemeProvider, createTheme } from '@mui/material/styles';
import DatabaseSummariesSection from '../DatabaseSummariesSection';
import type { DatabaseSummary } from '../types';

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

// Mock the Chart component (used by Sparkline) to avoid ECharts in tests.
vi.mock('../../../Chart', () => ({
    Chart: () => <div data-testid="sparkline-chart">chart</div>,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mock DatabaseSummary with sensible defaults. */
const makeDatabase = (
    overrides: Partial<DatabaseSummary> = {},
): DatabaseSummary => ({
    database_name: 'mydb',
    size_bytes: 1024,
    size_pretty: '1 KB',
    cache_hit_ratio: {
        current: 98.5,
        time_series: [
            { time: '2024-01-01T00:00:00Z', value: 98.0 },
            { time: '2024-01-01T00:01:00Z', value: 99.0 },
        ],
    },
    transaction_rate: 12.3,
    dead_tuple_ratio: 2.1,
    active_connections: 5,
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

const theme = createTheme();

const renderSection = () => render(
    <ThemeProvider theme={theme}>
        <DatabaseSummariesSection
            connectionId={1}
            connectionName="Test Server"
        />
    </ThemeProvider>,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DatabaseSummariesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    it('renders the Sparkline when time_series is populated', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ databases: [makeDatabase()] }),
        );

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByTestId('sparkline-chart'),
            ).toBeInTheDocument();
        });

        // The percentage still renders alongside the chart.
        expect(screen.getByText('98.5%')).toBeInTheDocument();
        // No placeholder is shown when history exists.
        expect(
            screen.queryByText('No history yet'),
        ).not.toBeInTheDocument();
    });

    it('shows a placeholder when current is valid but time_series is empty', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({
                databases: [
                    makeDatabase({
                        cache_hit_ratio: {
                            current: 97.2,
                            time_series: [],
                        },
                    }),
                ],
            }),
        );

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText('No history yet'),
            ).toBeInTheDocument();
        });

        // The current percentage still renders even without history.
        expect(screen.getByText('97.2%')).toBeInTheDocument();
        // The Sparkline chart is not rendered for an empty series.
        expect(
            screen.queryByTestId('sparkline-chart'),
        ).not.toBeInTheDocument();
    });

    it('shows the placeholder when cache_hit_ratio is undefined', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({
                databases: [
                    makeDatabase({
                        cache_hit_ratio: undefined as never,
                    }),
                ],
            }),
        );

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText('No history yet'),
            ).toBeInTheDocument();
        });

        // Current value falls back to the placeholder dash.
        expect(screen.getByText('--')).toBeInTheDocument();
    });

    it('shows the loading spinner while fetching', () => {
        mockApiFetch.mockReturnValue(new Promise(() => {}));

        renderSection();

        expect(
            screen.getByLabelText('Loading databases'),
        ).toBeInTheDocument();
    });

    it('shows an error message when the request fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, { error: 'boom' }),
        );

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('boom')).toBeInTheDocument();
        });
    });

    it('shows a fallback error message when the body has no error', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(503));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to fetch database summaries: 503/),
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

    it('shows the empty state when no databases are returned', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText('No database summaries available'),
            ).toBeInTheDocument();
        });
    });

    it('renders varied cache and dead-tuple color thresholds', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({
                databases: [
                    makeDatabase({
                        database_name: 'warn_db',
                        cache_hit_ratio: { current: 85, time_series: [] },
                        dead_tuple_ratio: 15,
                    }),
                    makeDatabase({
                        database_name: 'crit_db',
                        cache_hit_ratio: { current: 50, time_series: [] },
                        dead_tuple_ratio: 30,
                    }),
                ],
            }),
        );

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('warn_db')).toBeInTheDocument();
        });
        expect(screen.getByText('crit_db')).toBeInTheDocument();
        expect(screen.getByText('85.0%')).toBeInTheDocument();
        expect(screen.getByText('50.0%')).toBeInTheDocument();
    });

    it('renders dashes when numeric fields are undefined', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({
                databases: [
                    makeDatabase({
                        transaction_rate: undefined as never,
                        dead_tuple_ratio: undefined as never,
                        active_connections: undefined as never,
                    }),
                ],
            }),
        );

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('mydb')).toBeInTheDocument();
        });
        // Three undefined numeric fields each render a dash.
        expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(3);
    });

    it('opens the database overlay on click', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ databases: [makeDatabase()] }),
        );

        renderSection();

        const card = await screen.findByRole(
            'button',
            { name: /View details for database mydb/ },
        );
        fireEvent.click(card);

        expect(mockPushOverlay).toHaveBeenCalledWith(
            expect.objectContaining({
                level: 'database',
                databaseName: 'mydb',
                connectionId: 1,
            }),
        );
    });

    it('opens the database overlay on Enter and Space keys', async () => {
        mockApiFetch.mockResolvedValue(
            okResponse({ databases: [makeDatabase()] }),
        );

        renderSection();

        const card = await screen.findByRole(
            'button',
            { name: /View details for database mydb/ },
        );

        fireEvent.keyDown(card, { key: 'Enter' });
        fireEvent.keyDown(card, { key: ' ' });
        expect(mockPushOverlay).toHaveBeenCalledTimes(2);

        // A non-activating key does not trigger the overlay.
        fireEvent.keyDown(card, { key: 'a' });
        expect(mockPushOverlay).toHaveBeenCalledTimes(2);
    });
});
