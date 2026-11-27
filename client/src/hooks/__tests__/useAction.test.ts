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
import { useAction, POLL_INITIAL_MS, POLL_MAX_MS } from '../useAction';
import { ApiError } from '../../utils/apiClient';
import type { Action } from '../../types/actions';

const mockGet = vi.fn();
const mockPost = vi.fn();
vi.mock('../../utils/apiClient', async () => {
    const actual = await vi.importActual<
        typeof import('../../utils/apiClient')
    >('../../utils/apiClient');
    return {
        ...actual,
        apiGet: (...args: unknown[]) => mockGet(...args),
        apiPost: (...args: unknown[]) => mockPost(...args),
    };
});

const makeAction = (over: Partial<Action> = {}): Action => ({
    id: 7,
    connection_id: 1,
    statements: ['UPDATE t SET a = 1'],
    source: 'chat_block',
    review_status: 'passed',
    approval: 'approved',
    exec_status: 'executing',
    ...over,
});

describe('useAction', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        mockGet.mockReset();
        mockPost.mockReset();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('creates an action and reports the response', async () => {
        mockPost.mockResolvedValueOnce(
            makeAction({ exec_status: 'succeeded' }),
        );
        const { result } = renderHook(() => useAction());

        let created: Action | null = null;
        await act(async () => {
            created = await result.current.create(1, {
                sql: 'UPDATE t SET a = 1',
                source: 'chat_block',
                execute: true,
            });
        });

        expect(mockPost).toHaveBeenCalledWith(
            '/api/v1/connections/1/actions',
            expect.objectContaining({ execute: true }),
        );
        expect(created).not.toBeNull();
        expect(result.current.action?.exec_status).toBe('succeeded');
        expect(result.current.error).toBeNull();
        expect(result.current.busy).toBe(false);
    });

    it('surfaces execute_error from a 201 response', async () => {
        mockPost.mockResolvedValueOnce({
            ...makeAction({ exec_status: 'not_started', approval: 'none' }),
            execute_error: 'could not start',
        });
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
                execute: true,
            });
        });

        expect(result.current.error).toBe('could not start');
        expect(result.current.action?.exec_status).toBe('not_started');
    });

    it('reports a failed create', async () => {
        mockPost.mockRejectedValueOnce(new ApiError('no write access', 403));
        const { result } = renderHook(() => useAction());

        let created: Action | null = makeAction();
        await act(async () => {
            created = await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
            });
        });

        expect(created).toBeNull();
        expect(result.current.error).toBe('no write access');
    });

    it('polls with exponential back-off until a terminal status', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet
            .mockResolvedValueOnce(makeAction())
            .mockResolvedValueOnce(makeAction())
            .mockResolvedValueOnce(makeAction({ exec_status: 'succeeded' }));
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
                execute: true,
            });
        });
        expect(mockGet).not.toHaveBeenCalled();

        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS);
        });
        expect(mockGet).toHaveBeenCalledTimes(1);

        // The second poll waits twice as long.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS);
        });
        expect(mockGet).toHaveBeenCalledTimes(1);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS);
        });
        expect(mockGet).toHaveBeenCalledTimes(2);

        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS * 4);
        });
        expect(mockGet).toHaveBeenCalledTimes(3);
        expect(result.current.action?.exec_status).toBe('succeeded');

        // Terminal: no further polls.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS * 3);
        });
        expect(mockGet).toHaveBeenCalledTimes(3);
    });

    it('caps the back-off at the maximum delay', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet.mockResolvedValue(makeAction());
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
                execute: true,
            });
        });

        // 500 + 1000 + 2000 + 4000 reaches the cap; the next gap is 5s.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(7500);
        });
        expect(mockGet).toHaveBeenCalledTimes(4);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS);
        });
        expect(mockGet).toHaveBeenCalledTimes(5);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS);
        });
        expect(mockGet).toHaveBeenCalledTimes(6);
    });

    it('stops polling on unmount', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet.mockResolvedValue(makeAction());
        const { result, unmount } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
                execute: true,
            });
        });
        unmount();

        await vi.advanceTimersByTimeAsync(POLL_MAX_MS * 4);
        expect(mockGet).not.toHaveBeenCalled();
    });

    it('does not poll an action that awaits approval', async () => {
        mockPost.mockResolvedValueOnce(
            makeAction({ exec_status: 'not_started', approval: 'none' }),
        );
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
            });
        });
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS * 2);
        });

        expect(mockGet).not.toHaveBeenCalled();
    });

    it('keeps polling through a transient error but stops on 404', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet
            .mockRejectedValueOnce(new ApiError('boom', 500))
            .mockRejectedValueOnce(new ApiError('Action not found', 404));
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.create(1, {
                sql: 'x',
                source: 'chat_block',
                execute: true,
            });
        });
        await act(async () => {
            await vi.advanceTimersByTimeAsync(
                POLL_INITIAL_MS + POLL_INITIAL_MS * 2,
            );
        });
        expect(mockGet).toHaveBeenCalledTimes(2);
        expect(result.current.error).toBe('Action not found');

        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS * 2);
        });
        expect(mockGet).toHaveBeenCalledTimes(2);
    });

    it('executes without an override by sending no body', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet.mockResolvedValue(makeAction({ exec_status: 'succeeded' }));
        const { result } = renderHook(() => useAction());

        let ok = false;
        await act(async () => {
            ok = await result.current.execute(7);
        });

        expect(ok).toBe(true);
        expect(mockPost).toHaveBeenCalledWith(
            '/api/v1/actions/7/execute',
            undefined,
        );
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS);
        });
        expect(result.current.action?.exec_status).toBe('succeeded');
    });

    it('sends the override flag and confirmation text', async () => {
        mockPost.mockResolvedValueOnce(makeAction());
        mockGet.mockResolvedValue(makeAction());
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.execute(7, { confirm: 'prod-1' });
        });

        expect(mockPost).toHaveBeenCalledWith(
            '/api/v1/actions/7/execute',
            { override: true, confirm: 'prod-1' },
        );
    });

    it('does not poll when execute returns a terminal action', async () => {
        mockPost.mockResolvedValueOnce(
            makeAction({ exec_status: 'failed' }),
        );
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.execute(7);
        });
        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_MAX_MS);
        });

        expect(mockGet).not.toHaveBeenCalled();
    });

    it('reports a refused execute (409)', async () => {
        mockPost.mockRejectedValueOnce(
            new ApiError('already approved', 409),
        );
        const { result } = renderHook(() => useAction());

        let ok = true;
        await act(async () => {
            ok = await result.current.execute(7);
        });

        expect(ok).toBe(false);
        expect(result.current.error).toBe('already approved');
    });

    it.each([
        [409, 'already approved'],
        [503, 'busy'],
    ])('re-fetches the action after a refused execute (%i)', async (status, message) => {
        mockPost.mockRejectedValueOnce(new ApiError(message, status));
        mockGet.mockResolvedValueOnce(makeAction({ exec_status: 'succeeded' }));
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.execute(7);
        });

        expect(mockGet).toHaveBeenCalledWith('/api/v1/actions/7');
        expect(result.current.action?.exec_status).toBe('succeeded');
        expect(result.current.error).toBe(message);
        expect(result.current.writeDenied).toBe(false);
    });

    it.each([
        [403, true],
        [500, false],
    ])('does not re-fetch after execute fails with %i, and records a 403 as denied', async (status, denied) => {
        mockPost.mockRejectedValueOnce(new ApiError('refused', status));
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.execute(7);
        });

        expect(mockGet).not.toHaveBeenCalled();
        expect(result.current.writeDenied).toBe(denied);
    });

    it('keeps the refusal error when the re-fetch fails, and never clears it', async () => {
        mockPost.mockRejectedValueOnce(new ApiError('busy', 503));
        mockGet.mockRejectedValueOnce(new ApiError('gone', 500));
        const seen: Array<string | null> = [];
        const { result } = renderHook(() => {
            const hook = useAction();
            seen.push(hook.error);
            return hook;
        });

        await act(async () => {
            await result.current.execute(7);
        });

        expect(result.current.error).toBe('busy');
        // Once the refusal is shown it is never blanked while re-fetching.
        const first = seen.indexOf('busy');
        expect(seen.slice(first).every((e) => e === 'busy')).toBe(true);
    });

    it('cancels and reports cancel failures', async () => {
        mockPost.mockResolvedValueOnce({ cancel_requested: true });
        const { result } = renderHook(() => useAction());

        let ok = false;
        await act(async () => {
            ok = await result.current.cancel(7);
        });
        expect(ok).toBe(true);
        expect(mockPost).toHaveBeenCalledWith('/api/v1/actions/7/cancel');

        mockPost.mockRejectedValueOnce(new ApiError('not running', 409));
        await act(async () => {
            ok = await result.current.cancel(7);
        });
        expect(ok).toBe(false);
        expect(result.current.error).toBe('not running');
    });

    it('loads an existing action and follows it while it runs', async () => {
        mockGet
            .mockResolvedValueOnce(makeAction())
            .mockResolvedValueOnce(makeAction({ exec_status: 'unknown' }));
        const { result } = renderHook(() => useAction());

        await act(async () => {
            await result.current.load(7);
        });
        expect(result.current.action?.exec_status).toBe('executing');

        await act(async () => {
            await vi.advanceTimersByTimeAsync(POLL_INITIAL_MS);
        });
        expect(result.current.action?.exec_status).toBe('unknown');
    });

    it('reports a failed load', async () => {
        mockGet.mockRejectedValueOnce(new ApiError('Action not found', 404));
        const { result } = renderHook(() => useAction());

        let loaded: Action | null = makeAction();
        await act(async () => {
            loaded = await result.current.load(7);
        });

        expect(loaded).toBeNull();
        expect(result.current.error).toBe('Action not found');
    });
});
