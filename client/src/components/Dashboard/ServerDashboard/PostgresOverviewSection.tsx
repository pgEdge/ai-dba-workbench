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
import CircularProgress from '@mui/material/CircularProgress';
import StorageIcon from '@mui/icons-material/Storage';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useMetrics } from '../../../hooks/useMetrics';
import { MetricQueryParams, MetricSeries } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import ChartPanel from '../ChartPanel';
import { ServerSectionProps, extractSparklineData, extractLatestValue } from './types';

/** Number of data buckets for KPI sparklines */
const KPI_BUCKETS = 30;

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

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
 * including connections, transactions, block I/O, and tuple
 * operation statistics from pg_stat_database.
 */
const PostgresOverviewSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets) - all from pg_stat_database
    const connectionsKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['numbackends'],
    }), [connectionId, timeRange.range]);

    const txnKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit', 'xact_rollback'],
    }), [connectionId, timeRange.range]);

    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['blks_hit', 'blks_read'],
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
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['numbackends', 'sessions'],
    }), [connectionId, timeRange.range]);

    const txnChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit', 'xact_rollback'],
    }), [connectionId, timeRange.range]);

    const blockIoChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['blks_hit', 'blks_read'],
    }), [connectionId, timeRange.range]);

    const tupleChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'tup_fetched',
            'tup_inserted',
            'tup_updated',
            'tup_deleted',
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
    const numBackends = extractLatestValue(
        connectionsKpi.data, 'numbackends'
    );
    const xactCommit = extractLatestValue(
        txnKpi.data, 'xact_commit'
    );
    const xactRollback = extractLatestValue(
        txnKpi.data, 'xact_rollback'
    );
    const blksHit = extractLatestValue(
        cacheKpi.data, 'blks_hit'
    );
    const blksRead = extractLatestValue(
        cacheKpi.data, 'blks_read'
    );
    const cacheHitRatio = useMemo(() => {
        if (blksHit !== null && blksRead !== null) {
            const total = blksHit + blksRead;
            if (total > 0) { return (blksHit / total) * 100; }
        }
        return null;
    }, [blksHit, blksRead]);
    const tempBytes = extractLatestValue(
        tempKpi.data, 'temp_bytes'
    );

    // Build chart datasets
    const connectionChartData = useMemo(
        () => buildChartData(
            connectionChart.data,
            ['numbackends', 'sessions'],
            ['Backends', 'Sessions'],
        ),
        [connectionChart.data]
    );

    const txnChartData = useMemo(
        () => buildChartData(
            txnChart.data,
            ['xact_commit', 'xact_rollback'],
            ['Commits', 'Rollbacks'],
        ),
        [txnChart.data]
    );

    const blockIoChartData = useMemo(
        () => buildChartData(
            blockIoChart.data,
            ['blks_hit', 'blks_read'],
            ['Blocks Hit', 'Blocks Read'],
        ),
        [blockIoChart.data]
    );

    const tupleChartData = useMemo(
        () => buildChartData(
            tupleChart.data,
            [
                'tup_fetched',
                'tup_inserted',
                'tup_updated',
                'tup_deleted',
            ],
            ['Fetched', 'Inserted', 'Updated', 'Deleted'],
        ),
        [tupleChart.data]
    );

    const isKpiLoading = connectionsKpi.loading || txnKpi.loading
        || cacheKpi.loading || tempKpi.loading;

    return (
        <CollapsibleSection title="PostgreSQL Overview" icon={<StorageIcon sx={{ fontSize: 16 }} />} defaultExpanded>
            {isKpiLoading && !connectionsKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="Backends"
                    value={numBackends !== null
                        ? Math.round(numBackends).toString()
                        : '--'}
                    sparklineData={extractSparklineData(
                        connectionsKpi.data, 'numbackends'
                    )}
                    analysisContext={{
                        metricDescription: 'PostgreSQL backend connection count over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Commits"
                    value={xactCommit !== null
                        ? Math.round(xactCommit).toString()
                        : '--'}
                    sparklineData={extractSparklineData(
                        txnKpi.data, 'xact_commit'
                    )}
                    analysisContext={{
                        metricDescription: 'Transaction commit rate over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Cache Hit Ratio"
                    value={cacheHitRatio !== null
                        ? formatValue(cacheHitRatio)
                        : '--'}
                    unit={cacheHitRatio !== null ? '%' : undefined}
                    sparklineData={extractSparklineData(
                        cacheKpi.data, 'blks_hit'
                    )}
                    analysisContext={{
                        metricDescription: 'Buffer cache hit ratio over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Temp Bytes"
                    value={formatBytes(tempBytes)}
                    sparklineData={extractSparklineData(
                        tempKpi.data, 'temp_bytes'
                    )}
                    analysisContext={{
                        metricDescription: 'Temporary bytes written over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    <ChartPanel
                        title="Connections Over Time"
                        loading={connectionChart.loading && !connectionChartData}
                        hasData={!!connectionChartData}
                        emptyMessage="No connection data available"
                        height={CHART_HEIGHT}
                    >
                        {connectionChartData && (
                            <Chart
                                type="line"
                                data={connectionChartData}
                                title="Connections Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'PostgreSQL backend connections and sessions over time',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Transactions"
                        loading={txnChart.loading && !txnChartData}
                        hasData={!!txnChartData}
                        emptyMessage="No transaction data available"
                        height={CHART_HEIGHT}
                    >
                        {txnChartData && (
                            <Chart
                                type="line"
                                data={txnChartData}
                                title="Transactions"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Transaction commit and rollback rates',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Block I/O"
                        loading={blockIoChart.loading && !blockIoChartData}
                        hasData={!!blockIoChartData}
                        emptyMessage="No block I/O data available"
                        height={CHART_HEIGHT}
                    >
                        {blockIoChartData && (
                            <Chart
                                type="line"
                                data={blockIoChartData}
                                title="Block I/O"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Block I/O showing cache hits vs disk reads',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Tuple Operations"
                        loading={tupleChart.loading && !tupleChartData}
                        hasData={!!tupleChartData}
                        emptyMessage="No tuple operation data available"
                        height={CHART_HEIGHT}
                    >
                        {tupleChartData && (
                            <Chart
                                type="line"
                                data={tupleChartData}
                                title="Tuple Operations"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Tuple operations showing rows fetched, inserted, updated, and deleted',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default PostgresOverviewSection;
