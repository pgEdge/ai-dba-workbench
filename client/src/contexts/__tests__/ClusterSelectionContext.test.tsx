/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterSelectionContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ClusterSelectionProvider } from '../ClusterSelectionContext';
import { useClusterSelection } from '../useClusterSelection';
import type { ClusterGroup, ClusterServer, ClusterEntry } from '../ClusterDataContext';

vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    apiPost: vi.fn(),
    apiDelete: vi.fn(),
}));

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

// Control user + clusterData from the test.
let mockUser: { username: string } | null = { username: 'testuser' };
let mockClusterData: ClusterGroup[] = [];
vi.mock('../useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));
vi.mock('../useClusterData', () => ({
    useClusterData: () => ({ clusterData: mockClusterData }),
}));

import { apiGet, apiPost, apiDelete } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;
const mockApiPost = apiPost as unknown as ReturnType<typeof vi.fn>;
const mockApiDelete = apiDelete as unknown as ReturnType<typeof vi.fn>;

const server1: ClusterServer = { id: 1, name: 'pg-1', status: 'online' };
const server2: ClusterServer = { id: 2, name: 'pg-2', status: 'online' };
const cluster1: ClusterEntry = {
    id: 'cluster-1',
    name: 'us-east',
    servers: [server1, server2],
};
const baseData: ClusterGroup[] = [
    { id: 'group-1', name: 'prod', clusters: [cluster1] },
];

describe('ClusterSelectionContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <ClusterSelectionProvider>{children}</ClusterSelectionProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
        mockClusterData = [];
        mockApiGet.mockRejectedValue(new Error('no current connection'));
        mockApiPost.mockResolvedValue({ connection_id: 0 });
        mockApiDelete.mockResolvedValue({});
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('default state', () => {
        it('starts with no selection', () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            expect(result.current.selectedServer).toBeNull();
            expect(result.current.selectedCluster).toBeNull();
            expect(result.current.selectionType).toBeNull();
            expect(result.current.currentConnection).toBeNull();
        });

        it('provides all expected properties', () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            expect(result.current).toHaveProperty('selectedServer');
            expect(result.current).toHaveProperty('selectedCluster');
            expect(result.current).toHaveProperty('selectionType');
            expect(result.current).toHaveProperty('currentConnection');
            expect(result.current).toHaveProperty('selectServer');
            expect(result.current).toHaveProperty('selectCluster');
            expect(result.current).toHaveProperty('selectEstate');
            expect(result.current).toHaveProperty('clearSelection');
        });
    });

    describe('selectServer', () => {
        it('sets selectedServer and posts to /connections/current', async () => {
            mockApiPost.mockResolvedValueOnce({ connection_id: 1 });

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await act(async () => {
                await result.current.selectServer(server1);
            });

            expect(result.current.selectedServer).toEqual(server1);
            expect(result.current.selectedCluster).toBeNull();
            expect(result.current.selectionType).toBe('server');
            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/connections/current',
                { connection_id: 1 },
            );
            expect(result.current.currentConnection).toEqual({ connection_id: 1 });
        });

        it('logs but does not throw when POST fails', async () => {
            mockApiPost.mockRejectedValueOnce(new Error('boom'));

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await act(async () => {
                await result.current.selectServer(server1);
            });

            expect(result.current.selectedServer).toEqual(server1);
        });

        it('is a no-op when user is null', async () => {
            mockUser = null;

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await act(async () => {
                await result.current.selectServer(server1);
            });

            expect(mockApiPost).not.toHaveBeenCalled();
            expect(result.current.selectedServer).toBeNull();
        });
    });

    describe('selectCluster', () => {
        it('sets selectedCluster and clears others', () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            act(() => {
                result.current.selectCluster(cluster1);
            });

            expect(result.current.selectedCluster).toEqual(cluster1);
            expect(result.current.selectedServer).toBeNull();
            expect(result.current.currentConnection).toBeNull();
            expect(result.current.selectionType).toBe('cluster');
        });
    });

    describe('selectEstate', () => {
        it('clears selection and sets type to estate', () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            act(() => {
                result.current.selectEstate();
            });

            expect(result.current.selectedCluster).toBeNull();
            expect(result.current.selectedServer).toBeNull();
            expect(result.current.selectionType).toBe('estate');
        });
    });

    describe('clearSelection', () => {
        it('clears state and calls DELETE', async () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            act(() => {
                result.current.selectCluster(cluster1);
            });

            await act(async () => {
                await result.current.clearSelection();
            });

            expect(result.current.selectedCluster).toBeNull();
            expect(result.current.selectedServer).toBeNull();
            expect(result.current.selectionType).toBeNull();
            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/connections/current');
        });

        it('logs on DELETE failure but still clears local state', async () => {
            mockApiDelete.mockRejectedValueOnce(new Error('boom'));

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await act(async () => {
                await result.current.clearSelection();
            });

            expect(result.current.selectionType).toBeNull();
        });

        it('is a no-op when user is null', async () => {
            mockUser = null;
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await act(async () => {
                await result.current.clearSelection();
            });

            expect(mockApiDelete).not.toHaveBeenCalled();
        });
    });

    describe('fetchCurrentConnection (cluster data effect)', () => {
        it('syncs selectedServer based on current connection', async () => {
            mockApiGet.mockResolvedValueOnce({ connection_id: 1 });
            mockClusterData = baseData;

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await waitFor(() => {
                expect(result.current.selectedServer?.id).toBe(1);
            });

            expect(result.current.selectionType).toBe('server');
            expect(result.current.currentConnection).toEqual({ connection_id: 1 });
        });

        it('silently ignores errors from /connections/current', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('no current'));
            mockClusterData = baseData;

            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            // Wait for the effect to call the API, then verify state remains null.
            await waitFor(() => expect(mockApiGet).toHaveBeenCalled());
            expect(result.current.selectedServer).toBeNull();
        });
    });

    describe('user logout resets selection', () => {
        it('clears selection when user becomes null', async () => {
            const { result, rerender } = renderHook(() => useClusterSelection(), {
                wrapper,
            });

            act(() => {
                result.current.selectCluster(cluster1);
            });

            mockUser = null;
            rerender();

            await waitFor(() => {
                expect(result.current.selectedCluster).toBeNull();
            });

            expect(result.current.selectionType).toBeNull();
        });
    });

    describe('re-syncing on cluster data updates', () => {
        it('updates selectedCluster reference when cluster data refreshes', async () => {
            mockClusterData = baseData;

            const { result, rerender } = renderHook(() => useClusterSelection(), {
                wrapper,
            });

            act(() => {
                result.current.selectCluster(cluster1);
            });

            // Simulate a fresh reference for the same cluster id.
            const refreshedCluster: ClusterEntry = {
                ...cluster1,
                name: 'us-east-refreshed',
            };
            mockClusterData = [
                {
                    id: 'group-1',
                    name: 'prod',
                    clusters: [refreshedCluster],
                },
            ];
            rerender();

            await waitFor(() => {
                expect(result.current.selectedCluster).toBe(refreshedCluster);
            });
        });

        it('updates selectedServer reference when cluster data refreshes', async () => {
            mockClusterData = baseData;
            mockApiPost.mockResolvedValue({ connection_id: 1 });

            const { result, rerender } = renderHook(() => useClusterSelection(), {
                wrapper,
            });

            await act(async () => {
                await result.current.selectServer(server1);
            });

            const refreshedServer: ClusterServer = { ...server1, status: 'warning' };
            const refreshedCluster: ClusterEntry = {
                ...cluster1,
                servers: [refreshedServer, server2],
            };
            mockClusterData = [
                { id: 'group-1', name: 'prod', clusters: [refreshedCluster] },
            ];
            rerender();

            await waitFor(() => {
                expect(result.current.selectedServer).toBe(refreshedServer);
            });
        });

        it('finds servers nested inside children', async () => {
            const childServer: ClusterServer = {
                id: 50,
                name: 'replica',
                status: 'online',
            };
            const parentServer: ClusterServer = {
                id: 49,
                name: 'primary',
                status: 'online',
                children: [childServer],
            };
            const nestedCluster: ClusterEntry = {
                id: 'cluster-nested',
                name: 'nested',
                servers: [parentServer],
            };
            mockClusterData = [
                { id: 'group-n', name: 'nested-group', clusters: [nestedCluster] },
            ];
            mockApiPost.mockResolvedValue({ connection_id: 50 });

            const { result, rerender } = renderHook(() => useClusterSelection(), {
                wrapper,
            });

            await act(async () => {
                await result.current.selectServer(childServer);
            });

            const refreshedChild: ClusterServer = {
                ...childServer,
                status: 'warning',
            };
            const refreshedParent: ClusterServer = {
                ...parentServer,
                children: [refreshedChild],
            };
            mockClusterData = [
                {
                    id: 'group-n',
                    name: 'nested-group',
                    clusters: [{ ...nestedCluster, servers: [refreshedParent] }],
                },
            ];
            rerender();

            await waitFor(() => {
                expect(result.current.selectedServer).toBe(refreshedChild);
            });
        });
    });

    describe('hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterSelection());
            }).toThrow(
                'useClusterSelection must be used within a ClusterSelectionProvider',
            );
        });
    });
});
