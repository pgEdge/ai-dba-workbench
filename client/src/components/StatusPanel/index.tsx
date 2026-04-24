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

import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import {
    Alert,
    Box,
    Snackbar,
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
    AccountTree as AccountTreeIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/useAuth';
import { useAICapabilities } from '../../contexts/useAICapabilities';
import { useClusterData } from '../../contexts/useClusterData';
import { useDashboard } from '../../contexts/useDashboard';
import { apiPost, apiGet, apiDelete, ApiError } from '../../utils/apiClient';
import { collectServers } from '../../utils/clusterHelpers';
import { logger } from '../../utils/logger';
import EventTimeline from '../EventTimeline';
import BlackoutPanel from '../BlackoutPanel';
import AlertAnalysisDialog from '../AlertAnalysisDialog';
import ServerAnalysisDialog from '../ServerAnalysisDialog';
import { hasCachedServerAnalysis } from '../../hooks/useServerAnalysis';
import type { ServerSelection, ClusterSelection } from '../../types/selection';
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
import TopologySection from '../Dashboard/ClusterDashboard/TopologySection';
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
import type { StatusPanelProps } from './types';

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
                connectionName={currentOverlay.connectionName ?? ''}
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
}) => {
    const theme = useTheme();
    const { user, hasPermission } = useAuth();
    const { aiEnabled } = useAICapabilities();
    const { lastRefresh } = useClusterData();
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(false);
    const initialLoadDoneRef = React.useRef(false);
    const localAnalysisRef = React.useRef<Map<number, string>>(new Map());
    const [blackoutMgmtOpen, setBlackoutMgmtOpen] = useState(false);
    const [ackDialogOpen, setAckDialogOpen] = useState(false);
    const [selectedAlertForAck, setSelectedAlertForAck] = useState(null);
    const [selectedAlertsForGroupAck, setSelectedAlertsForGroupAck] = useState(null);
    const [analysisDialogOpen, setAnalysisDialogOpen] = useState(false);
    const [analysisAlert, setAnalysisAlert] = useState(null);
    const [overrideDialogOpen, setOverrideDialogOpen] = useState(false);
    const [overrideAlert, setOverrideAlert] = useState(null);
    const [serverAnalysisOpen, setServerAnalysisOpen] = useState(false);
    // Tracks IDs currently being unacknowledged so the UI can disable
    // the button and ignore duplicate clicks.
    const [unacknowledgingIds, setUnacknowledgingIds] = useState<Set<number | string>>(new Set());
    // Mirror of unacknowledgingIds used for synchronous guards. The
    // state-based set lags behind by a render so rapid double-clicks
    // would otherwise race past the `has(id)` check; reading and
    // writing through a ref closes the window.
    const unacknowledgingRef = useRef<Set<number | string>>(new Set());
    // In-component error state used to surface unacknowledge failures
    // to the user. A dedicated notification framework is not yet in
    // place, so a minimal MUI Snackbar+Alert is rendered here.
    const [errorMessage, setErrorMessage] = useState<string | null>(null);
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

    const topologySx = useMemo(() => ({
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
            // Preserve raw timestamps so display components can decide
            // whether `last_updated` differs from `triggered_at` and
            // should be surfaced separately.
            triggeredAt: alert.triggered_at,
            lastUpdated: alert.last_updated,
            lastUpdatedTime: alert.last_updated
                ? formatRelativeTime(alert.last_updated)
                : undefined,
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
        setSelectedAlertsForGroupAck(null);
        setSelectedAlertForAck(alert);
        setAckDialogOpen(true);
    };

    // Handle opening group ack dialog
    const handleAcknowledgeGroup = (alerts) => {
        setSelectedAlertForAck(null);
        setSelectedAlertsForGroupAck(alerts);
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

    const serverAnalysisSelection = useMemo(
        (): (ServerSelection | ClusterSelection) | null => {
            if (!selection) { return null; }
            if (selection.type === 'server') { return selection; }
            if (selection.type === 'cluster' && selection.serverIds.length > 0) {
                return selection;
            }
            return null;
        },
        [selection],
    );

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
            logger.error('Error acknowledging alert:', err);
        } finally {
            setAckDialogOpen(false);
            setSelectedAlertForAck(null);
            setSelectedAlertsForGroupAck(null);
        }
    };

    // Handle confirming group acknowledgment
    const handleAckConfirmMultiple = async (alertIds, message, falsePositive = false) => {
        if (!user || !alertIds?.length) {return;}

        try {
            for (const alertId of alertIds) {
                await apiPost('/api/v1/alerts/acknowledge', {
                    alert_id: alertId,
                    message: message || '',
                    false_positive: falsePositive,
                });
            }
            fetchAlertsData();
        } catch (err) {
            logger.error('Error acknowledging alerts:', err);
        } finally {
            setAckDialogOpen(false);
            setSelectedAlertForAck(null);
            setSelectedAlertsForGroupAck(null);
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
            logger.warn('Server selection missing ID, skipping alert fetch');
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
            let url = '/api/v1/alerts?exclude_cleared=true';
            if (selection.type === 'server') {
                url += `&connection_id=${selection.id}&limit=50`;
            } else if (selection.type === 'cluster' && selection.serverIds?.length) {
                url += `&connection_ids=${selection.serverIds.join(',')}&limit=50`;
            }
            // For estate, fetch all alerts (no connection filter, no limit)

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
            logger.error('Error fetching alerts:', err);
            setAlerts([]);
        } finally {
            setLoading(false);
        }
    }, [user, selection, transformAlerts]);

    // Handle unacknowledging an alert.
    //
    // Applies an optimistic update that immediately clears the
    // acknowledgment fields on the targeted alert so the row moves out
    // of the acknowledged list without waiting for the API round trip.
    // On failure the previous alert object is restored and a
    // user-visible error message is surfaced via the local Snackbar.
    // Concurrent clicks on the same alert are ignored while a request
    // is in flight.
    const handleUnacknowledge = useCallback(async (alertId) => {
        if (!user || !alertId) {return;}
        // Synchronous guard: block rapid duplicate clicks that fire
        // before React has re-rendered with the updated state.
        if (unacknowledgingRef.current.has(alertId)) {return;}
        unacknowledgingRef.current.add(alertId);

        // Capture the previous alert from inside the setAlerts updater
        // so we can roll back on failure. React strict mode may invoke
        // this updater twice; both invocations observe the same `prev`
        // snapshot so `previousAlert` stays consistent either way.
        let previousAlert = null;
        setAlerts(prev => {
            const found = prev.find(a => a.id === alertId);
            if (!found) {
                // Nothing to optimistically update; leave state alone.
                return prev;
            }
            previousAlert = found;
            return prev.map(a => (
                a.id === alertId
                    ? {
                        ...a,
                        acknowledgedAt: undefined,
                        acknowledgedBy: undefined,
                        ackMessage: undefined,
                        falsePositive: undefined,
                    }
                    : a
            ));
        });

        setUnacknowledgingIds(prev => {
            const next = new Set(prev);
            next.add(alertId);
            return next;
        });

        try {
            await apiDelete(`/api/v1/alerts/acknowledge?alert_id=${alertId}`);
            // Reconcile with server truth and beat the race with the
            // periodic cluster refresh.
            await fetchAlertsData();
        } catch (err) {
            // Roll back the optimistic update.
            if (previousAlert) {
                const restored = previousAlert;
                setAlerts(prev => prev.map(a => (
                    a.id === alertId ? restored : a
                )));
            }
            const message = err instanceof ApiError
                ? `Failed to restore alert: ${err.message} (HTTP ${err.statusCode})`
                : err instanceof Error
                    ? `Failed to restore alert: ${err.message}`
                    : 'Failed to restore alert';
            setErrorMessage(message);
            logger.error('Error unacknowledging alert:', err);
        } finally {
            unacknowledgingRef.current.delete(alertId);
            setUnacknowledgingIds(prev => {
                if (!prev.has(alertId)) {return prev;}
                const next = new Set(prev);
                next.delete(alertId);
                return next;
            });
        }
    }, [user, fetchAlertsData]);

    const isUnacknowledging = useCallback(
        (id: number | string) => unacknowledgingIds.has(id),
        [unacknowledgingIds],
    );

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
                {aiEnabled ? (
                    <Box sx={{ width: '100%', maxWidth: 600, mb: 3 }}>
                        <AIOverview />
                    </Box>
                ) : (
                    <Box sx={{
                        width: '100%',
                        maxWidth: 600,
                        mb: 3,
                        p: 3,
                        borderRadius: 2,
                        bgcolor: 'background.paper',
                        textAlign: 'center',
                    }}>
                        <Typography variant="h6" sx={{ mb: 1, fontWeight: 600 }}>
                            Welcome to AI DBA Workbench
                        </Typography>
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                            AI features are currently disabled. To enable AI-powered
                            analysis, configure an LLM provider in the server configuration.
                        </Typography>
                    </Box>
                )}
                <Box sx={emptyStateIconBoxSx}>
                    <ServerIcon sx={{ fontSize: 36, color: 'text.disabled' }} />
                </Box>
                <Typography variant="h6" sx={EMPTY_STATE_TITLE_SX}>
                    Select a server to get started
                </Typography>
                <Typography variant="body2" sx={EMPTY_STATE_DESC_SX}>
                    Choose a database server, cluster, or view the entire estate from the navigation panel
                </Typography>
                {aiEnabled && (
                    <Alert severity="warning" sx={{ mt: 3, maxWidth: 600, textAlign: 'left' }}>
                        AI can make mistakes. Quality and accuracy can vary based on the
                        models in use. Check all AI responses for accuracy.
                    </Alert>
                )}
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
                {aiEnabled && (
                    <Box sx={{ mb: 2 }}>
                        <AIOverview
                            selection={selection}
                            onAnalyze={serverAnalysisSelection ? handleServerAnalyze : undefined}
                            analysisCached={serverAnalysisCached}
                        />
                    </Box>
                )}

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
                            label="OK"
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
                    isUnacknowledging={isUnacknowledging}
                    onAnalyze={aiEnabled ? handleAnalyze : undefined}
                    onEditOverride={hasPermission('manage_alert_rules') ? handleEditOverride : undefined}
                    onAcknowledgeGroup={handleAcknowledgeGroup}
                />

                {/* Topology (cluster only) */}
                {selection.type === 'cluster' && (
                    <CollapsibleSection title="Topology" icon={<AccountTreeIcon sx={{ fontSize: 16 }} />} defaultExpanded sx={topologySx}>
                        <TopologySection selection={selection} />
                    </CollapsibleSection>
                )}

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
                alerts={selectedAlertsForGroupAck}
                onClose={() => {
                    setAckDialogOpen(false);
                    setSelectedAlertForAck(null);
                    setSelectedAlertsForGroupAck(null);
                }}
                onConfirm={handleAckConfirm}
                onConfirmMultiple={handleAckConfirmMultiple}
            />

            {/* Alert Analysis Dialog */}
            {aiEnabled && (
                <AlertAnalysisDialog
                    open={analysisDialogOpen}
                    alert={analysisAlert}
                    onClose={() => {
                        setAnalysisDialogOpen(false);
                        setAnalysisAlert(null);
                    }}
                    onAnalysisComplete={handleAnalysisComplete}
                />
            )}

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
            {aiEnabled && (
                <ServerAnalysisDialog
                    open={serverAnalysisOpen}
                    selection={serverAnalysisSelection}
                    onClose={() => setServerAnalysisOpen(false)}
                />
            )}

            {/* Transient error notifications (e.g. unacknowledge failures) */}
            <Snackbar
                open={!!errorMessage}
                autoHideDuration={6000}
                onClose={() => setErrorMessage(null)}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
            >
                <Alert
                    severity="error"
                    variant="filled"
                    onClose={() => setErrorMessage(null)}
                    sx={{ width: '100%' }}
                >
                    {errorMessage}
                </Alert>
            </Snackbar>
        </Box>
    );
};

export default StatusPanel;
