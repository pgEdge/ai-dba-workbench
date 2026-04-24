/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - BlackoutContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
    BlackoutProvider,
    useBlackouts,
    Blackout,
    BlackoutSchedule,
} from '../BlackoutContext';
import type { Selection } from '../../types/selection';

vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    apiPost: vi.fn(),
    apiPut: vi.fn(),
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

let mockUser: { username: string } | null = { username: 'testuser' };
vi.mock('../AuthContext', () => ({
    useAuth: () => ({ user: mockUser }),
}));

import {
    apiGet,
    apiPost,
    apiPut,
    apiDelete,
} from '../../utils/apiClient';

const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;
const mockApiPost = apiPost as unknown as ReturnType<typeof vi.fn>;
const mockApiPut = apiPut as unknown as ReturnType<typeof vi.fn>;
const mockApiDelete = apiDelete as unknown as ReturnType<typeof vi.fn>;

const activeEstateBlackout: Blackout = {
    id: 1,
    scope: 'estate',
    reason: 'estate wide',
    start_time: '2026-01-01T00:00:00Z',
    end_time: '2026-12-31T00:00:00Z',
    created_by: 'admin',
    created_at: '2026-01-01T00:00:00Z',
    is_active: true,
};

const activeClusterBlackout: Blackout = {
    id: 2,
    scope: 'cluster',
    cluster_id: 10,
    reason: 'cluster',
    start_time: '2026-01-01T00:00:00Z',
    end_time: '2026-12-31T00:00:00Z',
    created_by: 'admin',
    created_at: '2026-01-01T00:00:00Z',
    is_active: true,
};

const activeServerBlackout: Blackout = {
    id: 3,
    scope: 'server',
    connection_id: 100,
    reason: 'server',
    start_time: '2026-01-01T00:00:00Z',
    end_time: '2026-12-31T00:00:00Z',
    created_by: 'admin',
    created_at: '2026-01-01T00:00:00Z',
    is_active: true,
};

const inactiveBlackout: Blackout = {
    id: 4,
    scope: 'estate',
    reason: 'past',
    start_time: '2025-01-01T00:00:00Z',
    end_time: '2025-06-01T00:00:00Z',
    created_by: 'admin',
    created_at: '2025-01-01T00:00:00Z',
    is_active: false,
};

const schedule: BlackoutSchedule = {
    id: 5,
    scope: 'estate',
    name: 'nightly',
    cron_expression: '0 2 * * *',
    duration_minutes: 60,
    timezone: 'UTC',
    reason: 'maintenance',
    enabled: true,
    created_by: 'admin',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
};

const mockBlackoutsResponse = {
    blackouts: [
        activeEstateBlackout,
        activeClusterBlackout,
        activeServerBlackout,
        inactiveBlackout,
    ],
};

const mockSchedulesResponse = { schedules: [schedule] };

describe('BlackoutContext', () => {
    const buildWrapper = (selection: Selection | null = null) => {
        return ({ children }: { children: React.ReactNode }) => (
            <BlackoutProvider selection={selection}>{children}</BlackoutProvider>
        );
    };

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/blackouts') {
                return Promise.resolve(mockBlackoutsResponse);
            }
            if (url === '/api/v1/blackout-schedules') {
                return Promise.resolve(mockSchedulesResponse);
            }
            return Promise.reject(new Error('unknown url ' + url));
        });
        mockApiPost.mockResolvedValue({});
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('initial load', () => {
        it('fetches both blackouts and schedules on mount', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            expect(result.current.schedules.length).toBe(1);
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/blackouts');
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/blackout-schedules');
        });

        it('defaults to empty arrays when responses lack fields', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockResolvedValue({});
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.blackouts).toEqual([]);
            expect(result.current.schedules).toEqual([]);
        });

        it('sets error when fetch fails', async () => {
            mockApiGet.mockReset();
            mockApiGet.mockRejectedValue(new Error('fetch failed'));

            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.error).toBe('fetch failed');
            });
        });

        it('does not fetch when user is null', async () => {
            mockUser = null;

            renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            // Flush microtasks.
            await new Promise((r) => setTimeout(r, 10));
            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });

    describe('CRUD operations', () => {
        it('createBlackout POSTs and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            mockApiGet.mockClear();

            await act(async () => {
                await result.current.createBlackout({
                    scope: 'estate',
                    reason: 'new',
                    start_time: '2026-05-01T00:00:00Z',
                    end_time: '2026-05-02T00:00:00Z',
                });
            });

            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/blackouts',
                expect.objectContaining({ scope: 'estate', reason: 'new' }),
            );
            expect(mockApiGet).toHaveBeenCalled();
        });

        it('stopBlackout calls the stop endpoint and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            await act(async () => {
                await result.current.stopBlackout(1);
            });

            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/blackouts/1/stop');
        });

        it('deleteBlackout DELETEs and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            await act(async () => {
                await result.current.deleteBlackout(1);
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/blackouts/1');
        });

        it('createSchedule POSTs and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            await act(async () => {
                await result.current.createSchedule({
                    scope: 'estate',
                    name: 'maint',
                    cron_expression: '0 0 * * *',
                    duration_minutes: 10,
                    timezone: 'UTC',
                    reason: 'r',
                });
            });

            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/blackout-schedules',
                expect.objectContaining({ name: 'maint' }),
            );
        });

        it('updateSchedule PUTs and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            await act(async () => {
                await result.current.updateSchedule(5, { enabled: false });
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/blackout-schedules/5',
                { enabled: false },
            );
        });

        it('deleteSchedule DELETEs and refetches', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            await act(async () => {
                await result.current.deleteSchedule(5);
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/blackout-schedules/5');
        });
    });

    describe('activeBlackoutsForSelection', () => {
        it('returns only active blackouts when selection is null', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(null),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            expect(result.current.activeBlackoutsForSelection).toHaveLength(3);
            expect(
                result.current.activeBlackoutsForSelection.every(b => b.is_active),
            ).toBe(true);
        });

        it('estate selection shows all active blackouts', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({ type: 'estate', name: 'Estate', status: 'online', groups: [] }),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            expect(result.current.activeBlackoutsForSelection).toHaveLength(3);
        });

        it('cluster selection shows estate, matching cluster, and server-in-cluster blackouts', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({
                    type: 'cluster',
                    id: '10',
                    name: 'Test Cluster',
                    status: 'online',
                    description: '',
                    servers: [],
                    serverIds: [100],
                }),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            const ids = result.current.activeBlackoutsForSelection.map(b => b.id);
            expect(ids).toContain(1); // estate
            expect(ids).toContain(2); // matching cluster
            expect(ids).toContain(3); // server in cluster
        });

        it('cluster selection filters out non-matching cluster blackouts', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({
                    type: 'cluster',
                    id: '999',
                    name: 'Other Cluster',
                    status: 'online',
                    description: '',
                    servers: [],
                    serverIds: [999],
                }),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            const ids = result.current.activeBlackoutsForSelection.map(b => b.id);
            expect(ids).toContain(1); // estate still applies
            expect(ids).not.toContain(2);
            expect(ids).not.toContain(3);
        });

        it('server selection shows estate and matching server blackouts but excludes cluster', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({
                    type: 'server',
                    id: 100,
                    name: 'Server 100',
                    status: 'online',
                    description: '',
                    host: 'localhost',
                    port: 5432,
                    role: 'primary',
                    version: '16',
                    database: 'postgres',
                    username: 'postgres',
                    os: 'linux',
                    platform: 'x86_64',
                }),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            const ids = result.current.activeBlackoutsForSelection.map(b => b.id);
            expect(ids).toContain(1); // estate
            expect(ids).toContain(3); // server
            expect(ids).not.toContain(2); // cluster excluded for server view
        });

        it('server selection without match only shows estate blackouts', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({
                    type: 'server',
                    id: 9999,
                    name: 'Server 9999',
                    status: 'online',
                    description: '',
                    host: 'localhost',
                    port: 5432,
                    role: 'primary',
                    version: '16',
                    database: 'postgres',
                    username: 'postgres',
                    os: 'linux',
                    platform: 'x86_64',
                }),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            const ids = result.current.activeBlackoutsForSelection.map(b => b.id);
            expect(ids).toEqual([1]);
        });

        it('unknown selection type only keeps estate blackouts', async () => {
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper({ type: 'unknown-type' } as unknown as Selection),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            const ids = result.current.activeBlackoutsForSelection.map(b => b.id);
            expect(ids).toEqual([1]);
        });
    });

    describe('auto-refresh', () => {
        it('refetches every 30 seconds', async () => {
            vi.useFakeTimers();
            const { result } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await vi.runOnlyPendingTimersAsync();
            const callsBefore = mockApiGet.mock.calls.length;
            expect(result.current).toBeDefined();

            await act(async () => {
                await vi.advanceTimersByTimeAsync(30000);
            });

            expect(mockApiGet.mock.calls.length).toBeGreaterThan(callsBefore);
        });
    });

    describe('user logout clears state', () => {
        it('resets blackouts and schedules when user becomes null', async () => {
            const { result, rerender } = renderHook(() => useBlackouts(), {
                wrapper: buildWrapper(),
            });

            await waitFor(() => {
                expect(result.current.blackouts.length).toBe(4);
            });

            mockUser = null;
            rerender();

            await waitFor(() => {
                expect(result.current.blackouts).toEqual([]);
            });

            expect(result.current.schedules).toEqual([]);
        });
    });

    describe('useBlackouts hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useBlackouts());
            }).toThrow('useBlackouts must be used within a BlackoutProvider');
        });
    });
});
