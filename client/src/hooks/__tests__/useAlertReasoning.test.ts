/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
    useAlertReasoning,
    clearAlertReasoningCache,
} from '../useAlertReasoning';
import { ApiError } from '../../utils/apiClient';

const mockApiGet = vi.fn();
vi.mock('../../utils/apiClient', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../utils/apiClient')>();
    return {
        ...actual,
        apiGet: (...args: unknown[]) => mockApiGet(...args),
    };
});

const URL_FOR = (id: number | string) => `/api/v1/alerts/reasoning?alert_id=${id}`;

/** A promise whose resolution the test controls. */
const deferred = <T,>() => {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((r) => { resolve = r; });
    return { promise, resolve };
};

describe('useAlertReasoning', () => {
    beforeEach(() => {
        mockApiGet.mockReset();
        clearAlertReasoningCache();
    });

    it('does not fetch until load is called', () => {
        const { result } = renderHook(() => useAlertReasoning(1));
        expect(result.current.state).toEqual({ status: 'idle' });
        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('loads available reasoning and caches it for the session', async () => {
        mockApiGet.mockResolvedValue({
            alert_id: 1,
            status: 'available',
            source: 'decision',
            reasoning: 'Connections rose sharply.',
        });
        const { result } = renderHook(() => useAlertReasoning(1));

        act(() => { result.current.load(); });
        expect(result.current.state).toEqual({ status: 'loading' });
        await waitFor(() => {
            expect(result.current.state).toEqual({
                status: 'available',
                reasoning: 'Connections rose sharply.',
                source: 'decision',
            });
        });
        expect(mockApiGet).toHaveBeenCalledWith(URL_FOR(1));

        // A fresh consumer starts from the cache and never refetches,
        // even when forced.
        const second = renderHook(() => useAlertReasoning(1));
        expect(second.result.current.state.status).toBe('available');
        act(() => { second.result.current.load(true); });
        act(() => { result.current.load(true); });
        expect(mockApiGet).toHaveBeenCalledTimes(1);
    });

    it('reports unavailable and refetches only when forced', async () => {
        mockApiGet.mockResolvedValueOnce({ alert_id: 2, status: 'unavailable' });
        const { result } = renderHook(() => useAlertReasoning(2));

        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state).toEqual({ status: 'unavailable' });
        });

        act(() => { result.current.load(); });
        expect(mockApiGet).toHaveBeenCalledTimes(1);

        mockApiGet.mockResolvedValueOnce({
            alert_id: 2,
            status: 'available',
            source: 'reasoning',
            reasoning: 'Stored text.',
        });
        act(() => { result.current.load(true); });
        await waitFor(() => {
            expect(result.current.state.status).toBe('available');
        });
        expect(mockApiGet).toHaveBeenCalledTimes(2);
    });

    it('reports not_applicable without caching it', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 3, status: 'not_applicable' });
        const { result, unmount } = renderHook(() => useAlertReasoning(3));
        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state).toEqual({ status: 'not_applicable' });
        });
        unmount();
        const again = renderHook(() => useAlertReasoning(3));
        expect(again.result.current.state).toEqual({ status: 'idle' });
    });

    it('treats available without text as unavailable', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 4, status: 'available', reasoning: '   ' });
        const { result } = renderHook(() => useAlertReasoning(4));
        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state).toEqual({ status: 'unavailable' });
        });
    });

    it('treats an unknown status as a retryable error', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 5, status: 'bogus' });
        const { result } = renderHook(() => useAlertReasoning(5));
        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state).toMatchObject({ status: 'error', retryable: true });
        });
    });

    it.each([
        [new ApiError('not found', 404), false, /no longer exists/],
        [new ApiError('bad id', 400), false, /rejected/],
        [new ApiError('boom', 500), true, /could not be loaded/],
        [new TypeError('Failed to fetch'), true, /could not be loaded/],
    ])('maps %s to an error state', async (err, retryable, message) => {
        mockApiGet.mockRejectedValue(err);
        const { result } = renderHook(() => useAlertReasoning(6));
        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state.status).toBe('error');
        });
        const { state } = result.current;
        if (state.status !== 'error') {throw new Error('expected error');}
        expect(state.retryable).toBe(retryable);
        expect(state.message).toMatch(message);
    });

    it('shares one request between concurrent consumers', async () => {
        const pending = deferred<unknown>();
        mockApiGet.mockReturnValue(pending.promise);
        const a = renderHook(() => useAlertReasoning(7));
        const b = renderHook(() => useAlertReasoning(7));

        act(() => {
            a.result.current.load();
            b.result.current.load();
        });
        expect(mockApiGet).toHaveBeenCalledTimes(1);

        await act(async () => {
            pending.resolve({ alert_id: 7, status: 'available', reasoning: 'Shared.' });
            await pending.promise;
        });
        expect(a.result.current.state.status).toBe('available');
        expect(b.result.current.state.status).toBe('available');
    });

    it('caches a result that arrives after unmount', async () => {
        const pending = deferred<unknown>();
        mockApiGet.mockReturnValue(pending.promise);
        const { result, unmount } = renderHook(() => useAlertReasoning(8));
        act(() => { result.current.load(); });
        unmount();

        await act(async () => {
            pending.resolve({ alert_id: 8, status: 'available', reasoning: 'Late.' });
            await pending.promise;
        });
        const again = renderHook(() => useAlertReasoning(8));
        expect(again.result.current.state).toMatchObject({
            status: 'available',
            reasoning: 'Late.',
        });
    });

    it('drops a response for a previous alert id and resets state', async () => {
        const pending = deferred<unknown>();
        mockApiGet.mockReturnValue(pending.promise);
        const { result, rerender } = renderHook(
            ({ id }: { id: number }) => useAlertReasoning(id),
            { initialProps: { id: 9 } },
        );
        act(() => { result.current.load(); });
        rerender({ id: 10 });
        expect(result.current.state).toEqual({ status: 'idle' });

        await act(async () => {
            pending.resolve({ alert_id: 9, status: 'available', reasoning: 'Old.' });
            await pending.promise;
        });
        expect(result.current.state).toEqual({ status: 'idle' });
    });

    it('ignores a second load whilst one is in flight', async () => {
        const pending = deferred<unknown>();
        mockApiGet.mockReturnValue(pending.promise);
        const { result } = renderHook(() => useAlertReasoning(11));
        act(() => { result.current.load(); });
        act(() => { result.current.load(true); });
        expect(mockApiGet).toHaveBeenCalledTimes(1);
        await act(async () => {
            pending.resolve({ alert_id: 11, status: 'not_applicable' });
            await pending.promise;
        });
        expect(result.current.state).toEqual({ status: 'not_applicable' });
    });

    it('does not cache a result that arrives after the cache is cleared', async () => {
        const stale = deferred<unknown>();
        mockApiGet.mockReturnValueOnce(stale.promise);
        const { result, unmount } = renderHook(() => useAlertReasoning(12));
        act(() => { result.current.load(); });
        unmount();

        // Logout clears the cache whilst the request is in flight; a
        // new request for the same alert must not join the old one.
        clearAlertReasoningCache();
        const fresh = deferred<unknown>();
        mockApiGet.mockReturnValueOnce(fresh.promise);
        const next = renderHook(() => useAlertReasoning(12));
        act(() => { next.result.current.load(); });
        expect(mockApiGet).toHaveBeenCalledTimes(2);

        await act(async () => {
            stale.resolve({ alert_id: 12, status: 'available', reasoning: 'Previous user.' });
            await stale.promise;
        });
        expect(renderHook(() => useAlertReasoning(12)).result.current.state)
            .toEqual({ status: 'idle' });

        await act(async () => {
            fresh.resolve({ alert_id: 12, status: 'available', reasoning: 'Current user.' });
            await fresh.promise;
        });
        expect(next.result.current.state).toMatchObject({ reasoning: 'Current user.' });
        expect(renderHook(() => useAlertReasoning(12)).result.current.state)
            .toMatchObject({ reasoning: 'Current user.' });
    });

    it('encodes a string alert id in the query', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 0, status: 'not_applicable' });
        const { result } = renderHook(() => useAlertReasoning('a&b'));
        act(() => { result.current.load(); });
        await waitFor(() => {
            expect(result.current.state.status).toBe('not_applicable');
        });
        expect(mockApiGet).toHaveBeenCalledWith(URL_FOR('a%26b'));
    });
});
