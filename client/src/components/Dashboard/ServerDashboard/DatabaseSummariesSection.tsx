/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import ViewListIcon from '@mui/icons-material/ViewList';
import { useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/AuthContext';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/DashboardContext';
import CollapsibleSection from '../CollapsibleSection';
import Sparkline from '../Sparkline';
import { getDashboardTileSx } from '../styles';
import { MetricDataPoint } from '../types';
import { formatNumber } from '../../../utils/formatters';
import {
    ServerSectionProps,
    DatabaseSummary,
    ServerPerformanceSummary,
} from './types';

/** Grid layout for database summary cards */
const DB_GRID_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
    gap: 2,
    mb: 2,
};

/** Stat row within a database card */
const STAT_ROW_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    py: 0.25,
};

/** Stat label */
const STAT_LABEL_SX = {
    fontSize: '0.875rem',
    color: 'text.secondary',
};

/** Stat value */
const STAT_VALUE_SX = {
    fontSize: '0.875rem',
    fontWeight: 600,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

/** Database name style */
const DB_NAME_SX = {
    fontWeight: 600,
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    mb: 0.5,
};

/**
 * Determine color for cache hit ratio.
 */
const getCacheColor = (ratio: number): string => {
    if (ratio >= 95) { return '#4caf50'; }
    if (ratio >= 80) { return '#ff9800'; }
    return '#f44336';
};

/**
 * Determine color for dead tuple ratio.
 */
const getDeadTupleColor = (ratio: number): string => {
    if (ratio <= 5) { return '#4caf50'; }
    if (ratio <= 20) { return '#ff9800'; }
    return '#f44336';
};

/**
 * Database Summaries section displays a grid of cards showing
 * per-database health summaries. Each card shows the database
 * name, size, cache hit ratio with sparkline, transaction rate,
 * and dead tuple ratio.
 */
const DatabaseSummariesSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { user } = useAuth();
    const { refreshTrigger, pushOverlay } = useDashboard();
    const theme = useTheme();

    const [databases, setDatabases] = useState<DatabaseSummary[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const userRef = useRef(user);
    userRef.current = user;

    const isLoggedIn = !!user;

    const fetchData = useCallback(async (): Promise<void> => {
        if (!userRef.current) { return; }

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
                    || `Failed to fetch database summaries: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result: ServerPerformanceSummary = await response.json();
                setDatabases(result.databases ?? []);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching database summaries:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch database summaries'
                );
                setDatabases([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [connectionId]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId]);

    useEffect(() => {
        isMountedRef.current = true;

        if (isLoggedIn) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [isLoggedIn, fetchData, refreshTrigger]);

    const tileSx = useMemo(() => getDashboardTileSx(theme), [theme]);

    const handleDatabaseClick = useCallback((db: DatabaseSummary): void => {
        pushOverlay({
            level: 'database',
            title: db.database_name,
            entityId: connectionId,
            entityName: db.database_name,
            connectionId,
            connectionName,
            databaseName: db.database_name,
        });
    }, [pushOverlay, connectionId, connectionName]);

    /**
     * Convert cache hit ratio time series into MetricDataPoint array
     * for the Sparkline component.
     */
    const toSparklineData = (
        timeSeries: Array<{ time: string; value: number }> | undefined
    ): MetricDataPoint[] => {
        if (!timeSeries) { return []; }
        return timeSeries.map(p => ({
            time: p.time,
            value: p.value,
        }));
    };

    return (
        <CollapsibleSection title="Database Summaries" icon={<ViewListIcon sx={{ fontSize: 16 }} />} defaultExpanded>
            {loading && databases.length === 0 && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                    <CircularProgress size={24} aria-label="Loading databases" />
                </Box>
            )}

            {error && (
                <Typography
                    variant="body2"
                    color="error"
                    sx={{ textAlign: 'center', py: 2 }}
                >
                    {error}
                </Typography>
            )}

            {!loading && !error && databases.length === 0 && (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ textAlign: 'center', py: 3 }}
                >
                    No database summaries available
                </Typography>
            )}

            <Box sx={DB_GRID_SX}>
                {databases.map(db => (
                    <Paper
                        key={db.database_name}
                        elevation={0}
                        sx={{
                            ...tileSx as object,
                            display: 'flex',
                            flexDirection: 'column',
                        }}
                        onClick={() => handleDatabaseClick(db)}
                        tabIndex={0}
                        role="button"
                        aria-label={`View details for database ${db.database_name}`}
                        onKeyDown={(e: React.KeyboardEvent) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault();
                                handleDatabaseClick(db);
                            }
                        }}
                    >
                        <Typography sx={DB_NAME_SX}>
                            {db.database_name}
                        </Typography>
                        <Typography sx={{
                            color: 'text.secondary',
                            fontSize: '0.875rem',
                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                        }}>
                            {db.size_pretty}
                        </Typography>

                        <Box sx={{ mt: 1 }}>
                            <Box sx={STAT_ROW_SX}>
                                <Typography sx={STAT_LABEL_SX}>
                                    Cache Hit Ratio
                                </Typography>
                                <Typography
                                    sx={{
                                        ...STAT_VALUE_SX,
                                        color: getCacheColor(
                                            db.cache_hit_ratio?.current ?? 0
                                        ),
                                    }}
                                >
                                    {db.cache_hit_ratio?.current !== undefined
                                        ? `${db.cache_hit_ratio.current.toFixed(1)}%`
                                        : '--'}
                                </Typography>
                            </Box>

                            {db.cache_hit_ratio?.time_series && (
                                <Box sx={{ height: 30, mt: 0.5, mb: 5 }}>
                                    <Sparkline
                                        data={toSparklineData(
                                            db.cache_hit_ratio.time_series
                                        )}
                                        color={getCacheColor(
                                            db.cache_hit_ratio?.current ?? 0
                                        )}
                                        height={30}
                                    />
                                </Box>
                            )}

                            <Box sx={STAT_ROW_SX}>
                                <Typography sx={STAT_LABEL_SX}>
                                    Transaction Rate
                                </Typography>
                                <Typography sx={STAT_VALUE_SX}>
                                    {db.transaction_rate !== undefined
                                        ? `${formatNumber(Math.round(db.transaction_rate * 10) / 10)}/s`
                                        : '--'}
                                </Typography>
                            </Box>

                            <Box sx={STAT_ROW_SX}>
                                <Typography sx={STAT_LABEL_SX}>
                                    Dead Tuple Ratio
                                </Typography>
                                <Typography
                                    sx={{
                                        ...STAT_VALUE_SX,
                                        color: getDeadTupleColor(
                                            db.dead_tuple_ratio ?? 0
                                        ),
                                    }}
                                >
                                    {db.dead_tuple_ratio !== undefined
                                        ? `${db.dead_tuple_ratio.toFixed(1)}%`
                                        : '--'}
                                </Typography>
                            </Box>

                            <Box sx={STAT_ROW_SX}>
                                <Typography sx={STAT_LABEL_SX}>
                                    Active Connections
                                </Typography>
                                <Typography sx={STAT_VALUE_SX}>
                                    {db.active_connections !== undefined
                                        ? formatNumber(db.active_connections)
                                        : '--'}
                                </Typography>
                            </Box>
                        </Box>
                    </Paper>
                ))}
            </Box>
        </CollapsibleSection>
    );
};

export default DatabaseSummariesSection;
