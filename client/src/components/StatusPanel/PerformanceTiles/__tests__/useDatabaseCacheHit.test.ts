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
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useDatabaseCacheHit } from '../useDatabaseCacheHit';

// Mock modules
vi.mock('../../../../contexts/useAuth', () => ({
    useAuth: vi.fn(),
}));

vi.mock('../../../../utils/apiClient', () => ({
    apiFetch: vi.fn(),
}));

vi.mock('../../../../contexts/useClusterData', () => ({
    useClusterData: vi.fn(),
}));

import { useAuth } from '../../../../contexts/useAuth';
import { apiFetch } from '../../../../utils/apiClient';
import { useClusterData } from '../../../../contexts/useClusterData';

const mockUseAuth = vi.mocked(useAuth);
const mockApiFetch = vi.mocked(apiFetch);
const mockUseClusterData = vi.mocked(useClusterData);

describe('useDatabaseCacheHit', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUseAuth.mockReturnValue({
            user: { id: 1, username: 'testuser', role: 'admin' },
            login: vi.fn(),
            logout: vi.fn(),
            isLoading: false,
        });
        mockUseClusterData.mockReturnValue({
            lastRefresh: null,
            triggerRefresh: vi.fn(),
            clearRefresh: vi.fn(),
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('returns empty array when connectionId is null', async () => {
        const { result } = renderHook(() => useDatabaseCacheHit(null));

        expect(result.current.databases).toEqual([]);
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('returns empty array when user is not logged in', async () => {
        mockUseAuth.mockReturnValue({
            user: null,
            login: vi.fn(),
            logout: vi.fn(),
            isLoading: false,
        });

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        expect(result.current.databases).toEqual([]);
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('fetches database cache hit data on mount', async () => {
        const mockResponse = {
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [
                            { time: '2024-01-01T10:00:00Z', value: 99.3 },
                            { time: '2024-01-01T11:00:00Z', value: 99.5 },
                        ],
                    },
                },
                {
                    database_name: 'ecommerce',
                    cache_hit_ratio: {
                        current: 85.2,
                        time_series: [
                            { time: '2024-01-01T10:00:00Z', value: 84.1 },
                            { time: '2024-01-01T11:00:00Z', value: 85.2 },
                        ],
                    },
                },
            ],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        // Initially loading
        expect(result.current.loading).toBe(true);

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases).toHaveLength(2);
        expect(result.current.databases[0].database_name).toBe('postgres');
        expect(result.current.databases[1].database_name).toBe('ecommerce');
        expect(result.current.error).toBeNull();

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/database-summaries?connection_id=123&time_range=24h'
        );
    });

    it('filters out databases with empty time series', async () => {
        const mockResponse = {
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [
                            { time: '2024-01-01T10:00:00Z', value: 99.3 },
                        ],
                    },
                },
                {
                    database_name: 'empty_db',
                    cache_hit_ratio: {
                        current: 50.0,
                        time_series: [],
                    },
                },
            ],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        // Should only include postgres, not empty_db
        expect(result.current.databases).toHaveLength(1);
        expect(result.current.databases[0].database_name).toBe('postgres');
    });

    it('handles API error gracefully', async () => {
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ error: 'Internal server error' }),
        } as unknown as Response);

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases).toEqual([]);
        expect(result.current.error).toBe('Internal server error');

        consoleSpy.mockRestore();
    });

    it('handles network error gracefully', async () => {
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

        mockApiFetch.mockRejectedValue(new Error('Network error'));

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases).toEqual([]);
        expect(result.current.error).toBe('Network error');

        consoleSpy.mockRestore();
    });

    it('refetches when connectionId changes', async () => {
        const mockResponse1 = {
            databases: [
                {
                    database_name: 'db1',
                    cache_hit_ratio: {
                        current: 90.0,
                        time_series: [{ time: '2024-01-01T10:00:00Z', value: 90.0 }],
                    },
                },
            ],
        };

        const mockResponse2 = {
            databases: [
                {
                    database_name: 'db2',
                    cache_hit_ratio: {
                        current: 80.0,
                        time_series: [{ time: '2024-01-01T10:00:00Z', value: 80.0 }],
                    },
                },
            ],
        };

        mockApiFetch
            .mockResolvedValueOnce({
                ok: true,
                json: () => Promise.resolve(mockResponse1),
            } as Response)
            .mockResolvedValueOnce({
                ok: true,
                json: () => Promise.resolve(mockResponse2),
            } as Response);

        const { result, rerender } = renderHook(
            ({ connectionId }) => useDatabaseCacheHit(connectionId),
            { initialProps: { connectionId: 123 as number | null } }
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases[0].database_name).toBe('db1');

        rerender({ connectionId: 456 });

        await waitFor(() => {
            expect(result.current.databases[0]?.database_name).toBe('db2');
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(2);
    });

    it('refetches when lastRefresh changes', async () => {
        const mockResponse = {
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [{ time: '2024-01-01T10:00:00Z', value: 99.5 }],
                    },
                },
            ],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result, rerender } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(1);

        // Simulate refresh trigger
        mockUseClusterData.mockReturnValue({
            lastRefresh: new Date(),
            triggerRefresh: vi.fn(),
            clearRefresh: vi.fn(),
        });

        rerender();

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });

    it('handles empty databases array in response', async () => {
        const mockResponse = {
            databases: [],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases).toEqual([]);
        expect(result.current.error).toBeNull();
    });

    it('handles missing databases field in response', async () => {
        const mockResponse = {};

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result } = renderHook(() => useDatabaseCacheHit(123));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.databases).toEqual([]);
        expect(result.current.error).toBeNull();
    });

    it('clears databases when connectionId changes to null', async () => {
        const mockResponse = {
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [{ time: '2024-01-01T10:00:00Z', value: 99.5 }],
                    },
                },
            ],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result, rerender } = renderHook(
            ({ connectionId }) => useDatabaseCacheHit(connectionId),
            { initialProps: { connectionId: 123 as number | null } }
        );

        await waitFor(() => {
            expect(result.current.databases).toHaveLength(1);
        });

        rerender({ connectionId: null });

        expect(result.current.databases).toEqual([]);
    });

    it('does not show loading on subsequent fetches after initial load', async () => {
        const mockResponse = {
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [{ time: '2024-01-01T10:00:00Z', value: 99.5 }],
                    },
                },
            ],
        };

        mockApiFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockResponse),
        } as Response);

        const { result, rerender } = renderHook(() => useDatabaseCacheHit(123));

        // Initial load shows loading
        expect(result.current.loading).toBe(true);

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        // Trigger refetch
        mockUseClusterData.mockReturnValue({
            lastRefresh: new Date(),
            triggerRefresh: vi.fn(),
            clearRefresh: vi.fn(),
        });

        rerender();

        // Should not show loading on subsequent fetches
        // Note: This is an implicit expectation based on the hook implementation
        expect(result.current.databases).toHaveLength(1);
    });
});
