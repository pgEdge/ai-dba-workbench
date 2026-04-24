/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from '../../../contexts/AuthContext';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/ClusterDataContext';
import { DatabaseCacheHitData, DatabaseSummariesResponse } from './types';
import { logger } from '../../../utils/logger';

interface UseDatabaseCacheHitReturn {
    databases: DatabaseCacheHitData[];
    loading: boolean;
    error: string | null;
}

/**
 * Custom hook for fetching per-database cache hit ratio data.
 * Only fetches when a single server connection ID is provided.
 * Used by CacheHitTile to display per-database series.
 */
export const useDatabaseCacheHit = (
    connectionId: number | null
): UseDatabaseCacheHitReturn => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [databases, setDatabases] = useState<DatabaseCacheHitData[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user || connectionId === null) {
            setDatabases([]);
            return;
        }

        const url = `/api/v1/metrics/database-summaries`
            + `?connection_id=${connectionId}&time_range=24h`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await apiFetch(url);

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch database cache hit data: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result: DatabaseSummariesResponse = await response.json();
                const dbData: DatabaseCacheHitData[] = (result.databases ?? [])
                    .filter(db => db.cache_hit_ratio?.time_series?.length > 0)
                    .map(db => ({
                        database_name: db.database_name,
                        cache_hit_ratio: {
                            current: db.cache_hit_ratio.current,
                            time_series: db.cache_hit_ratio.time_series.map(p => ({
                                time: p.time,
                                value: p.value,
                            })),
                        },
                    }));
                setDatabases(dbData);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching database cache hit data:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch database cache hit data'
                );
                setDatabases([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId]);

    // Reset initial load state when connection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId]);

    // Fetch when dependencies change, or clear data when connectionId becomes null
    useEffect(() => {
        isMountedRef.current = true;

        if (connectionId === null) {
            // Clear data when switching away from single-server view
            setDatabases([]);
            setError(null);
            return () => {
                isMountedRef.current = false;
            };
        }

        if (user) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, connectionId, fetchData, lastRefresh]);

    return { databases, loading, error };
};

export default useDatabaseCacheHit;
