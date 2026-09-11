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
import CircularProgress from '@mui/material/CircularProgress';
import { Sync as SyncIcon } from '@mui/icons-material';
import { useDashboard } from '../../../contexts/useDashboard';
import { useMetrics } from '../../../hooks/useMetrics';
import type { MetricDataPoint, MetricQueryParams, MetricSeries } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import ChartPanel from '../ChartPanel';
import { formatBytes, formatLag, formatValue } from '../../../utils/formatters';
import { type ServerSectionProps, extractSparklineData, extractLatestValue } from './types';

/** Number of data buckets for KPI sparklines */
const KPI_BUCKETS = 30;

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/**
 * Determine replication lag status.
 */
const getLagStatus = (
    lagSeconds: number | null
): 'good' | 'warning' | 'critical' | undefined => {
    if (lagSeconds === null) { return undefined; }
    if (lagSeconds > 30) { return 'critical'; }
    if (lagSeconds > 5) { return 'warning'; }
    return 'good';
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
 * Build chart data for replication lag where each standby produces
 * its own series, identified by the series name field.
 */
const buildReplicationLagChartData = (
    series: MetricSeries[] | null,
    metricNames: string[],
    displayNames: string[],
) => {
    if (!series || series.length === 0) { return null; }

    const matchedSeries = metricNames.map((metric, idx) => {
        const found = series.find(s => s.metric === metric);
        return {
            name: displayNames[idx],
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
 * WAL and Replication section displays WAL generation rates,
 * replication lag, checkpoint performance, and WAL statistics.
 */
const WalReplicationSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets)
    const walKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_wal',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['wal_bytes_per_sec', 'wal_records_per_sec'],
    }), [connectionId, timeRange.range]);

    const replLagKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_replication',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['replay_lag', 'write_lag', 'flush_lag'],
    }), [connectionId, timeRange.range]);

    const checkpointKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_checkpointer',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['num_timed_delta', 'num_requested_delta'],
    }), [connectionId, timeRange.range]);

    // Chart queries (150 buckets)
    const walChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_wal',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['wal_bytes_per_sec', 'wal_records_per_sec'],
    }), [connectionId, timeRange.range]);

    const replLagChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_replication',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['write_lag', 'flush_lag', 'replay_lag'],
    }), [connectionId, timeRange.range]);

    /*
     * One query feeds both checkpoint charts, since the counts and the
     * buffers come from the same probe and differ only in how they are
     * drawn: the counts stack as per-interval bars, whilst the buffers
     * are a rate and belong on their own line chart.
     */
    const checkpointChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_checkpointer',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'num_timed_delta',
            'num_requested_delta',
            'buffers_written_per_sec',
        ],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const walKpi = useMetrics(walKpiParams);
    const replLagKpi = useMetrics(replLagKpiParams);
    const checkpointKpi = useMetrics(checkpointKpiParams);

    // Fetch chart data
    const walChart = useMetrics(walChartParams);
    const replLagChart = useMetrics(replLagChartParams);
    const checkpointChart = useMetrics(checkpointChartParams);

    // Extract current values
    const walBytesRate = extractLatestValue(
        walKpi.data, 'wal_bytes_per_sec'
    );
    const walRecordsRate = extractLatestValue(
        walKpi.data, 'wal_records_per_sec'
    );
    const replayLag = extractLatestValue(replLagKpi.data, 'replay_lag');

    /*
     * The checkpoint tile reports the share of checkpoints that were
     * requested rather than timed across the whole window, so both
     * counters are summed over their buckets rather than sampled at
     * the latest one.
     */
    const timedPoints = useMemo(
        () => extractSparklineData(checkpointKpi.data, 'num_timed_delta'),
        [checkpointKpi.data]
    );
    const requestedPoints = useMemo(
        () => extractSparklineData(checkpointKpi.data, 'num_requested_delta'),
        [checkpointKpi.data]
    );
    const requestedShare = useMemo(() => {
        const sum = (points: MetricDataPoint[]) =>
            points.reduce((total, point) => total + point.value, 0);
        const total = sum(timedPoints) + sum(requestedPoints);
        if (total <= 0) { return null; }
        return (sum(requestedPoints) / total) * 100;
    }, [timedPoints, requestedPoints]);

    /** Per-bucket total checkpoints, timed plus requested. */
    const checkpointSparkline = useMemo((): MetricDataPoint[] => {
        const len = Math.max(timedPoints.length, requestedPoints.length);
        const points: MetricDataPoint[] = [];
        for (let i = 0; i < len; i++) {
            const timed = i < timedPoints.length ? timedPoints[i] : null;
            const requested = i < requestedPoints.length
                ? requestedPoints[i] : null;
            points.push({
                time: (timed ?? requested as MetricDataPoint).time,
                value: (timed?.value ?? 0) + (requested?.value ?? 0),
            });
        }
        return points;
    }, [timedPoints, requestedPoints]);

    // Build chart datasets
    const walChartData = useMemo(
        () => buildChartData(
            walChart.data,
            ['wal_bytes_per_sec', 'wal_records_per_sec'],
            ['WAL Bytes/s', 'WAL Records/s'],
        ),
        [walChart.data]
    );

    const replLagChartData = useMemo(
        () => buildReplicationLagChartData(
            replLagChart.data,
            ['write_lag', 'flush_lag', 'replay_lag'],
            ['Write Lag', 'Flush Lag', 'Replay Lag'],
        ),
        [replLagChart.data]
    );

    const checkpointChartData = useMemo(
        () => buildChartData(
            checkpointChart.data,
            ['num_timed_delta', 'num_requested_delta'],
            ['Timed', 'Requested'],
        ),
        [checkpointChart.data]
    );

    const checkpointBuffersChartData = useMemo(
        () => buildChartData(
            checkpointChart.data,
            ['buffers_written_per_sec'],
            ['Buffers Written/s'],
        ),
        [checkpointChart.data]
    );

    const isKpiLoading = walKpi.loading || replLagKpi.loading
        || checkpointKpi.loading;

    return (
        <CollapsibleSection title="WAL and Replication" icon={<SyncIcon sx={{ fontSize: 16 }} />} defaultExpanded>
            {isKpiLoading && !walKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} aria-label="Loading" />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="WAL Bytes"
                    value={formatBytes(walBytesRate)}
                    unit={walBytesRate !== null ? '/s' : undefined}
                    sparklineData={extractSparklineData(
                        walKpi.data, 'wal_bytes_per_sec'
                    )}
                    analysisContext={{
                        metricDescription: 'WAL bytes generated per second over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="WAL Records"
                    value={formatValue(walRecordsRate)}
                    unit={walRecordsRate !== null ? '/s' : undefined}
                    sparklineData={extractSparklineData(
                        walKpi.data, 'wal_records_per_sec'
                    )}
                    analysisContext={{
                        metricDescription: 'WAL records generated per second over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Replication Lag"
                    value={formatLag(replayLag)}
                    status={getLagStatus(replayLag)}
                    sparklineData={extractSparklineData(
                        replLagKpi.data, 'replay_lag'
                    )}
                    analysisContext={{
                        metricDescription: 'Replication replay lag over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Requested Checkpoints"
                    value={formatValue(requestedShare)}
                    unit={requestedShare !== null ? '%' : undefined}
                    sparklineData={checkpointSparkline}
                    analysisContext={{
                        metricDescription: 'Share of checkpoints that were requested rather than timed; a high share suggests max_wal_size is too low',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    <ChartPanel
                        title="WAL Activity Over Time"
                        loading={walChart.loading && !walChartData}
                        hasData={!!walChartData}
                        emptyMessage="No WAL data available"
                        errorMessage={walChart.error}
                        height={CHART_HEIGHT}
                    >
                        {walChartData && (
                            <Chart
                                type="line"
                                data={walChartData}
                                title="WAL Activity Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                areaFill
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Write-ahead log activity showing WAL bytes and records generated per second',
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
                        title="Replication Lag Over Time"
                        loading={replLagChart.loading && !replLagChartData}
                        hasData={!!replLagChartData}
                        emptyMessage="No replication data available. Is this server a primary with standbys?"
                        errorMessage={replLagChart.error}
                        height={CHART_HEIGHT}
                    >
                        {replLagChartData && (
                            <Chart
                                type="line"
                                data={replLagChartData}
                                title="Replication Lag Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Replication lag showing delay between primary and replicas',
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
                        title="Checkpoints Over Time"
                        loading={checkpointChart.loading && !checkpointChartData}
                        hasData={!!checkpointChartData}
                        emptyMessage="No checkpoint data available"
                        errorMessage={checkpointChart.error}
                        height={CHART_HEIGHT}
                    >
                        {checkpointChartData && (
                            <Chart
                                type="bar"
                                data={checkpointChartData}
                                title="Checkpoints Over Time"
                                height={CHART_HEIGHT}
                                stacked
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Checkpoints completed in each interval, split between timed and requested',
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
                        title="Checkpoint Buffers Written"
                        loading={checkpointChart.loading
                            && !checkpointBuffersChartData}
                        hasData={!!checkpointBuffersChartData}
                        emptyMessage="No checkpoint buffer data available"
                        errorMessage={checkpointChart.error}
                        height={CHART_HEIGHT}
                    >
                        {checkpointBuffersChartData && (
                            <Chart
                                type="line"
                                data={checkpointBuffersChartData}
                                title="Checkpoint Buffers Written"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Buffers written by the checkpointer per second',
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

export default WalReplicationSection;
