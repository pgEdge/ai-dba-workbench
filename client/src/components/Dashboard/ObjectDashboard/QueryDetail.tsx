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
import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
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
import { useAuth } from '../../../contexts/useAuth';
import { useAICapabilities } from '../../../contexts/useAICapabilities';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/useDashboard';
import { useMetrics } from '../../../hooks/useMetrics';
import { useQueryOverview } from '../../../hooks/useQueryOverview';
import { logger } from '../../../utils/logger';
import {
    SERVER_INFO_LABEL_BASE_SX,
    SERVER_INFO_VALUE_BASE_SX,
} from '../../../theme/tokens';
import type { MetricQueryParams } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX, spinKeyframes } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import ChartPanel from '../ChartPanel';
import TimeRangeSelector from '../TimeRangeSelector';
import { Chart } from '../../Chart';
import { QueryAnalysisDialog } from '../../QueryAnalysisDialog';
import QueryPlanPanel from './QueryPlanPanel';
import {
    type ObjectDetailProps,
    type QueryDetailData,
    buildChartData,
    formatNumber,
    formatTime,
    formatTimestamp,
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
 * Address the server reports for a backend connected over a
 * Unix-domain socket, which has no network address of its own.
 */
const LOCAL_SOCKET_ADDR = 'local';

/**
 * Describe the client last seen running a statement. The observation
 * timestamp is null when no pg_stat_activity snapshot has ever caught
 * the query in flight, which is the only reliable signal that no
 * client was observed: the address itself is reported as 'local' for
 * a Unix-domain-socket backend. A hostname is shown alongside the
 * address when resolved.
 */
function formatObservedClient(
    addr: string | null,
    hostname: string | null,
    observedAt: string | null,
): string {
    if (observedAt === null) {
        return 'Not observed';
    }
    if (!addr) {
        return 'Unknown';
    }
    return hostname ? `${hostname} (${addr})` : addr;
}

/**
 * Tooltip text warning that the client association is best-effort.
 */
function observedClientTooltip(
    addr: string | null,
    observedAt: string | null,
): string {
    const base = 'pg_stat_activity is sampled periodically, so this is '
        + 'the client seen running the query in the most recent '
        + 'snapshot that caught it in flight. Other clients may also '
        + 'have run it.';
    if (observedAt === null) {
        return 'No pg_stat_activity snapshot has caught this query in '
            + 'flight yet. ' + base;
    }
    const when = formatTimestamp(observedAt);
    const local = addr === LOCAL_SOCKET_ADDR
        ? ' The client connected over a Unix-domain socket on the '
            + 'database host, so it has no network address.'
        : '';
    return `${base}${local} Last seen ${when}.`;
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
    const selectedRange = timeRange.range;
    const customStart = timeRange.customStart;
    const customEnd = timeRange.customEnd;
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
    // Each fetch takes a sequence number so that a response from an
    // earlier request is dropped once a later one has started. The
    // effect cleanup alone cannot do this: it clears isMountedRef, but
    // the next run sets it straight back, so a slow 30d response could
    // still land after a quick 1h one and overwrite it.
    const requestIdRef = useRef<number>(0);
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

    // Qualifier for the tiles that follow the time range selector, so
    // that they read as windowed beside the lifetime min and max.
    const windowLabel = selectedRange === 'custom'
        ? 'Custom Range'
        : `Last ${selectedRange}`;

    // objectName may be queryid or query text
    const fetchQueryData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        /*
         * A custom range without both bounds is a transient state the
         * server rejects with a 400, so skip the request entirely and
         * leave whatever data and error state is already in place.
         */
        if (selectedRange === 'custom' && (!customStart || !customEnd)) {
            return;
        }

        const params = new URLSearchParams({
            connection_id: connectionId.toString(),
            queryid: objectName,
            limit: '1',
            time_range: selectedRange,
        });
        if (selectedRange === 'custom' && customStart && customEnd) {
            params.set('time_start', customStart);
            params.set('time_end', customEnd);
        }

        const url = `/api/v1/metrics/top-queries?${params.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        const requestId = ++requestIdRef.current;
        // A response is only applied if the component is still mounted
        // and no newer request has been started since.
        const isCurrent = (): boolean =>
            isMountedRef.current && requestIdRef.current === requestId;

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

            const result = await response.json() as QueryDetailData[];

            if (isCurrent()) {
                setQueryData(
                    result.length > 0 ? result[0] : null
                );
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching query detail:', err);
            if (isCurrent()) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch query data'
                );
                setQueryData(null);
            }
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
    }, [
        user, connectionId, objectName,
        selectedRange, customStart, customEnd,
    ]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, databaseName, objectName]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            void fetchQueryData();
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
                queryId: queryData.queryid,
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
                queryId: queryData.queryid,
                timeRange: timeRange.range,
                buckets: CHART_BUCKETS,
                aggregation: 'avg',
                metrics: ['calls_per_sec'],
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
            execTimeChart.window,
        ),
        [execTimeChart.data, execTimeChart.window]
    );

    const callsChartData = useMemo(
        () => buildChartData(
            callsChart.data,
            ['calls_per_sec'],
            ['Calls/s'],
            callsChart.window,
        ),
        [callsChart.data, callsChart.window]
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
        : `${cleanedQuery.substring(0, COLLAPSED_QUERY_LENGTH)}...`;

    // The collector reports an empty username when the role OID
    // could not be resolved; say so rather than showing a blank.
    const databaseUser = queryData?.username
        ? queryData.username
        : 'Unknown';

    const observedClient = formatObservedClient(
        queryData?.client_addr ?? null,
        queryData?.client_hostname ?? null,
        queryData?.client_observed_at ?? null,
    );
    const observedClientTitle = observedClientTooltip(
        queryData?.client_addr ?? null,
        queryData?.client_observed_at ?? null,
    );

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
                <Box sx={{
                    display: 'flex',
                    alignItems: 'baseline',
                    flexWrap: 'wrap',
                    gap: 1,
                    mb: 0.5,
                }}>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            fontSize: '0.875rem',
                            color: 'text.secondary',
                            textTransform: 'uppercase',
                            letterSpacing: '0.05em',
                        }}
                    >
                        Query Text
                    </Typography>
                    <Box sx={{ flexGrow: 1 }} />
                    <Typography
                        component="span"
                        sx={{
                            ...SERVER_INFO_LABEL_BASE_SX,
                            color: 'text.secondary',
                        }}
                    >
                        Database User
                    </Typography>
                    <Typography
                        component="span"
                        sx={SERVER_INFO_VALUE_BASE_SX}
                        data-testid="query-username"
                    >
                        {databaseUser}
                    </Typography>
                    <Typography
                        component="span"
                        sx={{
                            ...SERVER_INFO_LABEL_BASE_SX,
                            color: 'text.secondary',
                        }}
                    >
                        Last Observed Client
                    </Typography>
                    <Tooltip title={observedClientTitle}>
                        <Typography
                            component="span"
                            tabIndex={0}
                            sx={SERVER_INFO_VALUE_BASE_SX}
                            data-testid="query-client"
                        >
                            {observedClient}
                        </Typography>
                    </Tooltip>
                </Box>
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
                                    { setAnalysisDialogOpen(true); }
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
                                { setInsightsCollapsed(
                                    prev => !prev
                                ); }
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
                                                            ? `${spinKeyframes} 1s linear infinite`
                                                            : 'none',
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
                        { setAnalysisDialogOpen(false); }
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
                {/*
                  * Calls, total time and mean time are aggregated
                  * over the selected range by the top-queries
                  * endpoint, which only returns a statement that was
                  * executed in the window, so a window with no data
                  * leaves every tile showing a dash rather than a
                  * misleading zero. Min and max cannot be
                  * delta-aggregated, so pg_stat_statements reports
                  * them for the life of the statement; the labels
                  * say which window each tile covers.
                  */}
                <Box sx={KPI_GRID_SX}>
                    <KpiTile
                        label={`Total Calls (${windowLabel})`}
                        value={queryData
                            ? formatNumber(queryData.calls)
                            : '--'}
                    />
                    <KpiTile
                        label={`Total Time (${windowLabel})`}
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
                        label="Min Time (All Time)"
                        value={queryData
                            ? formatTime(
                                queryData.min_exec_time
                            )
                            : '--'}
                    />
                    <KpiTile
                        label="Max Time (All Time)"
                        value={queryData
                            ? formatTime(
                                queryData.max_exec_time
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
                        <ChartPanel
                            title="Execution Time Over Time"
                            loading={execTimeChart.loading
                                && !execTimeChartData}
                            hasData={!!execTimeChartData}
                            emptyMessage="No execution time data available"
                            errorMessage={execTimeChart.error}
                            height={CHART_HEIGHT}
                        >
                            {execTimeChartData && (
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
                            )}
                        </ChartPanel>
                    </Box>

                    <Box>
                        <ChartPanel
                            title="Calls Over Time"
                            loading={callsChart.loading && !callsChartData}
                            hasData={!!callsChartData}
                            emptyMessage="No call frequency data available"
                            errorMessage={callsChart.error}
                            height={CHART_HEIGHT}
                        >
                            {callsChartData && (
                                <Chart
                                    type="line"
                                    data={callsChartData}
                                    title="Calls Over Time"
                                    height={CHART_HEIGHT}
                                    smooth
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: 'Query calls per second over time',
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
                                />
                            )}
                        </ChartPanel>
                    </Box>
                </Box>
            </CollapsibleSection>
        </Box>
    );
};

export default QueryDetail;
