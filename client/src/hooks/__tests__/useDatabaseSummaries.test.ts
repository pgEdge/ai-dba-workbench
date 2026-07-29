/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useDatabaseSummaries } from '../useDatabaseSummaries';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockUser = vi.fn<() => { id: number } | null>();
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser() }),
}));

/** Create a successful Response-like object. */
const okResponse = (data: unknown): Partial<Response> => ({
    ok: true,
    json: () => Promise.resolve(data),
});

const summary = (name: string) => ({ database_name: name });

describe('useDatabaseSummaries', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ id: 1 });
    });

    it('returns the databases reported by the API', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            databases: [summary('appdb'), summary('analytics')],
        }));

        const { result } = renderHook(() => useDatabaseSummaries(7));

        await waitFor(() => {
            expect(result.current.databases).toHaveLength(2);
        });
        expect(result.current.error).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/database-summaries'
            + '?connection_id=7&time_range=24h',
        );
    });

    it('honours a custom time range', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        renderHook(() => useDatabaseSummaries(3, 0, '1h'));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                expect.stringContaining('time_range=1h'),
            );
        });
    });

    it('treats a missing databases field as an empty list', async () => {
        mockApiFetch.mockResolvedValue(okResponse({}));

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.databases).toEqual([]);
    });

    it('surfaces the server error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ error: 'boom' }),
        } as Partial<Response>);

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.error).toBe('boom');
        });
        expect(result.current.databases).toEqual([]);
    });

    it('falls back to a status-based error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 503,
            json: () => Promise.reject(new Error('no body')),
        } as Partial<Response>);

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.error).toContain('503');
        });
    });

    it('does not fetch when no user is logged in', async () => {
        mockUser.mockReturnValue(null);
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.databases).toEqual([]);
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('refetches when the refresh key changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        const { rerender } = renderHook(
            ({ key }: { key: number }) => useDatabaseSummaries(1, key),
            { initialProps: { key: 0 } },
        );

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        rerender({ key: 1 });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });
});
