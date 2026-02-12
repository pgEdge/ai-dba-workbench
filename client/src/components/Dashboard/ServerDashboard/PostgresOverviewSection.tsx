/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useMetrics } from '../../../hooks/useMetrics';
import { MetricQueryParams, MetricSeries } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import { ServerSectionProps, extractSparklineData, extractLatestValue } from './types';

/** Number of data buckets for KPI sparklines */
const KPI_BUCKETS = 30;

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/**
 * Determine status for cache hit ratio values.
 */
const getCacheHitStatus = (
    value: number | null
): 'good' | 'warning' | 'critical' | undefined => {
    if (value === null) { return undefined; }
    if (value >= 95) { return 'good'; }
    if (value >= 80) { return 'warning'; }
    return 'critical';
};

/**
 * Format a numeric value for display.
 */
const formatValue = (value: number | null, decimals = 1): string => {
    if (value === null) { return '--'; }
    return value.toFixed(decimals);
};

/**
 * Format byte values into a human-readable string.
 */
const formatBytes = (bytes: number | null): string => {
    if (bytes === null) { return '--'; }
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = bytes;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024;
        unitIndex++;
    }
    return `${size.toFixed(1)} ${units[unitIndex]}`;
};

/**
 * Build chart data from metric series for the Chart component.
 */
const buildChartData = (
    series: MetricSeries[] | null,
    metricNames: string[],
    displayNames?: string[],
) => {
    if (!series) { return null; }

    const matchedSeries = metricNames.map((metric, idx) => {
        const found = series.find(s => s.metric === metric);
        return {
            name: displayNames?.[idx] ?? metric,
            data: found?.data.map(d => d.value) ?? [],
            categories: found?.data.map(d => d.time) ?? [],
        };
    });

    if (matchedSeries.every(s => s.data.length === 0)) { return null; }

    const categories = matchedSeries.find(
        s => s.categories.length > 0
    )?.categories ?? [];

    return {
        categories,
        series: matchedSeries.map(s => ({
            name: s.name,
            data: s.data,
        })),
    };
};

/**
 * PostgreSQL Overview section displays database-specific metrics
 * including connections, transaction rates, cache hit ratios, and
 * tuple operation statistics.
 */
const PostgresOverviewSection: React.FC<ServerSectionProps> = ({
    connectionId,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets)
    const connectionsKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_activity',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['active_count'],
    }), [connectionId, timeRange.range]);

    const txnKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec', 'xact_rollback_per_sec'],
    }), [connectionId, timeRange.range]);

    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['cache_hit_ratio'],
    }), [connectionId, timeRange.range]);

    const tempKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['temp_bytes'],
    }), [connectionId, timeRange.range]);

    // Chart queries (150 buckets)
    const connectionChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_activity',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'active_count',
            'idle_count',
            'idle_in_transaction_count',
            'waiting_count',
        ],
    }), [connectionId, timeRange.range]);

    const txnChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec', 'xact_rollback_per_sec'],
    }), [connectionId, timeRange.range]);

    const blockIoChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['blks_hit_per_sec', 'blks_read_per_sec'],
    }), [connectionId, timeRange.range]);

    const tupleChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'tup_inserted_per_sec',
            'tup_updated_per_sec',
            'tup_deleted_per_sec',
            'tup_fetched_per_sec',
        ],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const connectionsKpi = useMetrics(connectionsKpiParams);
    const txnKpi = useMetrics(txnKpiParams);
    const cacheKpi = useMetrics(cacheKpiParams);
    const tempKpi = useMetrics(tempKpiParams);

    // Fetch chart data
    const connectionChart = useMetrics(connectionChartParams);
    const txnChart = useMetrics(txnChartParams);
    const blockIoChart = useMetrics(blockIoChartParams);
    const tupleChart = useMetrics(tupleChartParams);

    // Extract current values
    const activeConnections = extractLatestValue(
        connectionsKpi.data, 'active_count'
    );
    const commitRate = extractLatestValue(
        txnKpi.data, 'xact_commit_per_sec'
    );
    const rollbackRate = extractLatestValue(
        txnKpi.data, 'xact_rollback_per_sec'
    );
    const txnRate = useMemo(() => {
        if (commitRate === null && rollbackRate === null) { return null; }
        return (commitRate ?? 0) + (rollbackRate ?? 0);
    }, [commitRate, rollbackRate]);

    const cacheHitRatio = extractLatestValue(
        cacheKpi.data, 'cache_hit_ratio'
    );
    const tempBytes = extractLatestValue(
        tempKpi.data, 'temp_bytes'
    );

    // Build chart datasets
    const connectionChartData = useMemo(
        () => buildChartData(
            connectionChart.data,
            [
                'active_count',
                'idle_count',
                'idle_in_transaction_count',
                'waiting_count',
            ],
            ['Active', 'Idle', 'Idle in Transaction', 'Waiting'],
        ),
        [connectionChart.data]
    );

    const txnChartData = useMemo(
        () => buildChartData(
            txnChart.data,
            ['xact_commit_per_sec', 'xact_rollback_per_sec'],
            ['Commits/s', 'Rollbacks/s'],
        ),
        [txnChart.data]
    );

    const blockIoChartData = useMemo(
        () => buildChartData(
            blockIoChart.data,
            ['blks_hit_per_sec', 'blks_read_per_sec'],
            ['Blocks Hit/s', 'Blocks Read/s'],
        ),
        [blockIoChart.data]
    );

    const tupleChartData = useMemo(
        () => buildChartData(
            tupleChart.data,
            [
                'tup_inserted_per_sec',
                'tup_updated_per_sec',
                'tup_deleted_per_sec',
                'tup_fetched_per_sec',
            ],
            ['Inserted/s', 'Updated/s', 'Deleted/s', 'Fetched/s'],
        ),
        [tupleChart.data]
    );

    const isKpiLoading = connectionsKpi.loading || txnKpi.loading
        || cacheKpi.loading || tempKpi.loading;

    return (
        <CollapsibleSection title="PostgreSQL Overview" defaultExpanded>
            {isKpiLoading && !connectionsKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="Active Connections"
                    value={activeConnections !== null
                        ? Math.round(activeConnections).toString()
                        : '--'}
                    sparklineData={extractSparklineData(
                        connectionsKpi.data, 'active_count'
                    )}
                />
                <KpiTile
                    label="Transactions/sec"
                    value={formatValue(txnRate)}
                    unit="txn/s"
                    sparklineData={extractSparklineData(
                        txnKpi.data, 'xact_commit_per_sec'
                    )}
                />
                <KpiTile
                    label="Cache Hit Ratio"
                    value={formatValue(cacheHitRatio)}
                    unit="%"
                    status={getCacheHitStatus(cacheHitRatio)}
                    sparklineData={extractSparklineData(
                        cacheKpi.data, 'cache_hit_ratio'
                    )}
                />
                <KpiTile
                    label="Temp Files Size"
                    value={formatBytes(tempBytes)}
                    sparklineData={extractSparklineData(
                        tempKpi.data, 'temp_bytes'
                    )}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    {connectionChart.loading && !connectionChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : connectionChartData ? (
                        <Chart
                            type="bar"
                            data={connectionChartData}
                            title="Connection States Over Time"
                            height={CHART_HEIGHT}
                            stacked
                            showLegend
                            showTooltip
                            showToolbar={false}
                        />
                    ) : (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'center', py: 4 }}
                        >
                            No connection state data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {txnChart.loading && !txnChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : txnChartData ? (
                        <Chart
                            type="line"
                            data={txnChartData}
                            title="Transaction Rate"
                            height={CHART_HEIGHT}
                            smooth
                            showLegend
                            showTooltip
                            showToolbar={false}
                        />
                    ) : (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'center', py: 4 }}
                        >
                            No transaction data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {blockIoChart.loading && !blockIoChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : blockIoChartData ? (
                        <Chart
                            type="line"
                            data={blockIoChartData}
                            title="Block I/O"
                            height={CHART_HEIGHT}
                            smooth
                            showLegend
                            showTooltip
                            showToolbar={false}
                        />
                    ) : (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'center', py: 4 }}
                        >
                            No block I/O data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {tupleChart.loading && !tupleChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : tupleChartData ? (
                        <Chart
                            type="line"
                            data={tupleChartData}
                            title="Tuple Operations"
                            height={CHART_HEIGHT}
                            smooth
                            showLegend
                            showTooltip
                            showToolbar={false}
                        />
                    ) : (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'center', py: 4 }}
                        >
                            No tuple operation data available
                        </Typography>
                    )}
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default PostgresOverviewSection;
