/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - DashboardContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { DashboardProvider } from '../DashboardContext';
import { useDashboard } from '../useDashboard';
import type { OverlayEntry } from '../../components/Dashboard/types';

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

describe('DashboardContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <DashboardProvider>{children}</DashboardProvider>
    );

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('Default state', () => {
        it('starts with default time range of 1h', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            expect(result.current.timeRange).toEqual({ range: '1h' });
        });

        it('starts with auto refresh enabled at 30000ms', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            expect(result.current.autoRefresh).toEqual({
                enabled: true,
                intervalMs: 30000,
            });
        });

        it('starts with empty overlay stack and null currentOverlay', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            expect(result.current.overlayStack).toEqual([]);
            expect(result.current.currentOverlay).toBeNull();
        });

        it('starts with refreshTrigger of 0', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            expect(result.current.refreshTrigger).toBe(0);
        });
    });

    describe('Time range', () => {
        it('setTimeRange updates to a preset range and clears custom values', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setCustomTimeRange('2026-01-01', '2026-01-02');
            });

            act(() => {
                result.current.setTimeRange('24h');
            });

            expect(result.current.timeRange).toEqual({
                range: '24h',
                customStart: undefined,
                customEnd: undefined,
            });
        });

        it('setCustomTimeRange preserves the range and sets custom values', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setCustomTimeRange('2026-01-01', '2026-01-02');
            });

            expect(result.current.timeRange.customStart).toBe('2026-01-01');
            expect(result.current.timeRange.customEnd).toBe('2026-01-02');
            expect(result.current.timeRange.range).toBe('1h');
        });
    });

    describe('Auto refresh config', () => {
        it('setAutoRefreshEnabled toggles the enabled flag', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setAutoRefreshEnabled(false);
            });

            expect(result.current.autoRefresh.enabled).toBe(false);
            expect(result.current.autoRefresh.intervalMs).toBe(30000);
        });

        it('setAutoRefreshInterval updates the interval', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setAutoRefreshInterval(5000);
            });

            expect(result.current.autoRefresh.intervalMs).toBe(5000);
            expect(result.current.autoRefresh.enabled).toBe(true);
        });
    });

    describe('Overlay stack', () => {
        const entry1: OverlayEntry = {
            level: 'cluster',
            title: 'Cluster A',
            entityId: 1,
            entityName: 'Cluster A',
        };

        const entry2: OverlayEntry = {
            level: 'server',
            title: 'Server B',
            entityId: 2,
            entityName: 'Server B',
        };

        it('pushOverlay adds to the stack and updates currentOverlay', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.pushOverlay(entry1);
            });

            expect(result.current.overlayStack).toEqual([entry1]);
            expect(result.current.currentOverlay).toEqual(entry1);
        });

        it('pushOverlay stacks multiple entries; currentOverlay is last', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.pushOverlay(entry1);
            });
            act(() => {
                result.current.pushOverlay(entry2);
            });

            expect(result.current.overlayStack).toEqual([entry1, entry2]);
            expect(result.current.currentOverlay).toEqual(entry2);
        });

        it('popOverlay removes the last entry', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.pushOverlay(entry1);
            });
            act(() => {
                result.current.pushOverlay(entry2);
            });
            act(() => {
                result.current.popOverlay();
            });

            expect(result.current.overlayStack).toEqual([entry1]);
            expect(result.current.currentOverlay).toEqual(entry1);
        });

        it('popOverlay on empty stack is a no-op', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.popOverlay();
            });

            expect(result.current.overlayStack).toEqual([]);
            expect(result.current.currentOverlay).toBeNull();
        });

        it('clearOverlays empties the stack', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.pushOverlay(entry1);
                result.current.pushOverlay(entry2);
            });
            act(() => {
                result.current.clearOverlays();
            });

            expect(result.current.overlayStack).toEqual([]);
            expect(result.current.currentOverlay).toBeNull();
        });
    });

    describe('Refresh trigger', () => {
        it('triggerRefresh increments the counter', () => {
            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.triggerRefresh();
            });

            expect(result.current.refreshTrigger).toBe(1);

            act(() => {
                result.current.triggerRefresh();
            });

            expect(result.current.refreshTrigger).toBe(2);
        });

        it('auto-refresh interval calls triggerRefresh periodically', () => {
            vi.useFakeTimers();

            const { result } = renderHook(() => useDashboard(), { wrapper });

            expect(result.current.refreshTrigger).toBe(0);

            act(() => {
                vi.advanceTimersByTime(30000);
            });

            expect(result.current.refreshTrigger).toBe(1);

            act(() => {
                vi.advanceTimersByTime(30000);
            });

            expect(result.current.refreshTrigger).toBe(2);
        });

        it('auto-refresh does not fire when disabled', () => {
            vi.useFakeTimers();

            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setAutoRefreshEnabled(false);
            });

            act(() => {
                vi.advanceTimersByTime(120000);
            });

            expect(result.current.refreshTrigger).toBe(0);
        });

        it('auto-refresh uses the updated interval', () => {
            vi.useFakeTimers();

            const { result } = renderHook(() => useDashboard(), { wrapper });

            act(() => {
                result.current.setAutoRefreshInterval(1000);
            });

            act(() => {
                vi.advanceTimersByTime(1000);
            });

            expect(result.current.refreshTrigger).toBe(1);
        });
    });

    describe('useDashboard hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useDashboard());
            }).toThrow('useDashboard must be used within a DashboardProvider');
        });
    });
});
