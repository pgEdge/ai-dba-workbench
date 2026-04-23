/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - AlertsContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { AlertsProvider, useAlerts } from '../AlertsContext';

// Mock apiGet
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
}));

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

// Mock AuthContext — provide a stable user object
let mockUser: { username: string } | null = { username: 'testuser' };
vi.mock('../AuthContext', () => ({
    useAuth: () => ({ user: mockUser }),
}));

import { apiGet } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;

describe('AlertsContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <AlertsProvider>{children}</AlertsProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('default state', () => {
        it('starts with zero alerts and no lastFetch', () => {
            mockApiGet.mockReturnValueOnce(new Promise(() => {}));

            const { result } = renderHook(() => useAlerts(), { wrapper });

            expect(result.current.alertCounts).toEqual({
                total: 0,
                byServer: {},
                byCluster: {},
            });
            expect(result.current.lastFetch).toBeNull();
        });

        it('provides all expected context properties', async () => {
            mockApiGet.mockResolvedValueOnce({
                total: 0,
                by_server: {},
                by_cluster: {},
            });
            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current).toHaveProperty('alertCounts');
            expect(result.current).toHaveProperty('loading');
            expect(result.current).toHaveProperty('lastFetch');
            expect(result.current).toHaveProperty('fetchAlertCounts');
            expect(result.current).toHaveProperty('getServerAlertCount');
            expect(result.current).toHaveProperty('getClusterAlertCount');
            expect(result.current).toHaveProperty('getTotalAlertCount');
        });
    });

    describe('fetchAlertCounts', () => {
        it('populates counts from the API response', async () => {
            mockApiGet.mockResolvedValueOnce({
                total: 5,
                by_server: { 1: 2, 2: 3 },
                by_cluster: { 'cluster-1': 5 },
            });

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.alertCounts.total).toBe(5);
            });

            expect(result.current.alertCounts.byServer).toEqual({ 1: 2, 2: 3 });
            expect(result.current.alertCounts.byCluster).toEqual({ 'cluster-1': 5 });
            expect(result.current.lastFetch).toBeInstanceOf(Date);
            expect(result.current.loading).toBe(false);
        });

        it('defaults missing fields to 0 / empty objects', async () => {
            mockApiGet.mockResolvedValueOnce({});

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.lastFetch).not.toBeNull();
            });

            expect(result.current.alertCounts).toEqual({
                total: 0,
                byServer: {},
                byCluster: {},
            });
        });

        it('logs error and keeps state when fetch fails', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('boom'));

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.alertCounts.total).toBe(0);
            expect(result.current.lastFetch).toBeNull();
        });

        it('fetchAlertCounts is a no-op when user is null', async () => {
            mockUser = null;
            const { result } = renderHook(() => useAlerts(), { wrapper });

            await act(async () => {
                await result.current.fetchAlertCounts();
            });

            expect(mockApiGet).not.toHaveBeenCalled();
        });

        it('calls the correct endpoint', async () => {
            mockApiGet.mockResolvedValueOnce({ total: 0 });
            renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/alerts/counts');
            });
        });
    });

    describe('helper selectors', () => {
        it('getServerAlertCount returns count for the server', async () => {
            mockApiGet.mockResolvedValueOnce({
                total: 10,
                by_server: { 42: 7 },
            });

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.alertCounts.total).toBe(10);
            });

            expect(result.current.getServerAlertCount(42)).toBe(7);
            expect(result.current.getServerAlertCount(99)).toBe(0);
        });

        it('getClusterAlertCount sums counts for all provided server IDs', async () => {
            mockApiGet.mockResolvedValueOnce({
                total: 10,
                by_server: { 1: 3, 2: 4, 3: 1 },
            });

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.alertCounts.total).toBe(10);
            });

            expect(result.current.getClusterAlertCount([1, 2, 3])).toBe(8);
            expect(result.current.getClusterAlertCount([1, 99])).toBe(3);
        });

        it('getClusterAlertCount returns 0 for empty or missing arrays', async () => {
            mockApiGet.mockResolvedValueOnce({ total: 0 });

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.getClusterAlertCount([])).toBe(0);
            expect(
                result.current.getClusterAlertCount(
                    null as unknown as number[],
                ),
            ).toBe(0);
        });

        it('getTotalAlertCount returns the running total', async () => {
            mockApiGet.mockResolvedValueOnce({ total: 42 });

            const { result } = renderHook(() => useAlerts(), { wrapper });

            await waitFor(() => {
                expect(result.current.alertCounts.total).toBe(42);
            });

            expect(result.current.getTotalAlertCount()).toBe(42);
        });
    });

    describe('auto-refresh', () => {
        it('refetches every 30 seconds', async () => {
            vi.useFakeTimers();
            mockApiGet.mockResolvedValue({ total: 0 });

            renderHook(() => useAlerts(), { wrapper });

            // Allow initial fetch to resolve.
            await vi.runOnlyPendingTimersAsync();
            const initialCallCount = mockApiGet.mock.calls.length;
            expect(initialCallCount).toBeGreaterThanOrEqual(1);

            await act(async () => {
                await vi.advanceTimersByTimeAsync(30000);
            });

            expect(mockApiGet.mock.calls.length).toBeGreaterThan(initialCallCount);
        });
    });

    describe('user-gating', () => {
        it('does not fetch when user is null', async () => {
            mockUser = null;
            renderHook(() => useAlerts(), { wrapper });

            // Allow any pending microtasks.
            await new Promise((r) => setTimeout(r, 10));
            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });

    describe('useAlerts hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useAlerts());
            }).toThrow('useAlerts must be used within an AlertsProvider');
        });
    });
});
