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
import Chip from '@mui/material/Chip';
import { alpha, useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/AuthContext';
import { useClusterData } from '../../../contexts/ClusterDataContext';

interface AlertSummarySectionProps {
    serverIds: number[];
}

interface AlertEntry {
    id: number;
    severity: string;
    title: string;
    description: string;
    serverName: string;
    triggeredAt: string;
}

interface GroupedAlerts {
    critical: AlertEntry[];
    warning: AlertEntry[];
    info: AlertEntry[];
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

const GROUP_HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mb: 1,
    mt: 1.5,
};

const GROUP_LABEL_SX = {
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.primary',
};

const ALERT_LIST_SX = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

const ALERT_TITLE_SX = {
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.primary',
    flex: 1,
};

const ALERT_SERVER_SX = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    fontWeight: 500,
};

const ALERT_TIME_SX = {
    fontSize: '0.75rem',
    color: 'text.disabled',
    flexShrink: 0,
};

/**
 * Format a timestamp as a relative time string.
 */
const formatRelativeTime = (dateStr: string): string => {
    const now = new Date();
    const then = new Date(dateStr);
    const diffMs = now.getTime() - then.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) { return 'just now'; }
    if (diffMins < 60) { return `${diffMins}m ago`; }
    if (diffHours < 24) { return `${diffHours}h ago`; }
    if (diffDays < 7) { return `${diffDays}d ago`; }
    return then.toLocaleDateString();
};

/**
 * AlertSummarySection shows active alerts for the cluster grouped
 * by severity. Fetches alerts filtered by the cluster's server
 * connection IDs and displays critical, warning, and info groups
 * with counts and details.
 */
const AlertSummarySection: React.FC<AlertSummarySectionProps> = ({ serverIds }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [grouped, setGrouped] = useState<GroupedAlerts>({
        critical: [],
        warning: [],
        info: [],
    });
    const [loading, setLoading] = useState<boolean>(false);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const serverIdsKey = serverIds.join(',');

    const fetchAlerts = useCallback(async (): Promise<void> => {
        if (!user || serverIds.length === 0) { return; }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }

        try {
            const response = await fetch(
                `/api/v1/alerts?connection_ids=${serverIds.join(',')}&exclude_cleared=true&limit=20`,
                { credentials: 'include' }
            );

            if (response.ok && isMountedRef.current) {
                const data = await response.json();
                const alerts = data.alerts || [];

                const groups: GroupedAlerts = {
                    critical: [],
                    warning: [],
                    info: [],
                };

                alerts.forEach((alert: Record<string, unknown>) => {
                    const entry: AlertEntry = {
                        id: alert.id as number,
                        severity: ((alert.severity as string) || 'info').toLowerCase(),
                        title: alert.title as string || 'Alert',
                        description: alert.description as string || '',
                        serverName: alert.server_name as string || 'Unknown',
                        triggeredAt: alert.triggered_at as string || '',
                    };

                    if (entry.severity === 'critical') {
                        groups.critical.push(entry);
                    } else if (entry.severity === 'warning') {
                        groups.warning.push(entry);
                    } else {
                        groups.info.push(entry);
                    }
                });

                setGrouped(groups);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching cluster alerts:', err);
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
            fetchAlerts();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, serverIds.length, fetchAlerts, lastRefresh]);

    const alertRowSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        py: 0.75,
        px: 1,
        borderRadius: 1,
        '&:hover': {
            bgcolor: theme.palette.mode === 'dark'
                ? alpha(theme.palette.grey[700], 0.3)
                : alpha(theme.palette.grey[100], 0.8),
        },
    }), [theme]);

    const totalAlerts = grouped.critical.length + grouped.warning.length + grouped.info.length;

    if (loading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_SX}>
                <CircularProgress size={28} />
            </Box>
        );
    }

    if (totalAlerts === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No active alerts for this cluster.
            </Typography>
        );
    }

    const renderAlertGroup = (
        label: string,
        alerts: AlertEntry[],
        chipColor: 'error' | 'warning' | 'info'
    ): React.ReactNode => {
        if (alerts.length === 0) { return null; }

        return (
            <Box key={label}>
                <Box sx={GROUP_HEADER_SX}>
                    <Chip
                        label={label}
                        size="small"
                        color={chipColor}
                        sx={{
                            height: 20,
                            fontSize: '0.7rem',
                            fontWeight: 700,
                            textTransform: 'uppercase',
                        }}
                    />
                    <Typography sx={GROUP_LABEL_SX}>
                        {alerts.length} alert{alerts.length !== 1 ? 's' : ''}
                    </Typography>
                </Box>
                <Box sx={ALERT_LIST_SX}>
                    {alerts.map(alert => (
                        <Box key={alert.id} sx={alertRowSx}>
                            <Typography sx={ALERT_TITLE_SX}>
                                {alert.title}
                            </Typography>
                            <Typography sx={ALERT_SERVER_SX}>
                                {alert.serverName}
                            </Typography>
                            <Typography sx={ALERT_TIME_SX}>
                                {alert.triggeredAt
                                    ? formatRelativeTime(alert.triggeredAt)
                                    : ''}
                            </Typography>
                        </Box>
                    ))}
                </Box>
            </Box>
        );
    };

    return (
        <Box>
            {renderAlertGroup('Critical', grouped.critical, 'error')}
            {renderAlertGroup('Warning', grouped.warning, 'warning')}
            {renderAlertGroup('Info', grouped.info, 'info')}
        </Box>
    );
};

export default AlertSummarySection;
