/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterDataContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
    ClusterDataProvider,
    type ClusterGroup,
} from '../ClusterDataContext';
import {
    generateDataFingerprint,
    collectServerFingerprints,
    transformConnectionsToHierarchy,
} from '../clusterDataHelpers';
import { useClusterData } from '../useClusterData';

// Mock apiClient — keep ApiError real for instanceof checks.
vi.mock('../../utils/apiClient', async () => {
    const actual = await vi.importActual<
        typeof import('../../utils/apiClient')
    >('../../utils/apiClient');
    return {
        ...actual,
        apiGet: vi.fn(),
    };
});

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

let mockUser: { username: string } | null = { username: 'testuser' };
vi.mock('../useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

import { apiGet, ApiError } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;

const defaultData: ClusterGroup[] = [
    {
        id: 'group-1',
        name: 'Production',
        clusters: [
            {
                id: 'cluster-1',
                name: 'us-east',
                servers: [
                    { id: 1, name: 'pg-1', status: 'online' },
                    { id: 2, name: 'pg-2', status: 'online' },
                ],
            },
        ],
    },
];

describe('ClusterDataContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <ClusterDataProvider>{children}</ClusterDataProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
        mockApiGet.mockResolvedValue(defaultData);
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('fingerprint helpers', () => {
        it('generateDataFingerprint returns empty for empty data', () => {
            expect(generateDataFingerprint([])).toBe('');
        });

        it('generateDataFingerprint encodes structure and values', () => {
            const fp = generateDataFingerprint(defaultData);
            expect(fp).toContain('group-1');
            expect(fp).toContain('Production');
            expect(fp).toContain('cluster-1');
            expect(fp).toContain('pg-1');
        });

        it('collectServerFingerprints handles empty lists', () => {
            expect(collectServerFingerprints([])).toBe('');
        });

        it('collectServerFingerprints recurses into children', () => {
            const fp = collectServerFingerprints([
                {
                    id: 1,
                    name: 'parent',
                    status: 'online',
                    children: [
                        { id: 2, name: 'child', status: 'online' },
                    ],
                },
            ]);
            expect(fp).toContain('parent');
            expect(fp).toContain('child');
        });
    });

    describe('transformConnectionsToHierarchy', () => {
        it('groups connections by cluster_group and cluster_name', () => {
            const conns = [
                {
                    id: 1,
                    name: 'srv1',
                    host: 'h1',
                    port: 5432,
                    cluster_group: 'prod',
                    cluster_name: 'cluster-a',
                },
                {
                    id: 2,
                    name: 'srv2',
                    host: 'h2',
                    port: 5432,
                    cluster_group: 'prod',
                    cluster_name: 'cluster-a',
                },
            ];

            const result = transformConnectionsToHierarchy(conns);

            expect(result).toHaveLength(1);
            expect(result[0].name).toBe('prod');
            const clusters = result[0].clusters ?? [];
            expect(clusters).toHaveLength(1);
            expect(clusters[0].servers).toHaveLength(2);
        });

        it('creates standalone clusters for connections without cluster_name', () => {
            const conns = [
                { id: 1, name: 'lone', host: 'h', port: 5432 },
            ];

            const result = transformConnectionsToHierarchy(conns);

            expect(result).toHaveLength(1);
            expect(result[0].name).toBe('Ungrouped');
            const clusters = result[0].clusters ?? [];
            expect(clusters[0].isStandalone).toBe(true);
            expect(clusters[0].servers[0].id).toBe(1);
        });

        it('uses Ungrouped when cluster_group is missing', () => {
            const conns = [
                {
                    id: 1,
                    name: 'srv',
                    host: 'h',
                    port: 5432,
                    cluster_name: 'c',
                },
            ];
            const result = transformConnectionsToHierarchy(conns);
            expect(result[0].name).toBe('Ungrouped');
        });
    });

    describe('initial fetch and state', () => {
        it('fetches cluster data on mount', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/clusters');
            expect(result.current.loading).toBe(false);
        });

        it('updates lastRefresh after fetch', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.lastRefresh).not.toBeNull();
            });

            expect(result.current.lastRefresh).toBeInstanceOf(Date);
        });

        it('provides all expected properties', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            expect(result.current).toHaveProperty('clusterData');
            expect(result.current).toHaveProperty('loading');
            expect(result.current).toHaveProperty('error');
            expect(result.current).toHaveProperty('lastRefresh');
            expect(result.current).toHaveProperty('autoRefreshEnabled');
            expect(result.current).toHaveProperty('setAutoRefreshEnabled');
            expect(result.current).toHaveProperty('fetchClusterData');
        });

        it('does not fetch when user is null', async () => {
            mockUser = null;
            renderHook(() => useClusterData(), { wrapper });
            await new Promise((r) => setTimeout(r, 10));
            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });

    describe('fingerprint-based change detection', () => {
        it('keeps the same reference when fingerprint is unchanged', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            const initial = result.current.clusterData;

            await act(async () => {
                await result.current.fetchClusterData();
            });

            expect(result.current.clusterData).toBe(initial);
        });

        it('updates reference when fingerprint changes', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            const initial = result.current.clusterData;

            const changed = JSON.parse(JSON.stringify(defaultData));
            changed[0].clusters[0].servers[0].status = 'offline';
            mockApiGet.mockResolvedValue(changed);

            await act(async () => {
                await result.current.fetchClusterData();
            });

            expect(result.current.clusterData).not.toBe(initial);
        });
    });

    describe('fallback to /connections on 404', () => {
        it('falls back when /clusters returns ApiError 404', async () => {
            const connectionRecords = [
                {
                    id: 1,
                    name: 'c1',
                    host: 'h',
                    port: 5432,
                    cluster_group: 'g',
                    cluster_name: 'c',
                },
            ];

            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/clusters') {
                    return Promise.reject(new ApiError('not found', 404));
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve(connectionRecords);
                }
                return Promise.reject(new Error(`unexpected ${url}`));
            });

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections');
            expect(result.current.clusterData[0].name).toBe('g');
        });

        it('sets error for non-404 API failures', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('boom'));

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.error).toBe('boom');
            });
        });

        it('keeps the error until a fetch succeeds, then clears it', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('boom'));

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.error).toBe('boom');
            });

            // A further failed retry leaves the error in place
            mockApiGet.mockRejectedValueOnce(new Error('still down'));
            await act(async () => {
                await result.current.fetchClusterData();
            });
            expect(result.current.error).toBe('still down');

            // A successful retry clears it and loads the data
            mockApiGet.mockResolvedValue(defaultData);
            await act(async () => {
                await result.current.fetchClusterData();
            });
            expect(result.current.error).toBeNull();
            expect(result.current.clusterData).toEqual(defaultData);
        });

        it('shows loading again on a retry after a failed first load', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('boom'));

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.error).toBe('boom');
            });
            expect(result.current.loading).toBe(false);

            let resolveFetch: (value: ClusterGroup[]) => void = () => {};
            mockApiGet.mockImplementationOnce(
                () => new Promise<ClusterGroup[]>((resolve) => {
                    resolveFetch = resolve;
                }),
            );

            let pending: Promise<void> = Promise.resolve();
            act(() => {
                pending = result.current.fetchClusterData();
            });

            // The error stays visible to consumers whilst loading
            expect(result.current.loading).toBe(true);
            expect(result.current.error).toBe('boom');

            await act(async () => {
                resolveFetch(defaultData);
                await pending;
            });

            expect(result.current.loading).toBe(false);
            expect(result.current.error).toBeNull();
        });

        it('does not show loading on a refresh after a successful load', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });
            expect(result.current.loading).toBe(false);

            let resolveFetch: (value: ClusterGroup[]) => void = () => {};
            mockApiGet.mockImplementationOnce(
                () => new Promise<ClusterGroup[]>((resolve) => {
                    resolveFetch = resolve;
                }),
            );

            let pending: Promise<void> = Promise.resolve();
            act(() => {
                pending = result.current.fetchClusterData();
            });

            expect(result.current.loading).toBe(false);

            await act(async () => {
                resolveFetch(defaultData);
                await pending;
            });

            expect(result.current.loading).toBe(false);
        });
    });

    describe('refreshing', () => {
        it('is set for a requested fetch, even after an empty load, and not for a poll', async () => {
            vi.useFakeTimers();
            mockApiGet.mockResolvedValue([]);

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await act(async () => {
                await vi.runOnlyPendingTimersAsync();
            });
            expect(result.current.refreshing).toBe(false);

            // A background poll that hangs does not set it
            mockApiGet.mockImplementation(() => new Promise<ClusterGroup[]>(() => {}));
            await act(async () => {
                await vi.advanceTimersByTimeAsync(30000);
            });
            expect(result.current.refreshing).toBe(false);

            // A requested fetch does, though loading stays false after the
            // successful (empty) first load, until it settles
            let rejectFetch: (err: Error) => void = () => {};
            mockApiGet.mockImplementationOnce(
                () => new Promise<ClusterGroup[]>((_resolve, reject) => {
                    rejectFetch = reject;
                }),
            );
            let pending: Promise<void> = Promise.resolve();
            act(() => {
                pending = result.current.fetchClusterData();
            });
            expect(result.current.refreshing).toBe(true);
            expect(result.current.loading).toBe(false);

            await act(async () => {
                rejectFetch(new Error('boom'));
                await pending;
            });
            expect(result.current.refreshing).toBe(false);
            expect(result.current.error).toBe('boom');
        });
    });

    describe('out-of-order completions', () => {
        it('ignores a failure that finishes after a newer fetch succeeded', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });
            await waitFor(() => {
                expect(result.current.clusterData).toEqual(defaultData);
            });

            let rejectOlder: (err: Error) => void = () => {};
            mockApiGet.mockImplementationOnce(
                () => new Promise<ClusterGroup[]>((_resolve, reject) => {
                    rejectOlder = reject;
                }),
            );
            let older: Promise<void> = Promise.resolve();
            act(() => {
                older = result.current.fetchClusterData();
            });

            // A newer fetch starts and succeeds whilst the older one hangs
            await act(async () => {
                await result.current.fetchClusterData();
            });
            expect(result.current.error).toBeNull();

            await act(async () => {
                rejectOlder(new Error('stale'));
                await older;
            });
            expect(result.current.error).toBeNull();
            expect(result.current.refreshing).toBe(false);
        });

        it('ignores data that finishes after a newer fetch failed', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });
            await waitFor(() => {
                expect(result.current.clusterData).toEqual(defaultData);
            });

            let resolveOlder: (value: ClusterGroup[]) => void = () => {};
            mockApiGet.mockImplementationOnce(
                () => new Promise<ClusterGroup[]>((resolve) => {
                    resolveOlder = resolve;
                }),
            );
            let older: Promise<void> = Promise.resolve();
            act(() => {
                older = result.current.fetchClusterData();
            });

            mockApiGet.mockRejectedValueOnce(new Error('newer failure'));
            await act(async () => {
                await result.current.fetchClusterData();
            });
            expect(result.current.error).toBe('newer failure');

            await act(async () => {
                resolveOlder([]);
                await older;
            });
            expect(result.current.error).toBe('newer failure');
            expect(result.current.clusterData).toEqual(defaultData);
        });
    });

    describe('error messages', () => {
        it.each([
            [
                'a JSON error body',
                new ApiError('database unavailable', 500, '{"error":"database unavailable"}'),
                'database unavailable',
            ],
            [
                'a non-JSON error page',
                new ApiError(
                    '<html><head><title>502 Bad Gateway</title></head></html>',
                    502,
                    '<html><head><title>502 Bad Gateway</title></head></html>',
                ),
                'The server returned an unexpected response (HTTP 502).',
            ],
            ['a network failure', new TypeError('Failed to fetch'), 'Failed to fetch'],
            ['a non-Error rejection', 'nope', 'Failed to load servers'],
        ])('describes %s', async (_case, rejection, expected) => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(rejection);

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.error).toBe(expected);
            });
        });
    });

    describe('auto-refresh', () => {
        it('does not show loading on a background poll after a failed load', async () => {
            vi.useFakeTimers();
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('boom'));

            const { result } = renderHook(() => useClusterData(), { wrapper });

            await act(async () => {
                await vi.runOnlyPendingTimersAsync();
            });
            expect(result.current.error).toBe('boom');
            expect(result.current.loading).toBe(false);

            // The poll hangs, so loading would still be set if it raised it
            mockApiGet.mockImplementation(() => new Promise<ClusterGroup[]>(() => {}));
            const before = mockApiGet.mock.calls.length;

            await act(async () => {
                await vi.advanceTimersByTimeAsync(30000);
            });

            expect(mockApiGet.mock.calls.length).toBeGreaterThan(before);
            expect(result.current.loading).toBe(false);
            expect(result.current.error).toBe('boom');
        });

        it('refetches on the 30-second interval when enabled', async () => {
            vi.useFakeTimers();
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await vi.runOnlyPendingTimersAsync();
            const initialCount = mockApiGet.mock.calls.length;

            await act(async () => {
                await vi.advanceTimersByTimeAsync(30000);
            });

            expect(mockApiGet.mock.calls.length).toBeGreaterThan(initialCount);
            expect(result.current.autoRefreshEnabled).toBe(true);
        });

        it('stops refetching when auto refresh is disabled', async () => {
            vi.useFakeTimers();
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await vi.runOnlyPendingTimersAsync();

            act(() => {
                result.current.setAutoRefreshEnabled(false);
            });

            const before = mockApiGet.mock.calls.length;

            await act(async () => {
                await vi.advanceTimersByTimeAsync(120000);
            });

            expect(mockApiGet.mock.calls.length).toBe(before);
        });
    });

    describe('user logout clears state', () => {
        it('resets clusterData when user becomes null', async () => {
            const { result, rerender } = renderHook(() => useClusterData(), {
                wrapper,
            });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            mockUser = null;
            rerender();

            await waitFor(() => {
                expect(result.current.clusterData).toEqual([]);
            });
        });

        it('clears a load error when user becomes null', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('boom'));
            const { result, rerender } = renderHook(() => useClusterData(), {
                wrapper,
            });

            await waitFor(() => {
                expect(result.current.error).toBe('boom');
            });

            mockUser = null;
            rerender();

            await waitFor(() => {
                expect(result.current.error).toBeNull();
            });
        });
    });

    describe('useClusterData hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterData());
            }).toThrow(
                'useClusterData must be used within a ClusterDataProvider',
            );
        });
    });
});
