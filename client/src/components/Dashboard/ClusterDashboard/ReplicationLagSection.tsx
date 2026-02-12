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

    const findPrimary = (serverList: Array<Record<string, unknown>>): number | null => {
        for (const s of serverList) {
            const role = ((s.primary_role || s.role || '') as string).toLowerCase();
            if (role === 'primary' || role === 'master' || role === 'writer') {
                return s.id as number;
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
 * Identify standby servers from the cluster selection.
 */
const findStandbyServers = (
    selection: Record<string, unknown>
): Array<{ id: number; name: string }> => {
    const standbys: Array<{ id: number; name: string }> = [];
    const servers = selection.servers as Array<Record<string, unknown>> | undefined;

    const collect = (serverList: Array<Record<string, unknown>> | undefined): void => {
        serverList?.forEach(s => {
            const role = ((s.primary_role || s.role || '') as string).toLowerCase();
            if (
                role === 'standby' ||
                role === 'replica' ||
                role === 'subscriber' ||
                role === 'reader'
            ) {
                standbys.push({
                    id: s.id as number,
                    name: s.name as string,
                });
            }
            if (s.children) {
                collect(s.children as Array<Record<string, unknown>>);
            }
        });
    };

    collect(servers);
    return standbys;
};

/**
 * Format a lag value from bytes to a human-readable string.
 */
const formatLagValue = (bytes: number): string => {
    if (bytes < 1024) {
        return `${bytes} B`;
    }
    if (bytes < 1024 * 1024) {
        return `${(bytes / 1024).toFixed(1)} KB`;
    }
    if (bytes < 1024 * 1024 * 1024) {
        return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    }
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
};

/**
 * ReplicationLagSection shows replication lag metrics for each
 * standby node in the cluster. Displays KPI tiles with current
 * lag values and a time-series chart with all standby lag
 * overlaid for comparison.
 */
const ReplicationLagSection: React.FC<ReplicationLagSectionProps> = ({
    selection,
    serverIds: _serverIds,
}) => {
    const { timeRange } = useDashboard();

    const primaryServerId = useMemo(() => findPrimaryServerId(selection), [selection]);
    const standbys = useMemo(() => findStandbyServers(selection), [selection]);

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

    const lagValues = useMemo((): Record<string, number> => {
        const values: Record<string, number> = {};
        if (!metricsData) { return values; }

        metricsData.forEach((series: MetricSeries) => {
            if (series.data && series.data.length > 0) {
                const lastPoint = series.data[series.data.length - 1];
                values[series.name] = lastPoint.value;
            }
        });

        return values;
    }, [metricsData]);

    const chartData = useMemo(() => {
        if (!metricsData || metricsData.length === 0) { return null; }

        const firstSeries = metricsData[0];
        if (!firstSeries || !firstSeries.data || firstSeries.data.length === 0) {
            return null;
        }

        const categories = firstSeries.data.map(d => d.time);
        const series = metricsData.map(s => ({
            name: s.name,
            data: s.data.map(d => d.value),
        }));

        return { categories, series };
    }, [metricsData]);

    if (standbys.length === 0 && !loading) {
        return (
            <Typography sx={EMPTY_SX}>
                No standby servers detected in this cluster.
            </Typography>
        );
    }

    if (loading && !metricsData) {
        return (
            <Box sx={LOADING_SX}>
                <CircularProgress size={28} />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography sx={EMPTY_SX}>
                {error}
            </Typography>
        );
    }

    return (
        <Box>
            <Box sx={KPI_GRID_SX}>
                {standbys.map(standby => {
                    const lagBytes = lagValues[standby.name] ?? null;
                    const displayValue = lagBytes !== null
                        ? formatLagValue(lagBytes)
                        : 'N/A';
                    const status = lagBytes === null
                        ? undefined
                        : lagBytes > 100 * 1024 * 1024
                            ? 'critical'
                            : lagBytes > 10 * 1024 * 1024
                                ? 'warning'
                                : 'good';

                    return (
                        <KpiTile
                            key={standby.id}
                            label={standby.name}
                            value={displayValue}
                            status={status as 'good' | 'warning' | 'critical' | undefined}
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
                        showToolbar={false}
                        title="Replication Lag Over Time"
                    />
                </Box>
            )}
        </Box>
    );
};

export default ReplicationLagSection;
