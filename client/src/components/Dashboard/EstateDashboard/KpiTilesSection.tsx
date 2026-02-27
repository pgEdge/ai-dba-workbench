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
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';
import { useAuth } from '../../../contexts/AuthContext';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/ClusterDataContext';
import KpiTile from '../KpiTile';
import { formatNumber } from '../../../utils/formatters';
import { KPI_GRID_SX } from '../styles';

interface KpiTilesSectionProps {
    selection: Record<string, unknown>;
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
 * Count all servers across the estate selection hierarchy.
 */
const countServers = (selection: Record<string, unknown>): number => {
    let count = 0;
    const groups = selection.groups as Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const collectServers = (servers: Array<Record<string, unknown>> | undefined): void => {
                servers?.forEach(s => {
                    count += 1;
                    if (s.children) {
                        collectServers(s.children as Array<Record<string, unknown>>);
                    }
                });
            };
            collectServers(cluster.servers as Array<Record<string, unknown>> | undefined);
        });
    });

    return count;
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
    const [aggregate, setAggregate] = useState<PerformanceAggregate | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const totalServers = useMemo(() => countServers(selection), [selection]);
    const serverIdsKey = serverIds.join(',');

    const fetchAggregateData = useCallback(async (): Promise<void> => {
        if (!user || serverIds.length === 0) { return; }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const [perfResponse, alertsResponse] = await Promise.all([
                apiFetch(
                    `/api/v1/metrics/performance-summary?connection_ids=${serverIds.join(',')}&time_range=24h`,
                ),
                apiFetch(
                    '/api/v1/alerts?exclude_cleared=true&limit=200',
                ),
            ]);

            if (!isMountedRef.current) { return; }

            let totalConnections = 0;
            let transactionRate = 0;

            if (perfResponse.ok) {
                const perfData = await perfResponse.json();
                const connections = perfData.connections || [];

                connections.forEach((conn: Record<string, unknown>) => {
                    totalConnections += 1;
                    const txns = conn.transactions as Record<string, unknown> | undefined;
                    if (txns && typeof txns.commits_per_sec === 'number') {
                        transactionRate += txns.commits_per_sec;
                    }
                });
            }

            let alertCount = 0;
            if (alertsResponse.ok) {
                const alertsData = await alertsResponse.json();
                alertCount = (alertsData.alerts || []).length;
            }

            setAggregate({
                totalServers,
                totalConnections,
                transactionRate: Math.round(transactionRate * 100) / 100,
                alertCount,
            });

            initialLoadDoneRef.current = true;
        } catch (err) {
            console.error('Error fetching estate KPI data:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch KPI data');
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, serverIds, totalServers]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [serverIdsKey]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user && serverIds.length > 0) {
            fetchAggregateData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, serverIds.length, fetchAggregateData, lastRefresh]);

    if (loading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_CONTAINER_SX}>
                <CircularProgress size={28} aria-label="Loading" />
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

    const data = aggregate || {
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
