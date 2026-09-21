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
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/useClusterData';
import { useDashboard } from '../../../contexts/useDashboard';
import {
    appendTimeRangeParams,
    isTimeRangeQueryable,
} from '../../../utils/timeRangeParams';
import KpiTile from '../KpiTile';
import { formatNumber } from '../../../utils/formatters';
import { KPI_GRID_SX } from '../styles';
import { countEstateServers } from '../../../utils/clusterHelpers';
import { logger } from '../../../utils/logger';
import { useRetryingFetch } from '../../../hooks/useRetryingFetch';
import type { EstateSelection } from '../../../types/selection';

interface KpiTilesSectionProps {
    selection: EstateSelection;
    serverIds: number[];
}

interface PerformanceAggregate {
    totalServers: number;
    totalConnections: number;
    transactionRate: number;
    alertCount: number;
}

const LOADING_CONTAINER_SX = {
    display: 'flex',
    justifyContent: 'center',
    py: 3,
};

const ERROR_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 2,
};

/**
 * KpiTilesSection shows fleet-wide KPI tiles for the estate:
 * total servers, total connections, transaction rate, and
 * alert count. Fetches aggregate data from the performance
 * summary and alerts endpoints.
 */
const KpiTilesSection: React.FC<KpiTilesSectionProps> = ({ selection, serverIds }) => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    // These tiles render inside the Monitoring section alongside the
    // time selector, so they follow the selected window.
    const { timeRange } = useDashboard();
    const { range, customStart, customEnd } = timeRange;
    const [aggregate, setAggregate] = useState<PerformanceAggregate | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const { run, retrying } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: !!user && serverIds.length > 0,
    });

    const totalServers = useMemo(() => countEstateServers(selection), [selection]);
    const serverIdsKey = serverIds.join(',');

    const fetchAggregateData = useCallback(async (): Promise<boolean> => {
        if (!user || serverIds.length === 0) { return true; }

        /*
         * A custom range without both bounds is a transient state the
         * server rejects with a 400, so skip the request entirely and
         * leave whatever data and error state is already in place.
         * Reported as a success so the retry controller does not
         * reschedule a fetch that is not actually failing.
         */
        const selectedWindow = { range, customStart, customEnd };
        if (!isTimeRangeQueryable(selectedWindow)) { return true; }

        const perfParams = new URLSearchParams({
            connection_ids: serverIds.join(','),
        });
        appendTimeRangeParams(perfParams, selectedWindow);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const [perfResponse, alertsResponse] = await Promise.all([
                apiFetch(
                    '/api/v1/metrics/performance-summary'
                    + `?${perfParams.toString()}`,
                ),
                apiFetch(
                    '/api/v1/alerts?exclude_cleared=true&limit=200',
                ),
            ]);

            if (!isMountedRef.current) { return true; }

            // A non-OK response is a real failure. Surface it so the
            // retry controller reschedules the fetch instead of silently
            // rendering zero/partial KPI data as if the load succeeded.
            if (!perfResponse.ok || !alertsResponse.ok) {
                if (isMountedRef.current) {
                    setError('Failed to fetch KPI data');
                }
                return false;
            }

            const perfData = await perfResponse.json();
            const alertsData = await alertsResponse.json();

            let totalConnections = 0;
            let transactionRate = 0;
            const connections = perfData.connections || [];

            connections.forEach((conn: Record<string, unknown>) => {
                totalConnections += 1;
                const txns = conn.transactions as Record<string, unknown> | undefined;
                if (txns && typeof txns.commits_per_sec === 'number') {
                    transactionRate += txns.commits_per_sec;
                }
            });

            const alertCount = (alertsData.alerts || []).length;

            // A late resolution after unmount must not touch state; a
            // superseded/unmounted attempt is not a failure, so report
            // success to keep it out of the retry schedule.
            if (!isMountedRef.current) { return true; }

            setAggregate({
                totalServers,
                totalConnections,
                transactionRate: Math.round(transactionRate * 100) / 100,
                alertCount,
            });

            initialLoadDoneRef.current = true;
            return true;
        } catch (err) {
            logger.error('Error fetching estate KPI data:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch KPI data');
            }
            return false;
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, serverIds, totalServers, range, customStart, customEnd]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [serverIdsKey]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user && serverIds.length > 0) {
            void run(fetchAggregateData);
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, serverIds.length, run, fetchAggregateData, lastRefresh]);

    if (loading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_CONTAINER_SX}>
                <CircularProgress size={28} aria-label="Loading" />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography sx={ERROR_SX} role="status">
                {retrying ? 'Reconnecting…' : error}
            </Typography>
        );
    }

    const data = aggregate ?? {
        totalServers,
        totalConnections: 0,
        transactionRate: 0,
        alertCount: 0,
    };

    return (
        <Box sx={KPI_GRID_SX}>
            <KpiTile
                label="Total Servers"
                value={formatNumber(data.totalServers)}
                status="good"
            />
            <KpiTile
                label="Total Connections"
                value={formatNumber(data.totalConnections)}
            />
            <KpiTile
                label="Transaction Rate"
                value={formatNumber(Math.round(data.transactionRate * 100) / 100)}
                unit="tx/s"
            />
            <KpiTile
                label="Active Alerts"
                value={formatNumber(data.alertCount)}
                status={data.alertCount > 0 ? 'warning' : 'good'}
            />
        </Box>
    );
};

export default KpiTilesSection;
