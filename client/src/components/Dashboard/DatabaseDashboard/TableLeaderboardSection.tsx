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
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { alpha, useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/AuthContext';
import { useDashboard } from '../../../contexts/DashboardContext';
import CollapsibleSection from '../CollapsibleSection';
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
    formatBytes,
    formatNumber,
} from './types';

/** Sort criteria options */
const SORT_OPTIONS: {
    value: TableSortCriteria;
    label: string;
    orderBy: string;
    order: string;
}[] = [
    { value: 'size', label: 'Size', orderBy: 'table_size', order: 'desc' },
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
    mb: 2,
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
            return row.table_size_pretty ?? formatBytes(row.table_size);
        case 'seq_scans':
            return formatNumber(row.seq_scan);
        case 'dead_tuples':
            return formatNumber(row.n_dead_tup);
        case 'modifications':
            return formatNumber(
                row.n_tup_ins + row.n_tup_upd + row.n_tup_del
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
            return `${formatNumber(row.n_live_tup)} rows`;
        case 'seq_scans':
            return `${formatNumber(row.idx_scan)} idx scans`;
        case 'dead_tuples': {
            const total = row.n_live_tup + row.n_dead_tup;
            const ratio = total > 0
                ? ((row.n_dead_tup / total) * 100).toFixed(1)
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
            return row.table_size;
        case 'seq_scans':
            return row.seq_scan;
        case 'dead_tuples':
            return row.n_dead_tup;
        case 'modifications':
            return row.n_tup_ins + row.n_tup_upd + row.n_tup_del;
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

    const [sortCriteria, setSortCriteria] = useState<TableSortCriteria>(
        'size'
    );
    const [tables, setTables] = useState<TableLeaderboardRow[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

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
        const url = `/api/v1/metrics/query?${params.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await fetch(url, {
                credentials: 'include',
            });

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch table data: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json() as
                    LeaderboardResponse<TableLeaderboardRow>
                    | TableLeaderboardRow[];

                if (Array.isArray(result)) {
                    setTables(result);
                } else {
                    setTables(result.rows ?? []);
                }
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching table leaderboard:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch table data'
                );
                setTables([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId, databaseName, activeSortOption]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, databaseName, sortCriteria]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
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

    return (
        <CollapsibleSection title="Table Leaderboard" defaultExpanded>
            <Box sx={TAB_CONTAINER_SX}>
                {SORT_OPTIONS.map(option => (
                    <Box
                        key={option.value}
                        component="button"
                        sx={sortCriteria === option.value
                            ? TAB_BUTTON_ACTIVE_SX
                            : TAB_BUTTON_SX}
                        onClick={() => handleSortChange(option.value)}
                        role="tab"
                        tabIndex={0}
                        aria-selected={sortCriteria === option.value}
                        aria-label={
                            `Sort tables by ${option.label}`
                        }
                        onKeyDown={(e: React.KeyboardEvent) => {
                            if (
                                e.key === 'Enter'
                                || e.key === ' '
                            ) {
                                e.preventDefault();
                                handleSortChange(option.value);
                            }
                        }}
                    >
                        {option.label}
                    </Box>
                ))}
            </Box>

            {loading && tables.length === 0 && (
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
                                    `${row.schemaname}.${row.relname}`
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
                                                width: `${barWidth}%`,
                                                height: '100%',
                                                bgcolor:
                                                    'primary.main',
                                                borderRadius: 3,
                                                transition:
                                                    'width 0.3s',
                                            }}
                                        />
                                    </Box>
                                    <Typography sx={SECONDARY_SX}>
                                        {getSecondaryInfo(
                                            row, sortCriteria
                                        )}
                                    </Typography>
                                </Box>
                                <Typography
                                    sx={LEADERBOARD_VALUE_SX}
                                >
                                    {getPrimaryValue(
                                        row, sortCriteria
                                    )}
                                </Typography>
                            </Box>
                        );
                    })}
                </Box>
            )}
        </CollapsibleSection>
    );
};

export default TableLeaderboardSection;
