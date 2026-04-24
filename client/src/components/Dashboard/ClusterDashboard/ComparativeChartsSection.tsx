/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useState, useCallback, useEffect, useRef } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/useClusterData';
import { Chart } from '../../Chart';
import { CHART_SECTION_SX } from '../styles';
import { logger } from '../../../utils/logger';

interface ComparativeChartsSectionProps {
    serverIds: number[];
}

interface ConnectionMetrics {
    connectionName: string;
    cacheHitRatio: number;
    commitsPerSec: number;
    rollbackPercent: number;
    connectionId: number;
}

const LOADING_SX = {
    display: 'flex',
    justifyContent: 'center',
    py: 4,
};

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 4,
};

const ERROR_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 2,
};

/**
 * ComparativeChartsSection shows metrics compared across all
 * cluster members. Displays bar charts for transaction rate,
 * cache hit ratio, and rollback rate, with one bar per server
 * for easy comparison.
 */
const ComparativeChartsSection: React.FC<ComparativeChartsSectionProps> = ({ serverIds }) => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [metrics, setMetrics] = useState<ConnectionMetrics[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const serverIdsKey = serverIds.join(',');

    const fetchMetrics = useCallback(async (): Promise<void> => {
        if (!user || serverIds.length === 0) { return; }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await apiFetch(
                `/api/v1/metrics/performance-summary?connection_ids=${serverIds.join(',')}&time_range=24h`,
            );

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({})) as { error?: string };
                throw new Error(errorData.error || `Failed to fetch data: ${response.status}`);
            }

            if (isMountedRef.current) {
                const data = await response.json();
                const connections = data.connections || [];

                const parsed: ConnectionMetrics[] = connections.map(
                    (conn: Record<string, unknown>) => {
                        const txns = conn.transactions as Record<string, unknown> | undefined;
                        const cache = conn.cache_hit_ratio as Record<string, unknown> | undefined;

                        return {
                            connectionName: conn.connection_name as string || `Server ${conn.connection_id}`,
                            connectionId: conn.connection_id as number,
                            cacheHitRatio: cache && typeof cache.current === 'number'
                                ? Math.round(cache.current * 10000) / 100
                                : 0,
                            commitsPerSec: txns && typeof txns.commits_per_sec === 'number'
                                ? Math.round(txns.commits_per_sec * 100) / 100
                                : 0,
                            rollbackPercent: txns && typeof txns.rollback_percent === 'number'
                                ? Math.round(txns.rollback_percent * 100) / 100
                                : 0,
                        };
                    }
                );

                setMetrics(parsed);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching comparative metrics:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch metrics');
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, serverIds]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [serverIdsKey]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user && serverIds.length > 0) {
            fetchMetrics();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, serverIds.length, fetchMetrics, lastRefresh]);

    const serverNames = useMemo(
        () => metrics.map(m => m.connectionName),
        [metrics]
    );

    const txRateData = useMemo(() => ({
        categories: serverNames,
        series: [{
            name: 'Commits/sec',
            data: metrics.map(m => m.commitsPerSec),
        }],
    }), [serverNames, metrics]);

    const cacheHitData = useMemo(() => ({
        categories: serverNames,
        series: [{
            name: 'Cache Hit Ratio (%)',
            data: metrics.map(m => m.cacheHitRatio),
        }],
    }), [serverNames, metrics]);

    const rollbackData = useMemo(() => ({
        categories: serverNames,
        series: [{
            name: 'Rollback Rate (%)',
            data: metrics.map(m => m.rollbackPercent),
        }],
    }), [serverNames, metrics]);

    const connectionCountData = useMemo(() => ({
        categories: serverNames,
        series: [{
            name: 'Connections',
            data: metrics.map(() => 1),
        }],
    }), [serverNames, metrics]);

    if (loading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_SX}>
                <CircularProgress size={28} aria-label="Loading charts" />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography sx={ERROR_SX}>
                {error}
            </Typography>
        );
    }

    if (metrics.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No performance data available for comparison.
            </Typography>
        );
    }

    return (
        <Box sx={CHART_SECTION_SX}>
            <Chart
                type="bar"
                data={txRateData}
                height={220}
                showLegend={false}
                showTooltip
                enableExport={false}
                title="Transaction Rate (commits/sec)"
                analysisContext={{
                    metricDescription: 'Comparative transaction rates across cluster servers',
                }}
            />
            <Chart
                type="bar"
                data={cacheHitData}
                height={220}
                showLegend={false}
                showTooltip
                enableExport={false}
                title="Cache Hit Ratio (%)"
                analysisContext={{
                    metricDescription: 'Comparative cache hit ratios across cluster servers',
                }}
            />
            <Chart
                type="bar"
                data={rollbackData}
                height={220}
                showLegend={false}
                showTooltip
                enableExport={false}
                title="Rollback Rate (%)"
                analysisContext={{
                    metricDescription: 'Comparative rollback rates across cluster servers',
                }}
            />
            <Chart
                type="bar"
                data={connectionCountData}
                height={220}
                showLegend={false}
                showTooltip
                enableExport={false}
                title="Connection Count"
                analysisContext={{
                    metricDescription: 'Comparative connection counts across cluster servers',
                }}
            />
        </Box>
    );
};

export default ComparativeChartsSection;
