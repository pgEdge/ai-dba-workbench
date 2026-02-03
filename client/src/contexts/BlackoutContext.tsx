/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import React, { createContext, useContext, useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { useAuth } from './AuthContext';

export interface Blackout {
    id: number;
    scope: 'estate' | 'group' | 'cluster' | 'server';
    group_id?: number;
    cluster_id?: number;
    connection_id?: number;
    database_name?: string;
    reason: string;
    start_time: string;
    end_time: string;
    created_by: string;
    created_at: string;
    is_active: boolean;
}

export interface BlackoutSchedule {
    id: number;
    scope: 'estate' | 'group' | 'cluster' | 'server';
    group_id?: number;
    cluster_id?: number;
    connection_id?: number;
    database_name?: string;
    name: string;
    cron_expression: string;
    duration_minutes: number;
    timezone: string;
    reason: string;
    enabled: boolean;
    created_by: string;
    created_at: string;
    updated_at: string;
}

export interface CreateBlackoutRequest {
    scope: 'estate' | 'group' | 'cluster' | 'server';
    group_id?: number;
    cluster_id?: number;
    connection_id?: number;
    database_name?: string;
    reason: string;
    start_time: string;
    end_time: string;
}

export interface CreateScheduleRequest {
    scope: 'estate' | 'group' | 'cluster' | 'server';
    group_id?: number;
    cluster_id?: number;
    connection_id?: number;
    database_name?: string;
    name: string;
    cron_expression: string;
    duration_minutes: number;
    timezone: string;
    reason: string;
}

export interface UpdateScheduleRequest {
    name?: string;
    cron_expression?: string;
    duration_minutes?: number;
    timezone?: string;
    reason?: string;
    enabled?: boolean;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export interface BlackoutSelection {
    type: string;
    id?: string | number;
    name?: string;
    serverIds?: number[];
    [key: string]: unknown;
}

export interface BlackoutContextValue {
    blackouts: Blackout[];
    schedules: BlackoutSchedule[];
    loading: boolean;
    error: string | null;
    fetchBlackouts: () => Promise<void>;
    createBlackout: (data: CreateBlackoutRequest) => Promise<void>;
    stopBlackout: (id: number) => Promise<void>;
    deleteBlackout: (id: number) => Promise<void>;
    createSchedule: (data: CreateScheduleRequest) => Promise<void>;
    updateSchedule: (id: number, data: UpdateScheduleRequest) => Promise<void>;
    deleteSchedule: (id: number) => Promise<void>;
    activeBlackoutsForSelection: Blackout[];
}

interface BlackoutProviderProps {
    selection: BlackoutSelection | null;
    children: React.ReactNode;
}

const BlackoutContext = createContext<BlackoutContextValue | null>(null);

export const BlackoutProvider = ({ selection, children }: BlackoutProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const [blackouts, setBlackouts] = useState<Blackout[]>([]);
    const [schedules, setSchedules] = useState<BlackoutSchedule[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const autoRefreshInterval = 30000; // 30 seconds

    // Track if this is the initial load
    const isInitialLoadRef = useRef<boolean>(true);

    /**
     * Fetch all blackouts and schedules from the API.
     * Only shows loading state on initial load, not during auto-refresh.
     */
    const fetchBlackouts = useCallback(async (): Promise<void> => {
        if (!user) return;

        if (isInitialLoadRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const [blackoutsResponse, schedulesResponse] = await Promise.all([
                fetch('/api/v1/blackouts', {
                    credentials: 'include',
                    headers: { 'Content-Type': 'application/json' },
                }),
                fetch('/api/v1/blackout-schedules', {
                    credentials: 'include',
                    headers: { 'Content-Type': 'application/json' },
                }),
            ]);

            if (!blackoutsResponse.ok) {
                throw new Error('Failed to fetch blackouts');
            }
            if (!schedulesResponse.ok) {
                throw new Error('Failed to fetch blackout schedules');
            }

            const blackoutsData = await blackoutsResponse.json();
            const schedulesData = await schedulesResponse.json();

            setBlackouts(blackoutsData.blackouts || []);
            setSchedules(schedulesData.schedules || []);
        } catch (err) {
            console.error('Error fetching blackouts:', err);
            setError((err as Error).message);
        } finally {
            if (isInitialLoadRef.current) {
                isInitialLoadRef.current = false;
            }
            setLoading(false);
        }
    }, [user]);

    /**
     * Create a new blackout window.
     */
    const createBlackout = useCallback(async (data: CreateBlackoutRequest): Promise<void> => {
        const response = await fetch('/api/v1/blackouts', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data),
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to create blackout');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    /**
     * Stop an active blackout by setting its end time to now.
     */
    const stopBlackout = useCallback(async (id: number): Promise<void> => {
        const response = await fetch(`/api/v1/blackouts/${id}/stop`, {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to stop blackout');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    /**
     * Delete a blackout record.
     */
    const deleteBlackout = useCallback(async (id: number): Promise<void> => {
        const response = await fetch(`/api/v1/blackouts/${id}`, {
            method: 'DELETE',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to delete blackout');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    /**
     * Create a new blackout schedule.
     */
    const createSchedule = useCallback(async (data: CreateScheduleRequest): Promise<void> => {
        const response = await fetch('/api/v1/blackout-schedules', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data),
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to create schedule');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    /**
     * Update an existing blackout schedule.
     */
    const updateSchedule = useCallback(async (id: number, data: UpdateScheduleRequest): Promise<void> => {
        const response = await fetch(`/api/v1/blackout-schedules/${id}`, {
            method: 'PUT',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data),
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to update schedule');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    /**
     * Delete a blackout schedule.
     */
    const deleteSchedule = useCallback(async (id: number): Promise<void> => {
        const response = await fetch(`/api/v1/blackout-schedules/${id}`, {
            method: 'DELETE',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
        });

        if (!response.ok) {
            const errorBody = await response.text();
            throw new Error(errorBody || 'Failed to delete schedule');
        }

        await fetchBlackouts();
    }, [fetchBlackouts]);

    // Fetch blackouts when user changes
    useEffect(() => {
        if (user) {
            fetchBlackouts();
        } else {
            setBlackouts([]);
            setSchedules([]);
        }
    }, [user, fetchBlackouts]);

    // Auto-refresh every 30 seconds
    useEffect(() => {
        if (!user) return;

        const intervalId = setInterval(() => {
            fetchBlackouts();
        }, autoRefreshInterval);

        return () => clearInterval(intervalId);
    }, [user, fetchBlackouts]);

    /**
     * Filter active blackouts that apply to the current selection.
     * A blackout applies if:
     * - It is estate-scoped (applies to everything)
     * - Its scope matches the selection type and IDs match
     * - It is a broader scope that contains the selection
     */
    const activeBlackoutsForSelection = useMemo(() => {
        const active = blackouts.filter(b => b.is_active);
        if (!selection) return active;

        return active.filter(b => {
            // Estate-scoped blackouts apply to everything
            if (b.scope === 'estate') return true;

            if (selection.type === 'estate') {
                // Estate view shows all active blackouts
                return true;
            }

            if (selection.type === 'cluster') {
                // Show cluster-scoped blackouts matching this cluster
                if (b.scope === 'cluster' && b.cluster_id !== undefined) {
                    return String(b.cluster_id) === String(selection.id);
                }
                // Show server-scoped blackouts for servers in this cluster
                if (b.scope === 'server' && b.connection_id !== undefined && selection.serverIds) {
                    return selection.serverIds.includes(b.connection_id);
                }
                return false;
            }

            if (selection.type === 'server') {
                // Show cluster-scoped blackouts if this server is in that cluster
                if (b.scope === 'cluster') return false;
                // Show server-scoped blackouts matching this server
                if (b.scope === 'server' && b.connection_id !== undefined) {
                    return b.connection_id === selection.id;
                }
                return false;
            }

            return false;
        });
    }, [blackouts, selection]);

    const value: BlackoutContextValue = {
        blackouts,
        schedules,
        loading,
        error,
        fetchBlackouts,
        createBlackout,
        stopBlackout,
        deleteBlackout,
        createSchedule,
        updateSchedule,
        deleteSchedule,
        activeBlackoutsForSelection,
    };

    return (
        <BlackoutContext.Provider value={value}>
            {children}
        </BlackoutContext.Provider>
    );
};

export const useBlackouts = (): BlackoutContextValue => {
    const context = useContext(BlackoutContext);
    if (!context) {
        throw new Error('useBlackouts must be used within a BlackoutProvider');
    }
    return context;
};

export default BlackoutContext;
