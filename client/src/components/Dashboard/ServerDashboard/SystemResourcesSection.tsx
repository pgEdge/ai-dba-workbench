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
import { useCallback, useEffect, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import type { SelectChangeEvent } from '@mui/material/Select';
import { Computer as ComputerIcon } from '@mui/icons-material';
import { useDashboard } from '../../../contexts/useDashboard';
import { useMetrics } from '../../../hooks/useMetrics';
import type { MetricQueryParams } from '../types';
import { buildMetricChartData as buildChartData } from '../metricsChart';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import { DASHBOARD_CONTROL_TEXT_SX } from '../../../theme/tokens';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import ChartPanel from '../ChartPanel';
import { formatBytes, formatValue } from '../../../utils/formatters';
import { useDiskMounts } from './diskMounts';
import {
    type ServerSectionProps, extractSparklineData, extractLatestValue, hasNonZeroData,
} from './types';

/** Number of data buckets for KPI sparklines */
const KPI_BUCKETS = 30;

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/** Selector bar holding the disk mount picker above the disk chart */
const MOUNT_SELECT_BAR_SX = {
    display: 'flex',
    justifyContent: 'flex-end',
    mb: 1,
};

/** Width and typography of the disk mount picker */
const MOUNT_SELECT_SX = {
    minWidth: 180,
    '& .MuiInputBase-root': DASHBOARD_CONTROL_TEXT_SX,
    '& .MuiInputLabel-root': DASHBOARD_CONTROL_TEXT_SX,
};

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
 * System Resources section displays system-level metrics including
 * CPU, memory, disk, load average, and network I/O from pg_sys_*
 * collector probes.
 */
const SystemResourcesSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { timeRange } = useDashboard();

    /*
     * pg_sys_disk_info records one row per mounted filesystem, so both
     * the disk tile and the disk chart describe a single mount chosen
     * here. Without the filter the query averages the data volume, the
     * WAL volume and every pseudo filesystem into a figure that
     * corresponds to no real filesystem at all.
     */
    const { mounts: diskMounts, settled: mountsSettled } =
        useDiskMounts(connectionId);
    const mountNames = useMemo(
        () => diskMounts.map(mount => mount.mount_point ?? ''),
        [diskMounts],
    );
    const [selectedMount, setSelectedMount] = useState<string>('');

    /*
     * Default to the fullest real filesystem, which is the one the
     * alerter's disk threshold reports on, and fall back to the new
     * default whenever the connection changes or the selected mount
     * disappears from the list.
     */
    useEffect(() => {
        setSelectedMount((current) => {
            if (current !== '' && mountNames.includes(current)) {
                return current;
            }
            return mountNames[0] ?? '';
        });
    }, [mountNames]);

    const handleMountChange = useCallback((event: SelectChangeEvent): void => {
        setSelectedMount(event.target.value);
    }, []);

    const diskLabelSuffix = selectedMount ? ` (${selectedMount})` : '';
    const diskChartTitle = `Disk Space Over Time${diskLabelSuffix}`;

    /*
     * The mount lookup gates both disk queries: without a mount the
     * server would fall back to averaging every mounted filesystem,
     * which is the very figure this section no longer reports. The
     * panel therefore stays in its loading state whilst the lookup is
     * in flight, and shows the empty state only once it has settled
     * with no real filesystem to chart.
     */
    const diskMountPending = !mountsSettled && selectedMount === '';
    const diskMountMissing = mountsSettled && selectedMount === '';
    const diskEmptyMessage = diskMountMissing
        ? 'No real filesystem reported for this server.'
        : 'No disk data available. Is the system_stats extension installed?';

    // KPI sparkline queries (30 buckets)
    const cpuKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_cpu_usage_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['usermode_normal_process_percent', 'kernelmode_process_percent',
            'idle_mode_percent'],
    }), [connectionId, timeRange.range]);

    const memoryKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_memory_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['used_memory', 'total_memory'],
    }), [connectionId, timeRange.range]);

    const diskKpiParams = useMemo((): MetricQueryParams | null => (
        selectedMount === '' ? null : {
            probeName: 'pg_sys_disk_info',
            connectionId,
            timeRange: timeRange.range,
            buckets: KPI_BUCKETS,
            aggregation: 'avg',
            mountPoint: selectedMount,
            metrics: ['used_space', 'free_space', 'total_space'],
        }
    ), [connectionId, timeRange.range, selectedMount]);

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
        metrics: [
            'usermode_normal_process_percent',
            'kernelmode_process_percent',
            'io_completion_percent',
            'idle_mode_percent',
        ],
    }), [connectionId, timeRange.range]);

    const memoryChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_memory_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['used_memory', 'free_memory', 'cache_total'],
    }), [connectionId, timeRange.range]);

    const diskChartParams = useMemo((): MetricQueryParams | null => (
        selectedMount === '' ? null : {
            probeName: 'pg_sys_disk_info',
            connectionId,
            timeRange: timeRange.range,
            buckets: CHART_BUCKETS,
            aggregation: 'avg',
            mountPoint: selectedMount,
            metrics: ['used_space', 'free_space'],
        }
    ), [connectionId, timeRange.range, selectedMount]);

    const loadChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_load_avg_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'load_avg_one_minute',
            'load_avg_five_minutes',
            'load_avg_ten_minutes',
            'load_avg_fifteen_minutes',
        ],
    }), [connectionId, timeRange.range]);

    const networkChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_sys_network_info',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['tx_bytes_per_sec', 'rx_bytes_per_sec'],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const cpuKpi = useMetrics(cpuKpiParams);
    const memoryKpi = useMetrics(memoryKpiParams);
    const diskKpi = useMetrics(diskKpiParams);
    const loadKpi = useMetrics(loadKpiParams);

    // Fetch chart data
    const cpuChart = useMetrics(cpuChartParams);
    const memoryChart = useMetrics(memoryChartParams);
    const diskChart = useMetrics(diskChartParams);
    const loadChart = useMetrics(loadChartParams);
    const networkChart = useMetrics(networkChartParams);

    // Check if system_stats probes have real data. CPU idle will
    // always have non-zero values if the extension is installed.
    const hasSystemStats = useMemo(() => {
        return hasNonZeroData(cpuKpi.data, 'idle_mode_percent');
    }, [cpuKpi.data]);

    // Extract current values for KPI tiles
    const cpuUser = extractLatestValue(
        cpuKpi.data, 'usermode_normal_process_percent'
    );
    const cpuSystem = extractLatestValue(
        cpuKpi.data, 'kernelmode_process_percent'
    );
    const cpuIdle = extractLatestValue(cpuKpi.data, 'idle_mode_percent');
    const cpuUsage = useMemo(() => {
        if (!hasSystemStats) { return null; }
        if (cpuIdle !== null) { return 100 - cpuIdle; }
        if (cpuUser !== null || cpuSystem !== null) {
            return (cpuUser ?? 0) + (cpuSystem ?? 0);
        }
        return null;
    }, [cpuUser, cpuSystem, cpuIdle, hasSystemStats]);

    const usedMemory = extractLatestValue(memoryKpi.data, 'used_memory');
    const totalMemory = extractLatestValue(memoryKpi.data, 'total_memory');
    const memoryUsagePercent = useMemo(() => {
        if (!hasSystemStats) { return null; }
        if (usedMemory !== null && totalMemory !== null && totalMemory > 0) {
            return (usedMemory / totalMemory) * 100;
        }
        return null;
    }, [usedMemory, totalMemory, hasSystemStats]);

    const usedSpace = extractLatestValue(diskKpi.data, 'used_space');
    const totalSpace = extractLatestValue(diskKpi.data, 'total_space');

    /*
     * The percentage comes from the reported total rather than from
     * used plus free, because a filesystem with reserved blocks holds
     * back space that is counted in neither, and the two figures then
     * disagree.
     */
    const diskUsagePercent = useMemo(() => {
        if (!hasSystemStats) { return null; }
        if (usedSpace !== null && totalSpace !== null && totalSpace > 0) {
            return (usedSpace / totalSpace) * 100;
        }
        return null;
    }, [usedSpace, totalSpace, hasSystemStats]);

    const loadValue = extractLatestValue(loadKpi.data, 'load_avg_one_minute');

    // Build chart datasets
    const cpuChartData = useMemo(
        () => buildChartData(
            cpuChart.data,
            [
                'usermode_normal_process_percent',
                'kernelmode_process_percent',
                'io_completion_percent',
                'idle_mode_percent',
            ],
            ['User', 'System', 'I/O Wait', 'Idle'],
            cpuChart.window,
        ),
        [cpuChart.data, cpuChart.window]
    );

    const memoryChartData = useMemo(
        () => buildChartData(
            memoryChart.data,
            ['used_memory', 'free_memory', 'cache_total'],
            ['Used', 'Free', 'Cached'],
            memoryChart.window,
        ),
        [memoryChart.data, memoryChart.window]
    );

    const diskChartData = useMemo(
        () => buildChartData(
            diskChart.data,
            ['used_space', 'free_space'],
            ['Used', 'Free'],
            diskChart.window,
        ),
        [diskChart.data, diskChart.window]
    );

    const loadChartData = useMemo(
        () => buildChartData(
            loadChart.data,
            [
                'load_avg_one_minute',
                'load_avg_five_minutes',
                'load_avg_ten_minutes',
                'load_avg_fifteen_minutes',
            ],
            ['1 min', '5 min', '10 min', '15 min'],
            loadChart.window,
        ),
        [loadChart.data, loadChart.window]
    );

    const networkChartData = useMemo(
        () => buildChartData(
            networkChart.data,
            ['tx_bytes_per_sec', 'rx_bytes_per_sec'],
            ['TX Bytes/s', 'RX Bytes/s'],
            networkChart.window,
        ),
        [networkChart.data, networkChart.window]
    );

    const isKpiLoading = cpuKpi.loading || memoryKpi.loading
        || diskKpi.loading || loadKpi.loading;

    // Determine if initial data load is complete. We only force collapse
    // after loading finishes to avoid flickering during the initial fetch.
    const initialLoadComplete = !cpuKpi.loading && cpuKpi.data !== null;

    // Force collapse the section when all system resource data is unavailable
    // (typically when system_stats extension is not installed). Only apply
    // this after initial load completes to avoid premature collapse.
    const forceCollapsed = initialLoadComplete && !hasSystemStats;

    return (
        <CollapsibleSection
            title="System Resources"
            icon={<ComputerIcon sx={{ fontSize: 16 }} />}
            defaultExpanded
            forceCollapsed={forceCollapsed}
            forceCollapsedMessage="No data available. Is the system_stats extension installed?"
        >
            {isKpiLoading && !cpuKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} aria-label="Loading" />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="CPU Usage"
                    value={formatValue(cpuUsage)}
                    unit="%"
                    status={getPercentageStatus(cpuUsage)}
                    sparklineData={extractSparklineData(
                        cpuKpi.data,
                        'usermode_normal_process_percent'
                    )}
                    analysisContext={{
                        metricDescription: 'CPU usage percentage over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Memory Usage"
                    value={!hasSystemStats ? '--'
                        : memoryUsagePercent !== null
                            ? formatValue(memoryUsagePercent)
                            : formatBytes(usedMemory)}
                    unit={!hasSystemStats ? undefined
                        : memoryUsagePercent !== null ? '%' : undefined}
                    status={!hasSystemStats ? undefined
                        : getPercentageStatus(memoryUsagePercent)}
                    sparklineData={extractSparklineData(
                        memoryKpi.data, 'used_memory'
                    )}
                    analysisContext={{
                        metricDescription: 'Memory usage over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label={`Disk Usage${diskLabelSuffix}`}
                    value={!hasSystemStats ? '--'
                        : diskUsagePercent !== null
                            ? formatValue(diskUsagePercent)
                            : formatBytes(usedSpace)}
                    unit={!hasSystemStats ? undefined
                        : diskUsagePercent !== null ? '%' : undefined}
                    status={!hasSystemStats ? undefined
                        : getPercentageStatus(diskUsagePercent)}
                    sparklineData={extractSparklineData(
                        diskKpi.data, 'used_space'
                    )}
                    analysisContext={{
                        metricDescription: `Disk space usage over time for ${selectedMount || 'the selected filesystem'}`,
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Load Average"
                    value={!hasSystemStats ? '--' : formatValue(loadValue, 2)}
                    sparklineData={extractSparklineData(
                        loadKpi.data, 'load_avg_one_minute'
                    )}
                    analysisContext={{
                        metricDescription: 'System load average (1 minute) over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    <ChartPanel
                        title="CPU Usage Over Time"
                        loading={cpuChart.loading && !cpuChartData}
                        hasData={hasSystemStats && !!cpuChartData}
                        emptyMessage="No CPU data available. Is the system_stats extension installed?"
                        errorMessage={cpuChart.error}
                        height={CHART_HEIGHT}
                    >
                        {cpuChartData && (
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
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'CPU usage breakdown showing user, system, I/O wait, and idle percentages',
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
                        title="Memory Usage Over Time"
                        loading={memoryChart.loading && !memoryChartData}
                        hasData={hasSystemStats && !!memoryChartData}
                        emptyMessage="No memory data available. Is the system_stats extension installed?"
                        errorMessage={memoryChart.error}
                        height={CHART_HEIGHT}
                    >
                        {memoryChartData && (
                            <Chart
                                type="line"
                                data={memoryChartData}
                                title="Memory Usage Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                areaFill
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Memory usage showing used, free, and cached memory',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    {mountNames.length > 1 && (
                        <Box sx={MOUNT_SELECT_BAR_SX}>
                            <FormControl size="small" sx={MOUNT_SELECT_SX}>
                                <InputLabel id="disk-mount-select-label">
                                    Filesystem
                                </InputLabel>
                                <Select
                                    labelId="disk-mount-select-label"
                                    id="disk-mount-select"
                                    label="Filesystem"
                                    value={selectedMount}
                                    onChange={handleMountChange}
                                >
                                    {mountNames.map(name => (
                                        <MenuItem key={name} value={name}>
                                            {name}
                                        </MenuItem>
                                    ))}
                                </Select>
                            </FormControl>
                        </Box>
                    )}
                    <ChartPanel
                        title={diskChartTitle}
                        loading={diskMountPending
                            || (diskChart.loading && !diskChartData)}
                        hasData={hasSystemStats && !!diskChartData}
                        emptyMessage={diskEmptyMessage}
                        errorMessage={diskChart.error}
                        height={CHART_HEIGHT}
                    >
                        {diskChartData && (
                            <Chart
                                type="line"
                                data={diskChartData}
                                title={diskChartTitle}
                                height={CHART_HEIGHT}
                                smooth
                                areaFill
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: `Disk space usage showing used and free space for ${selectedMount || 'the selected filesystem'}`,
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
                        title="Load Average Over Time"
                        loading={loadChart.loading && !loadChartData}
                        hasData={hasSystemStats && !!loadChartData}
                        emptyMessage="No load average data available. Is the system_stats extension installed?"
                        errorMessage={loadChart.error}
                        height={CHART_HEIGHT}
                    >
                        {loadChartData && (
                            <Chart
                                type="line"
                                data={loadChartData}
                                title="Load Average Over Time"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'System load average over 1, 5, 10, and 15 minute intervals',
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
                        title="Network I/O"
                        loading={networkChart.loading && !networkChartData}
                        hasData={hasSystemStats && !!networkChartData}
                        emptyMessage="No network data available. Is the system_stats extension installed?"
                        errorMessage={networkChart.error}
                        height={CHART_HEIGHT}
                    >
                        {networkChartData && (
                            <Chart
                                type="line"
                                data={networkChartData}
                                title="Network I/O"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Network throughput showing transmitted and received bytes per second',
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

export default SystemResourcesSection;
