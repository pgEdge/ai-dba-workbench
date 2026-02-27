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
import IconButton from '@mui/material/IconButton';
import Skeleton from '@mui/material/Skeleton';
import { alpha, Paper, Collapse, Tooltip, useTheme } from '@mui/material';
import {
    AutoAwesome as SparkleIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Psychology as PsychologyIcon,
    Refresh as RefreshIcon,
} from '@mui/icons-material';
import { useAuth } from '../../../contexts/AuthContext';
import { useAICapabilities } from '../../../contexts/AICapabilitiesContext';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useMetrics } from '../../../hooks/useMetrics';
import { useQueryOverview } from '../../../hooks/useQueryOverview';
import { MetricQueryParams } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import TimeRangeSelector from '../TimeRangeSelector';
import { Chart } from '../../Chart';
import { QueryAnalysisDialog } from '../../QueryAnalysisDialog';
import QueryPlanPanel from './QueryPlanPanel';
import {
    ObjectDetailProps,
    QueryDetailData,
    buildChartData,
    formatNumber,
    formatTime,
    formatValue,
} from './types';

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/** Maximum characters to display in collapsed query text */
const COLLAPSED_QUERY_LENGTH = 120;

/** Query text container */
const QUERY_TEXT_SX = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.8125rem',
    lineHeight: 1.5,
    p: 1.5,
    borderRadius: 1,
    bgcolor: 'action.hover',
    whiteSpace: 'pre-wrap' as const,
    wordBreak: 'break-word' as const,
    maxHeight: 300,
    overflow: 'auto',
};

/** Toggle button for expanding/collapsing query text */
const TOGGLE_SX = {
    fontSize: '0.75rem',
    fontWeight: 600,
    color: 'primary.main',
    cursor: 'pointer',
    mt: 0.5,
    '&:hover': {
        textDecoration: 'underline',
    },
};

/**
 * Format a Date as a human-readable relative time string.
 */
function formatRelativeTime(date: Date): string {
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSecs = Math.floor(diffMs / 1000);
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);

    if (diffSecs < 60) {
        return 'Updated just now';
    }
    if (diffMins < 60) {
        return `Updated ${diffMins} min ago`;
    }
    if (diffHours < 24) {
        return `Updated ${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
    }
    return `Updated ${date.toLocaleDateString()}`;
}

/**
 * Clean and format a query string for display.
 */
const cleanQuery = (query: string): string => {
    return query.replace(/\s+/g, ' ').trim();
};

/**
 * QueryDetail displays comprehensive information for a single
 * query from pg_stat_statements including KPI tiles, execution
 * time charts, and call frequency charts.
 */
const QueryDetail: React.FC<ObjectDetailProps> = ({
    connectionId,
    databaseName,
    objectName,
}) => {
    const { user } = useAuth();
    const { timeRange, refreshTrigger, currentOverlay } = useDashboard();
    const { aiEnabled } = useAICapabilities();
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';

    const [queryData, setQueryData] = useState<QueryDetailData | null>(
        null
    );
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [expanded, setExpanded] = useState<boolean>(false);
    const [analysisDialogOpen, setAnalysisDialogOpen] =
        useState<boolean>(false);
    const [insightsCollapsed, setInsightsCollapsed] =
        useState<boolean>(false);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    // AI query overview (brief plain-text summary)
    const {
        summary: overviewSummary,
        loading: overviewLoading,
        error: overviewError,
        generatedAt: overviewGeneratedAt,
        refresh: refreshOverview,
    } = useQueryOverview(
        aiEnabled && queryData ? {
            queryText: queryData.query,
            queryId: queryData.queryid,
            calls: queryData.calls,
            totalExecTime: queryData.total_exec_time,
            meanExecTime: queryData.mean_exec_time,
            rows: queryData.rows,
            sharedBlksHit: queryData.shared_blks_hit,
            sharedBlksRead: queryData.shared_blks_read,
            connectionId,
            databaseName,
        } : null
    );

    const connectionName = currentOverlay?.connectionName;

    // objectName may be queryid or query text
    const fetchQueryData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            connection_id: connectionId.toString(),
            queryid: objectName,
            limit: '1',
        });

        const url = `/api/v1/metrics/top-queries?${params.toString()}`;

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
                    || `Failed to fetch query data: `
                    + `${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json() as QueryDetailData[];
                setQueryData(
                    result.length > 0 ? result[0] : null
                );
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching query detail:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch query data'
                );
                setQueryData(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId, databaseName, objectName]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, databaseName, objectName]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            fetchQueryData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchQueryData, refreshTrigger]);

    // Chart query - execution time over time
    const execTimeChartParams = useMemo(
        (): MetricQueryParams | null => {
            if (!queryData?.queryid) { return null; }
            return {
                probeName: 'pg_stat_statements',
                connectionId,
                databaseName,
                timeRange: timeRange.range,
                buckets: CHART_BUCKETS,
                aggregation: 'avg',
                metrics: [
                    'mean_exec_time',
                    'min_exec_time',
                    'max_exec_time',
                ],
            };
        },
        [
            connectionId, databaseName, timeRange.range,
            queryData?.queryid,
        ]
    );

    // Chart query - calls over time
    const callsChartParams = useMemo(
        (): MetricQueryParams | null => {
            if (!queryData?.queryid) { return null; }
            return {
                probeName: 'pg_stat_statements',
                connectionId,
                databaseName,
                timeRange: timeRange.range,
                buckets: CHART_BUCKETS,
                aggregation: 'sum',
                metrics: ['calls'],
            };
        },
        [
            connectionId, databaseName, timeRange.range,
            queryData?.queryid,
        ]
    );

    const execTimeChart = useMetrics(execTimeChartParams);
    const callsChart = useMetrics(callsChartParams);

    const execTimeChartData = useMemo(
        () => buildChartData(
            execTimeChart.data,
            ['mean_exec_time', 'min_exec_time', 'max_exec_time'],
            ['Mean Time (ms)', 'Min Time (ms)', 'Max Time (ms)'],
        ),
        [execTimeChart.data]
    );

    const callsChartData = useMemo(
        () => buildChartData(
            callsChart.data,
            ['calls'],
            ['Calls'],
        ),
        [callsChart.data]
    );

    const handleToggleExpand = useCallback((): void => {
        setExpanded(prev => !prev);
    }, []);

    // Determine the query text to show
    const queryText = queryData?.query ?? objectName;
    const cleanedQuery = cleanQuery(queryText);
    const isLong = cleanedQuery.length > COLLAPSED_QUERY_LENGTH;
    const displayQuery = expanded || !isLong
        ? queryText
        : cleanedQuery.substring(0, COLLAPSED_QUERY_LENGTH) + '...';

    // Compute rows per call
    const rowsPerCall = useMemo(() => {
        if (!queryData || queryData.calls === 0) { return null; }
        return queryData.rows / queryData.calls;
    }, [queryData]);

    if (loading && !queryData) {
        return (
            <Box sx={{
                display: 'flex',
                justifyContent: 'center',
                py: 4,
            }}>
                <CircularProgress size={32} aria-label="Loading query details" />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography
                variant="body2"
                color="error"
                sx={{ textAlign: 'center', py: 4 }}
            >
                {error}
            </Typography>
        );
    }

    return (
        <Box>
            <Box sx={{ mb: 2 }}>
                <Typography
                    sx={{
                        fontWeight: 600,
                        fontSize: '0.875rem',
                        color: 'text.secondary',
                        textTransform: 'uppercase',
                        letterSpacing: '0.05em',
                        mb: 0.5,
                    }}
                >
                    Query Text
                </Typography>
                <Typography sx={QUERY_TEXT_SX}>
                    {displayQuery}
                </Typography>
                {isLong && (
                    <Typography
                        sx={TOGGLE_SX}
                        onClick={handleToggleExpand}
                        role="button"
                        tabIndex={0}
                        aria-label={
                            expanded
                                ? 'Collapse query text'
                                : 'Expand query text'
                        }
                        onKeyDown={(e: React.KeyboardEvent) => {
                            if (
                                e.key === 'Enter'
                                || e.key === ' '
                            ) {
                                e.preventDefault();
                                handleToggleExpand();
                            }
                        }}
                    >
                        {expanded
                            ? 'Show less'
                            : 'Show full query'}
                    </Typography>
                )}
            </Box>

            {aiEnabled && queryData && !overviewError && (
                <Paper
                    elevation={0}
                    sx={{
                        p: 1.5,
                        mb: 2,
                        bgcolor: isDark
                            ? alpha(theme.palette.background.paper, 0.4)
                            : alpha(theme.palette.grey[50], 0.8),
                        border: '1px solid',
                        borderColor: 'divider',
                    }}
                >
                    <Box sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.5,
                        mb: insightsCollapsed ? 0 : 0.75,
                    }}>
                        <SparkleIcon sx={{
                            fontSize: 16,
                            color: 'primary.main',
                        }} />
                        <Typography sx={{
                            fontSize: '1rem',
                            fontWeight: 600,
                            color: 'text.primary',
                            lineHeight: 1,
                        }}>
                            AI Overview
                        </Typography>
                        <Tooltip title="Open full analysis">
                            <IconButton
                                size="small"
                                onClick={() =>
                                    setAnalysisDialogOpen(true)
                                }
                                aria-label="Open full analysis"
                                sx={{
                                    p: 0.25,
                                    color: 'secondary.main',
                                    '&:hover': {
                                        bgcolor: alpha(
                                            theme.palette.secondary.main,
                                            0.1,
                                        ),
                                    },
                                }}
                            >
                                <PsychologyIcon
                                    sx={{ fontSize: 18 }}
                                />
                            </IconButton>
                        </Tooltip>
                        <Box sx={{ flexGrow: 1 }} />
                        <IconButton
                            size="small"
                            onClick={() =>
                                setInsightsCollapsed(
                                    prev => !prev
                                )
                            }
                            aria-label={
                                insightsCollapsed
                                    ? 'Expand AI Overview'
                                    : 'Collapse AI Overview'
                            }
                            sx={{
                                p: 0.25,
                                color: 'text.secondary',
                            }}
                        >
                            {insightsCollapsed
                                ? <ExpandMoreIcon
                                    sx={{ fontSize: 18 }}
                                />
                                : <ExpandLessIcon
                                    sx={{ fontSize: 18 }}
                                />
                            }
                        </IconButton>
                    </Box>
                    <Collapse in={!insightsCollapsed}>
                        {overviewLoading && !overviewSummary && (
                            <>
                                <Skeleton
                                    variant="text"
                                    width="90%"
                                    height={18}
                                />
                                <Skeleton
                                    variant="text"
                                    width="75%"
                                    height={18}
                                />
                                <Skeleton
                                    variant="text"
                                    width="40%"
                                    height={14}
                                    sx={{ mt: 0.5 }}
                                />
                            </>
                        )}

                        {!overviewSummary
                            && !overviewLoading && (
                            <Typography
                                variant="body2"
                                sx={{
                                    color: 'text.secondary',
                                    fontStyle: 'italic',
                                }}
                            >
                                Generating overview...
                            </Typography>
                        )}

                        {overviewSummary && (
                            <>
                                <Typography
                                    variant="body2"
                                    sx={{
                                        color: 'text.primary',
                                        lineHeight: 1.5,
                                        whiteSpace: 'pre-wrap',
                                    }}
                                >
                                    {overviewSummary}
                                </Typography>
                                {overviewGeneratedAt && (
                                    <Box sx={{
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: 0.5,
                                        mt: 0.75,
                                    }}>
                                        <Tooltip title={
                                            overviewLoading
                                                ? 'Refreshing...'
                                                : 'Refresh now'
                                        }>
                                            <IconButton
                                                size="small"
                                                onClick={
                                                    refreshOverview
                                                }
                                                disabled={
                                                    overviewLoading
                                                }
                                                aria-label="Refresh overview"
                                                sx={{
                                                    p: 0.25,
                                                    color:
                                                        'text.secondary',
                                                    '&:hover': {
                                                        bgcolor:
                                                            'action.hover',
                                                    },
                                                }}
                                            >
                                                <RefreshIcon sx={{
                                                    fontSize: 14,
                                                    animation:
                                                        overviewLoading
                                                            ? 'spin 1s linear infinite'
                                                            : 'none',
                                                    '@keyframes spin':
                                                        {
                                                            '0%': {
                                                                transform:
                                                                    'rotate(0deg)',
                                                            },
                                                            '100%':
                                                                {
                                                                    transform:
                                                                        'rotate(360deg)',
                                                                },
                                                        },
                                                }} />
                                            </IconButton>
                                        </Tooltip>
                                        <Typography
                                            variant="caption"
                                            sx={{
                                                color:
                                                    'text.secondary',
                                            }}
                                        >
                                            {formatRelativeTime(
                                                overviewGeneratedAt
                                            )}
                                        </Typography>
                                    </Box>
                                )}
                            </>
                        )}
                    </Collapse>
                </Paper>
            )}

            {aiEnabled && queryData && (
                <QueryAnalysisDialog
                    open={analysisDialogOpen}
                    onClose={() =>
                        setAnalysisDialogOpen(false)
                    }
                    isDark={isDark}
                    queryText={queryData.query}
                    queryId={queryData.queryid}
                    stats={{
                        calls: queryData.calls,
                        totalExecTime:
                            queryData.total_exec_time,
                        meanExecTime:
                            queryData.mean_exec_time,
                        rows: queryData.rows,
                        sharedBlksHit:
                            queryData.shared_blks_hit,
                        sharedBlksRead:
                            queryData.shared_blks_read,
                    }}
                    connectionId={connectionId}
                    connectionName={connectionName}
                    databaseName={databaseName}
                />
            )}

            {queryData && (
                <QueryPlanPanel
                    connectionId={connectionId}
                    databaseName={databaseName}
                    queryText={queryData.query}
                />
            )}

            <CollapsibleSection
                title="Query Statistics"
                defaultExpanded
            >
                <Box sx={KPI_GRID_SX}>
                    <KpiTile
                        label="Total Calls"
                        value={queryData
                            ? formatNumber(queryData.calls)
                            : '--'}
                    />
                    <KpiTile
                        label="Total Time"
                        value={queryData
                            ? formatTime(
                                queryData.total_exec_time
                            )
                            : '--'}
                    />
                    <KpiTile
                        label="Mean Time"
                        value={queryData
                            ? formatTime(
                                queryData.mean_exec_time
                            )
                            : '--'}
                    />
                    <KpiTile
                        label="Avg Rows/Call"
                        value={rowsPerCall !== null
                            ? formatValue(rowsPerCall)
                            : '--'}
                    />
                </Box>
            </CollapsibleSection>

            <CollapsibleSection
                title="Performance Charts"
                defaultExpanded
                headerRight={<TimeRangeSelector />}
            >
                <Box sx={CHART_SECTION_SX}>
                    <Box>
                        {execTimeChart.loading
                            && !execTimeChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} aria-label="Loading chart" />
                                </Box>
                            ) : execTimeChartData ? (
                                <Chart
                                    type="line"
                                    data={execTimeChartData}
                                    title={
                                        'Execution Time Over Time'
                                    }
                                    height={CHART_HEIGHT}
                                    smooth
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: 'Query execution time trends',
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
                                />
                            ) : (
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                    sx={{
                                        textAlign: 'center',
                                        py: 4,
                                    }}
                                >
                                    No execution time data available
                                </Typography>
                            )}
                    </Box>

                    <Box>
                        {callsChart.loading
                            && !callsChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} aria-label="Loading chart" />
                                </Box>
                            ) : callsChartData ? (
                                <Chart
                                    type="bar"
                                    data={callsChartData}
                                    title="Calls Over Time"
                                    height={CHART_HEIGHT}
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: 'Query call frequency over time',
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
                                />
                            ) : (
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                    sx={{
                                        textAlign: 'center',
                                        py: 4,
                                    }}
                                >
                                    No call frequency data available
                                </Typography>
                            )}
                    </Box>
                </Box>
            </CollapsibleSection>
        </Box>
    );
};

export default QueryDetail;
