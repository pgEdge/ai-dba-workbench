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
import ConnectionsSection from '../ConnectionsSection';
import type { ConnectionGroupRow } from '../types';
import type { TimeRange } from '../../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

let mockRange: TimeRange = '24h';
let mockRefreshTrigger = 0;
vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: mockRange },
        refreshTrigger: mockRefreshTrigger,
    }),
}));

vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({
        user: { id: 1, username: 'testuser' },
    }),
}));

vi.mock('../../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mock ConnectionGroupRow with sensible defaults. */
const makeGroupRow = (
    overrides: Partial<ConnectionGroupRow> = {},
): ConnectionGroupRow => ({
    group_label: 'app_rw',
    client_hostname: null,
    total: 16,
    active: 12,
    idle: 4,
    idle_in_transaction: 0,
    other: 0,
    ...overrides,
});

/** Build a successful response payload. */
const okResponse = (
    groups: ConnectionGroupRow[] = [makeGroupRow()],
    collectedAt: string | null = '2026-07-29T10:00:00Z',
) => ({ collected_at: collectedAt, groups });

/** Extract a query parameter from a recorded request URL. */
const paramOf = (callIndex: number, name: string): string | null => {
    const url = mockApiGet.mock.calls[callIndex][0] as string;
    return new URL(url, 'https://example.test').searchParams.get(name);
};

/** Render the section with the given connection id. */
const renderSection = (connectionId = 1) => render(
    <ConnectionsSection
        connectionId={connectionId}
        connectionName="Test Server"
    />,
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ConnectionsSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        mockRange = '24h';
        mockRefreshTrigger = 0;
    });

    it('renders the "Connections" section title', () => {
        mockApiGet.mockResolvedValue(okResponse([]));

        renderSection();

        expect(screen.getByText('Connections')).toBeInTheDocument();
    });

    it('renders the three grouping tabs with a11y wiring', () => {
        mockApiGet.mockResolvedValue(okResponse([]));

        renderSection();

        const tablist = screen.getByRole('tablist', {
            name: 'Connection groupings',
        });
        expect(tablist).toBeInTheDocument();

        const userTab = screen.getByRole('tab', { name: 'By User' });
        expect(screen.getByRole('tab', { name: 'By Client' }))
            .toBeInTheDocument();
        expect(screen.getByRole('tab', { name: 'By Database' }))
            .toBeInTheDocument();

        expect(userTab).toHaveAttribute('id', 'connections-tab-user');
        expect(userTab).toHaveAttribute(
            'aria-controls', 'connections-tabpanel-user',
        );

        const panel = screen.getByRole('tabpanel');
        expect(panel).toHaveAttribute('id', 'connections-tabpanel-user');
        expect(panel).toHaveAttribute(
            'aria-labelledby', 'connections-tab-user',
        );
    });

    it('shows the loading spinner while fetching', () => {
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderSection();

        expect(screen.getByLabelText('Loading connections'))
            .toBeInTheDocument();
    });

    it('shows the error message when the request fails', async () => {
        mockApiGet.mockRejectedValue(new Error('groups unavailable'));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('groups unavailable'))
                .toBeInTheDocument();
        });
        expect(screen.queryByText(/As of /)).not.toBeInTheDocument();
    });

    it('shows an honest empty state when no groups come back', async () => {
        mockApiGet.mockResolvedValue(okResponse([], null));

        renderSection();

        await waitFor(() => {
            expect(
                screen.getByText(/No connection snapshots in the selected/),
            ).toBeInTheDocument();
        });
    });

    it('renders the user grouping and requests group_by=user', async () => {
        mockApiGet.mockResolvedValue(okResponse([
            makeGroupRow({ group_label: 'app_rw' }),
            makeGroupRow({
                group_label: 'reporting',
                total: 3,
                active: 1,
                idle: 1,
                idle_in_transaction: 1,
                other: 0,
            }),
        ]));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('app_rw')).toBeInTheDocument();
        });
        expect(screen.getByText('reporting')).toBeInTheDocument();
        expect(screen.getByText('User')).toBeInTheDocument();
        expect(screen.getByText('Total')).toBeInTheDocument();
        expect(screen.getByText('Active')).toBeInTheDocument();
        expect(screen.getByText('Idle')).toBeInTheDocument();
        expect(screen.getByText('Idle in transaction')).toBeInTheDocument();
        expect(screen.getByText('Other')).toBeInTheDocument();
        expect(screen.getByText('16')).toBeInTheDocument();
        expect(screen.getByText('12')).toBeInTheDocument();
        expect(paramOf(0, 'group_by')).toBe('user');
        expect(paramOf(0, 'connection_id')).toBe('1');
    });

    it('requests group_by=client and shows the client hostname',
        async () => {
            mockApiGet.mockResolvedValueOnce(okResponse());
            mockApiGet.mockResolvedValueOnce(okResponse([
                makeGroupRow({
                    group_label: '192.0.2.10',
                    client_hostname: 'app01.example.com',
                }),
                makeGroupRow({ group_label: 'local' }),
            ]));

            renderSection();

            await waitFor(() => {
                expect(screen.getByText('app_rw')).toBeInTheDocument();
            });

            fireEvent.click(screen.getByRole('tab', { name: 'By Client' }));

            await waitFor(() => {
                expect(screen.getByText('192.0.2.10')).toBeInTheDocument();
            });
            expect(screen.getByText('app01.example.com'))
                .toBeInTheDocument();
            expect(screen.getByText('local')).toBeInTheDocument();
            expect(screen.getByText('Client')).toBeInTheDocument();
            expect(paramOf(1, 'group_by')).toBe('client');

            const panel = screen.getByRole('tabpanel');
            expect(panel).toHaveAttribute(
                'id', 'connections-tabpanel-client',
            );
        });

    it('renders a row whose label came back empty', async () => {
        mockApiGet.mockResolvedValue(okResponse([
            makeGroupRow({ group_label: '', total: 2 }),
        ]));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('2')).toBeInTheDocument();
        });
    });

    it('does not render a hostname on the user tab', async () => {
        mockApiGet.mockResolvedValue(okResponse([
            makeGroupRow({
                group_label: 'app_rw',
                client_hostname: 'app01.example.com',
            }),
        ]));

        renderSection();

        await waitFor(() => {
            expect(screen.getByText('app_rw')).toBeInTheDocument();
        });
        expect(screen.queryByText('app01.example.com'))
            .not.toBeInTheDocument();
    });

    it('requests group_by=database and renders the database grouping',
        async () => {
            mockApiGet.mockResolvedValueOnce(okResponse());
            mockApiGet.mockResolvedValueOnce(okResponse([
                makeGroupRow({ group_label: 'appdb' }),
            ]));

            renderSection();

            await waitFor(() => {
                expect(screen.getByText('app_rw')).toBeInTheDocument();
            });

            fireEvent.click(
                screen.getByRole('tab', { name: 'By Database' }),
            );

            await waitFor(() => {
                expect(screen.getByText('appdb')).toBeInTheDocument();
            });
            expect(screen.getByText('Database')).toBeInTheDocument();
            expect(paramOf(1, 'group_by')).toBe('database');
        });

    it('forwards the selected time range', async () => {
        mockRange = '7d';
        mockApiGet.mockResolvedValue(okResponse());

        renderSection();

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });
        expect(paramOf(0, 'time_range')).toBe('7d');
    });

    it('refetches when the refresh trigger changes', async () => {
        mockApiGet.mockResolvedValue(okResponse());

        const { rerender } = renderSection();

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        mockRefreshTrigger = 1;
        rerender(
            <ConnectionsSection
                connectionId={1}
                connectionName="Test Server"
            />,
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('refetches when the connection changes', async () => {
        mockApiGet.mockResolvedValue(okResponse());

        const { rerender } = renderSection(1);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender(
            <ConnectionsSection
                connectionId={42}
                connectionName="Other Server"
            />,
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
        expect(paramOf(1, 'connection_id')).toBe('42');
    });

    it('shows the snapshot timestamp when one is reported', async () => {
        mockApiGet.mockResolvedValue(okResponse());

        renderSection();

        await waitFor(() => {
            expect(screen.getByText(/^As of /)).toBeInTheDocument();
        });
    });

    it('omits the snapshot timestamp when collected_at is null',
        async () => {
            mockApiGet.mockResolvedValue(
                okResponse([makeGroupRow()], null),
            );

            renderSection();

            await waitFor(() => {
                expect(screen.getByText('app_rw')).toBeInTheDocument();
            });
            expect(screen.queryByText(/As of /)).not.toBeInTheDocument();
        });

    it('omits the snapshot timestamp when collected_at is unparseable',
        async () => {
            mockApiGet.mockResolvedValue(
                okResponse([makeGroupRow()], 'not-a-timestamp'),
            );

            renderSection();

            await waitFor(() => {
                expect(screen.getByText('app_rw')).toBeInTheDocument();
            });
            expect(screen.queryByText(/As of /)).not.toBeInTheDocument();
        });
});
