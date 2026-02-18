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
import {
    DatabaseSectionProps,
    extractSparklineData,
    extractLatestValue,
    formatValue,
    formatBytes,
} from './types';

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
 * Determine status for dead tuple ratio values.
 */
const getDeadTupleStatus = (
    value: number | null
): 'good' | 'warning' | 'critical' | undefined => {
    if (value === null) { return undefined; }
    if (value <= 5) { return 'good'; }
    if (value <= 20) { return 'warning'; }
    return 'critical';
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
 * Database Performance Overview section displays database-specific
 * metrics including size, cache hit ratio, transaction rate, and
 * dead tuple ratio with accompanying time-series charts.
 */
const PerformanceSection: React.FC<DatabaseSectionProps> = ({
    connectionId,
    databaseName,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets)
    const sizeKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['database_size'],
    }), [connectionId, databaseName, timeRange.range]);

    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['cache_hit_ratio'],
    }), [connectionId, databaseName, timeRange.range]);

    const txnKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec', 'xact_rollback_per_sec'],
    }), [connectionId, databaseName, timeRange.range]);

    const deadTupleKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_tables',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['dead_tuple_ratio'],
    }), [connectionId, databaseName, timeRange.range]);

    // Chart queries (150 buckets)
    const txnChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec', 'xact_rollback_per_sec'],
    }), [connectionId, databaseName, timeRange.range]);

    const cacheChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['cache_hit_ratio'],
    }), [connectionId, databaseName, timeRange.range]);

    // Fetch KPI data
    const sizeKpi = useMetrics(sizeKpiParams);
    const cacheKpi = useMetrics(cacheKpiParams);
    const txnKpi = useMetrics(txnKpiParams);
    const deadTupleKpi = useMetrics(deadTupleKpiParams);

    // Fetch chart data
    const txnChart = useMetrics(txnChartParams);
    const cacheChart = useMetrics(cacheChartParams);

    // Extract current values
    const databaseSize = extractLatestValue(
        sizeKpi.data, 'database_size'
    );
    const cacheHitRatio = extractLatestValue(
        cacheKpi.data, 'cache_hit_ratio'
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
    const deadTupleRatio = extractLatestValue(
        deadTupleKpi.data, 'dead_tuple_ratio'
    );

    // Build chart datasets
    const txnChartData = useMemo(
        () => buildChartData(
            txnChart.data,
            ['xact_commit_per_sec', 'xact_rollback_per_sec'],
            ['Commits/s', 'Rollbacks/s'],
        ),
        [txnChart.data]
    );

    const cacheChartData = useMemo(
        () => buildChartData(
            cacheChart.data,
            ['cache_hit_ratio'],
            ['Cache Hit Ratio %'],
        ),
        [cacheChart.data]
    );

    const isKpiLoading = sizeKpi.loading || cacheKpi.loading
        || txnKpi.loading || deadTupleKpi.loading;

    return (
        <CollapsibleSection title="Performance Overview" defaultExpanded>
            {isKpiLoading && !sizeKpi.data && (
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'center',
                    py: 2,
                }}>
                    <CircularProgress size={24} aria-label="Loading" />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="Database Size"
                    value={formatBytes(databaseSize)}
                    sparklineData={extractSparklineData(
                        sizeKpi.data, 'database_size'
                    )}
                    analysisContext={{
                        metricDescription: 'Database size over time',
                        connectionId,
                        databaseName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Cache Hit Ratio"
                    value={formatValue(cacheHitRatio)}
                    unit="%"
                    status={getCacheHitStatus(cacheHitRatio)}
                    sparklineData={extractSparklineData(
                        cacheKpi.data, 'cache_hit_ratio'
                    )}
                    analysisContext={{
                        metricDescription: 'Buffer cache hit ratio over time',
                        connectionId,
                        databaseName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Transaction Rate"
                    value={formatValue(txnRate)}
                    unit="txn/s"
                    sparklineData={extractSparklineData(
                        txnKpi.data, 'xact_commit_per_sec'
                    )}
                    analysisContext={{
                        metricDescription: 'Transaction commit rate over time',
                        connectionId,
                        databaseName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Dead Tuple Ratio"
                    value={formatValue(deadTupleRatio)}
                    unit="%"
                    status={getDeadTupleStatus(deadTupleRatio)}
                    sparklineData={extractSparklineData(
                        deadTupleKpi.data, 'dead_tuple_ratio'
                    )}
                    analysisContext={{
                        metricDescription: 'Dead tuple ratio over time',
                        connectionId,
                        databaseName,
                        timeRange: timeRange.range,
                    }}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    {txnChart.loading && !txnChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} aria-label="Loading chart" />
                        </Box>
                    ) : txnChartData ? (
                        <Chart
                            type="line"
                            data={txnChartData}
                            title="Transaction Rate Over Time"
                            height={CHART_HEIGHT}
                            smooth
                            showLegend
                            showTooltip
                            enableExport={false}
                            analysisContext={{
                                metricDescription: 'Transaction commit and rollback rate for the database',
                                connectionId,
                                databaseName,
                                timeRange: timeRange.range,
                            }}
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
                    {cacheChart.loading && !cacheChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} aria-label="Loading chart" />
                        </Box>
                    ) : cacheChartData ? (
                        <Chart
                            type="line"
                            data={cacheChartData}
                            title="Cache Hit Ratio Over Time"
                            height={CHART_HEIGHT}
                            smooth
                            areaFill
                            showLegend
                            showTooltip
                            enableExport={false}
                            analysisContext={{
                                metricDescription: 'Buffer cache hit ratio showing cache effectiveness',
                                connectionId,
                                databaseName,
                                timeRange: timeRange.range,
                            }}
                        />
                    ) : (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'center', py: 4 }}
                        >
                            No cache hit ratio data available
                        </Typography>
                    )}
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default PerformanceSection;
