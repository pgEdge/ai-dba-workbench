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
import SyncIcon from '@mui/icons-material/Sync';
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
 * Format a lag value (in seconds) into a human-readable string.
 */
const formatLag = (lagSeconds: number | null): string => {
    if (lagSeconds === null) { return '--'; }
    if (lagSeconds < 1) { return `${(lagSeconds * 1000).toFixed(0)} ms`; }
    if (lagSeconds < 60) { return `${lagSeconds.toFixed(1)} s`; }
    if (lagSeconds < 3600) { return `${(lagSeconds / 60).toFixed(1)} min`; }
    return `${(lagSeconds / 3600).toFixed(1)} h`;
};

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
 * Format a numeric value for display.
 */
const formatValue = (value: number | null, decimals = 0): string => {
    if (value === null) { return '--'; }
    return value.toFixed(decimals);
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
        metrics: ['wal_bytes', 'wal_records'],
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
        metrics: ['num_timed', 'num_requested', 'buffers_written'],
    }), [connectionId, timeRange.range]);

    // Chart queries (150 buckets)
    const walChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_wal',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['wal_bytes', 'wal_records'],
    }), [connectionId, timeRange.range]);

    const replLagChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_replication',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['write_lag', 'flush_lag', 'replay_lag'],
    }), [connectionId, timeRange.range]);

    const checkpointChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_checkpointer',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['num_timed', 'num_requested', 'buffers_written'],
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
    const walBytes = extractLatestValue(walKpi.data, 'wal_bytes');
    const walRecords = extractLatestValue(walKpi.data, 'wal_records');
    const replayLag = extractLatestValue(replLagKpi.data, 'replay_lag');
    const numTimed = extractLatestValue(
        checkpointKpi.data, 'num_timed'
    );
    const numRequested = extractLatestValue(
        checkpointKpi.data, 'num_requested'
    );
    const totalCheckpoints = useMemo(() => {
        if (numTimed !== null || numRequested !== null) {
            return (numTimed ?? 0) + (numRequested ?? 0);
        }
        return null;
    }, [numTimed, numRequested]);

    // Build chart datasets
    const walChartData = useMemo(
        () => buildChartData(
            walChart.data,
            ['wal_bytes', 'wal_records'],
            ['WAL Bytes', 'WAL Records'],
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
            ['num_timed', 'num_requested', 'buffers_written'],
            ['Timed', 'Requested', 'Buffers Written'],
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
                    value={formatBytes(walBytes)}
                    sparklineData={extractSparklineData(
                        walKpi.data, 'wal_bytes'
                    )}
                    analysisContext={{
                        metricDescription: 'WAL bytes generated over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="WAL Records"
                    value={walRecords !== null
                        ? formatValue(walRecords)
                        : '--'}
                    sparklineData={extractSparklineData(
                        walKpi.data, 'wal_records'
                    )}
                    analysisContext={{
                        metricDescription: 'WAL record count over time',
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
                    label="Checkpoints"
                    value={totalCheckpoints !== null
                        ? formatValue(totalCheckpoints)
                        : '--'}
                    sparklineData={extractSparklineData(
                        checkpointKpi.data, 'num_timed'
                    )}
                    analysisContext={{
                        metricDescription: 'Checkpoint frequency over time',
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
                                    metricDescription: 'Write-ahead log activity including WAL bytes generated',
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
                        height={CHART_HEIGHT}
                    >
                        {checkpointChartData && (
                            <Chart
                                type="line"
                                data={checkpointChartData}
                                title="Checkpoints Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Checkpoint activity showing timed and requested checkpoints',
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
