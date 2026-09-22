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
import { useMemo, useState, useCallback, useEffect, useRef } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/useClusterData';
import { useDashboard } from '../../../contexts/useDashboard';
import {
    appendTimeRangeParams,
    isTimeRangeQueryable,
} from '../../../utils/timeRangeParams';
import { useRequestSequence } from '../../../hooks/useRequestSequence';
import { Chart } from '../../Chart';
import { CHART_SECTION_SX } from '../styles';
import { logger } from '../../../utils/logger';

interface ComparativeChartsSectionProps {
    serverIds: number[];
}

interface ConnectionMetrics {
    connectionName: string;
    /** Percentage, or null when the latest interval saw no block access. */
    cacheHitRatio: number | null;
    commitsPerSec: number;
    rollbackPercent: number;
    activeConnections: number;
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
    // These charts render inside the Monitoring section alongside the
    // time selector, so they follow the selected window.
    const { timeRange } = useDashboard();
    const { range, customStart, customEnd } = timeRange;
    const [metrics, setMetrics] = useState<ConnectionMetrics[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    // Orders overlapping fetches so that a response for a superseded
    // window cannot overwrite the metrics for the current one; see
    // useRequestSequence for why a mounted flag alone cannot do this.
    const { beginRequest, supersedeRequest } = useRequestSequence();
    const initialLoadDoneRef = useRef<boolean>(false);
    const serverIdsKey = serverIds.join(',');

    const fetchMetrics = useCallback(async (): Promise<void> => {
        if (!user || serverIds.length === 0) { return; }

        /*
         * A custom range without both bounds is a transient state the
         * server rejects with a 400, so skip the request entirely and
         * leave whatever data and error state is already in place.
         */
        const selectedWindow = { range, customStart, customEnd };
        if (!isTimeRangeQueryable(selectedWindow)) {
            // Abandon any request still in flight for the previous
            // window, so its response cannot land and be shown as
            // though it described the newly selected one.
            supersedeRequest();
            setLoading(false);
            return;
        }

        const params = new URLSearchParams({
            connection_ids: serverIds.join(','),
        });
        appendTimeRangeParams(params, selectedWindow);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        // A response is only applied if the component is still mounted
        // and no newer request has been started since.
        const isCurrent = beginRequest();

        try {
            const response = await apiFetch(
                `/api/v1/metrics/performance-summary?${params.toString()}`,
            );

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({})) as { error?: string };
                throw new Error(errorData.error || `Failed to fetch data: ${response.status}`);
            }

            // Checked before parsing as well as after, so that a
            // response for a superseded window is not parsed at all,
            // and so that a request started during the parse wins.
            if (isCurrent()) {
                const data = await response.json();
                const connections = data.connections || [];

                const parsed: ConnectionMetrics[] = connections.map(
                    (conn: Record<string, unknown>) => {
                        const txns = conn.transactions as Record<string, unknown> | undefined;
                        const cache = conn.cache_hit_ratio as Record<string, unknown> | undefined;

                        return {
                            connectionName: conn.connection_name as string || `Server ${conn.connection_id}`,
                            connectionId: conn.connection_id as number,
                            // The server already reports a percentage; a
                            // null current (no block access) stays null so
                            // the bar is left empty rather than drawn at 0.
                            cacheHitRatio: cache && typeof cache.current === 'number'
                                ? Math.round(cache.current * 100) / 100
                                : null,
                            commitsPerSec: txns && typeof txns.commits_per_sec === 'number'
                                ? Math.round(txns.commits_per_sec * 100) / 100
                                : 0,
                            rollbackPercent: txns && typeof txns.rollback_percent === 'number'
                                ? Math.round(txns.rollback_percent * 100) / 100
                                : 0,
                            activeConnections:
                                typeof conn.active_connections === 'number'
                                    ? conn.active_connections
                                    : 0,
                        };
                    }
                );

                if (isCurrent()) {
                    setMetrics(parsed);
                    initialLoadDoneRef.current = true;
                }
            }
        } catch (err) {
            logger.error('Error fetching comparative metrics:', err);
            if (isCurrent()) {
                setError((err as Error).message || 'Failed to fetch metrics');
            }
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
    }, [
        user, serverIds, range, customStart, customEnd,
        beginRequest, supersedeRequest,
    ]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [serverIdsKey]);

    useEffect(() => {
        if (user && serverIds.length > 0) {
            void fetchMetrics();
        }
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
            data: metrics.map(m => m.activeConnections),
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
