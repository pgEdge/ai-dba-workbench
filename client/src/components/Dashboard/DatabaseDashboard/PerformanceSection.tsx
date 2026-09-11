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
import { useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useDashboard } from '../../../contexts/useDashboard';
import { useMetrics } from '../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries, MetricDataPoint } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import {
    CACHE_HIT_METRICS,
    CACHE_HIT_CAVEAT,
    buildCacheHitRatioPoints,
    latestCacheHitRatio,
} from '../cacheHitRatio';
import {
    type DatabaseSectionProps,
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

/** Analysis description for the cache hit ratio KPI and chart. */
const CACHE_HIT_DESCRIPTION =
    'Buffer cache hit ratio per interval (null buckets had no block '
    + `access). ${CACHE_HIT_CAVEAT}`;

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
        probeName: 'pg_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['database_size_bytes'],
    }), [connectionId, databaseName, timeRange.range]);

    // The ratio is derived from per-interval rates rather than the
    // lifetime counters, so it reflects the current interval and an
    // idle bucket becomes a gap (see issue #401).
    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: CACHE_HIT_METRICS,
    }), [connectionId, databaseName, timeRange.range]);

    const txnKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['xact_commit', 'xact_rollback'],
    }), [connectionId, databaseName, timeRange.range]);

    const deadTupleKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_tables',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['n_dead_tup', 'n_live_tup'],
    }), [connectionId, databaseName, timeRange.range]);

    // Chart queries (150 buckets)
    const txnChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'last',
        metrics: ['xact_commit', 'xact_rollback'],
    }), [connectionId, databaseName, timeRange.range]);

    const cacheChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'last',
        metrics: CACHE_HIT_METRICS,
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
        sizeKpi.data, 'database_size_bytes'
    );

    // Per-bucket cache hit ratio from the hit and read rates; idle
    // buckets are null (gaps) and the headline is the latest bucket
    // that has a ratio, so a trailing idle bucket does not blank it.
    const cacheHitSparkline = useMemo(
        () => buildCacheHitRatioPoints(cacheKpi.data),
        [cacheKpi.data]
    );
    const cacheHitRatio = useMemo(
        () => latestCacheHitRatio(cacheHitSparkline),
        [cacheHitSparkline]
    );

    // Transaction rate: use raw cumulative xact_commit
    const txnCommit = extractLatestValue(
        txnKpi.data, 'xact_commit'
    );
    const txnRollback = extractLatestValue(
        txnKpi.data, 'xact_rollback'
    );
    const txnRate = useMemo(() => {
        if (txnCommit === null && txnRollback === null) {
            return null;
        }
        return (txnCommit ?? 0) + (txnRollback ?? 0);
    }, [txnCommit, txnRollback]);

    // Dead tuple ratio from raw n_dead_tup and n_live_tup
    const nDeadTup = extractLatestValue(
        deadTupleKpi.data, 'n_dead_tup'
    );
    const nLiveTup = extractLatestValue(
        deadTupleKpi.data, 'n_live_tup'
    );
    const deadTupleRatio = useMemo(() => {
        if (nDeadTup === null && nLiveTup === null) { return null; }
        const dead = nDeadTup ?? 0;
        const live = nLiveTup ?? 0;
        const total = dead + live;
        if (total === 0) { return 0; }
        return (dead / total) * 100;
    }, [nDeadTup, nLiveTup]);

    // Build per-point sparkline for dead tuple ratio
    const deadTupleSparkline = useMemo((): MetricDataPoint[] => {
        if (!deadTupleKpi.data) { return []; }
        const deadSeries = deadTupleKpi.data.find(
            s => s.metric === 'n_dead_tup'
        );
        const liveSeries = deadTupleKpi.data.find(
            s => s.metric === 'n_live_tup'
        );
        if (!deadSeries || !liveSeries) { return []; }

        const len = Math.min(
            deadSeries.data.length, liveSeries.data.length
        );
        const points: MetricDataPoint[] = [];
        for (let i = 0; i < len; i++) {
            const d = deadSeries.data[i].value;
            const l = liveSeries.data[i].value;
            const total = d + l;
            points.push({
                time: deadSeries.data[i].time,
                value: total > 0 ? (d / total) * 100 : 0,
            });
        }
        return points;
    }, [deadTupleKpi.data]);

    // Build chart datasets
    const txnChartData = useMemo(
        () => buildChartData(
            txnChart.data,
            ['xact_commit', 'xact_rollback'],
            ['Commits', 'Rollbacks'],
        ),
        [txnChart.data]
    );

    // Build the cache hit ratio chart from the per-bucket ratio; null
    // buckets are passed through so ECharts draws them as gaps.
    const cacheChartData = useMemo(() => {
        const points = buildCacheHitRatioPoints(cacheChart.data);
        if (points.length === 0) { return null; }
        return {
            categories: points.map(p => p.time),
            series: [{
                name: 'Cache Hit Ratio %',
                data: points.map(p => p.value),
            }],
        };
    }, [cacheChart.data]);

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
                        sizeKpi.data, 'database_size_bytes'
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
                    sparklineData={cacheHitSparkline}
                    analysisContext={{
                        metricDescription: CACHE_HIT_DESCRIPTION,
                        connectionId,
                        databaseName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Transactions"
                    value={formatValue(txnRate, 0)}
                    unit="total"
                    sparklineData={extractSparklineData(
                        txnKpi.data, 'xact_commit'
                    )}
                    analysisContext={{
                        metricDescription: 'Transaction commit count over time',
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
                    sparklineData={deadTupleSparkline}
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
                            title="Transactions Over Time"
                            height={CHART_HEIGHT}
                            smooth
                            showLegend
                            showTooltip
                            enableExport={false}
                            analysisContext={{
                                metricDescription: 'Transaction commit and rollback counts for the database',
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
                                metricDescription: CACHE_HIT_DESCRIPTION,
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
