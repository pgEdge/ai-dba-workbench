/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Status Panel Component
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Main content panel showing status and alerts for the current selection
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useMemo, useCallback } from 'react';
import {
    Box,
    Typography,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Storage as ServerIcon,
    Warning as WarningIcon,
    Error as ErrorIcon,
    CheckCircle as HealthyIcon,
    Dns as ClusterIcon,
    Language as EstateIcon,
    MonitorHeart as MonitorHeartIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import { useClusterData } from '../../contexts/ClusterDataContext';
import { useDashboard } from '../../contexts/DashboardContext';
import { apiPost, apiGet, apiDelete } from '../../utils/apiClient';
import { collectServers } from '../../utils/clusterHelpers';
import EventTimeline from '../EventTimeline';
import BlackoutPanel from '../BlackoutPanel';
import AlertAnalysisDialog from '../AlertAnalysisDialog';
import ServerAnalysisDialog from '../ServerAnalysisDialog';
import { ServerAnalysisInput, hasCachedServerAnalysis } from '../../hooks/useServerAnalysis';
import AlertOverrideEditDialog from '../AlertOverrideEditDialog';
import BlackoutManagementDialog from '../BlackoutManagementDialog';
import AIOverview from '../AIOverview';
import {
    ServerDashboard,
    EstateDashboard,
    ClusterDashboard,
    DatabaseDashboard,
    ObjectDashboard,
    MetricOverlay,
} from '../Dashboard';
import CollapsibleSection from '../Dashboard/CollapsibleSection';
import TimeRangeSelector from '../Dashboard/TimeRangeSelector';
import SelectionHeader from './SelectionHeader';
import ServerInfoCard from './ServerInfoCard';
import MetricCard from './MetricCard';
import AlertsSection from './AlertsSection';
import PerformanceTiles from './PerformanceTiles';
import AcknowledgeDialog from './AcknowledgeDialog';
import {
    getStatusColors,
    EMPTY_STATE_CONTAINER_SX,
    EMPTY_STATE_TITLE_SX,
    EMPTY_STATE_DESC_SX,
    PANEL_ROOT_SX,
    METRICS_GRID_SX,
} from './styles';
import { StatusPanelProps } from './types';

/**
 * DashboardOverlayContent - Renders the appropriate dashboard
 * component based on the current overlay level. Used as the child
 * of MetricOverlay.
 */
const DashboardOverlayContent: React.FC = () => {
    const { currentOverlay } = useDashboard();

    if (!currentOverlay) {
        return null;
    }

    if (currentOverlay.level === 'database') {
        return (
            <DatabaseDashboard
                connectionId={currentOverlay.connectionId ?? 0}
                databaseName={currentOverlay.databaseName ?? currentOverlay.entityName}
            />
        );
    }

    if (currentOverlay.level === 'object' && currentOverlay.objectType) {
        return (
            <ObjectDashboard
                connectionId={currentOverlay.connectionId ?? 0}
                databaseName={currentOverlay.databaseName ?? ''}
                schemaName={currentOverlay.schemaName ?? 'public'}
                objectName={currentOverlay.objectName ?? currentOverlay.entityName}
                objectType={currentOverlay.objectType}
            />
        );
    }

    return null;
};

/**
 * StatusPanel - Main component showing status and alerts
 */
const StatusPanel: React.FC<StatusPanelProps> = ({
    selection,
    mode = 'light',
}) => {
    const theme = useTheme();
    const isDark = mode === 'dark';
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(false);
    const initialLoadDoneRef = React.useRef(false);
    const localAnalysisRef = React.useRef<Map<number, string>>(new Map());
    const [blackoutMgmtOpen, setBlackoutMgmtOpen] = useState(false);
    const [ackDialogOpen, setAckDialogOpen] = useState(false);
    const [selectedAlertForAck, setSelectedAlertForAck] = useState(null);
    const [analysisDialogOpen, setAnalysisDialogOpen] = useState(false);
    const [analysisAlert, setAnalysisAlert] = useState(null);
    const [overrideDialogOpen, setOverrideDialogOpen] = useState(false);
    const [overrideAlert, setOverrideAlert] = useState(null);
    const [serverAnalysisOpen, setServerAnalysisOpen] = useState(false);
    const { clearOverlays } = useDashboard();

    // Clear dashboard overlay stack when the selection changes
    useEffect(() => {
        clearOverlays();
    }, [selection?.type, selection?.id, clearOverlays]);

    const statusColors = useMemo(() => getStatusColors(theme), [theme]);

    const monitoringSx = useMemo(() => ({
        mt: 2,
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.background.paper, 0.4)
            : theme.palette.grey[100],
    }), [theme]);

    // Calculate metrics based on selection type
    const metrics = useMemo(() => {
        if (!selection) {return null;}

        if (selection.type === 'server') {
            const isOffline = selection.status === 'offline';
            const hasAlerts = selection.active_alert_count > 0;
            const effectiveStatus = isOffline ? 'offline' : (hasAlerts ? 'warning' : 'online');

            return {
                status: effectiveStatus,
                servers: { total: 1, online: effectiveStatus === 'online' ? 1 : 0 },
            };
        }

        if (selection.type === 'cluster') {
            const servers = selection.servers || [];
            const offline = servers.filter(s => s.status === 'offline').length;
            const warning = servers.filter(s => s.status !== 'offline' && s.active_alert_count > 0).length;
            const online = servers.filter(s => s.status !== 'offline' && !s.active_alert_count).length;

            return {
                status: offline === servers.length ? 'offline' : (warning > 0 || offline > 0 ? 'warning' : 'online'),
                servers: {
                    total: servers.length,
                    online,
                    warning,
                    offline,
                },
            };
        }

        if (selection.type === 'estate') {
            const allServers = [];
            selection.groups?.forEach(group => {
                group.clusters?.forEach(cluster => {
                    allServers.push(...collectServers(cluster.servers || []));
                });
            });

            const offline = allServers.filter(s => s.status === 'offline').length;
            const warning = allServers.filter(s => s.status !== 'offline' && s.active_alert_count > 0).length;
            const online = allServers.filter(s => s.status !== 'offline' && !s.active_alert_count).length;

            return {
                status: offline === allServers.length && allServers.length > 0 ? 'offline' : (warning > 0 || offline > 0 ? 'warning' : 'online'),
                servers: {
                    total: allServers.length,
                    online,
                    warning,
                    offline,
                },
                clusters: selection.groups?.reduce((acc, g) => acc + (g.clusters?.length || 0), 0) || 0,
                groups: selection.groups?.length || 0,
            };
        }

        return null;
    }, [selection]);

    // Format relative time from a date
    const formatRelativeTime = (date) => {
        if (!date) {return '';}
        const now = new Date();
        const then = new Date(date);
        const diffMs = now - then;
        const diffSecs = Math.floor(diffMs / 1000);
        const diffMins = Math.floor(diffSecs / 60);
        const diffHours = Math.floor(diffMins / 60);
        const diffDays = Math.floor(diffHours / 24);

        if (diffSecs < 60) {return 'just now';}
        if (diffMins < 60) {return `${diffMins} min ago`;}
        if (diffHours < 24) {return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;}
        if (diffDays < 7) {return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;}
        return then.toLocaleDateString();
    };

    // Transform API alerts to component format
    const transformAlerts = useCallback((apiAlerts) => {
        return apiAlerts.map(alert => ({
            id: alert.id,
            severity: alert.severity?.toLowerCase() || 'info',
            title: alert.title,
            description: alert.description,
            time: formatRelativeTime(alert.triggered_at),
            server: alert.server_name,
            connectionId: alert.connection_id,
            databaseName: alert.database_name,
            objectName: alert.object_name,
            // Threshold info
            alertType: alert.alert_type,
            ruleId: alert.rule_id,
            metricValue: alert.metric_value,
            metricUnit: alert.metric_unit,
            thresholdValue: alert.threshold_value,
            operator: alert.operator,
            // Acknowledgment info
            acknowledgedAt: alert.acknowledged_at,
            acknowledgedBy: alert.acknowledged_by,
            ackMessage: alert.ack_message,
            falsePositive: alert.false_positive,
            // AI analysis cache
            aiAnalysis: alert.ai_analysis,
            aiAnalysisMetricValue: alert.ai_analysis_metric_value,
        }));
    }, []);

    // Handle opening ack dialog
    const handleAcknowledge = (alert) => {
        setSelectedAlertForAck(alert);
        setAckDialogOpen(true);
    };

    // Handle opening analysis dialog
    const handleAnalyze = (alert) => {
        setAnalysisAlert(alert);
        setAnalysisDialogOpen(true);
    };

    // Update local alert state when AI analysis completes
    const handleAnalysisComplete = useCallback((alertId: number, analysis: string) => {
        localAnalysisRef.current.set(alertId, analysis);
        setAlerts(prev => prev.map(a =>
            a.id === alertId ? { ...a, aiAnalysis: analysis } : a
        ));
    }, []);

    // Handle opening override edit dialog
    const handleEditOverride = (alert) => {
        setOverrideAlert(alert);
        setOverrideDialogOpen(true);
    };

    // Handle opening server analysis dialog from AI Overview
    const handleServerAnalyze = useCallback(() => {
        setServerAnalysisOpen(true);
    }, []);

    const serverAnalysisSelection = useMemo((): ServerAnalysisInput | null => {
        if (!selection) return null;
        if (selection.type === 'server' && typeof selection.id === 'number') {
            return {
                type: 'server',
                id: selection.id,
                name: selection.name || `Server ${selection.id}`,
            };
        }
        if (selection.type === 'cluster' && selection.serverIds?.length) {
            const servers = (selection.servers || []).map(s => ({
                id: typeof s.id === 'number' ? s.id : parseInt(String(s.id), 10),
                name: s.name || s.server_name || `Server ${s.id}`,
            }));
            return {
                type: 'cluster',
                id: selection.id || selection.name || 'cluster',
                name: selection.name || 'Cluster',
                serverIds: selection.serverIds,
                servers,
            };
        }
        return null;
    }, [selection]);

    const serverAnalysisCached = serverAnalysisSelection
        ? hasCachedServerAnalysis(serverAnalysisSelection.type, serverAnalysisSelection.id)
        : false;

    // Handle confirming acknowledgment
    const handleAckConfirm = async (alertId, message, falsePositive = false) => {
        if (!user || !alertId) {return;}

        try {
            await apiPost('/api/v1/alerts/acknowledge', {
                alert_id: alertId,
                message: message || '',
                false_positive: falsePositive,
            });
            // Refresh alerts to show updated status
            fetchAlertsData();
        } catch (err) {
            console.error('Error acknowledging alert:', err);
        } finally {
            setAckDialogOpen(false);
            setSelectedAlertForAck(null);
        }
    };

    // Handle unacknowledging an alert
    const handleUnacknowledge = async (alertId) => {
        if (!user || !alertId) {return;}

        try {
            await apiDelete(`/api/v1/alerts/acknowledge?alert_id=${alertId}`);
            // Refresh alerts to show updated status
            fetchAlertsData();
        } catch (err) {
            console.error('Error unacknowledging alert:', err);
        }
    };

    // Fetch alerts data function
    const fetchAlertsData = useCallback(async () => {
        if (!user || !selection) {
            setAlerts([]);
            setLoading(false);
            return;
        }

        // For server selections, require a valid ID
        if (selection.type === 'server' && (selection.id === undefined || selection.id === null)) {
            console.warn('Server selection missing ID, skipping alert fetch');
            setAlerts([]);
            setLoading(false);
            return;
        }

        // Only show loading on initial fetch to prevent flashing (use ref to avoid re-renders)
        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }

        try {
            // Build query params based on selection type
            // Fetch active and acknowledged alerts, but exclude cleared ones
            let url = '/api/v1/alerts?limit=50&exclude_cleared=true';
            if (selection.type === 'server') {
                // Use explicit check for ID - must be a number (including 0)
                url += `&connection_id=${selection.id}`;
            } else if (selection.type === 'cluster' && selection.serverIds?.length) {
                // For cluster, filter by multiple connection IDs
                url += `&connection_ids=${selection.serverIds.join(',')}`;
            }
            // For estate, fetch all alerts (no connection filter)

            const data = await apiGet<{ alerts?: unknown[] }>(url);
            const transformedAlerts = transformAlerts(data.alerts || []);
            // Merge locally-cached AI analyses that the server
            // may not have persisted yet, then prune entries
            // the server has caught up with.
            const merged = transformedAlerts.map(a => {
                const local = a.id != null ? localAnalysisRef.current.get(a.id) : undefined;
                if (local && !a.aiAnalysis) {
                    return { ...a, aiAnalysis: local };
                }
                return a;
            });
            // Remove local entries that the server now has
            for (const a of merged) {
                if (a.id != null && a.aiAnalysis && localAnalysisRef.current.has(a.id)) {
                    localAnalysisRef.current.delete(a.id);
                }
            }
            setAlerts(merged);
            initialLoadDoneRef.current = true;
        } catch (err) {
            console.error('Error fetching alerts:', err);
            setAlerts([]);
        } finally {
            setLoading(false);
        }
    }, [user, selection, transformAlerts]);

    // Reset initial load state when selection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [selection?.type, selection?.id]);

    // Fetch alerts on selection change or cluster data refresh
    useEffect(() => {
        fetchAlertsData();
    }, [fetchAlertsData, lastRefresh]);

    // Count only active (non-acknowledged) alerts for the header indicator
    const activeAlertCount = useMemo(() => {
        return alerts.filter(a => !a.acknowledgedAt).length;
    }, [alerts]);

    const activeAlertSeverities = useMemo(() => {
        const active = alerts.filter(a => !a.acknowledgedAt);
        const counts: Record<string, number> = {};
        active.forEach(a => {
            const sev = a.severity || 'unknown';
            counts[sev] = (counts[sev] || 0) + 1;
        });
        return counts;
    }, [alerts]);

    const emptyStateIconBoxSx = useMemo(() => ({
        width: 80,
        height: 80,
        borderRadius: 3,
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[800], 0.8)
            : alpha(theme.palette.grey[100], 0.8),
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        mb: 2,
    }), [theme]);


    const dividerSx = useMemo(() => ({
        height: 1,
        background: `linear-gradient(90deg, transparent, ${theme.palette.divider} 20%, ${theme.palette.divider} 80%, transparent)`,
        mb: 2,
    }), [theme.palette.divider]);

    if (!selection) {
        return (
            <Box sx={EMPTY_STATE_CONTAINER_SX}>
                <Box sx={{ width: '100%', maxWidth: 600, mb: 3 }}>
                    <AIOverview mode={isDark ? 'dark' : 'light'} />
                </Box>
                <Box sx={emptyStateIconBoxSx}>
                    <ServerIcon sx={{ fontSize: 36, color: 'text.disabled' }} />
                </Box>
                <Typography variant="h6" sx={EMPTY_STATE_TITLE_SX}>
                    Select a server to get started
                </Typography>
                <Typography variant="body2" sx={EMPTY_STATE_DESC_SX}>
                    Choose a database server, cluster, or view the entire estate from the navigation panel
                </Typography>
            </Box>
        );
    }

    return (
        <Box sx={PANEL_ROOT_SX}>
            {/* Content Container */}
            <Box>
                {/* Selection Header */}
                <SelectionHeader selection={selection} alertCount={activeAlertCount} alertSeverities={activeAlertSeverities} onBlackoutClick={() => setBlackoutMgmtOpen(true)} />

                {/* Divider with gradient */}
                <Box sx={dividerSx} />

                {/* AI Overview */}
                <Box sx={{ mb: 2 }}>
                    <AIOverview mode={isDark ? 'dark' : 'light'} selection={selection} onAnalyze={handleServerAnalyze} analysisCached={serverAnalysisCached} />
                </Box>

                {/* Server Info Card */}
                {selection.type === 'server' && (
                    <Box sx={{ mb: 2 }}>
                        <ServerInfoCard selection={selection} />
                    </Box>
                )}

                {/* Metrics Grid for cluster/estate */}
                {metrics && (selection.type === 'cluster' || selection.type === 'estate') && (
                    <Box sx={METRICS_GRID_SX}>
                        <MetricCard
                            label="Online"
                            value={metrics.servers.online}
                            icon={HealthyIcon}
                            color={statusColors.online}
                        />
                        <MetricCard
                            label="Warning"
                            value={metrics.servers.warning || 0}
                            icon={WarningIcon}
                            color={statusColors.warning}
                        />
                        <MetricCard
                            label="Offline"
                            value={metrics.servers.offline || 0}
                            icon={ErrorIcon}
                            color={statusColors.offline}
                        />
                        {selection.type === 'estate' && (
                            <>
                                <MetricCard
                                    label="Clusters"
                                    value={metrics.clusters}
                                    icon={ClusterIcon}
                                />
                                <MetricCard
                                    label="Groups"
                                    value={metrics.groups}
                                    icon={EstateIcon}
                                />
                            </>
                        )}
                    </Box>
                )}

                {/* Event Timeline */}
                <EventTimeline
                    selection={selection}
                />

                {/* Performance Summary Tiles */}
                <PerformanceTiles selection={selection} />

                {/* Blackout Management */}
                <BlackoutPanel selection={selection} />

                {/* Alerts Section */}
                <AlertsSection
                    alerts={alerts}
                    loading={loading}
                    showServer={selection.type !== 'server'}
                    onAcknowledge={handleAcknowledge}
                    onUnacknowledge={handleUnacknowledge}
                    onAnalyze={handleAnalyze}
                    onEditOverride={handleEditOverride}
                />

                {/* Monitoring Dashboards */}
                <CollapsibleSection title="Monitoring" icon={<MonitorHeartIcon sx={{ fontSize: 16 }} />} defaultExpanded sx={monitoringSx} headerRight={<TimeRangeSelector />}>
                    {selection.type === 'server' && (
                        <ServerDashboard selection={selection} />
                    )}
                    {selection.type === 'cluster' && (
                        <ClusterDashboard selection={selection} />
                    )}
                    {selection.type === 'estate' && (
                        <EstateDashboard selection={selection} />
                    )}
                </CollapsibleSection>
            </Box>

            {/* Dashboard Overlay for drill-downs */}
            <MetricOverlay>
                <DashboardOverlayContent />
            </MetricOverlay>

            {/* Acknowledge Dialog */}
            <AcknowledgeDialog
                open={ackDialogOpen}
                alert={selectedAlertForAck}
                onClose={() => {
                    setAckDialogOpen(false);
                    setSelectedAlertForAck(null);
                }}
                onConfirm={handleAckConfirm}
            />

            {/* Alert Analysis Dialog */}
            <AlertAnalysisDialog
                open={analysisDialogOpen}
                alert={analysisAlert}
                onClose={() => {
                    setAnalysisDialogOpen(false);
                    setAnalysisAlert(null);
                }}
                onAnalysisComplete={handleAnalysisComplete}
                isDark={isDark}
            />

            {/* Alert Override Edit Dialog */}
            <AlertOverrideEditDialog
                open={overrideDialogOpen}
                alert={overrideAlert}
                onClose={() => {
                    setOverrideDialogOpen(false);
                    setOverrideAlert(null);
                }}
            />

            {/* Blackout Management Dialog */}
            <BlackoutManagementDialog
                open={blackoutMgmtOpen}
                onClose={() => setBlackoutMgmtOpen(false)}
                selection={selection}
            />

            {/* Server Analysis Dialog */}
            <ServerAnalysisDialog
                open={serverAnalysisOpen}
                selection={serverAnalysisSelection}
                onClose={() => setServerAnalysisOpen(false)}
                isDark={isDark}
            />
        </Box>
    );
};

export default StatusPanel;
