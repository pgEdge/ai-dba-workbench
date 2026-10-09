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
import { renderHook, waitFor } from '@testing-library/react';
import {
    buildReplicationSlotsUrl,
    REPLICATION_SLOT_LIMIT,
    useReplicationSlots,
} from '../useReplicationSlots';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

let mockUser: { id: number; username: string } | null = {
    id: 1,
    username: 'testuser',
};
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

let mockRefreshTrigger = 0;
vi.mock('../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: mockRefreshTrigger,
        timeRange: { range: '1h' },
    }),
}));

vi.mock('../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const slotRow = (name: string) => ({
    slot_name: name,
    slot_type: 'physical',
    active: true,
    wal_status: 'reserved',
    safe_wal_size: 1024,
    retained_bytes: 2048,
});

interface Deferred<T> {
    promise: Promise<T>;
    resolve: (value: T) => void;
    reject: (reason: unknown) => void;
}

/** A promise whose settlement the test controls. */
const makeDeferred = <T,>(): Deferred<T> => {
    let resolveFn: (value: T) => void = () => {};
    let rejectFn: (reason: unknown) => void = () => {};
    const promise = new Promise<T>((resolve, reject) => {
        resolveFn = resolve;
        rejectFn = reject;
    });
    return {
        promise,
        resolve: value => { resolveFn(value); },
        reject: reason => { rejectFn(reason); },
    };
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('buildReplicationSlotsUrl', () => {
    it('requests the latest pg_replication_slots snapshot by name', () => {
        const url = buildReplicationSlotsUrl(7);
        expect(url.startsWith('/api/v1/metrics/latest?')).toBe(true);
        const params = new URL(url, 'https://example.test').searchParams;
        expect(params.get('probe_name')).toBe('pg_replication_slots');
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('order_by')).toBe('slot_name');
        expect(params.get('order')).toBe('asc');
        expect(params.get('limit')).toBe(String(REPLICATION_SLOT_LIMIT));
    });
});

describe('useReplicationSlots', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { id: 1, username: 'testuser' };
        mockRefreshTrigger = 0;
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('issues no request without an authenticated user', () => {
        mockUser = null;

        const { result } = renderHook(() => useReplicationSlots(1));

        expect(mockApiGet).not.toHaveBeenCalled();
        expect(result.current.slots).toEqual([]);
        expect(result.current.loading).toBe(false);
    });

    it('fetches and normalises the slots', async () => {
        mockApiGet.mockResolvedValue({
            rows: [slotRow('a'), { slot_name: '' }, slotRow('b')],
            total_count: 2,
        });

        const { result } = renderHook(() => useReplicationSlots(3));

        await waitFor(() => {
            expect(result.current.slots).toHaveLength(2);
        });
        expect(mockApiGet.mock.calls[0][0])
            .toBe(buildReplicationSlotsUrl(3));
        expect(result.current.totalCount).toBe(2);
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
    });

    it('never reports fewer slots in total than it shows', async () => {
        mockApiGet.mockResolvedValue({ rows: [slotRow('a')] });

        const { result } = renderHook(() => useReplicationSlots(1));

        await waitFor(() => {
            expect(result.current.slots).toHaveLength(1);
        });
        expect(result.current.totalCount).toBe(1);
    });

    it('keeps a larger total count from the server', async () => {
        mockApiGet.mockResolvedValue({
            rows: [slotRow('a')],
            total_count: 250,
        });

        const { result } = renderHook(() => useReplicationSlots(1));

        await waitFor(() => {
            expect(result.current.totalCount).toBe(250);
        });
    });

    it('reports an error and clears the slots on failure', async () => {
        mockApiGet.mockRejectedValue(new Error('boom'));

        const { result } = renderHook(() => useReplicationSlots(1));

        await waitFor(() => {
            expect(result.current.error).toBe('boom');
        });
        expect(result.current.slots).toEqual([]);
        expect(result.current.totalCount).toBe(0);
        expect(result.current.loading).toBe(false);
    });

    it('falls back to a generic message when the error has none',
        async () => {
            mockApiGet.mockRejectedValue(new Error(''));

            const { result } = renderHook(() => useReplicationSlots(1));

            await waitFor(() => {
                expect(result.current.error)
                    .toBe('Failed to fetch replication slots');
            });
        });

    it('shows the spinner only on the first load', async () => {
        const first = makeDeferred<unknown>();
        mockApiGet.mockReturnValueOnce(first.promise);

        const { result, rerender } = renderHook(
            () => useReplicationSlots(1),
        );

        expect(result.current.loading).toBe(true);
        first.resolve({ rows: [slotRow('a')], total_count: 1 });
        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        const second = makeDeferred<unknown>();
        mockApiGet.mockReturnValueOnce(second.promise);
        mockRefreshTrigger = 1;
        rerender();

        expect(mockApiGet).toHaveBeenCalledTimes(2);
        expect(result.current.loading).toBe(false);
        expect(result.current.slots).toHaveLength(1);

        second.resolve({ rows: [slotRow('a'), slotRow('b')] });
        await waitFor(() => {
            expect(result.current.slots).toHaveLength(2);
        });
    });

    it('drops the previous server slots and ignores its late response',
        async () => {
            const stale = makeDeferred<unknown>();
            const fresh = makeDeferred<unknown>();
            mockApiGet
                .mockResolvedValueOnce({ rows: [slotRow('old')] })
                .mockReturnValueOnce(stale.promise)
                .mockReturnValueOnce(fresh.promise);

            const { result, rerender } = renderHook(
                ({ id }) => useReplicationSlots(id),
                { initialProps: { id: 1 } },
            );
            await waitFor(() => {
                expect(result.current.slots).toHaveLength(1);
            });

            mockRefreshTrigger = 1;
            rerender({ id: 1 });
            rerender({ id: 2 });

            expect(result.current.slots).toEqual([]);
            expect(result.current.loading).toBe(true);

            fresh.resolve({ rows: [slotRow('new')] });
            await waitFor(() => {
                expect(result.current.slots[0]?.slot_name).toBe('new');
            });

            stale.resolve({ rows: [slotRow('stale')] });
            await Promise.resolve();
            expect(result.current.slots[0].slot_name).toBe('new');
        });

    it('ignores a late failure from a superseded request', async () => {
        const stale = makeDeferred<unknown>();
        mockApiGet
            .mockReturnValueOnce(stale.promise)
            .mockResolvedValueOnce({ rows: [slotRow('new')] });

        const { result, rerender } = renderHook(
            ({ id }) => useReplicationSlots(id),
            { initialProps: { id: 1 } },
        );
        rerender({ id: 2 });

        await waitFor(() => {
            expect(result.current.slots).toHaveLength(1);
        });
        stale.reject(new Error('late'));
        await Promise.resolve();
        await Promise.resolve();
        expect(result.current.error).toBeNull();
        expect(result.current.slots[0].slot_name).toBe('new');
    });

    it('never renders the previous server slots under a new server',
        async () => {
            const fresh = makeDeferred<unknown>();
            mockApiGet
                .mockResolvedValueOnce({ rows: [slotRow('old')] })
                .mockReturnValueOnce(fresh.promise);

            const renders: {
                id: number;
                value: ReturnType<typeof useReplicationSlots>;
            }[] = [];
            const { result, rerender } = renderHook(
                ({ id }) => {
                    const value = useReplicationSlots(id);
                    renders.push({ id, value });
                    return value;
                },
                { initialProps: { id: 1 } },
            );
            await waitFor(() => {
                expect(result.current.slots).toHaveLength(1);
            });

            rerender({ id: 2 });

            const forTwo = renders.filter(r => r.id === 2);
            expect(forTwo.length).toBeGreaterThan(0);
            for (const { value } of forTwo) {
                expect(value.slots).toEqual([]);
                expect(value.totalCount).toBe(0);
                expect(value.loading).toBe(true);
            }

            fresh.resolve({ rows: [slotRow('new')] });
            await waitFor(() => {
                expect(result.current.slots[0]?.slot_name).toBe('new');
            });
            expect(renders.filter(r => r.id === 2).every(
                r => r.value.slots.every(slot => slot.slot_name !== 'old'),
            )).toBe(true);
        });

    it('never shows the previous server error under a new server',
        async () => {
            const fresh = makeDeferred<unknown>();
            mockApiGet
                .mockRejectedValueOnce(new Error('server one is down'))
                .mockReturnValueOnce(fresh.promise);

            const renders: {
                id: number;
                value: ReturnType<typeof useReplicationSlots>;
            }[] = [];
            const { result, rerender } = renderHook(
                ({ id }) => {
                    const value = useReplicationSlots(id);
                    renders.push({ id, value });
                    return value;
                },
                { initialProps: { id: 1 } },
            );
            await waitFor(() => {
                expect(result.current.error).toBe('server one is down');
            });

            rerender({ id: 2 });

            expect(renders.filter(r => r.id === 2).every(
                r => r.value.error === null && r.value.loading,
            )).toBe(true);
            fresh.resolve({ rows: [] });
            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });
            expect(result.current.error).toBeNull();
        });

    it('keeps an error on refresh until the retry succeeds', async () => {
        const retry = makeDeferred<unknown>();
        mockApiGet
            .mockRejectedValueOnce(new Error('down'))
            .mockReturnValueOnce(retry.promise);

        const { result, rerender } = renderHook(
            () => useReplicationSlots(1),
        );
        await waitFor(() => {
            expect(result.current.error).toBe('down');
        });

        mockRefreshTrigger = 1;
        rerender();
        expect(mockApiGet).toHaveBeenCalledTimes(2);
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBe('down');

        retry.resolve({ rows: [slotRow('a')] });
        await waitFor(() => {
            expect(result.current.slots).toHaveLength(1);
        });
        expect(result.current.error).toBeNull();
    });
});
