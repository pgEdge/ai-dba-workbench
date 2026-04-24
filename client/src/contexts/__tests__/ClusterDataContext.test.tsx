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
            expect(result[0].clusters).toHaveLength(1);
            expect(result[0].clusters[0].servers).toHaveLength(2);
        });

        it('creates standalone clusters for connections without cluster_name', () => {
            const conns = [
                { id: 1, name: 'lone', host: 'h', port: 5432 },
            ];

            const result = transformConnectionsToHierarchy(conns);

            expect(result).toHaveLength(1);
            expect(result[0].name).toBe('Ungrouped');
            expect(result[0].clusters[0].isStandalone).toBe(true);
            expect(result[0].clusters[0].servers[0].id).toBe(1);
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
    });

    describe('auto-refresh', () => {
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
