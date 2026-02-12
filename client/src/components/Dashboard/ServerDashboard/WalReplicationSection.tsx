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
 * Format bytes per second into a human-readable rate string.
 */
const formatBytesRate = (bytesPerSec: number | null): string => {
    if (bytesPerSec === null) { return '--'; }
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    let size = bytesPerSec;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024;
        unitIndex++;
    }
    return `${size.toFixed(1)} ${units[unitIndex]}`;
};

/**
 * Format a duration in milliseconds to a human-readable string.
 */
const formatDuration = (ms: number | null): string => {
    if (ms === null) { return '--'; }
    if (ms < 1000) { return `${ms.toFixed(0)} ms`; }
    if (ms < 60000) { return `${(ms / 1000).toFixed(1)} s`; }
    return `${(ms / 60000).toFixed(1)} min`;
};

/**
 * Format a lag value into a human-readable string.
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
) => {
    if (!series || series.length === 0) { return null; }

    const lagSeries = series.filter(s => s.metric === 'replay_lag');
    if (lagSeries.length === 0) { return null; }

    const categories = lagSeries[0]?.data.map(d => d.time) ?? [];

    return {
        categories,
        series: lagSeries.map(s => ({
            name: s.name || 'Standby',
            data: s.data.map(d => d.value),
        })),
    };
};

/**
 * WAL and Replication section displays WAL generation rates,
 * replication lag, replication slot status, and checkpoint
 * performance metrics.
 */
const WalReplicationSection: React.FC<ServerSectionProps> = ({
    connectionId,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets)
    const walKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_wal',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['wal_bytes_per_sec'],
    }), [connectionId, timeRange.range]);

    const replLagKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_replication',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['replay_lag'],
    }), [connectionId, timeRange.range]);

    const replSlotsKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_replication_slots',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'last',
        metrics: ['active_count'],
    }), [connectionId, timeRange.range]);

    const checkpointKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_checkpointer',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['checkpoint_write_time'],
    }), [connectionId, timeRange.range]);

    // Chart queries (150 buckets)
    const walChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_wal',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['wal_bytes_per_sec'],
    }), [connectionId, timeRange.range]);

    const replLagChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_replication',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['replay_lag'],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const walKpi = useMetrics(walKpiParams);
    const replLagKpi = useMetrics(replLagKpiParams);
    const replSlotsKpi = useMetrics(replSlotsKpiParams);
    const checkpointKpi = useMetrics(checkpointKpiParams);

    // Fetch chart data
    const walChart = useMetrics(walChartParams);
    const replLagChart = useMetrics(replLagChartParams);

    // Extract current values
    const walRate = extractLatestValue(walKpi.data, 'wal_bytes_per_sec');
    const replayLag = extractLatestValue(replLagKpi.data, 'replay_lag');
    const activeSlots = extractLatestValue(
        replSlotsKpi.data, 'active_count'
    );
    const checkpointTime = extractLatestValue(
        checkpointKpi.data, 'checkpoint_write_time'
    );

    // Build chart datasets
    const walChartData = useMemo(
        () => buildChartData(
            walChart.data,
            ['wal_bytes_per_sec'],
            ['WAL Bytes/s'],
        ),
        [walChart.data]
    );

    const replLagChartData = useMemo(
        () => buildReplicationLagChartData(replLagChart.data),
        [replLagChart.data]
    );

    const isKpiLoading = walKpi.loading || replLagKpi.loading
        || replSlotsKpi.loading || checkpointKpi.loading;

    return (
        <CollapsibleSection title="WAL and Replication" defaultExpanded>
            {isKpiLoading && !walKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="WAL Generation Rate"
                    value={formatBytesRate(walRate)}
                    sparklineData={extractSparklineData(
                        walKpi.data, 'wal_bytes_per_sec'
                    )}
                />
                <KpiTile
                    label="Replication Lag"
                    value={formatLag(replayLag)}
                    status={getLagStatus(replayLag)}
                    sparklineData={extractSparklineData(
                        replLagKpi.data, 'replay_lag'
                    )}
                />
                <KpiTile
                    label="Active Replication Slots"
                    value={activeSlots !== null
                        ? Math.round(activeSlots).toString()
                        : '--'}
                    sparklineData={extractSparklineData(
                        replSlotsKpi.data, 'active_count'
                    )}
                />
                <KpiTile
                    label="Checkpoint Duration"
                    value={formatDuration(checkpointTime)}
                    sparklineData={extractSparklineData(
                        checkpointKpi.data, 'checkpoint_write_time'
                    )}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    {walChart.loading && !walChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : walChartData ? (
                        <Chart
                            type="line"
                            data={walChartData}
                            title="WAL Generation Over Time"
                            height={CHART_HEIGHT}
                            smooth
                            areaFill
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
                            No WAL data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {replLagChart.loading && !replLagChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : replLagChartData ? (
                        <Chart
                            type="line"
                            data={replLagChartData}
                            title="Replication Lag Over Time"
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
                            No replication lag data available
                        </Typography>
                    )}
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default WalReplicationSection;
