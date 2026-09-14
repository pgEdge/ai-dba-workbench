/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { createContext, useState, useCallback, useRef, useEffect, useMemo } from 'react';
import type {
    TimeRange,
    TimeRangeState,
    AutoRefreshConfig,
    OverlayEntry,
} from '../components/Dashboard/types';

export interface DashboardContextValue {
    /** Time range */
    timeRange: TimeRangeState;
    setTimeRange: (range: TimeRange) => void;
    setCustomTimeRange: (start: string, end: string) => void;

    /** Auto refresh */
    autoRefresh: AutoRefreshConfig;
    setAutoRefreshEnabled: (enabled: boolean) => void;
    setAutoRefreshInterval: (intervalMs: number) => void;
    /**
     * True whilst auto-refresh is suspended because a custom time range
     * is active. The value is derived rather than stored, so the user's
     * own autoRefresh.enabled preference is untouched and resumes as
     * soon as they return to a preset range.
     */
    autoRefreshSuspended: boolean;

    /** Overlay stack */
    overlayStack: OverlayEntry[];
    currentOverlay: OverlayEntry | null;
    pushOverlay: (entry: OverlayEntry) => void;
    popOverlay: () => void;
    clearOverlays: () => void;

    /** Refresh trigger (incremented to trigger re-fetches) */
    refreshTrigger: number;
    triggerRefresh: () => void;
}

interface DashboardProviderProps {
    children: React.ReactNode;
}

const DashboardContext = createContext<DashboardContextValue | null>(null);

export const DashboardProvider = ({ children }: DashboardProviderProps): React.ReactElement => {
    // Time range state
    const [timeRange, setTimeRangeState] = useState<TimeRangeState>({
        range: '1h',
    });

    // Auto refresh state
    const [autoRefresh, setAutoRefreshState] = useState<AutoRefreshConfig>({
        enabled: true,
        intervalMs: 30000,
    });

    // Overlay stack
    const [overlayStack, setOverlayStack] = useState<OverlayEntry[]>([]);

    // Refresh trigger counter
    const [refreshTrigger, setRefreshTrigger] = useState<number>(0);

    // Stable ref for auto-refresh interval
    const triggerRefreshRef = useRef<() => void>(() => {});

    const setTimeRange = useCallback((range: TimeRange): void => {
        setTimeRangeState({
            range,
            customStart: undefined,
            customEnd: undefined,
        });
    }, []);

    const setCustomTimeRange = useCallback((start: string, end: string): void => {
        setTimeRangeState({
            range: 'custom',
            customStart: start,
            customEnd: end,
        });
    }, []);

    const setAutoRefreshEnabled = useCallback((enabled: boolean): void => {
        setAutoRefreshState(prev => ({ ...prev, enabled }));
    }, []);

    const setAutoRefreshInterval = useCallback((intervalMs: number): void => {
        setAutoRefreshState(prev => ({ ...prev, intervalMs }));
    }, []);

    const pushOverlay = useCallback((entry: OverlayEntry): void => {
        setOverlayStack(prev => [...prev, entry]);
    }, []);

    const popOverlay = useCallback((): void => {
        setOverlayStack(prev => prev.slice(0, -1));
    }, []);

    const clearOverlays = useCallback((): void => {
        setOverlayStack([]);
    }, []);

    const triggerRefresh = useCallback((): void => {
        setRefreshTrigger(prev => prev + 1);
    }, []);

    // Keep triggerRefresh ref up to date
    useEffect(() => {
        triggerRefreshRef.current = triggerRefresh;
    }, [triggerRefresh]);

    /*
     * Auto-refresh is suspended whilst a custom range is active, because
     * re-fetching a fixed historical window returns identical data on
     * every poll.
     */
    const autoRefreshSuspended = timeRange.range === 'custom';

    // Auto-refresh interval effect
    useEffect(() => {
        if (!autoRefresh.enabled || autoRefreshSuspended) { return; }

        const intervalId = setInterval(() => {
            triggerRefreshRef.current();
        }, autoRefresh.intervalMs);

        return () => { clearInterval(intervalId); };
    }, [autoRefresh.enabled, autoRefresh.intervalMs, autoRefreshSuspended]);

    const currentOverlay = overlayStack.length > 0
        ? overlayStack[overlayStack.length - 1]
        : null;

    const value: DashboardContextValue = useMemo(() => ({
        timeRange,
        setTimeRange,
        setCustomTimeRange,
        autoRefresh,
        setAutoRefreshEnabled,
        setAutoRefreshInterval,
        autoRefreshSuspended,
        overlayStack,
        currentOverlay,
        pushOverlay,
        popOverlay,
        clearOverlays,
        refreshTrigger,
        triggerRefresh,
    }), [
        timeRange,
        setTimeRange,
        setCustomTimeRange,
        autoRefresh,
        setAutoRefreshEnabled,
        setAutoRefreshInterval,
        autoRefreshSuspended,
        overlayStack,
        currentOverlay,
        pushOverlay,
        popOverlay,
        clearOverlays,
        refreshTrigger,
        triggerRefresh,
    ]);

    return (
        <DashboardContext.Provider value={value}>
            {children}
        </DashboardContext.Provider>
    );
};

export default DashboardContext;
