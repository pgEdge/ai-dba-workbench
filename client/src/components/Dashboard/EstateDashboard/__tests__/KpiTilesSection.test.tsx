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
import type { TimeRangeState } from '../../types';

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

let mockTimeRange: TimeRangeState = { range: '24h' };

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({ timeRange: mockTimeRange }),
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

const estateSelection = (serverIds: number[]): EstateSelection => ({
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
} as unknown as EstateSelection);

const renderSection = (serverIds: number[]) => render(
    <ThemeProvider theme={theme}>
        <KpiTilesSection
            selection={estateSelection(serverIds)}
            serverIds={serverIds}
        />
    </ThemeProvider>,
);

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
        mockTimeRange = { range: '24h' };
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('requests the summary for the selected preset window', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );
        mockTimeRange = { range: '7d' };

        renderSection([1, 2]);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                '/api/v1/metrics/performance-summary'
                + '?connection_ids=1%2C2&time_range=7d',
            );
        });
    });

    it('sends both bounds for a custom window', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );
        mockTimeRange = {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        };

        renderSection([1]);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                expect.stringContaining('time_range=custom'),
            );
        });
        const url = mockApiFetch.mock.calls
            .map(call => call[0] as string)
            .find(candidate => candidate.includes('performance-summary'));
        expect(url).toContain('time_start=');
        expect(url).toContain('time_end=');
    });

    it('makes no request when a custom bound is missing', async () => {
        mockApiFetch.mockImplementation((url: string) =>
            url.includes('/alerts')
                ? Promise.resolve(okResponse(alertsBody))
                : Promise.resolve(okResponse(perfBody)),
        );
        mockTimeRange = {
            range: 'custom',
            customEnd: '2026-09-02T00:00:00Z',
        };

        renderSection([1]);

        await waitFor(() => {
            expect(screen.getByText('Total Servers')).toBeInTheDocument();
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
    });


    it('discards a response for a superseded time range', async () => {
        // Hold the performance-summary request open so that the window
        // can be changed whilst the first one is still in flight; the
        // alerts request answers immediately either way.
        const resolvers: Record<string, (body: unknown) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            if (url.includes('/alerts')) {
                return Promise.resolve(okResponse(alertsBody));
            }
            const range = new URLSearchParams(url.split('?')[1] ?? '')
                .get('time_range') ?? '';
            return new Promise(resolve => {
                resolvers[range] = (body: unknown) => resolve(
                    okResponse(body),
                );
            });
        });

        mockTimeRange = { range: '24h' };
        const { rerender } = renderSection([1]);
        await waitFor(() => {
            expect(resolvers['24h']).toBeDefined();
        });

        mockTimeRange = { range: '1h' };
        rerender(
            <ThemeProvider theme={theme}>
                <KpiTilesSection selection={estateSelection([1])} serverIds={[1]} />
            </ThemeProvider>,
        );
        await waitFor(() => {
            expect(resolvers['1h']).toBeDefined();
        });

        // The newly selected window answers first.
        await act(async () => {
            resolvers['1h'](perfBody);
        });
        await waitFor(() => {
            expect(screen.getByText('4.75 tx/s')).toBeInTheDocument();
        });

        // The superseded request then arrives late and must not
        // overwrite the KPI values belonging to the current window.
        await act(async () => {
            resolvers['24h']({
                connections: [{ transactions: { commits_per_sec: 99 } }],
            });
        });
        expect(screen.getByText('4.75 tx/s')).toBeInTheDocument();
        expect(screen.queryByText('99 tx/s')).not.toBeInTheDocument();
    });

    it('discards a pending response when a custom bound is cleared',
        async () => {
            let resolvePending: ((body: unknown) => void) | null = null;
            mockApiFetch.mockImplementation((url: string) => {
                if (url.includes('/alerts')) {
                    return Promise.resolve(okResponse(alertsBody));
                }
                return new Promise(resolve => {
                    resolvePending = (body: unknown) => resolve(
                        okResponse(body),
                    );
                });
            });

            mockTimeRange = { range: '24h' };
            const { rerender } = renderSection([1]);
            await waitFor(() => {
                expect(resolvePending).not.toBeNull();
            });

            // A half-entered custom range makes no request of its own,
            // so the in-flight one has to be abandoned explicitly.
            mockTimeRange = {
                range: 'custom',
                customStart: '2026-09-01T00:00:00Z',
            };
            rerender(
                <ThemeProvider theme={theme}>
                    <KpiTilesSection
                        selection={estateSelection([1])}
                        serverIds={[1]}
                    />
                </ThemeProvider>,
            );

            await act(async () => {
                resolvePending!(perfBody);
            });

            // The tiles keep their placeholder zeroes rather than
            // showing figures for the abandoned window.
            expect(screen.getByText('0 tx/s')).toBeInTheDocument();
            expect(screen.queryByText('4.75 tx/s')).not.toBeInTheDocument();
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
