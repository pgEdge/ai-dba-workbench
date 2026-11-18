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
import HealthOverviewSection, { ALERT_COUNT_REQUEST_TIMEOUT_MS } from '../HealthOverviewSection';
import { DEFAULT_RETRY_BASE_DELAY_MS } from '../../../../hooks/useRetryingFetch';
import type { EstateSelection } from '../../../../types/selection';
import { MAX_CONNECTION_IDS_PER_REQUEST } from '../../../../utils/connectionIdBatches';
import { onDisconnect, resetConnectionHealth } from '../../../../utils/apiClient';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();

// apiFetch is replaced so each test can answer the requests; the rest
// of the module, including its connection-health tracking, is real.
vi.mock('../../../../utils/apiClient', async (importOriginal) => ({
    ...(await importOriginal<typeof import('../../../../utils/apiClient')>()),
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

/** A request that answers only by rejecting when it is aborted. */
const hangingRequest = (init?: RequestInit): Promise<Response> =>
    new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('Aborted', 'AbortError'));
        });
    });

/**
 * A response with the given status whose body never arrives: json()
 * rejects only when the request is aborted.
 */
const stalledBodyResponse = (status: number, init?: RequestInit): Response => ({
    ok: status >= 200 && status < 300,
    status,
    json: () => hangingRequest(init),
} as unknown as Response);

/** Let the pending promise callbacks run without advancing time. */
const flush = async (): Promise<void> => {
    await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
    });
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

        // The same array on both renders, so only lastRefresh changes.
        const serverIds = [1];
        const { rerender } = renderSection(serverIds);

        await waitFor(() => {
            expect(alertChartText()).toBe('Critical=1 Warning=2 Info=3');
        });
        expect(alertUrls()).toHaveLength(3);

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 5, warning: 0, info: 0 }),
        );
        mockLastRefresh = 1;
        rerender(section(serverIds));

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

    it('shows the counts as unavailable, not as zero, while a failed first load retries', async () => {
        vi.useFakeTimers();
        mockApiFetch.mockImplementation(() => Promise.resolve(errorResponse()));

        renderSection([1]);
        await flush();

        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();
        expect(screen.queryByText('No active alerts')).not.toBeInTheDocument();
        expect(screen.queryByTestId('chart-Alerts')).not.toBeInTheDocument();

        // A successful zero result is a real "no alerts".
        mockApiFetch.mockImplementation(totalsBySeverity({}));
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(screen.getByText('No active alerts')).toBeInTheDocument();
        expect(screen.queryByText('Alert counts unavailable')).not.toBeInTheDocument();
    });

    it('does not show the previous servers\' counts when the new servers fail', async () => {
        vi.useFakeTimers();
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 4, warning: 4, info: 4 }),
        );

        const { rerender } = renderSection([1]);
        await flush();
        expect(alertChartText()).toBe('Critical=4 Warning=4 Info=4');

        mockApiFetch.mockImplementation(() => Promise.resolve(errorResponse()));
        rerender(section([7]));
        await flush();

        expect(screen.queryByTestId('chart-Alerts')).not.toBeInTheDocument();
        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();
    });

    it('fails the attempt at the first failed request without waiting for the others', async () => {
        vi.useFakeTimers();
        // The first request fails; any other would never answer.
        mockApiFetch.mockImplementation((url: string, init?: RequestInit) =>
            paramsOf(url).get('severity') === 'critical'
                ? Promise.resolve(errorResponse())
                : hangingRequest(init),
        );

        renderSection([1]);
        await flush();
        expect(alertUrls()).toHaveLength(1);

        // The failure is known, so the retry is scheduled straight away.
        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1, warning: 2, info: 3 }),
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(alertChartText()).toBe('Critical=1 Warning=2 Info=3');
    });

    it('aborts a request that never answers and retries', async () => {
        vi.useFakeTimers();
        mockApiFetch.mockImplementation(
            (_url: string, init?: RequestInit) => hangingRequest(init),
        );

        renderSection([1]);
        await flush();
        expect(alertUrls()).toHaveLength(1);

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 0, warning: 5, info: 0 }),
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(ALERT_COUNT_REQUEST_TIMEOUT_MS);
        });
        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(alertChartText()).toBe('Critical=0 Warning=5 Info=0');
    });

    it('aborts a failed response whose body never arrives and retries', async () => {
        vi.useFakeTimers();
        let signal: AbortSignal | undefined;
        mockApiFetch.mockImplementation((_url: string, init?: RequestInit) => {
            signal = init?.signal ?? undefined;
            return Promise.resolve(stalledBodyResponse(500, init));
        });

        renderSection([1]);
        await flush();

        // The attempt fails on the status alone and cancels the body
        // rather than leaving it unbounded once the timer is cleared.
        expect(alertUrls()).toHaveLength(1);
        expect(signal?.aborted).toBe(true);
        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 2, warning: 0, info: 1 }),
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(alertChartText()).toBe('Critical=2 Warning=0 Info=1');
    });

    it('times out a successful response whose body never arrives and retries', async () => {
        vi.useFakeTimers();
        // The headers arrive at once; only the body stalls, so the
        // timer must still be running while the body is read.
        mockApiFetch.mockImplementation(
            (_url: string, init?: RequestInit) =>
                Promise.resolve(stalledBodyResponse(200, init)),
        );

        renderSection([1]);
        await flush();
        expect(alertUrls()).toHaveLength(1);
        expect(screen.getByLabelText('Loading alert distribution')).toBeInTheDocument();

        mockApiFetch.mockImplementation(
            totalsBySeverity({ critical: 1, warning: 1, info: 1 }),
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(ALERT_COUNT_REQUEST_TIMEOUT_MS);
        });
        expect(screen.queryByLabelText('Loading alert distribution')).not.toBeInTheDocument();
        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(alertChartText()).toBe('Critical=1 Warning=1 Info=1');
    });

    it('times out each request, not the whole attempt, so slow batches still load', async () => {
        vi.useFakeTimers();
        // Each request answers just inside its own limit, so the six
        // requests for two batches take far longer than one limit.
        const delay = ALERT_COUNT_REQUEST_TIMEOUT_MS - 1000;
        const answer = totalsBySeverity({ critical: 1, warning: 2, info: 3 });
        mockApiFetch.mockImplementation((url: string, init?: RequestInit) =>
            new Promise<Response>((resolve, reject) => {
                const timer = setTimeout(() => { void answer(url).then(resolve); }, delay);
                init?.signal?.addEventListener('abort', () => {
                    clearTimeout(timer);
                    reject(new DOMException('Aborted', 'AbortError'));
                });
            }),
        );

        const serverIds = Array.from(
            { length: MAX_CONNECTION_IDS_PER_REQUEST + 1 },
            (_, i) => i + 1,
        );
        renderSection(serverIds);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(delay * 6);
        });
        expect(alertUrls()).toHaveLength(6);
        expect(alertChartText()).toBe('Critical=2 Warning=4 Info=6');
    });

    it('keeps Server Status visible while the alert counts load', async () => {
        vi.useFakeTimers();
        mockApiFetch.mockImplementation(
            (_url: string, init?: RequestInit) => hangingRequest(init),
        );

        renderSection([1, 2]);
        await flush();

        expect(screen.getByTestId('chart-Servers').textContent)
            .toBe('OK=2 Warning=0 Offline=0');
        expect(screen.getByLabelText('Loading alert distribution')).toBeInTheDocument();
        expect(screen.queryByTestId('chart-Alerts')).not.toBeInTheDocument();

        // Still there once the attempt has failed and is waiting to retry.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(ALERT_COUNT_REQUEST_TIMEOUT_MS);
        });
        expect(screen.getByTestId('chart-Servers')).toBeInTheDocument();
        expect(screen.getByText('Alert counts unavailable')).toBeInTheDocument();
    });

    it('counts a failed attempt at most once toward the disconnect threshold', async () => {
        vi.useFakeTimers();
        resetConnectionHealth();
        const disconnected = vi.fn();
        const unsubscribe = onDisconnect(disconnected);
        const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(
            () => Promise.resolve(new Response('{}', { status: 500 })),
        );
        const { apiFetch: realApiFetch } = await vi.importActual<
            typeof import('../../../../utils/apiClient')
        >('../../../../utils/apiClient');
        mockApiFetch.mockImplementation(realApiFetch);

        try {
            renderSection([1]);
            await flush();

            // Every request would fail with a 5xx, but the attempt stops
            // at the first, so it makes one request and counts one
            // failure; three would have reached the threshold.
            expect(fetchSpy).toHaveBeenCalledTimes(1);
            expect(disconnected).not.toHaveBeenCalled();

            // The retry is a second attempt and a second failure.
            await act(async () => {
                await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
            });
            expect(fetchSpy).toHaveBeenCalledTimes(2);
            expect(disconnected).not.toHaveBeenCalled();
        } finally {
            unsubscribe();
            fetchSpy.mockRestore();
            resetConnectionHealth();
        }
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
        let slowSignal: AbortSignal | undefined;
        mockApiFetch.mockImplementation((url: string, init?: RequestInit) => {
            if (paramsOf(url).get('severity') === 'critical') {
                slowSignal = init?.signal ?? undefined;
                return new Promise<Response>(r => { resolveSlow = r; });
            }
            return totalsBySeverity({})(url);
        });
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

        // A superseded request is not aborted while it is in flight; only
        // its response is discarded once it arrives.
        expect(slowSignal?.aborted).toBe(false);

        await act(async () => {
            resolveSlow(okResponse({ alerts: [], total: 99 }));
            await Promise.resolve();
        });
        expect(alertChartText()).toBe('Critical=3 Warning=0 Info=0');
        // The discarded response is aborted so its body is not left
        // downloading without a deadline.
        expect(slowSignal?.aborted).toBe(true);
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
