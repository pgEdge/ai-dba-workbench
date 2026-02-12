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
 * Determine a status level from a percentage value.
 */
const getPercentageStatus = (
    value: number | null
): 'good' | 'warning' | 'critical' | undefined => {
    if (value === null) { return undefined; }
    if (value >= 90) { return 'critical'; }
    if (value >= 75) { return 'warning'; }
    return 'good';
};

/**
 * Format a numeric value for display in a KPI tile.
 */
const formatKpiValue = (value: number | null, decimals = 1): string => {
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

    const categories = matchedSeries.find(s => s.categories.length > 0)?.categories ?? [];

    return {
        categories,
        series: matchedSeries.map(s => ({
            name: s.name,
            data: s.data,
        })),
    };
};

/**
 * System Resources section displays system-level metrics including
 * CPU, memory, disk, load average, and network I/O from pg_sys_*
 * collector probes.
 */
const SystemResourcesSection: React.FC<ServerSectionProps> = ({
    connectionId,
}) => {
    const { timeRange } = useDashboard();

    // KPI sparkline queries (30 buckets)
    const cpuKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_cpu_usage_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['cpu_usage_percent'],
    }), [connectionId, timeRange.range]);

    const memoryKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_memory_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['used_percent'],
    }), [connectionId, timeRange.range]);

    const diskKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_disk_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['used_percent'],
    }), [connectionId, timeRange.range]);

    const loadKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_load_avg_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['load_avg_one_minute'],
    }), [connectionId, timeRange.range]);

    // Full chart queries (150 buckets)
    const cpuChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_cpu_usage_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['user_percent', 'system_percent', 'iowait_percent'],
    }), [connectionId, timeRange.range]);

    const memoryChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_memory_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['used', 'buffers', 'cached'],
    }), [connectionId, timeRange.range]);

    const diskIoChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_disk_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['reads_per_sec', 'writes_per_sec'],
    }), [connectionId, timeRange.range]);

    const networkChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_network_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['bytes_recv_per_sec', 'bytes_sent_per_sec'],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const cpuKpi = useMetrics(cpuKpiParams);
    const memoryKpi = useMetrics(memoryKpiParams);
    const diskKpi = useMetrics(diskKpiParams);
    const loadKpi = useMetrics(loadKpiParams);

    // Fetch chart data
    const cpuChart = useMetrics(cpuChartParams);
    const memoryChart = useMetrics(memoryChartParams);
    const diskIoChart = useMetrics(diskIoChartParams);
    const networkChart = useMetrics(networkChartParams);

    // Extract current values for KPI tiles
    const cpuValue = extractLatestValue(cpuKpi.data, 'cpu_usage_percent');
    const memoryValue = extractLatestValue(memoryKpi.data, 'used_percent');
    const diskValue = extractLatestValue(diskKpi.data, 'used_percent');
    const loadValue = extractLatestValue(loadKpi.data, 'load_avg_one_minute');

    // Build chart datasets
    const cpuChartData = useMemo(
        () => buildChartData(
            cpuChart.data,
            ['user_percent', 'system_percent', 'iowait_percent'],
            ['User %', 'System %', 'IO Wait %'],
        ),
        [cpuChart.data]
    );

    const memoryChartData = useMemo(
        () => buildChartData(
            memoryChart.data,
            ['used', 'buffers', 'cached'],
            ['Used', 'Buffers', 'Cached'],
        ),
        [memoryChart.data]
    );

    const diskIoChartData = useMemo(
        () => buildChartData(
            diskIoChart.data,
            ['reads_per_sec', 'writes_per_sec'],
            ['Reads/s', 'Writes/s'],
        ),
        [diskIoChart.data]
    );

    const networkChartData = useMemo(
        () => buildChartData(
            networkChart.data,
            ['bytes_recv_per_sec', 'bytes_sent_per_sec'],
            ['Bytes Recv/s', 'Bytes Sent/s'],
        ),
        [networkChart.data]
    );

    const isKpiLoading = cpuKpi.loading || memoryKpi.loading
        || diskKpi.loading || loadKpi.loading;

    return (
        <CollapsibleSection title="System Resources" defaultExpanded>
            {isKpiLoading && !cpuKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="CPU Usage"
                    value={formatKpiValue(cpuValue)}
                    unit="%"
                    status={getPercentageStatus(cpuValue)}
                    sparklineData={extractSparklineData(
                        cpuKpi.data, 'cpu_usage_percent'
                    )}
                />
                <KpiTile
                    label="Memory Usage"
                    value={formatKpiValue(memoryValue)}
                    unit="%"
                    status={getPercentageStatus(memoryValue)}
                    sparklineData={extractSparklineData(
                        memoryKpi.data, 'used_percent'
                    )}
                />
                <KpiTile
                    label="Disk Usage"
                    value={formatKpiValue(diskValue)}
                    unit="%"
                    status={getPercentageStatus(diskValue)}
                    sparklineData={extractSparklineData(
                        diskKpi.data, 'used_percent'
                    )}
                />
                <KpiTile
                    label="Load Average"
                    value={formatKpiValue(loadValue, 2)}
                    sparklineData={extractSparklineData(
                        loadKpi.data, 'load_avg_one_minute'
                    )}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    {cpuChart.loading && !cpuChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : cpuChartData ? (
                        <Chart
                            type="line"
                            data={cpuChartData}
                            title="CPU Usage Over Time"
                            height={CHART_HEIGHT}
                            smooth
                            areaFill
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
                            No CPU data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {memoryChart.loading && !memoryChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : memoryChartData ? (
                        <Chart
                            type="line"
                            data={memoryChartData}
                            title="Memory Usage Over Time"
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
                            No memory data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {diskIoChart.loading && !diskIoChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : diskIoChartData ? (
                        <Chart
                            type="line"
                            data={diskIoChartData}
                            title="Disk I/O"
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
                            No disk I/O data available
                        </Typography>
                    )}
                </Box>

                <Box>
                    {networkChart.loading && !networkChartData ? (
                        <Box sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            alignItems: 'center',
                            height: CHART_HEIGHT,
                        }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : networkChartData ? (
                        <Chart
                            type="line"
                            data={networkChartData}
                            title="Network I/O"
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
                            No network data available
                        </Typography>
                    )}
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default SystemResourcesSection;
