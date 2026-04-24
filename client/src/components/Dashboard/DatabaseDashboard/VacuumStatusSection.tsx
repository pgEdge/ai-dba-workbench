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
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import PsychologyIcon from '@mui/icons-material/Psychology';
import { useTheme, Theme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/useDashboard';
import { useAICapabilities } from '../../../contexts/useAICapabilities';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';
import CollapsibleSection from '../CollapsibleSection';
import { ChartAnalysisDialog } from '../../ChartAnalysisDialog';
import { ChartAnalysisContext, ChartData } from '../../Chart/types';
import { logger } from '../../../utils/logger';
import {
    DatabaseSectionProps,
    TableLeaderboardRow,
    LeaderboardResponse,
    formatNumber,
    formatTimestamp,
    getVacuumStatus,
} from './types';

/** Table container styles */
const TABLE_CONTAINER_SX = {
    overflowX: 'auto' as const,
    mb: 2,
};

/** Table header row */
const TABLE_HEADER_SX = {
    display: 'grid',
    gridTemplateColumns: '2fr 1.2fr 1.2fr 0.8fr 0.8fr',
    gap: 1,
    px: 1.5,
    py: 1,
    borderBottom: '2px solid',
    borderColor: 'divider',
    minWidth: 600,
};

/** Table header cell */
const HEADER_CELL_SX = {
    fontSize: '0.75rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
    color: 'text.secondary',
};

/** Table row */
const TABLE_ROW_SX = {
    display: 'grid',
    gridTemplateColumns: '2fr 1.2fr 1.2fr 0.8fr 0.8fr',
    gap: 1,
    px: 1.5,
    py: 1,
    borderBottom: '1px solid',
    borderColor: 'divider',
    minWidth: 600,
    '&:last-child': {
        borderBottom: 'none',
    },
};

/** Table name cell */
const NAME_CELL_SX = {
    fontSize: '0.8125rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontWeight: 500,
    whiteSpace: 'nowrap' as const,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    color: 'text.primary',
};

/** Timestamp cell */
const TIMESTAMP_CELL_SX = {
    fontSize: '0.75rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.primary',
};

/** Numeric cell */
const NUMERIC_CELL_SX = {
    fontSize: '0.8125rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontWeight: 500,
    textAlign: 'right' as const,
    color: 'text.primary',
};

/**
 * Get color for a vacuum status indicator.
 */
const getStatusColor = (
    status: 'good' | 'warning' | 'critical',
    theme: Theme,
): string => {
    switch (status) {
        case 'good':
            return theme.palette.success.main;
        case 'warning':
            return theme.palette.warning.main;
        case 'critical':
            return theme.palette.error.main;
        default:
            return theme.palette.text.primary;
    }
};

/**
 * Calculate the dead tuple ratio for a table row.
 */
const getDeadTupleRatio = (row: TableLeaderboardRow): number => {
    const live = row.n_live_tup ?? 0;
    const dead = row.n_dead_tup ?? 0;
    const total = live + dead;
    if (total === 0) { return 0; }
    return (dead / total) * 100;
};

/**
 * Get the more recent vacuum timestamp between manual and auto.
 */
const getMostRecentVacuum = (
    row: TableLeaderboardRow
): string | undefined => {
    if (!row.last_vacuum && !row.last_autovacuum) {
        return undefined;
    }
    if (!row.last_vacuum) { return row.last_autovacuum; }
    if (!row.last_autovacuum) { return row.last_vacuum; }

    const vacuumDate = new Date(row.last_vacuum);
    const autoDate = new Date(row.last_autovacuum);
    return vacuumDate > autoDate
        ? row.last_vacuum
        : row.last_autovacuum;
};

/**
 * Vacuum Status section displays the vacuum and autovacuum status
 * for all tables in the database, sorted by dead tuple ratio in
 * descending order. Color-coded timestamps indicate freshness of
 * the last vacuum operation.
 */
const VacuumStatusSection: React.FC<DatabaseSectionProps> = ({
    connectionId,
    databaseName,
}) => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();

    const [tables, setTables] = useState<TableLeaderboardRow[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [analysisOpen, setAnalysisOpen] = useState(false);
    const isMountedRef = useRef<boolean>(true);
    const shouldClearRef = useRef<boolean>(true);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            probe_name: 'pg_stat_all_tables',
            connection_id: connectionId.toString(),
            database_name: databaseName,
            limit: '20',
            order_by: 'n_dead_tup',
            order: 'desc',
        });
        const url = `/api/v1/metrics/latest?${params.toString()}`;

        if (shouldClearRef.current) {
            setLoading(true);
            setTables([]);
            shouldClearRef.current = false;
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
                    || `Failed to fetch vacuum status: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json() as
                    LeaderboardResponse<TableLeaderboardRow>
                    | TableLeaderboardRow[];

                let rows: TableLeaderboardRow[];
                if (Array.isArray(result)) {
                    rows = result;
                } else {
                    rows = result.rows ?? [];
                }

                // Sort by dead tuple ratio descending
                rows.sort((a, b) => {
                    return getDeadTupleRatio(b)
                        - getDeadTupleRatio(a);
                });

                setTables(rows);
            }
        } catch (err) {
            logger.error('Error fetching vacuum status:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch vacuum status'
                );
                setTables([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId, databaseName]);

    useEffect(() => {
        shouldClearRef.current = true;
    }, [connectionId, databaseName]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            void fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchData, refreshTrigger]);

    const headerRowSx = useMemo(() => ({
        ...TABLE_HEADER_SX,
        borderColor: theme.palette.divider,
    }), [theme.palette.divider]);

    const chartData = useMemo((): ChartData | null => {
        if (tables.length === 0) { return null; }

        const tableNames = tables.map(
            t => `${t.schemaname}.${t.relname}`
        );

        return {
            categories: tableNames,
            series: [
                { name: 'Dead Tuples', data: tables.map(t => t.n_dead_tup ?? 0) },
                { name: 'Live Tuples', data: tables.map(t => t.n_live_tup ?? 0) },
                { name: 'Sequential Scans', data: tables.map(t => t.seq_scan ?? 0) },
                { name: 'Index Scans', data: tables.map(t => t.idx_scan ?? 0) },
                { name: 'Inserts', data: tables.map(t => t.n_tup_ins ?? 0) },
                { name: 'Updates', data: tables.map(t => t.n_tup_upd ?? 0) },
                { name: 'Deletes', data: tables.map(t => t.n_tup_del ?? 0) },
            ],
        };
    }, [tables]);

    const analysisContext = useMemo(
        (): ChartAnalysisContext | undefined => {
            if (tables.length === 0) { return undefined; }
            return {
                metricDescription:
                    `Vacuum Status`,
                connectionId,
                databaseName,
            };
        },
        [tables, connectionId, databaseName]
    );

    const isCached = analysisContext
        ? hasCachedAnalysis(
            analysisContext.metricDescription,
            analysisContext.connectionId,
            analysisContext.databaseName,
            analysisContext.timeRange,
        )
        : false;

    return (
        <CollapsibleSection
            title="Vacuum Status"
            defaultExpanded={false}
        >
            {loading && tables.length === 0 && (
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'center',
                    py: 3,
                }}>
                    <CircularProgress size={24} aria-label="Loading vacuum status" />
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

            {!loading && !error && tables.length === 0 && (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ textAlign: 'center', py: 3 }}
                >
                    No vacuum status data available
                </Typography>
            )}

            {tables.length > 0 && aiEnabled
                && analysisContext && chartData && (
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'flex-end',
                    mb: 1,
                }}>
                    <Tooltip title="AI Analysis">
                        <IconButton
                            size="small"
                            color={
                                isCached ? 'warning' : 'secondary'
                            }
                            onClick={
                                () => setAnalysisOpen(true)
                            }
                        >
                            <PsychologyIcon
                                sx={{ fontSize: 16 }}
                            />
                        </IconButton>
                    </Tooltip>
                </Box>
            )}

            {tables.length > 0 && (
                <Box sx={TABLE_CONTAINER_SX}>
                    <Box sx={headerRowSx}>
                        <Typography sx={HEADER_CELL_SX}>
                            Table
                        </Typography>
                        <Typography sx={HEADER_CELL_SX}>
                            Last Vacuum
                        </Typography>
                        <Typography sx={HEADER_CELL_SX}>
                            Last Autovacuum
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Dead Tuples
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Dead Ratio
                        </Typography>
                    </Box>

                    {tables.map((row) => {
                        const deadRatio = getDeadTupleRatio(row);
                        const mostRecent = getMostRecentVacuum(row);
                        const vacuumStatus = getVacuumStatus(
                            mostRecent
                        );
                        const statusColor = getStatusColor(
                            vacuumStatus, theme
                        );

                        return (
                            <Box
                                key={
                                    `${row.schemaname ?? 'public'}`
                                    + `.${row.relname ?? 'unknown'}`
                                }
                                sx={TABLE_ROW_SX}
                            >
                                <Typography
                                    sx={NAME_CELL_SX}
                                    title={
                                        `${row.schemaname ?? 'public'}`
                                        + `.${row.relname ?? 'unknown'}`
                                    }
                                >
                                    {row.schemaname ?? 'public'}.{row.relname ?? 'unknown'}
                                </Typography>
                                <Typography
                                    sx={{
                                        ...TIMESTAMP_CELL_SX,
                                        color: getStatusColor(
                                            getVacuumStatus(
                                                row.last_vacuum
                                            ),
                                            theme,
                                        ),
                                    }}
                                >
                                    {formatTimestamp(
                                        row.last_vacuum
                                    )}
                                </Typography>
                                <Typography
                                    sx={{
                                        ...TIMESTAMP_CELL_SX,
                                        color: getStatusColor(
                                            getVacuumStatus(
                                                row.last_autovacuum
                                            ),
                                            theme,
                                        ),
                                    }}
                                >
                                    {formatTimestamp(
                                        row.last_autovacuum
                                    )}
                                </Typography>
                                <Typography sx={NUMERIC_CELL_SX}>
                                    {formatNumber(row.n_dead_tup)}
                                </Typography>
                                <Typography
                                    sx={{
                                        ...NUMERIC_CELL_SX as object,
                                        color: statusColor,
                                        fontWeight: 600,
                                    }}
                                >
                                    {deadRatio.toFixed(1)}%
                                </Typography>
                            </Box>
                        );
                    })}
                </Box>
            )}
            <ChartAnalysisDialog
                open={analysisOpen}
                onClose={() => setAnalysisOpen(false)}
                isDark={theme.palette.mode === 'dark'}
                analysisContext={
                    analysisContext ?? { metricDescription: '' }
                }
                chartData={chartData ?? { series: [] }}
            />
        </CollapsibleSection>
    );
};

export default VacuumStatusSection;
