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
import { useTheme } from '@mui/material/styles';
import { Chart } from '../../Chart';
import { useAuth } from '../../../contexts/AuthContext';

interface HealthOverviewSectionProps {
    selection: Record<string, unknown>;
}

interface ServerCounts {
    online: number;
    warning: number;
    offline: number;
}

interface AlertCounts {
    critical: number;
    warning: number;
    info: number;
}

const RINGS_CONTAINER_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 2,
    '@media (max-width: 600px)': {
        gridTemplateColumns: '1fr',
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
 * Compute server status counts from the estate selection data.
 */
const computeServerCounts = (selection: Record<string, unknown>): ServerCounts => {
    const counts: ServerCounts = { online: 0, warning: 0, offline: 0 };
    const groups = selection.groups as Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const collectServers = (servers: Array<Record<string, unknown>> | undefined): void => {
                servers?.forEach(s => {
                    const status = s.status as string;
                    const alertCount = s.active_alert_count as number | undefined;
                    if (status === 'offline') {
                        counts.offline += 1;
                    } else if (alertCount && alertCount > 0) {
                        counts.warning += 1;
                    } else {
                        counts.online += 1;
                    }
                    if (s.children) {
                        collectServers(s.children as Array<Record<string, unknown>>);
                    }
                });
            };
            collectServers(cluster.servers as Array<Record<string, unknown>> | undefined);
        });
    });

    return counts;
};

/**
 * HealthOverviewSection shows fleet health via two donut charts:
 * one for server status distribution (online/warning/offline) and
 * one for alert severity distribution (critical/warning/info).
 */
const HealthOverviewSection: React.FC<HealthOverviewSectionProps> = ({ selection }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const [alertCounts, setAlertCounts] = useState<AlertCounts>({ critical: 0, warning: 0, info: 0 });
    const [alertsLoading, setAlertsLoading] = useState<boolean>(false);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const serverCounts = useMemo(() => computeServerCounts(selection), [selection]);

    const fetchAlertCounts = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        if (!initialLoadDoneRef.current) {
            setAlertsLoading(true);
        }

        try {
            const response = await fetch('/api/v1/alerts?exclude_cleared=true&limit=200', {
                credentials: 'include',
            });

            if (response.ok && isMountedRef.current) {
                const data = await response.json();
                const alerts = data.alerts || [];
                const counts: AlertCounts = { critical: 0, warning: 0, info: 0 };

                alerts.forEach((alert: Record<string, unknown>) => {
                    const severity = (alert.severity as string || '').toLowerCase();
                    if (severity === 'critical') {
                        counts.critical += 1;
                    } else if (severity === 'warning') {
                        counts.warning += 1;
                    } else {
                        counts.info += 1;
                    }
                });

                setAlertCounts(counts);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching alert counts:', err);
        } finally {
            if (isMountedRef.current) {
                setAlertsLoading(false);
            }
        }
    }, [user]);

    useEffect(() => {
        isMountedRef.current = true;
        fetchAlertCounts();

        return () => {
            isMountedRef.current = false;
        };
    }, [fetchAlertCounts]);

    const serverStatusData = useMemo(() => ({
        categories: ['Online', 'Warning', 'Offline'],
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
