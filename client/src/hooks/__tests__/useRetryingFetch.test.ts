/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
    useRetryingFetch,
    DEFAULT_RETRY_BASE_DELAY_MS,
    DEFAULT_RETRY_MAX_DELAY_MS,
} from '../useRetryingFetch';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Flush pending microtasks so awaited promise chains inside the hook
 * settle before assertions run.
 */
async function flushMicrotasks(): Promise<void> {
    await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
    });
}

describe('useRetryingFetch', () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.runOnlyPendingTimers();
        vi.useRealTimers();
    });

    it('runs the fetch and does not retry on success', async () => {
        const fetchFn = vi.fn().mockResolvedValue(true);
        const { result } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });

        expect(fetchFn).toHaveBeenCalledTimes(1);
        expect(result.current.retrying).toBe(false);

        // Advance well beyond any backoff window; no further calls.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 4);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('schedules a retry after a failed fetch and heals on recovery', async () => {
        const fetchFn = vi
            .fn()
            .mockResolvedValueOnce(false)
            .mockResolvedValue(true);
        const { result } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });

        expect(fetchFn).toHaveBeenCalledTimes(1);
        expect(result.current.retrying).toBe(true);

        // First retry fires after the base delay.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });

        expect(fetchFn).toHaveBeenCalledTimes(2);
        expect(result.current.retrying).toBe(false);

        // No further retries once healed.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        expect(fetchFn).toHaveBeenCalledTimes(2);
    });

    it('does not retry before the base delay elapses', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS - 1);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('grows the delay exponentially and caps it', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result } = renderHook(() =>
            useRetryingFetch({ baseDelayMs: 1000, maxDelayMs: 8000 }),
        );

        await act(async () => {
            await result.current.run(fetchFn);
        });
        // Attempt 1 done; retry 1 scheduled at 1000ms.
        expect(fetchFn).toHaveBeenCalledTimes(1);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(1000);
        });
        // Retry 1 fired; retry 2 scheduled at 2000ms.
        expect(fetchFn).toHaveBeenCalledTimes(2);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(2000);
        });
        // Retry 2 fired; retry 3 scheduled at 4000ms.
        expect(fetchFn).toHaveBeenCalledTimes(3);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(4000);
        });
        // Retry 3 fired; retry 4 scheduled at 8000ms (would be 8000, capped).
        expect(fetchFn).toHaveBeenCalledTimes(4);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(8000);
        });
        // Retry 4 fired; retry 5 scheduled, still capped at 8000ms.
        expect(fetchFn).toHaveBeenCalledTimes(5);

        // Confirm the cap: nothing fires at 8000ms - 1.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(7999);
        });
        expect(fetchFn).toHaveBeenCalledTimes(5);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(1);
        });
        expect(fetchFn).toHaveBeenCalledTimes(6);
    });

    it('treats a rejected promise as a failure', async () => {
        const fetchFn = vi
            .fn()
            .mockRejectedValueOnce(new Error('boom'))
            .mockResolvedValue(true);
        const { result } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(result.current.retrying).toBe(true);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
        });
        expect(fetchFn).toHaveBeenCalledTimes(2);
        expect(result.current.retrying).toBe(false);
    });

    it('resets the backoff when the reset key changes', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result, rerender } = renderHook(
            ({ key }) => useRetryingFetch({ resetKey: key }),
            { initialProps: { key: 1 } },
        );

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(result.current.retrying).toBe(true);

        // Changing the reset key cancels the pending retry.
        rerender({ key: 2 });
        await flushMicrotasks();
        expect(result.current.retrying).toBe(false);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        // The cancelled retry never fires; only the original attempt ran.
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('cancels pending retries when disabled', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result, rerender } = renderHook(
            ({ enabled }) => useRetryingFetch({ enabled }),
            { initialProps: { enabled: true } },
        );

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(result.current.retrying).toBe(true);

        rerender({ enabled: false });
        await flushMicrotasks();
        expect(result.current.retrying).toBe(false);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('does not run the fetch while disabled', async () => {
        const fetchFn = vi.fn().mockResolvedValue(true);
        const { result } = renderHook(() =>
            useRetryingFetch({ enabled: false }),
        );

        let outcome: boolean | undefined;
        await act(async () => {
            outcome = await result.current.run(fetchFn);
        });

        expect(fetchFn).not.toHaveBeenCalled();
        expect(outcome).toBe(false);
    });

    it('cancel() clears a pending retry and resets state', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(result.current.retrying).toBe(true);

        act(() => {
            result.current.cancel();
        });
        expect(result.current.retrying).toBe(false);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('a fresh run supersedes a pending retry and resets the delay', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result } = renderHook(() =>
            useRetryingFetch({ baseDelayMs: 1000, maxDelayMs: 8000 }),
        );

        await act(async () => {
            await result.current.run(fetchFn);
        });
        // Grow the backoff by letting one retry fire.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(1000);
        });
        expect(fetchFn).toHaveBeenCalledTimes(2);

        // A fresh run resets the failure count, so the next retry is
        // scheduled at the base delay again rather than the grown value.
        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(3);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(1000);
        });
        expect(fetchFn).toHaveBeenCalledTimes(4);
    });

    it('ignores a stale in-flight attempt superseded by a fresh run', async () => {
        // The first attempt never resolves until we release it manually,
        // modelling a slow request still in flight when a fresh run wins.
        let resolveFirst: ((value: boolean) => void) | undefined;
        const firstPromise = new Promise<boolean>((res) => {
            resolveFirst = res;
        });
        const fetchFn = vi
            .fn()
            .mockReturnValueOnce(firstPromise)
            .mockResolvedValueOnce(true);

        const { result } = renderHook(() => useRetryingFetch());

        // Kick off the first attempt; it stays pending.
        let firstRun: Promise<boolean> | undefined;
        act(() => {
            firstRun = result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);

        // A fresh run supersedes the in-flight attempt and succeeds.
        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(2);
        expect(result.current.retrying).toBe(false);

        // Now the stale first attempt resolves with a FAILURE. Its result
        // must be ignored: no retry scheduled, retrying stays false.
        await act(async () => {
            resolveFirst?.(false);
            await firstRun;
        });
        expect(result.current.retrying).toBe(false);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        // No extra fetch fired from a stale-scheduled retry.
        expect(fetchFn).toHaveBeenCalledTimes(2);
    });

    it('ignores a stale in-flight attempt superseded by a resetKey change', async () => {
        let resolveFirst: ((value: boolean) => void) | undefined;
        const firstPromise = new Promise<boolean>((res) => {
            resolveFirst = res;
        });
        const fetchFn = vi.fn().mockReturnValueOnce(firstPromise);

        const { result, rerender } = renderHook(
            ({ key }) => useRetryingFetch({ resetKey: key }),
            { initialProps: { key: 1 } },
        );

        let firstRun: Promise<boolean> | undefined;
        act(() => {
            firstRun = result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);

        // A manual refresh (resetKey change) supersedes the in-flight
        // attempt while its fetch promise is still pending.
        rerender({ key: 2 });
        await flushMicrotasks();
        expect(result.current.retrying).toBe(false);

        // The now-stale attempt resolves with a failure and is ignored.
        await act(async () => {
            resolveFirst?.(false);
            await firstRun;
        });
        expect(result.current.retrying).toBe(false);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        // Only the original attempt ran; the stale failure scheduled nothing.
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });

    it('clears the pending timer on unmount', async () => {
        const fetchFn = vi.fn().mockResolvedValue(false);
        const { result, unmount } = renderHook(() => useRetryingFetch());

        await act(async () => {
            await result.current.run(fetchFn);
        });
        expect(fetchFn).toHaveBeenCalledTimes(1);

        unmount();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_MAX_DELAY_MS * 2);
        });
        // No retry runs after unmount.
        expect(fetchFn).toHaveBeenCalledTimes(1);
    });
});
