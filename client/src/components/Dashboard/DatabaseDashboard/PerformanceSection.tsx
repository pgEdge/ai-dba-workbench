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
import { MetricQueryParams, MetricSeries, MetricDataPoint } from '../types';
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
        probeName: 'pg_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['database_size_bytes'],
    }), [connectionId, databaseName, timeRange.range]);

    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        databaseName,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['blks_hit', 'blks_read'],
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
        metrics: ['blks_hit', 'blks_read'],
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

    // Compute cache hit ratio from raw blks_hit and blks_read
    const blksHit = extractLatestValue(
        cacheKpi.data, 'blks_hit'
    );
    const blksRead = extractLatestValue(
        cacheKpi.data, 'blks_read'
    );
    const cacheHitRatio = useMemo(() => {
        if (blksHit === null && blksRead === null) { return null; }
        const hit = blksHit ?? 0;
        const read = blksRead ?? 0;
        const total = hit + read;
        if (total === 0) { return 100; }
        return (hit / total) * 100;
    }, [blksHit, blksRead]);

    // Build per-point sparkline for cache hit ratio
    const cacheHitSparkline = useMemo((): MetricDataPoint[] => {
        if (!cacheKpi.data) { return []; }
        const hitSeries = cacheKpi.data.find(
            s => s.metric === 'blks_hit'
        );
        const readSeries = cacheKpi.data.find(
            s => s.metric === 'blks_read'
        );
        if (!hitSeries || !readSeries) { return []; }

        const len = Math.min(
            hitSeries.data.length, readSeries.data.length
        );
        const points: MetricDataPoint[] = [];
        for (let i = 0; i < len; i++) {
            const h = hitSeries.data[i].value;
            const r = readSeries.data[i].value;
            const total = h + r;
            points.push({
                time: hitSeries.data[i].time,
                value: total > 0 ? (h / total) * 100 : 0,
            });
        }
        return points;
    }, [cacheKpi.data]);

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

    // Build cache hit ratio chart from per-point computation
    const cacheChartData = useMemo(() => {
        if (!cacheChart.data) { return null; }
        const hitSeries = cacheChart.data.find(
            s => s.metric === 'blks_hit'
        );
        const readSeries = cacheChart.data.find(
            s => s.metric === 'blks_read'
        );
        if (!hitSeries || !readSeries) { return null; }
        if (
            hitSeries.data.length === 0
            && readSeries.data.length === 0
        ) {
            return null;
        }

        const len = Math.min(
            hitSeries.data.length, readSeries.data.length
        );
        const ratioData: number[] = [];
        const categories: string[] = [];
        for (let i = 0; i < len; i++) {
            const h = hitSeries.data[i].value;
            const r = readSeries.data[i].value;
            const total = h + r;
            ratioData.push(
                total > 0 ? (h / total) * 100 : 0
            );
            categories.push(hitSeries.data[i].time);
        }

        return {
            categories,
            series: [{
                name: 'Cache Hit Ratio %',
                data: ratioData,
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
                        metricDescription: 'Buffer cache hit ratio over time',
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
