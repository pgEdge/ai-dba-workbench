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
import { useTheme } from '@mui/material/styles';
import { Chart } from '../../Chart';
import { useAuth } from '../../../contexts/useAuth';
import { useClusterData } from '../../../contexts/useClusterData';
import { apiFetch } from '../../../utils/apiClient';
import { computeEstateServerCounts } from '../../../utils/clusterHelpers';
import { chunkConnectionIds } from '../../../utils/connectionIdBatches';
import { logger } from '../../../utils/logger';
import { useRetryingFetch } from '../../../hooks/useRetryingFetch';
import { useRequestSequence } from '../../../hooks/useRequestSequence';
import type { EstateSelection } from '../../../types/selection';

interface HealthOverviewSectionProps {
    selection: EstateSelection;
    serverIds: number[];
}

interface AlertCounts {
    critical: number;
    warning: number;
    info: number;
}

const SEVERITIES = ['critical', 'warning', 'info'] as const;

const NO_ALERTS: AlertCounts = { critical: 0, warning: 0, info: 0 };

const RINGS_CONTAINER_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
    gap: 2,
    '@media (max-width: 600px)': {
        gridTemplateColumns: 'minmax(0, 1fr)',
    },
};

const RING_LABEL_SX = {
    textAlign: 'center',
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.secondary',
    mt: 1,
};

const LOADING_CONTAINER_SX = {
    display: 'flex',
    justifyContent: 'center',
    py: 4,
};

/**
 * HealthOverviewSection shows fleet health via two donut charts:
 * one for server status distribution (online/warning/offline) and
 * one for the active alerts on the estate's servers by severity
 * (critical/warning/info). The alert counts refresh with the
 * estate data.
 */
const HealthOverviewSection: React.FC<HealthOverviewSectionProps> = ({ selection, serverIds }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [alertCounts, setAlertCounts] = useState<AlertCounts>(NO_ALERTS);
    const [alertsLoading, setAlertsLoading] = useState<boolean>(false);
    // useRetryingFetch checks its own attempt generation only after the
    // fetch has written state, so overlapping refreshes are ordered
    // here instead.
    const { beginRequest, supersedeRequest } = useRequestSequence();
    const initialLoadDoneRef = useRef<boolean>(false);
    const { run } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: !!user && serverIds.length > 0,
    });

    const serverCounts = useMemo(() => computeEstateServerCounts(selection), [selection]);
    const serverIdsKey = serverIds.join(',');

    const fetchAlertCounts = useCallback(async (): Promise<boolean> => {
        if (!user || serverIds.length === 0) { return true; }

        // The counts match the Active Alerts tile: status=active leaves
        // out acknowledged and cleared alerts, and naming the estate's
        // servers leaves out system alerts and other servers. Only each
        // response's total is read, so one row per page is enough and
        // the count is not capped by the page size. The batches are
        // disjoint, so their totals sum.
        const requests = chunkConnectionIds(serverIds).flatMap(batch =>
            SEVERITIES.map(severity => {
                const params = new URLSearchParams({
                    connection_ids: batch.join(','),
                    status: 'active',
                    severity,
                    limit: '1',
                });
                return {
                    severity,
                    url: `/api/v1/alerts?${params.toString()}`,
                };
            }),
        );

        if (!initialLoadDoneRef.current) {
            setAlertsLoading(true);
        }

        const isCurrent = beginRequest();

        try {
            const responses = await Promise.all(
                requests.map(req => apiFetch(req.url)),
            );

            // A superseded or unmounted attempt is not a failure.
            if (!isCurrent()) { return true; }

            // One failed request fails the refresh, so the chart keeps
            // its last complete distribution and the fetch is retried
            // rather than showing a partial one.
            if (responses.some(r => !r.ok)) { return false; }

            const bodies: { total?: unknown }[] = await Promise.all(
                responses.map(r => r.json()),
            );
            if (!isCurrent()) { return true; }

            const counts: AlertCounts = { ...NO_ALERTS };
            bodies.forEach((body, i) => {
                if (typeof body.total === 'number') {
                    counts[requests[i].severity] += body.total;
                }
            });

            setAlertCounts(counts);
            initialLoadDoneRef.current = true;
            return true;
        } catch (err) {
            logger.error('Error fetching alert counts:', err);
            return false;
        } finally {
            if (isCurrent()) {
                setAlertsLoading(false);
            }
        }
    }, [user, serverIds, beginRequest]);

    // A different set of servers is a fresh load; an estate with no
    // servers has no alerts to show.
    useEffect(() => {
        initialLoadDoneRef.current = false;
        if (serverIdsKey === '') {
            // Drop any request still in flight for the old servers.
            supersedeRequest();
            setAlertCounts(NO_ALERTS);
            setAlertsLoading(false);
        }
    }, [serverIdsKey, supersedeRequest]);

    // Refetch on mount, when the servers change, and on every estate
    // data refresh (lastRefresh), as the KPI tiles do.
    useEffect(() => {
        if (user && serverIds.length > 0) {
            void run(fetchAlertCounts);
        }
    }, [user, serverIds.length, run, fetchAlertCounts, lastRefresh]);

    const serverStatusData = useMemo(() => ({
        categories: ['OK', 'Warning', 'Offline'],
        series: [{
            name: 'Servers',
            data: [serverCounts.online, serverCounts.warning, serverCounts.offline],
        }],
    }), [serverCounts]);

    const alertDistData = useMemo(() => ({
        categories: ['Critical', 'Warning', 'Info'],
        series: [{
            name: 'Alerts',
            data: [alertCounts.critical, alertCounts.warning, alertCounts.info],
        }],
    }), [alertCounts]);

    const serverColors = useMemo(() => [
        theme.palette.success.main,
        theme.palette.warning.main,
        theme.palette.error.main,
    ], [theme]);

    const alertColors = useMemo(() => [
        theme.palette.error.main,
        theme.palette.warning.main,
        theme.palette.info.main,
    ], [theme]);

    const totalAlerts = alertCounts.critical + alertCounts.warning + alertCounts.info;

    if (alertsLoading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_CONTAINER_SX}>
                <CircularProgress size={32} aria-label="Loading health overview" />
            </Box>
        );
    }

    return (
        <Box sx={RINGS_CONTAINER_SX}>
            <Box>
                <Chart
                    type="donut"
                    data={serverStatusData}
                    height={200}
                    showLegend
                    showTooltip
                    enableExport={false}
                    colorPalette={serverColors}
                    showPercentage
                    analysisContext={{
                        metricDescription: 'Distribution of server health statuses across the estate',
                    }}
                />
                <Typography sx={RING_LABEL_SX}>
                    Server Status
                </Typography>
            </Box>
            <Box>
                {totalAlerts > 0 ? (
                    <Chart
                        type="donut"
                        data={alertDistData}
                        height={200}
                        showLegend
                        showTooltip
                        enableExport={false}
                        colorPalette={alertColors}
                        showPercentage
                        analysisContext={{
                            metricDescription: 'Distribution of alerts by severity across the estate',
                        }}
                    />
                ) : (
                    <Box sx={{
                        height: 200,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                    }}>
                        <Typography
                            sx={{
                                color: 'text.secondary',
                                fontSize: '0.875rem',
                            }}
                        >
                            No active alerts
                        </Typography>
                    </Box>
                )}
                <Typography sx={RING_LABEL_SX}>
                    Alert Distribution
                </Typography>
            </Box>
        </Box>
    );
};

export default HealthOverviewSection;
