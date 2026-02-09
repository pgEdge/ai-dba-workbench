/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiGet, apiPost, apiPut, apiDelete, ApiError } from '../apiClient';

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
    let fetchSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
        fetchSpy = vi.fn();
        globalThis.fetch = fetchSpy;
    });

    // ----- apiGet ----------------------------------------------------------

    describe('apiGet', () => {
        it('sends a GET request with credentials included', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({ items: [1, 2, 3] }));

            const result = await apiGet<{ items: number[] }>('/api/v1/items');

            expect(fetchSpy).toHaveBeenCalledOnce();
            const [url, opts] = fetchSpy.mock.calls[0];
            expect(url).toBe('/api/v1/items');
            expect(opts.method).toBe('GET');
            expect(opts.credentials).toBe('include');
            expect(result).toEqual({ items: [1, 2, 3] });
        });

        it('does not set Content-Type header for GET requests', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({}));
            await apiGet('/api/v1/items');

            const headers = fetchSpy.mock.calls[0][1].headers;
            expect(headers['Content-Type']).toBeUndefined();
        });

        it('merges additional headers', async () => {
            fetchSpy.mockResolvedValueOnce(mockResponse({}));
            await apiGet('/api/v1/items', {
                headers: { 'X-Custom': 'value' },
            });

            const headers = fetchSpy.mock.calls[0][1].headers;
            expect(headers['X-Custom']).toBe('value');
        });
    });

    // ----- apiPost ---------------------------------------------------------

    describe('apiPost', () => {
        it('sends a POST request with JSON body', async () => {
            const payload = { name: 'test' };
            fetchSpy.mockResolvedValueOnce(mockResponse({ id: 1 }));

            const result = await apiPost<{ id: number }>('/api/v1/items', payload);

            const [url, opts] = fetchSpy.mock.calls[0];
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

            const opts = fetchSpy.mock.calls[0][1];
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

            const [url, opts] = fetchSpy.mock.calls[0];
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

            const [url, opts] = fetchSpy.mock.calls[0];
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
});
