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
import { useAuth } from '../../../contexts/AuthContext';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useAICapabilities } from '../../../contexts/AICapabilitiesContext';
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
    IndexLeaderboardRow,
    IndexSortCriteria,
    LeaderboardResponse,
    formatNumber,
} from './types';

/** Sort criteria options for indexes */
const SORT_OPTIONS: {
    value: IndexSortCriteria;
    label: string;
    orderBy: string;
    order: string;
}[] = [
    {
        value: 'size',
        label: 'Reads',
        orderBy: 'idx_tup_read',
        order: 'desc',
    },
    {
        value: 'scans',
        label: 'Scans',
        orderBy: 'idx_scan',
        order: 'desc',
    },
    {
        value: 'unused',
        label: 'Unused',
        orderBy: 'idx_scan',
        order: 'asc',
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
 * Get the primary display value for an index row based on the
 * active sort criteria.
 */
const getPrimaryValue = (
    row: IndexLeaderboardRow,
    criteria: IndexSortCriteria,
): string => {
    switch (criteria) {
        case 'size':
            return `${formatNumber(row.idx_tup_read)} reads`;
        case 'scans':
        case 'unused':
            return formatNumber(row.idx_scan);
        default:
            return '--';
    }
};

/**
 * Get secondary display info for an index row.
 */
const getSecondaryInfo = (
    row: IndexLeaderboardRow,
    criteria: IndexSortCriteria,
): string => {
    switch (criteria) {
        case 'size':
            return `${formatNumber(row.idx_scan)} scans`;
        case 'scans':
            return `${formatNumber(row.idx_tup_read)} reads`;
        case 'unused':
            return `${formatNumber(row.idx_tup_read)} reads`;
        default:
            return '';
    }
};

/**
 * Get the numeric value used to calculate relative bar width.
 */
const getNumericValue = (
    row: IndexLeaderboardRow,
    criteria: IndexSortCriteria,
): number => {
    switch (criteria) {
        case 'size':
            return row.idx_tup_read ?? 0;
        case 'scans':
            return row.idx_scan ?? 0;
        case 'unused':
            return row.idx_scan ?? 0;
        default:
            return 0;
    }
};

/**
 * Index Leaderboard section displays the top indexes in the database
 * sorted by a selectable criteria. Each row is clickable to drill
 * down to the index detail view via overlay push.
 */
const IndexLeaderboardSection: React.FC<DatabaseSectionProps> = ({
    connectionId,
    databaseName,
}) => {
    const { user } = useAuth();
    const { refreshTrigger, pushOverlay } = useDashboard();
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();

    const [sortCriteria, setSortCriteria] = useState<IndexSortCriteria>(
        'size'
    );
    const [indexes, setIndexes] = useState<IndexLeaderboardRow[]>([]);
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
            probe_name: 'pg_stat_all_indexes',
            connection_id: connectionId.toString(),
            database_name: databaseName,
            limit: '10',
            order_by: activeSortOption.orderBy,
            order: activeSortOption.order,
        });
        const url = `/api/v1/metrics/latest?${params.toString()}`;

        if (shouldClearRef.current) {
            setLoading(true);
            setIndexes([]);
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
                    || `Failed to fetch index data: ${response.status}`
                );
            }

            const result = await response.json() as
                LeaderboardResponse<IndexLeaderboardRow>
                | IndexLeaderboardRow[];

            if (Array.isArray(result)) {
                setIndexes(result);
            } else {
                setIndexes(result.rows ?? []);
            }
        } catch (err) {
            logger.error('Error fetching index leaderboard:', err);
            setError(
                (err as Error).message
                || 'Failed to fetch index data'
            );
            setIndexes([]);
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

    const handleIndexClick = useCallback(
        (row: IndexLeaderboardRow): void => {
            pushOverlay({
                level: 'object',
                title: `${row.schemaname}.${row.indexrelname}`,
                entityId:
                    `${row.schemaname}.${row.indexrelname}`,
                entityName: row.indexrelname,
                objectType: 'index',
                connectionId,
                databaseName,
                schemaName: row.schemaname,
                objectName: row.indexrelname,
            });
        },
        [pushOverlay, connectionId, databaseName]
    );

    const handleSortChange = useCallback(
        (criteria: IndexSortCriteria): void => {
            setSortCriteria(criteria);
        },
        []
    );

    const maxValue = useMemo(() => {
        if (indexes.length === 0) { return 1; }
        return Math.max(
            ...indexes.map(i => getNumericValue(i, sortCriteria)),
            1
        );
    }, [indexes, sortCriteria]);

    const chartData = useMemo((): ChartData | null => {
        if (indexes.length === 0) { return null; }

        const indexNames = indexes.map(
            i => `${i.schemaname}.${i.indexrelname}`
        );

        return {
            categories: indexNames,
            series: [
                { name: 'Index Scans', data: indexes.map(i => i.idx_scan ?? 0) },
                { name: 'Tuples Read', data: indexes.map(i => i.idx_tup_read ?? 0) },
                { name: 'Tuples Fetched', data: indexes.map(i => i.idx_tup_fetch ?? 0) },
            ],
        };
    }, [indexes]);

    const analysisContext = useMemo(
        (): ChartAnalysisContext | undefined => {
            if (indexes.length === 0) { return undefined; }
            return {
                metricDescription:
                    `Index Leaderboard — ${activeSortOption.label}`,
                connectionId,
                databaseName,
            };
        },
        [indexes, activeSortOption, connectionId, databaseName]
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
        <CollapsibleSection title="Index Leaderboard" defaultExpanded>
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
                                () => handleSortChange(
                                    option.value
                                )
                            }
                            role="tab"
                            tabIndex={0}
                            aria-selected={
                                sortCriteria === option.value
                            }
                            aria-label={
                                `Sort indexes by ${option.label}`
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
                                () => setAnalysisOpen(true)
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
                    <CircularProgress size={24} aria-label="Loading indexes" />
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

            {!loading && !error && indexes.length === 0 && (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ textAlign: 'center', py: 3 }}
                >
                    No index data available
                </Typography>
            )}

            {indexes.length > 0 && (
                <Box>
                    {indexes.map((row) => {
                        const barWidth = (
                            getNumericValue(row, sortCriteria)
                            / maxValue
                        ) * 100;

                        return (
                            <Box
                                key={
                                    `${row.schemaname}`
                                    + `.${row.indexrelname}`
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
                                onClick={
                                    () => handleIndexClick(row)
                                }
                                tabIndex={0}
                                role="button"
                                aria-label={
                                    `View details for index `
                                    + `${row.schemaname}`
                                    + `.${row.indexrelname}`
                                }
                                onKeyDown={(
                                    e: React.KeyboardEvent
                                ) => {
                                    if (
                                        e.key === 'Enter'
                                        || e.key === ' '
                                    ) {
                                        e.preventDefault();
                                        handleIndexClick(row);
                                    }
                                }}
                            >
                                <Box sx={ROW_CONTENT_SX}>
                                    <Box sx={{
                                        flex: 1,
                                        minWidth: 0,
                                    }}>
                                        <Typography
                                            sx={{
                                                ...(LEADERBOARD_NAME_SX as object),
                                                overflow: 'hidden',
                                                textOverflow:
                                                    'ellipsis',
                                                whiteSpace: 'nowrap',
                                            }}
                                            title={
                                                `${row.schemaname}`
                                                + `.${row.indexrelname}`
                                            }
                                        >
                                            {row.schemaname}
                                            .{row.indexrelname}
                                        </Typography>
                                        <Typography
                                            sx={{
                                                fontSize: '0.6875rem',
                                                color:
                                                    'text.secondary',
                                            }}
                                        >
                                            on {row.relname ?? '--'}
                                        </Typography>
                                    </Box>
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

export default IndexLeaderboardSection;
