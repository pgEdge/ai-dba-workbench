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
import { useAuth } from '../../../contexts/AuthContext';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useMetrics } from '../../../hooks/useMetrics';
import { MetricQueryParams } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import TimeRangeSelector from '../TimeRangeSelector';
import { Chart } from '../../Chart';
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
    const { timeRange, refreshTrigger } = useDashboard();

    const [queryData, setQueryData] = useState<QueryDetailData | null>(
        null
    );
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [expanded, setExpanded] = useState<boolean>(false);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

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
            const response = await fetch(url, {
                credentials: 'include',
            });

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
                <CircularProgress size={32} />
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
                                    <CircularProgress size={24} />
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
                                    <CircularProgress size={24} />
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
