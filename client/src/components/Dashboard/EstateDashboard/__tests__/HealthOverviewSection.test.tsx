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
import HealthOverviewSection from '../HealthOverviewSection';
import { DEFAULT_RETRY_BASE_DELAY_MS } from '../../../../hooks/useRetryingFetch';
import type { EstateSelection } from '../../../../types/selection';
import { MAX_CONNECTION_IDS_PER_REQUEST } from '../../../../utils/connectionIdBatches';

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

// Render each chart's series as text so the counts can be asserted
// without ECharts.
vi.mock('../../../Chart', () => ({
    Chart: ({ data }: {
        data: { categories: string[]; series: { name: string; data: number[] }[] };
    }) => (
        <div data-testid={`chart-${data.series[0].name}`}>
            {data.categories
                .map((c, i) => `${c}=${data.series[0].data[i]}`)
                .join(' ')}
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
                    servers: serverIds.map(id => ({
                        id, name: `s${id}`, status: 'online',
                    })),
                },
            ],
        },
    ],
} as unknown as EstateSelection);

const section = (serverIds: number[]) => (
    <ThemeProvider theme={theme}>
        <HealthOverviewSection
            selection={estateSelection(serverIds)}
            serverIds={serverIds}
        />
    </ThemeProvider>
);

const renderSection = (serverIds: number[]) => render(section(serverIds));

const alertUrls = (): string[] =>
    mockApiFetch.mock.calls
        .map(call => call[0] as string)
        .filter(url => url.includes('/alerts'));

const paramsOf = (url: string): URLSearchParams =>
    new URLSearchParams(url.split('?')[1]);

/**
 * Answer each alerts request with a single row and the given total for
 * its severity, as the server does for limit=1.
 */
const totalsBySeverity = (totals: Record<string, number>) =>
    (url: string): Promise<Response> => {
        const severity = paramsOf(url).get('severity') ?? '';
        return Promise.resolve(okResponse({
            alerts: [{ id: 1, severity }],
            total: totals[severity] ?? 0,
        }));
    };

const alertChartText = (): string =>
    screen.getByTestId('chart-Alerts').textContent ?? '';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('HealthOverviewSection alert distribution', () => {
    beforeEach(() => {
        mockApiFetch.mockReset();
        mockLastRefresh = 0;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('counts each severity from the response total, not one page (issue #864)', async () => {
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 450, warning: 12, info: 3 }),
        );

        renderSection([1, 2]);

        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=450 Warning=12 Info=3');
        });
    });

    it('asks for active alerts on the estate servers only (issue #864)', async () => {
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1 }),
        );

        renderSection([4, 9]);

        await screen.findByTestId('chart-Alerts');

        const urls = alertUrls();
        expect(urls).toHaveLength(3);
        expect(urls.map(u => paramsOf(u).get('severity')).sort())
            .toEqual(['critical', 'info', 'warning']);
        urls.forEach(url => {
            const params = paramsOf(url);
            // Scoped to the selection's servers, which also leaves
            // system alerts out, as the Active Alerts tile does.
            expect(params.get('connection_ids')).toBe('4,9');
            // status=active excludes acknowledged and cleared alerts.
            expect(params.get('status')).toBe('active');
            expect(params.get('limit')).toBe('1');
            expect(params.has('exclude_cleared')).toBe(false);
        });
    });

    it('makes no request and shows no alerts for an empty estate', async () => {
        renderSection([]);

        expect(await screen.findByText('No active alerts')).toBeInTheDocument();
        expect(alertUrls()).toHaveLength(0);
    });

    it('refreshes when the estate data refreshes (issue #864)', async () => {
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1, warning: 2, info: 3 }),
        );

        const { rerender } = renderSection([1]);

        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=1 Warning=2 Info=3');
        });
        expect(alertUrls()).toHaveLength(3);

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 5, warning: 0, info: 0 }),
        );
        mockLastRefresh = 1;
        rerender(section([1]));

        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=5 Warning=0 Info=0');
        });
        expect(alertUrls()).toHaveLength(6);
    });

    it('refetches for the new servers when the selection changes', async () => {
        mockApiFetch.mockImplementation(
            totalsBySeverity({ warning: 2 }),
        );

        const { rerender } = renderSection([1]);
        await screen.findByTestId('chart-Alerts');

        rerender(section([1, 7]));

        await waitFor(() => {
            expect(alertUrls()).toHaveLength(6);
        });
        alertUrls().slice(3).forEach(url => {
            expect(paramsOf(url).get('connection_ids')).toBe('1,7');
        });
    });

    it('sums the totals across connection_ids batches', async () => {
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 2, warning: 1, info: 0 }),
        );

        const serverIds = Array.from(
            { length: MAX_CONNECTION_IDS_PER_REQUEST + 50 },
            (_, i) => i + 1,
        );
        renderSection(serverIds);

        // Two batches, three severities each, and every batch total is
        // added in.
        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=4 Warning=2 Info=0');
        });
        const urls = alertUrls();
        expect(urls).toHaveLength(6);
        const named = urls
            .filter(u => paramsOf(u).get('severity') === 'critical')
            .flatMap(u => (paramsOf(u).get('connection_ids') ?? '').split(','))
            .map(Number);
        expect(named).toEqual(serverIds);
    });

    it('keeps the previous counts and retries when a request fails', async () => {
        vi.useFakeTimers();
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1, warning: 1, info: 1 }),
        );

        const { rerender } = renderSection([1]);
        await act(async () => {
            await vi.runOnlyPendingTimersAsync();
        });
        expect(alertChartText()).toBe('Critical=1 Warning=1 Info=1');

        // One failed severity fails the refresh rather than showing a
        // partial distribution.
        mockApiFetch.mockImplementation((url: string) =>
            paramsOf(url).get('severity') === 'warning'
                ? Promise.resolve(errorResponse())
                : totalsBySeverity({ critical: 9, info: 9 })(url),
        );
        mockLastRefresh = 1;
        rerender(section([1]));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(alertChartText()).toBe('Critical=1 Warning=1 Info=1');

        // The retry picks up the recovered endpoint.
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 2, warning: 2, info: 2 }),
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(alertChartText()).toBe('Critical=2 Warning=2 Info=2');
    });

    it('ignores a response that a newer refresh has superseded', async () => {
        let resolveSlow: (r: Response) => void = () => {};
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1 }),
        );

        const { rerender } = renderSection([1]);
        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=1 Warning=0 Info=0');
        });

        // The first refresh hangs on its critical count; a second
        // refresh then completes first.
        mockApiFetch.mockImplementation((url: string) =>
            paramsOf(url).get('severity') === 'critical'
                ? new Promise<Response>(r => { resolveSlow = r; })
                : totalsBySeverity({})(url),
        );
        mockLastRefresh = 1;
        rerender(section([1]));

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 3 }),
        );
        mockLastRefresh = 2;
        rerender(section([1]));
        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=3 Warning=0 Info=0');
        });

        await act(async () => {
            resolveSlow(okResponse({ alerts: [], total: 99 }));
            await Promise.resolve();
        });
        expect(alertChartText()).toBe('Critical=3 Warning=0 Info=0');
    });

    it('still renders the server status chart from the selection', async () => {
        mockApiFetch.mockImplementation(totalsBySeverity({}));

        renderSection([1, 2, 3]);

        await waitFor(() => {
            expect(screen.getByTestId('chart-Servers').textContent)
                .toBe('OK=3 Warning=0 Offline=0');
        });
        expect(screen.getByText('No active alerts')).toBeInTheDocument();
    });
});
