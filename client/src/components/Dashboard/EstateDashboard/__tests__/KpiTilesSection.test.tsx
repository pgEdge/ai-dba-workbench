/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import KpiTilesSection from '../KpiTilesSection';
import { DEFAULT_RETRY_BASE_DELAY_MS } from '../../../../hooks/useRetryingFetch';
import type { EstateSelection } from '../../../../types/selection';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();

vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockUser = { id: 1, username: 'testuser' };

vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

let mockLastRefresh = 0;

vi.mock('../../../../contexts/useClusterData', () => ({
    useClusterData: () => ({ lastRefresh: mockLastRefresh }),
}));

// Stub KpiTile to a plain element so the test does not depend on the
// AICapabilities provider or MUI theming details of the real tile.
vi.mock('../../KpiTile', () => ({
    default: ({ label, value, unit }: { label: string; value: string; unit?: string }) => (
        <div data-testid="kpi-tile">
            <span>{label}</span>
            <span>{unit ? `${value} ${unit}` : value}</span>
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function okResponse(body: unknown): Response {
    return {
        ok: true,
        status: 200,
        json: () => Promise.resolve(body),
    } as unknown as Response;
}

function errorResponse(): Response {
    return {
        ok: false,
        status: 500,
        json: () => Promise.resolve({}),
    } as unknown as Response;
}

const theme = createTheme();

const renderSection = (serverIds: number[]) => {
    const selection = {
        type: 'estate',
        name: 'Estate',
        status: 'online',
        groups: [
            {
                name: 'group-1',
                clusters: [
                    {
                        name: 'c1',
                        servers: serverIds.map(id => ({ id, name: `s${id}` })),
                    },
                ],
            },
        ],
    } as unknown as EstateSelection;

    return render(
        <ThemeProvider theme={theme}>
            <KpiTilesSection selection={selection} serverIds={serverIds} />
        </ThemeProvider>,
    );
};

const perfBody = {
    connections: [
        { transactions: { commits_per_sec: 3.5 } },
        { transactions: { commits_per_sec: 1.25 } },
    ],
};

const alertsBody = { alerts: [{ id: 1 }, { id: 2 }, { id: 3 }] };

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('KpiTilesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockLastRefresh = 0;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('renders aggregated KPI tiles after fetching', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );

        renderSection([1, 2]);

        // Two servers in the estate.
        await screen.findByText('Total Servers');
        await waitFor(() => {
            // Transaction rate is the sum of commits_per_sec, rounded.
            expect(screen.getByText('4.75 tx/s')).toBeInTheDocument();
        });

        // Total connections equals the number of connection entries.
        expect(screen.getByText('Total Connections')).toBeInTheDocument();
        // Active alerts reflects the alerts payload length.
        expect(screen.getByText('3')).toBeInTheDocument();
    });

    it('renders nothing to fetch when there are no server ids', async () => {
        renderSection([]);

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiFetch).not.toHaveBeenCalled();
        // Falls back to the zero-value tiles.
        expect(screen.getByText('Total Servers')).toBeInTheDocument();
    });

    it('treats a non-ok performance response as a failure and retries', async () => {
        vi.useFakeTimers();
        // First attempt: the performance call is non-OK, so the whole
        // fetch must fail rather than rendering zero/partial data.
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(errorResponse()),
        );

        renderSection([1]);

        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        // The failure surfaces as the reconnecting indicator; no KPI
        // tiles are shown from the partial data.
        expect(screen.getByText('Reconnecting…')).toBeInTheDocument();
        expect(screen.queryByText('Active Alerts')).not.toBeInTheDocument();

        // The scheduled retry now sees both calls succeed and recovers.
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });

        expect(screen.queryByText('Reconnecting…')).not.toBeInTheDocument();
        expect(screen.getByText('Active Alerts')).toBeInTheDocument();
        expect(screen.getByText('3')).toBeInTheDocument();
    });

    it('shows a reconnecting indicator while retrying after a failure', async () => {
        vi.useFakeTimers();
        // First attempt rejects (both calls in the Promise.all fail).
        mockApiFetch.mockRejectedValueOnce(new Error('down'));
        mockApiFetch.mockRejectedValueOnce(new Error('down'));

        renderSection([1]);

        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(screen.getByText('Reconnecting…')).toBeInTheDocument();

        // Recover on the scheduled retry.
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });

        expect(screen.queryByText('Reconnecting…')).not.toBeInTheDocument();
        expect(screen.getByText('Active Alerts')).toBeInTheDocument();
    });
});
