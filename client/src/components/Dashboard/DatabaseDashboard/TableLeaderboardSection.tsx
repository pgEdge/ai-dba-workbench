/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import PsychologyIcon from '@mui/icons-material/Psychology';
import { alpha, useTheme } from '@mui/material/styles';
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
    LEADERBOARD_ROW_SX,
    LEADERBOARD_NAME_SX,
    LEADERBOARD_VALUE_SX,
} from '../styles';
import {
    DatabaseSectionProps,
    TableLeaderboardRow,
    TableSortCriteria,
    LeaderboardResponse,
    formatNumber,
} from './types';

/** Sort criteria options */
const SORT_OPTIONS: {
    value: TableSortCriteria;
    label: string;
    orderBy: string;
    order: string;
}[] = [
    { value: 'size', label: 'Rows', orderBy: 'n_live_tup', order: 'desc' },
    {
        value: 'seq_scans',
        label: 'Seq Scans',
        orderBy: 'seq_scan',
        order: 'desc',
    },
    {
        value: 'dead_tuples',
        label: 'Dead Tuples',
        orderBy: 'n_dead_tup',
        order: 'desc',
    },
    {
        value: 'modifications',
        label: 'Modifications',
        orderBy: 'n_tup_ins',
        order: 'desc',
    },
];

/** Tab-like selector styles */
const TAB_CONTAINER_SX = {
    display: 'flex',
    gap: 0.5,
    flexWrap: 'wrap' as const,
};

const TAB_BUTTON_SX = {
    px: 1.5,
    py: 0.5,
    fontSize: '0.75rem',
    fontWeight: 600,
    borderRadius: 1,
    border: '1px solid',
    borderColor: 'divider',
    cursor: 'pointer',
    transition: 'all 0.15s',
    bgcolor: 'transparent',
    color: 'text.secondary',
    '&:hover': {
        borderColor: 'primary.main',
        color: 'primary.main',
    },
};

const TAB_BUTTON_ACTIVE_SX = {
    ...TAB_BUTTON_SX,
    bgcolor: 'primary.main',
    color: 'primary.contrastText',
    borderColor: 'primary.main',
    '&:hover': {
        bgcolor: 'primary.dark',
        borderColor: 'primary.dark',
    },
};

/** Bar fill for relative size indicator */
const BAR_CONTAINER_SX = {
    width: 60,
    height: 6,
    borderRadius: 3,
    overflow: 'hidden',
    flexShrink: 0,
};

/** Row layout with name, bar, and value */
const ROW_CONTENT_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    flex: 1,
    minWidth: 0,
};

/** Secondary info text */
const SECONDARY_SX = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    whiteSpace: 'nowrap' as const,
    minWidth: 80,
    textAlign: 'right' as const,
};

/** Stats group on the right side of each row */
const STATS_GROUP_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    flexShrink: 0,
};

/**
 * Get the primary display value for a table row based on the
 * active sort criteria.
 */
const getPrimaryValue = (
    row: TableLeaderboardRow,
    criteria: TableSortCriteria,
): string => {
    switch (criteria) {
        case 'size':
            return `${formatNumber(row.n_live_tup)} rows`;
        case 'seq_scans':
            return formatNumber(row.seq_scan);
        case 'dead_tuples':
            return formatNumber(row.n_dead_tup);
        case 'modifications':
            return formatNumber(
                (row.n_tup_ins ?? 0)
                + (row.n_tup_upd ?? 0)
                + (row.n_tup_del ?? 0)
            );
        default:
            return '--';
    }
};

/**
 * Get secondary display info for a table row.
 */
const getSecondaryInfo = (
    row: TableLeaderboardRow,
    criteria: TableSortCriteria,
): string => {
    switch (criteria) {
        case 'size':
            return `${formatNumber(row.n_dead_tup)} dead`;
        case 'seq_scans':
            return `${formatNumber(row.idx_scan)} idx scans`;
        case 'dead_tuples': {
            const live = row.n_live_tup ?? 0;
            const dead = row.n_dead_tup ?? 0;
            const total = live + dead;
            const ratio = total > 0
                ? ((dead / total) * 100).toFixed(1)
                : '0.0';
            return `${ratio}% dead`;
        }
        case 'modifications':
            return `${formatNumber(row.n_live_tup)} live`;
        default:
            return '';
    }
};

/**
 * Get the numeric value used to calculate relative bar width.
 */
const getNumericValue = (
    row: TableLeaderboardRow,
    criteria: TableSortCriteria,
): number => {
    switch (criteria) {
        case 'size':
            return row.n_live_tup ?? 0;
        case 'seq_scans':
            return row.seq_scan ?? 0;
        case 'dead_tuples':
            return row.n_dead_tup ?? 0;
        case 'modifications':
            return (row.n_tup_ins ?? 0)
                + (row.n_tup_upd ?? 0)
                + (row.n_tup_del ?? 0);
        default:
            return 0;
    }
};

/**
 * Table Leaderboard section displays the top tables in the database
 * sorted by a selectable criteria. Each row is clickable to drill
 * down to the table detail view via overlay push.
 */
const TableLeaderboardSection: React.FC<DatabaseSectionProps> = ({
    connectionId,
    databaseName,
}) => {
    const { user } = useAuth();
    const { refreshTrigger, pushOverlay } = useDashboard();
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();

    const [sortCriteria, setSortCriteria] = useState<TableSortCriteria>(
        'size'
    );
    const [tables, setTables] = useState<TableLeaderboardRow[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [analysisOpen, setAnalysisOpen] = useState(false);
    const shouldClearRef = useRef<boolean>(true);

    const activeSortOption = useMemo(
        () => SORT_OPTIONS.find(o => o.value === sortCriteria)
            ?? SORT_OPTIONS[0],
        [sortCriteria]
    );

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            probe_name: 'pg_stat_all_tables',
            connection_id: connectionId.toString(),
            database_name: databaseName,
            limit: '10',
            order_by: activeSortOption.orderBy,
            order: activeSortOption.order,
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
                    || `Failed to fetch table data: ${response.status}`
                );
            }

            const result = await response.json() as
                LeaderboardResponse<TableLeaderboardRow>
                | TableLeaderboardRow[];

            if (Array.isArray(result)) {
                setTables(result);
            } else {
                setTables(result.rows ?? []);
            }
        } catch (err) {
            logger.error('Error fetching table leaderboard:', err);
            setError(
                (err as Error).message
                || 'Failed to fetch table data'
            );
            setTables([]);
        } finally {
            setLoading(false);
        }
    }, [user, connectionId, databaseName, activeSortOption]);

    useEffect(() => {
        shouldClearRef.current = true;
    }, [connectionId, databaseName, sortCriteria]);

    useEffect(() => {
        if (user) {
            fetchData();
        }
    }, [user, fetchData, refreshTrigger]);

    const handleTableClick = useCallback(
        (row: TableLeaderboardRow): void => {
            pushOverlay({
                level: 'object',
                title: `${row.schemaname}.${row.relname}`,
                entityId: `${row.schemaname}.${row.relname}`,
                entityName: row.relname,
                objectType: 'table',
                connectionId,
                databaseName,
                schemaName: row.schemaname,
                objectName: row.relname,
            });
        },
        [pushOverlay, connectionId, databaseName]
    );

    const handleSortChange = useCallback(
        (criteria: TableSortCriteria): void => {
            setSortCriteria(criteria);
        },
        []
    );

    const maxValue = useMemo(() => {
        if (tables.length === 0) { return 1; }
        return Math.max(
            ...tables.map(t => getNumericValue(t, sortCriteria)),
            1
        );
    }, [tables, sortCriteria]);

    const chartData = useMemo((): ChartData | null => {
        if (tables.length === 0) { return null; }

        const tableNames = tables.map(
            t => `${t.schemaname}.${t.relname}`
        );

        return {
            categories: tableNames,
            series: [
                { name: 'Live Rows', data: tables.map(t => t.n_live_tup ?? 0) },
                { name: 'Dead Tuples', data: tables.map(t => t.n_dead_tup ?? 0) },
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
                    `Table Leaderboard — ${activeSortOption.label}`,
                connectionId,
                databaseName,
            };
        },
        [tables, activeSortOption, connectionId, databaseName]
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
        <CollapsibleSection title="Table Leaderboard" defaultExpanded>
            <Box sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                mb: 2,
            }}>
                <Box sx={TAB_CONTAINER_SX}>
                    {SORT_OPTIONS.map(option => (
                        <Box
                            key={option.value}
                            component="button"
                            sx={sortCriteria === option.value
                                ? TAB_BUTTON_ACTIVE_SX
                                : TAB_BUTTON_SX}
                            onClick={
                                () => handleSortChange(option.value)
                            }
                            role="tab"
                            tabIndex={0}
                            aria-selected={
                                sortCriteria === option.value
                            }
                            aria-label={
                                `Sort tables by ${option.label}`
                            }
                            onKeyDown={(
                                e: React.KeyboardEvent
                            ) => {
                                if (
                                    e.key === 'Enter'
                                    || e.key === ' '
                                ) {
                                    e.preventDefault();
                                    handleSortChange(
                                        option.value
                                    );
                                }
                            }}
                        >
                            {option.label}
                        </Box>
                    ))}
                </Box>
                {aiEnabled && analysisContext && chartData && (
                    <Tooltip title="AI Analysis">
                        <IconButton
                            size="small"
                            color={
                                isCached ? 'warning' : 'secondary'
                            }
                            onClick={
                                () => { setAnalysisOpen(true); }
                            }
                        >
                            <PsychologyIcon
                                sx={{ fontSize: 16 }}
                            />
                        </IconButton>
                    </Tooltip>
                )}
            </Box>

            {loading && (
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'center',
                    py: 3,
                }}>
                    <CircularProgress size={24} aria-label="Loading tables" />
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
                    No table data available
                </Typography>
            )}

            {tables.length > 0 && (
                <Box>
                    {tables.map((row) => {
                        const barWidth = (
                            getNumericValue(row, sortCriteria)
                            / maxValue
                        ) * 100;

                        return (
                            <Box
                                key={
                                    `${row.schemaname ?? 'public'}.${row.relname ?? 'unknown'}`
                                }
                                sx={{
                                    ...LEADERBOARD_ROW_SX as object,
                                    cursor: 'pointer',
                                    transition:
                                        'background-color 0.15s',
                                    '&:hover': {
                                        bgcolor: 'action.hover',
                                    },
                                }}
                                onClick={() => handleTableClick(row)}
                                tabIndex={0}
                                role="button"
                                aria-label={
                                    `View details for table `
                                    + `${row.schemaname}.${row.relname}`
                                }
                                onKeyDown={(
                                    e: React.KeyboardEvent
                                ) => {
                                    if (
                                        e.key === 'Enter'
                                        || e.key === ' '
                                    ) {
                                        e.preventDefault();
                                        handleTableClick(row);
                                    }
                                }}
                            >
                                <Box sx={ROW_CONTENT_SX}>
                                    <Typography
                                        sx={{
                                            ...(LEADERBOARD_NAME_SX as object),
                                            flex: 1,
                                            minWidth: 0,
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap',
                                        }}
                                        title={
                                            `${row.schemaname}`
                                            + `.${row.relname}`
                                        }
                                    >
                                        {row.schemaname}.{row.relname}
                                    </Typography>
                                    <Box sx={STATS_GROUP_SX}>
                                        <Typography
                                            sx={SECONDARY_SX}
                                        >
                                            {getSecondaryInfo(
                                                row, sortCriteria
                                            )}
                                        </Typography>
                                        <Box
                                            sx={{
                                                ...BAR_CONTAINER_SX,
                                                bgcolor: alpha(
                                                    theme.palette
                                                        .primary.main,
                                                    0.15,
                                                ),
                                            }}
                                        >
                                            <Box
                                                sx={{
                                                    width:
                                                        `${barWidth}%`,
                                                    height: '100%',
                                                    bgcolor:
                                                        'primary.main',
                                                    borderRadius: 3,
                                                    transition:
                                                        'width 0.3s',
                                                }}
                                            />
                                        </Box>
                                        <Typography
                                            sx={{
                                                ...(LEADERBOARD_VALUE_SX as object),
                                                minWidth: 100,
                                                textAlign: 'right',
                                            }}
                                        >
                                            {getPrimaryValue(
                                                row, sortCriteria
                                            )}
                                        </Typography>
                                    </Box>
                                </Box>
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

export default TableLeaderboardSection;
