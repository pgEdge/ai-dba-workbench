/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import {
    apiGet,
    apiPost,
    apiPut,
    apiDelete,
    apiPatch,
    apiFetch,
    ApiError,
    onDisconnect,
    resetConnectionHealth,
    OUTAGE_PROBE_URL,
    OUTAGE_PROBE_TIMEOUT_MS,
} from '../apiClient';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a minimal Response-like object for the global fetch mock. */
function mockResponse(
    body: unknown,
    init: { status?: number; ok?: boolean } = {},
): Response {
    const status = init.status ?? 200;
    const ok = init.ok ?? (status >= 200 && status < 300);
    const bodyStr = typeof body === 'string' ? body : JSON.stringify(body);

    return {
        ok,
        status,
        json: () => Promise.resolve(typeof body === 'string' ? JSON.parse(body) : body),
        text: () => Promise.resolve(bodyStr),
        headers: new Headers({ 'Content-Type': 'application/json' }),
    } as unknown as Response;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('apiClient', () => {
    let fetchSpy: Mock<typeof globalThis.fetch>;

    beforeEach(() => {
        fetchSpy = vi.fn<typeof globalThis.fetch>();
        globalThis.fetch = fetchSpy;
    });

    /*
     * Read one recorded `fetch` call, narrowing the arguments to the
     * shapes `apiClient` actually sends: a string URL, and an init
     * object whose headers are the plain record built by
     * `buildHeaders`. Anything else is a genuine regression, so the
     * helper throws rather than widening the type.
     */
    function fetchCall(index: number): {
        url: string;
        opts: RequestInit & { headers: Record<string, string> };
    } {
        const call = fetchSpy.mock.calls[index];
        if (!call) {
            throw new Error(`fetch was not called ${index + 1} time(s)`);
        }
        const [url, opts] = call;
        if (typeof url !== 'string') {
            throw new Error('expected fetch to be called with a string URL');
        }
        if (!opts) {
            throw new Error('expected fetch to be called with init options');
        }
        const { headers } = opts;
        if (
            headers === undefined ||
            headers instanceof Headers ||
            Array.isArray(headers)
        ) {
            throw new Error('expected a plain headers record');
        }
        return { url, opts: { ...opts, headers } };
    }

    // ----- apiGet ----------------------------------------------------------

    describe('apiGet', () => {
        it('sends a GET request with credentials included', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({ items: [1, 2, 3] }));

            const result = await apiGet<{ items: number[] }>('/api/v1/items');

            expect(fetchSpy).toHaveBeenCalledOnce();
            const { url, opts } = fetchCall(0);
            expect(url).toBe('/api/v1/items');
            expect(opts.method).toBe('GET');
            expect(opts.credentials).toBe('include');
            expect(result).toEqual({ items: [1, 2, 3] });
        });

        it('does not set Content-Type header for GET requests', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({}));
            await apiGet('/api/v1/items');

            const { opts } = fetchCall(0);
            const headers = opts.headers;
            expect(headers['Content-Type']).toBeUndefined();
        });

        it('merges additional headers', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({}));
            await apiGet('/api/v1/items', {
                headers: { 'X-Custom': 'value' },
            });

            const { opts } = fetchCall(0);
            const headers = opts.headers;
            expect(headers['X-Custom']).toBe('value');
        });
    });

    // ----- apiPost ---------------------------------------------------------

    describe('apiPost', () => {
        it('sends a POST request with JSON body', async () => {
            const payload = { name: 'test' };
            fetchSpy.mockResolvedValueOnce(mockResponse({ id: 1 }));

            const result = await apiPost<{ id: number }>('/api/v1/items', payload);

            const { url, opts } = fetchCall(0);
            expect(url).toBe('/api/v1/items');
            expect(opts.method).toBe('POST');
            expect(opts.credentials).toBe('include');
            expect(opts.headers['Content-Type']).toBe('application/json');
            expect(opts.body).toBe(JSON.stringify(payload));
            expect(result).toEqual({ id: 1 });
        });

        it('sends a POST request without body', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({ success: true }));

            await apiPost('/api/v1/items/1/stop');

            const { opts } = fetchCall(0);
            expect(opts.body).toBeUndefined();
            expect(opts.headers['Content-Type']).toBeUndefined();
        });
    });

    // ----- apiPut ----------------------------------------------------------

    describe('apiPut', () => {
        it('sends a PUT request with JSON body', async () => {
            const payload = { enabled: true };
            fetchSpy.mockResolvedValueOnce(mockResponse({ updated: true }));

            await apiPut('/api/v1/items/1', payload);

            const { url, opts } = fetchCall(0);
            expect(url).toBe('/api/v1/items/1');
            expect(opts.method).toBe('PUT');
            expect(opts.headers['Content-Type']).toBe('application/json');
            expect(opts.body).toBe(JSON.stringify(payload));
        });
    });

    // ----- apiDelete -------------------------------------------------------

    describe('apiDelete', () => {
        it('sends a DELETE request', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('', { status: 204 }),
            );

            const result = await apiDelete('/api/v1/items/1');

            const { url, opts } = fetchCall(0);
            expect(url).toBe('/api/v1/items/1');
            expect(opts.method).toBe('DELETE');
            expect(opts.credentials).toBe('include');
            expect(result).toBeUndefined();
        });
    });

    // ----- Error handling --------------------------------------------------

    describe('error handling', () => {
        it('throws ApiError with JSON error body', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse(
                    { error: 'Not found' },
                    { status: 404, ok: false },
                ),
            );

            await expect(apiGet('/api/v1/missing')).rejects.toThrow(ApiError);

            try {
                fetchSpy.mockResolvedValueOnce(
                    mockResponse(
                        { error: 'Not found' },
                        { status: 404, ok: false },
                    ),
                );
                await apiGet('/api/v1/missing');
            } catch (err) {
                expect(err).toBeInstanceOf(ApiError);
                const apiErr = err as ApiError;
                expect(apiErr.message).toBe('Not found');
                expect(apiErr.statusCode).toBe(404);
            }
        });

        it('throws ApiError with plain text error body', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('Something went wrong', { status: 500, ok: false }),
            );

            try {
                await apiGet('/api/v1/broken');
            } catch (err) {
                expect(err).toBeInstanceOf(ApiError);
                const apiErr = err as ApiError;
                expect(apiErr.message).toBe('Something went wrong');
                expect(apiErr.statusCode).toBe(500);
            }
        });

        it('throws ApiError with "message" field from JSON body', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse(
                    { message: 'Validation failed' },
                    { status: 400, ok: false },
                ),
            );

            try {
                await apiPost('/api/v1/items', { bad: 'data' });
            } catch (err) {
                expect(err).toBeInstanceOf(ApiError);
                expect((err as ApiError).message).toBe('Validation failed');
            }
        });

        it('uses fallback message when error body is empty', async () => {
            const emptyResponse = {
                ok: false,
                status: 500,
                json: () => Promise.reject(new Error('no body')),
                text: () => Promise.resolve(''),
                headers: new Headers(),
            } as unknown as Response;
            fetchSpy.mockResolvedValueOnce(emptyResponse);

            try {
                await apiGet('/api/v1/empty');
            } catch (err) {
                expect(err).toBeInstanceOf(ApiError);
                expect((err as ApiError).message).toBe(
                    'Request failed: GET /api/v1/empty',
                );
            }
        });

        /** Build a failed response carrying the given Retry-After value. */
        const retryAfterResponse = (retryAfter: string | null): Response => {
            const headers = new Headers();
            if (retryAfter !== null) {
                headers.set('Retry-After', retryAfter);
            }
            return {
                ok: false,
                status: 429,
                json: () => Promise.reject(new Error('no body')),
                text: () => Promise.resolve('Too many requests'),
                headers,
            } as unknown as Response;
        };

        it.each([
            ['42', 42],
            [' 7 ', 7],
            ['0', 0],
            [null, null],
            ['Wed, 21 Oct 2026 07:28:00 GMT', null],
            ['-5', null],
            ['1.5', null],
        ])(
            'reads Retry-After %j as retryAfterSeconds %j',
            async (header, expected) => {
                fetchSpy.mockResolvedValueOnce(retryAfterResponse(header));

                const err = await apiGet('/api/v1/limited').catch(
                    (e: unknown) => e,
                );
                expect(err).toBeInstanceOf(ApiError);
                expect((err as ApiError).statusCode).toBe(429);
                expect((err as ApiError).retryAfterSeconds).toBe(expected);
            },
        );

        it('defaults retryAfterSeconds to null when constructed directly', () => {
            expect(new ApiError('x', 500).retryAfterSeconds).toBeNull();
        });
    });

    // ----- 204 No Content --------------------------------------------------

    describe('204 No Content', () => {
        it('returns undefined for 204 responses', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('', { status: 204 }),
            );

            const result = await apiDelete('/api/v1/items/1');
            expect(result).toBeUndefined();
        });
    });

    // ----- Raw response ----------------------------------------------------

    describe('rawResponse option', () => {
        it('returns raw text when rawResponse is true', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('plain text output'),
            );

            const result = await apiGet<string>('/api/v1/text', {
                rawResponse: true,
            });

            expect(result).toBe('plain text output');
        });
    });

    // ----- skipHealthTracking option ---------------------------------------

    describe('skipHealthTracking option', () => {
        beforeEach(() => {
            resetConnectionHealth();
        });

        it('leaves the failure counter alone when set', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockRejectedValue(new TypeError('Failed to fetch'));

            // Six failures: twice the threshold, so a counted request
            // would have latched the disconnect long ago.
            for (let i = 0; i < 6; i += 1) {
                await expect(
                    apiGet('/api/v1/capabilities', { skipHealthTracking: true }),
                ).rejects.toThrow();
            }

            expect(listener).not.toHaveBeenCalled();
            unsubscribe();
        });

        it('still counts failures by default', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockRejectedValue(new TypeError('Failed to fetch'));

            for (let i = 0; i < 3; i += 1) {
                await expect(apiGet('/api/v1/alerts')).rejects.toThrow();
            }

            // The threshold now triggers an outage probe first, and
            // the disconnect follows once that probe fails too.
            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledWith('network');
            });
            unsubscribe();
            resetConnectionHealth();
        });

        it('does not reset the counter on a skipped success', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy
                .mockRejectedValueOnce(new TypeError('Failed to fetch'))
                .mockRejectedValueOnce(new TypeError('Failed to fetch'));

            await expect(apiGet('/api/v1/alerts')).rejects.toThrow();
            await expect(apiGet('/api/v1/alerts')).rejects.toThrow();

            fetchSpy.mockResolvedValueOnce(mockResponse({ ok: true }));
            await apiGet('/api/v1/capabilities', { skipHealthTracking: true });

            // The probe neither cleared the two real failures nor added
            // to them, so the next real failure is still the third.
            fetchSpy.mockRejectedValueOnce(new TypeError('Failed to fetch'));
            await expect(apiGet('/api/v1/alerts')).rejects.toThrow();

            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledTimes(1);
            });
            unsubscribe();
            resetConnectionHealth();
        });
    });

    // ----- PATCH, abort signals and auth failures ---------------------------

    describe('apiPatch', () => {
        it('sends a PATCH with a JSON body', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({ patched: true }));

            const result = await apiPatch('/api/v1/items/1', { title: 'New' });

            const { url, opts } = fetchCall(0);
            expect(url).toBe('/api/v1/items/1');
            expect(opts.method).toBe('PATCH');
            expect(opts.body).toBe(JSON.stringify({ title: 'New' }));
            expect(result).toEqual({ patched: true });
        });
    });

    describe('signal option', () => {
        it('passes the abort signal through to fetch', async () => {
            const controller = new AbortController();
            fetchSpy.mockResolvedValueOnce(mockResponse({}));

            await apiGet('/api/v1/items', { signal: controller.signal });

            expect(fetchCall(0).opts.signal).toBe(controller.signal);
        });
    });

    describe('auth failure tracking', () => {
        beforeEach(() => {
            resetConnectionHealth();
        });

        it('reports an auth disconnect once, on the first 401', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockResolvedValue(
                mockResponse({ error: 'unauthorised' }, { status: 401, ok: false }),
            );

            await expect(apiGet('/api/v1/alerts')).rejects.toThrow(ApiError);
            await expect(apiGet('/api/v1/alerts')).rejects.toThrow(ApiError);

            // Further failures after the latch are ignored.
            expect(listener).toHaveBeenCalledTimes(1);
            expect(listener).toHaveBeenCalledWith('auth');
            unsubscribe();
            resetConnectionHealth();
        });
    });

    // ----- apiFetch ----------------------------------------------------------

    describe('apiFetch', () => {
        beforeEach(() => {
            resetConnectionHealth();
        });

        it('returns the raw response with credentials included', async () => {
            const response = mockResponse({ ok: true });
            fetchSpy.mockResolvedValueOnce(response);

            const result = await apiFetch('/api/v1/llm/chat', { method: 'POST' });

            expect(result).toBe(response);
            const [, init] = fetchSpy.mock.calls[0];
            expect(init?.credentials).toBe('include');
            expect(init?.method).toBe('POST');
        });

        it('returns a failed response without throwing', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('nope', { status: 404, ok: false }),
            );

            const result = await apiFetch('/api/v1/missing');

            expect(result.status).toBe(404);
        });

        it('reports an auth disconnect on a 401', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockResolvedValueOnce(
                mockResponse('unauthorised', { status: 401, ok: false }),
            );

            await apiFetch('/api/v1/llm/chat');

            expect(listener).toHaveBeenCalledWith('auth');
            unsubscribe();
            resetConnectionHealth();
        });

        it('counts server errors towards a disconnect', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockResolvedValue(
                mockResponse('broken', { status: 503, ok: false }),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch('/api/v1/llm/chat');
            }

            // The threshold now triggers an outage probe first, and
            // the disconnect follows once that probe fails too.
            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledWith('server');
            });
            unsubscribe();
            resetConnectionHealth();
        });

        it('records network errors and rethrows them', async () => {
            const listener = vi.fn();
            const unsubscribe = onDisconnect(listener);
            fetchSpy.mockRejectedValue(new TypeError('Failed to fetch'));

            for (let i = 0; i < 3; i += 1) {
                await expect(apiFetch('/api/v1/llm/chat')).rejects.toThrow(
                    'Failed to fetch',
                );
            }

            // The threshold now triggers an outage probe first, and
            // the disconnect follows once that probe fails too.
            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledWith('network');
            });
            unsubscribe();
            resetConnectionHealth();
        });
    });
    // ----- Outage confirmation (#872) ----------------------------------------

    describe('outage confirmation probe', () => {
        const FAILING_URL = '/api/v1/timeline/events';

        /*
         * Route the fetch mock by URL: the probe gets `probe`, the
         * failing endpoint gets a 500, and anything else succeeds.
         */
        function routeFetch(
            probe: () => Promise<Response>,
        ): void {
            fetchSpy.mockImplementation((input) => {
                const url = String(input);
                if (url === OUTAGE_PROBE_URL) {
                    return probe();
                }
                if (url === FAILING_URL) {
                    return Promise.resolve(
                        mockResponse('boom', { status: 500, ok: false }),
                    );
                }
                return Promise.resolve(mockResponse({ ok: true }));
            });
        }

        function probeCalls(): number {
            return fetchSpy.mock.calls.filter(
                ([input]) => String(input) === OUTAGE_PROBE_URL,
            ).length;
        }

        /** Let pending probe promises settle. */
        async function settle(): Promise<void> {
            for (let i = 0; i < 10; i += 1) {
                await Promise.resolve();
            }
        }

        let listener: Mock;
        let unsubscribe: () => void;

        beforeEach(() => {
            resetConnectionHealth();
            listener = vi.fn();
            unsubscribe = onDisconnect(listener);
        });

        afterEach(() => {
            unsubscribe();
            resetConnectionHealth();
            vi.useRealTimers();
        });

        it('does not disconnect when one endpoint fails and others succeed', async () => {
            routeFetch(() => Promise.resolve(mockResponse({ ok: true })));

            for (let i = 0; i < 9; i += 1) {
                await expect(apiGet(FAILING_URL)).rejects.toThrow();
                await apiGet('/api/v1/alerts');
            }
            await settle();

            expect(listener).not.toHaveBeenCalled();
        });

        it('does not disconnect when one endpoint fails with no other traffic but the server answers the probe', async () => {
            routeFetch(() => Promise.resolve(mockResponse({ ok: true })));

            // Nine consecutive failures of the same endpoint, which is
            // what the timeline's retries produced in the report.
            for (let i = 0; i < 9; i += 1) {
                await expect(apiGet(FAILING_URL)).rejects.toThrow();
                await settle();
            }

            expect(listener).not.toHaveBeenCalled();
            // One probe per run of three consecutive failures.
            expect(probeCalls()).toBe(3);
        });

        it('sends the probe with credentials and outside health tracking', async () => {
            routeFetch(() => Promise.resolve(mockResponse({ ok: true })));

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            await settle();

            const probe = fetchSpy.mock.calls.find(
                ([input]) => String(input) === OUTAGE_PROBE_URL,
            );
            expect(probe?.[1]?.credentials).toBe('include');
            expect(probe?.[1]?.signal).toBeInstanceOf(AbortSignal);
            expect(listener).not.toHaveBeenCalled();
        });

        it('treats a 4xx probe answer as the server being reachable', async () => {
            routeFetch(() =>
                Promise.resolve(mockResponse('nope', { status: 404, ok: false })),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            await settle();

            expect(listener).not.toHaveBeenCalled();
        });

        it('disconnects when the server cannot be reached at all', async () => {
            fetchSpy.mockRejectedValue(new TypeError('Failed to fetch'));

            for (let i = 0; i < 3; i += 1) {
                await expect(apiGet('/api/v1/alerts')).rejects.toThrow();
            }

            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledWith('network');
            });
            expect(listener).toHaveBeenCalledTimes(1);
            expect(probeCalls()).toBe(1);
        });

        it('disconnects when every request, the probe included, answers 5xx', async () => {
            fetchSpy.mockResolvedValue(
                mockResponse('bad gateway', { status: 502, ok: false }),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch('/api/v1/alerts');
            }

            await vi.waitFor(() => {
                expect(listener).toHaveBeenCalledWith('server');
            });
        });

        it('disconnects when the probe does not answer in time', async () => {
            vi.useFakeTimers();
            routeFetch(
                () =>
                    new Promise<Response>((_resolve, reject) => {
                        const signal = fetchSpy.mock.calls.at(-1)?.[1]?.signal;
                        signal?.addEventListener('abort', () => {
                            reject(new DOMException('aborted', 'AbortError'));
                        });
                    }),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            await settle();
            expect(listener).not.toHaveBeenCalled();

            await vi.advanceTimersByTimeAsync(OUTAGE_PROBE_TIMEOUT_MS);

            expect(listener).toHaveBeenCalledWith('server');
        });

        it('runs only one probe at a time', async () => {
            let answerProbe: (r: Response) => void = () => {};
            routeFetch(
                () =>
                    new Promise<Response>((resolve) => {
                        answerProbe = resolve;
                    }),
            );

            for (let i = 0; i < 8; i += 1) {
                await apiFetch(FAILING_URL);
            }
            expect(probeCalls()).toBe(1);

            answerProbe(mockResponse({ ok: true }));
            await settle();
            expect(listener).not.toHaveBeenCalled();
        });

        it('does not disconnect when a request succeeds while the probe is pending', async () => {
            let failProbe: (e: Error) => void = () => {};
            routeFetch(
                () =>
                    new Promise<Response>((_resolve, reject) => {
                        failProbe = reject;
                    }),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            await apiGet('/api/v1/alerts');

            failProbe(new TypeError('Failed to fetch'));
            await settle();

            expect(listener).not.toHaveBeenCalled();
        });

        it('ignores a probe superseded by a success and a new run of failures', async () => {
            const probes: Array<{
                resolve: (r: Response) => void;
                reject: (e: Error) => void;
            }> = [];
            routeFetch(
                () =>
                    new Promise<Response>((resolve, reject) => {
                        probes.push({ resolve, reject });
                    }),
            );

            // Run 1 starts probe P1; a success interrupts the run.
            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            await apiGet('/api/v1/alerts');

            // Run 2 reaches the threshold again before P1 settles, and
            // must start a fresh probe rather than reuse P1's answer.
            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            expect(probeCalls()).toBe(2);

            probes[0].reject(new TypeError('Failed to fetch'));
            await settle();
            expect(listener).not.toHaveBeenCalled();

            probes[1].resolve(mockResponse({ ok: true }));
            await settle();
            expect(listener).not.toHaveBeenCalled();
        });

        it('ignores a probe that resolves after the health state was reset', async () => {
            let failProbe: (e: Error) => void = () => {};
            routeFetch(
                () =>
                    new Promise<Response>((_resolve, reject) => {
                        failProbe = reject;
                    }),
            );

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            resetConnectionHealth();

            failProbe(new TypeError('Failed to fetch'));
            await settle();

            expect(listener).not.toHaveBeenCalled();
        });

        it('still reports a 401 immediately, without a probe', async () => {
            fetchSpy.mockResolvedValueOnce(
                mockResponse('unauthorised', { status: 401, ok: false }),
            );

            await expect(apiGet('/api/v1/alerts')).rejects.toThrow();

            expect(listener).toHaveBeenCalledWith('auth');
            expect(probeCalls()).toBe(0);
        });

        it('reports a 401 that arrives while a probe is pending', async () => {
            let answerProbe: (r: Response) => void = () => {};
            fetchSpy.mockImplementation((input) => {
                if (String(input) === OUTAGE_PROBE_URL) {
                    return new Promise<Response>((resolve) => {
                        answerProbe = resolve;
                    });
                }
                return Promise.resolve(
                    mockResponse('boom', { status: 500, ok: false }),
                );
            });

            for (let i = 0; i < 3; i += 1) {
                await apiFetch(FAILING_URL);
            }
            fetchSpy.mockImplementationOnce(() =>
                Promise.resolve(
                    mockResponse('unauthorised', { status: 401, ok: false }),
                ),
            );
            await apiFetch('/api/v1/alerts');
            expect(listener).toHaveBeenCalledWith('auth');

            // The late probe answer must not fire a second disconnect.
            answerProbe(mockResponse('down', { status: 503, ok: false }));
            await settle();
            expect(listener).toHaveBeenCalledTimes(1);
        });
    });
});
