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
import { MetricQueryParams } from '../types';
import { formatLag } from '../../../utils/formatters';
import KpiTile from '../KpiTile';
import { Chart } from '../../Chart';
import { KPI_GRID_SX } from '../styles';

interface ReplicationLagSectionProps {
    selection: Record<string, unknown>;
    serverIds: number[];
}

const LOADING_SX = {
    display: 'flex',
    justifyContent: 'center',
    py: 3,
};

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 3,
};

const CHART_CONTAINER_SX = {
    mt: 2,
};

/**
 * Find the primary server ID from the cluster selection.
 * Returns the first server that has a primary or master role.
 */
const findPrimaryServerId = (selection: Record<string, unknown>): number | null => {
    const servers = selection.servers as Array<Record<string, unknown>> | undefined;
    if (!servers) { return null; }
    const visited = new Set<number>();

    const findPrimary = (serverList: Array<Record<string, unknown>>): number | null => {
        for (const s of serverList) {
            const id = s.id as number;
            if (visited.has(id)) { continue; }
            visited.add(id);

            const role = (s.primary_role || s.role || '') as string;
            if (
                role === 'binary_primary' ||
                role === 'logical_publisher' ||
                role === 'spock_node'
            ) {
                return id;
            }
            if (s.children) {
                const found = findPrimary(s.children as Array<Record<string, unknown>>);
                if (found !== null) { return found; }
            }
        }
        return null;
    };

    return findPrimary(servers);
};

/**
 * Format a metric name for display: "replay_lag" -> "Replay Lag".
 */
const formatMetricLabel = (metric: string): string => {
    return metric
        .split('_')
        .map(w => w.charAt(0).toUpperCase() + w.slice(1))
        .join(' ');
};

/**
 * ReplicationLagSection shows replication lag metrics queried from
 * the primary server. Displays KPI tiles for each lag metric
 * (replay_lag, write_lag, flush_lag) with current values and a
 * time-series chart showing all lag series over time.
 */
const ReplicationLagSection: React.FC<ReplicationLagSectionProps> = ({
    selection,
    serverIds: _serverIds,
}) => {
    const { timeRange } = useDashboard();

    const primaryServerId = useMemo(
        () => findPrimaryServerId(selection),
        [selection.servers],
    );

    const metricsParams = useMemo((): MetricQueryParams | null => {
        if (primaryServerId === null) { return null; }
        return {
            probeName: 'pg_stat_replication',
            connectionId: primaryServerId,
            timeRange: timeRange.range,
            buckets: 30,
        };
    }, [primaryServerId, timeRange.range]);

    const { data: metricsData, loading, error } = useMetrics(metricsParams);

    /* Build KPI values from the latest data point of each series. */
    const kpiItems = useMemo(() => {
        if (!metricsData) { return []; }
        return metricsData
            .filter(series =>
                series.data &&
                series.data.length > 0 &&
                (series.metric || series.name).includes('lag'),
            )
            .map(series => {
                const lastPoint = series.data[series.data.length - 1];
                return {
                    metric: series.metric || series.name,
                    label: formatMetricLabel(series.metric || series.name),
                    value: lastPoint.value,
                };
            });
    }, [metricsData]);

    /* Build chart data from all series. */
    const chartData = useMemo(() => {
        if (!metricsData || metricsData.length === 0) { return null; }
        const withData = metricsData.filter(
            s => s.data &&
                s.data.length > 0 &&
                (s.metric || s.name).includes('lag'),
        );
        if (withData.length === 0) { return null; }

        const categories = withData[0].data.map(d => d.time);
        const series = withData.map(s => ({
            name: formatMetricLabel(s.metric || s.name),
            data: s.data.map(d => d.value),
        }));
        return { categories, series };
    }, [metricsData]);

    if (primaryServerId === null && !loading) {
        return (
            <Typography sx={EMPTY_SX}>
                No primary server detected in this cluster.
            </Typography>
        );
    }

    if (loading && !metricsData) {
        return (
            <Box sx={LOADING_SX}>
                <CircularProgress size={28} aria-label="Loading replication data" />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography sx={EMPTY_SX}>{error}</Typography>
        );
    }

    if (kpiItems.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No replication lag data available. Lag metrics
                require active streaming replication.
            </Typography>
        );
    }

    return (
        <Box>
            <Box sx={KPI_GRID_SX}>
                {kpiItems.map(item => {
                    const status = item.value > 30
                        ? 'critical'
                        : item.value > 5
                            ? 'warning'
                            : 'good';
                    return (
                        <KpiTile
                            key={item.metric}
                            label={item.label}
                            value={formatLag(item.value)}
                            status={status as 'good' | 'warning' | 'critical'}
                        />
                    );
                })}
            </Box>
            {chartData && (
                <Box sx={CHART_CONTAINER_SX}>
                    <Chart
                        type="line"
                        data={chartData}
                        height={250}
                        smooth
                        showLegend
                        showTooltip
                        enableExport={false}
                        title="Replication Lag Over Time"
                        analysisContext={{
                            metricDescription: 'Replication lag across cluster members over time',
                            connectionId: primaryServerId ?? undefined,
                        }}
                        echartsOptions={{
                            yAxis: {
                                axisLabel: {
                                    formatter: (v: number) =>
                                        formatLag(v),
                                },
                            },
                            tooltip: {
                                valueFormatter: (v: number) =>
                                    formatLag(v),
                            },
                        }}
                    />
                </Box>
            )}
        </Box>
    );
};

export default ReplicationLagSection;
