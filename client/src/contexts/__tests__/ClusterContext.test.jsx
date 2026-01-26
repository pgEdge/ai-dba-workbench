/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterContext Tests
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ClusterProvider, useCluster } from '../ClusterContext';

// Mock the AuthContext
vi.mock('../AuthContext', () => ({
    useAuth: () => ({
        sessionToken: 'test-token',
    }),
}));

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

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

    const wrapper = ({ children }) => (
        <ClusterProvider>{children}</ClusterProvider>
    );

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
        let resolvePromise;
        const controlledPromise = new Promise(resolve => {
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
});
