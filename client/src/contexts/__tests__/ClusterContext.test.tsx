/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ClusterProvider, useCluster, useClusterData, useClusterSelection, useClusterActions } from '../ClusterContext';

// Create stable user object outside the mock to avoid infinite re-renders
const mockUser = { username: 'testuser' };

// Mock the AuthContext
vi.mock('../AuthContext', () => ({
    useAuth: () => ({
        user: mockUser,
    }),
}));

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch as unknown as typeof fetch;

// Mock cluster data
const mockClusterData = [
    {
        id: 'group-1',
        name: 'Production',
        clusters: [
            {
                id: 'cluster-1',
                name: 'US East',
                servers: [
                    { id: 1, name: 'pg-1', status: 'online', primary_role: 'binary_primary' },
                    { id: 2, name: 'pg-2', status: 'online', primary_role: 'binary_standby' },
                ],
            },
        ],
    },
];

describe('ClusterContext', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockFetch.mockResolvedValue({
            ok: true,
            json: () => Promise.resolve(mockClusterData),
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <ClusterProvider>{children}</ClusterProvider>
    );

    describe('useCluster (backward compatibility)', () => {
        it('fetches cluster data on mount', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            expect(mockFetch).toHaveBeenCalledWith('/api/v1/clusters', expect.any(Object));
        });

        it('does not update state when data fingerprint is unchanged', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            const initialData = result.current.clusterData;

            // Trigger another fetch with same data
            await act(async () => {
                await result.current.fetchClusterData();
            });

            // Data reference should be the same since fingerprint hasn't changed
            expect(result.current.clusterData).toBe(initialData);
        });

        it('updates state when data fingerprint changes', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            const initialData = result.current.clusterData;

            // Update mock to return different data
            const updatedData = JSON.parse(JSON.stringify(mockClusterData));
            updatedData[0].clusters[0].servers[0].status = 'warning';
            mockFetch.mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(updatedData),
            });

            // Trigger another fetch with different data
            await act(async () => {
                await result.current.fetchClusterData();
            });

            // Data should be updated since fingerprint changed
            expect(result.current.clusterData).not.toBe(initialData);
            expect(result.current.clusterData[0].clusters[0].servers[0].status).toBe('warning');
        });

        it('updates lastRefresh even when data is unchanged', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            await waitFor(() => {
                expect(result.current.lastRefresh).not.toBeNull();
            });

            const initialRefresh = result.current.lastRefresh;

            // Wait a bit to ensure time difference
            await new Promise(resolve => setTimeout(resolve, 10));

            // Trigger another fetch
            await act(async () => {
                await result.current.fetchClusterData();
            });

            // lastRefresh should be updated
            expect(result.current.lastRefresh.getTime()).toBeGreaterThanOrEqual(
                initialRefresh.getTime()
            );
        });

        it('only shows loading state on initial load', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            // Wait for initial load to complete
            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Create a promise we can control
            let resolvePromise: (value: unknown) => void;
            const controlledPromise = new Promise((resolve) => {
                resolvePromise = resolve;
            });

            mockFetch.mockReturnValue(controlledPromise);

            // Trigger refresh - should NOT show loading
            act(() => {
                result.current.fetchClusterData();
            });

            // Loading should still be false (not initial load)
            expect(result.current.loading).toBe(false);

            // Resolve the promise
            await act(async () => {
                resolvePromise({
                    ok: true,
                    json: () => Promise.resolve(mockClusterData),
                });
            });
        });

        it('provides all expected properties', async () => {
            const { result } = renderHook(() => useCluster(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            // Data properties
            expect(result.current).toHaveProperty('clusterData');
            expect(result.current).toHaveProperty('loading');
            expect(result.current).toHaveProperty('error');
            expect(result.current).toHaveProperty('lastRefresh');
            expect(result.current).toHaveProperty('autoRefreshEnabled');
            expect(result.current).toHaveProperty('setAutoRefreshEnabled');
            expect(result.current).toHaveProperty('fetchClusterData');

            // Selection properties
            expect(result.current).toHaveProperty('selectedServer');
            expect(result.current).toHaveProperty('selectedCluster');
            expect(result.current).toHaveProperty('selectionType');
            expect(result.current).toHaveProperty('currentConnection');
            expect(result.current).toHaveProperty('selectServer');
            expect(result.current).toHaveProperty('selectCluster');
            expect(result.current).toHaveProperty('selectEstate');
            expect(result.current).toHaveProperty('clearSelection');

            // Action properties
            expect(result.current).toHaveProperty('updateGroupName');
            expect(result.current).toHaveProperty('updateClusterName');
            expect(result.current).toHaveProperty('updateServerName');
            expect(result.current).toHaveProperty('getServer');
            expect(result.current).toHaveProperty('createServer');
            expect(result.current).toHaveProperty('updateServer');
            expect(result.current).toHaveProperty('deleteServer');
            expect(result.current).toHaveProperty('createGroup');
            expect(result.current).toHaveProperty('deleteGroup');
            expect(result.current).toHaveProperty('moveClusterToGroup');
        });
    });

    describe('useClusterData', () => {
        it('provides data-related properties', async () => {
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

        it('does not include selection or action properties', async () => {
            const { result } = renderHook(() => useClusterData(), { wrapper });

            await waitFor(() => {
                expect(result.current.clusterData.length).toBeGreaterThan(0);
            });

            // Should not have selection properties
            expect(result.current).not.toHaveProperty('selectedServer');
            expect(result.current).not.toHaveProperty('selectServer');

            // Should not have action properties
            expect(result.current).not.toHaveProperty('createServer');
            expect(result.current).not.toHaveProperty('deleteServer');
        });
    });

    describe('useClusterSelection', () => {
        it('provides selection-related properties', async () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await waitFor(() => {
                expect(result.current.selectServer).toBeDefined();
            });

            expect(result.current).toHaveProperty('selectedServer');
            expect(result.current).toHaveProperty('selectedCluster');
            expect(result.current).toHaveProperty('selectionType');
            expect(result.current).toHaveProperty('currentConnection');
            expect(result.current).toHaveProperty('selectServer');
            expect(result.current).toHaveProperty('selectCluster');
            expect(result.current).toHaveProperty('selectEstate');
            expect(result.current).toHaveProperty('clearSelection');
        });

        it('does not include data or action properties', async () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await waitFor(() => {
                expect(result.current.selectServer).toBeDefined();
            });

            // Should not have data properties
            expect(result.current).not.toHaveProperty('clusterData');
            expect(result.current).not.toHaveProperty('fetchClusterData');

            // Should not have action properties
            expect(result.current).not.toHaveProperty('createServer');
            expect(result.current).not.toHaveProperty('deleteServer');
        });

        it('can select estate', async () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            await waitFor(() => {
                expect(result.current.selectEstate).toBeDefined();
            });

            act(() => {
                result.current.selectEstate();
            });

            expect(result.current.selectionType).toBe('estate');
            expect(result.current.selectedServer).toBeNull();
            expect(result.current.selectedCluster).toBeNull();
        });

        it('can select a cluster', async () => {
            const { result } = renderHook(() => useClusterSelection(), { wrapper });

            // Wait for the async cluster data fetch to complete before
            // calling selectCluster. ClusterSelectionProvider runs a
            // re-sync effect (keyed on clusterData changes) that rewrites
            // selectedCluster to the fresh fixture reference whenever the
            // selected cluster's id matches an id in the loaded data.
            // Without this wait, the fetch may resolve mid-assertion and
            // the effect could overwrite the hand-rolled stub, producing
            // a flake. Re-sync behaviour itself is covered by dedicated
            // tests in ClusterSelectionContext.test.tsx.
            await waitFor(() => {
                expect(result.current.selectCluster).toBeDefined();
            });
            await waitFor(() => {
                expect(mockFetch).toHaveBeenCalled();
            });

            // Use an id that is not present in mockClusterData so the
            // re-sync effect cannot find a match and cannot replace the
            // stub. This isolates the pure-setter behaviour of
            // selectCluster from the re-sync effect.
            const testCluster = { id: 'cluster-nonexistent', name: 'Test Cluster' };

            act(() => {
                result.current.selectCluster(testCluster);
            });

            expect(result.current.selectionType).toBe('cluster');
            expect(result.current.selectedCluster).toEqual(testCluster);
            expect(result.current.selectedServer).toBeNull();
        });
    });

    describe('useClusterActions', () => {
        it('provides action-related properties', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await waitFor(() => {
                expect(result.current.createServer).toBeDefined();
            });

            expect(result.current).toHaveProperty('updateGroupName');
            expect(result.current).toHaveProperty('updateClusterName');
            expect(result.current).toHaveProperty('updateServerName');
            expect(result.current).toHaveProperty('getServer');
            expect(result.current).toHaveProperty('createServer');
            expect(result.current).toHaveProperty('updateServer');
            expect(result.current).toHaveProperty('deleteServer');
            expect(result.current).toHaveProperty('createGroup');
            expect(result.current).toHaveProperty('deleteGroup');
            expect(result.current).toHaveProperty('moveClusterToGroup');
        });

        it('does not include data or selection properties', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await waitFor(() => {
                expect(result.current.createServer).toBeDefined();
            });

            // Should not have data properties
            expect(result.current).not.toHaveProperty('clusterData');
            expect(result.current).not.toHaveProperty('loading');

            // Should not have selection properties
            expect(result.current).not.toHaveProperty('selectedServer');
            expect(result.current).not.toHaveProperty('selectServer');
        });
    });

    describe('Error handling', () => {
        it('throws error when useCluster is used outside provider', () => {
            expect(() => {
                renderHook(() => useCluster());
            }).toThrow('useCluster must be used within a ClusterProvider');
        });

        it('throws error when useClusterData is used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterData());
            }).toThrow('useClusterData must be used within a ClusterDataProvider');
        });

        it('throws error when useClusterSelection is used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterSelection());
            }).toThrow('useClusterSelection must be used within a ClusterSelectionProvider');
        });

        it('throws error when useClusterActions is used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterActions());
            }).toThrow('useClusterActions must be used within a ClusterActionsProvider');
        });
    });
});
