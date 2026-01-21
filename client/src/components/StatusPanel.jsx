/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Status Panel Component
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Main content panel showing status and alerts for the current selection
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useMemo } from 'react';
import {
    Box,
    Typography,
    Paper,
    Chip,
    alpha,
    Divider,
    IconButton,
    Tooltip,
    Collapse,
    LinearProgress,
    Skeleton,
} from '@mui/material';
import {
    Circle as StatusIcon,
    Storage as ServerIcon,
    Dns as ClusterIcon,
    Language as EstateIcon,
    Warning as WarningIcon,
    Error as ErrorIcon,
    CheckCircle as HealthyIcon,
    Schedule as ScheduleIcon,
    TrendingUp as TrendingUpIcon,
    TrendingDown as TrendingDownIcon,
    NotificationsActive as AlertIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Refresh as RefreshIcon,
    Info as InfoIcon,
} from '@mui/icons-material';
import { useAuth } from '../contexts/AuthContext';

// Status color mapping
const STATUS_COLORS = {
    online: '#22C55E',
    warning: '#F59E0B',
    offline: '#EF4444',
    unknown: '#6B7280',
};

// Severity colors for alerts
const SEVERITY_COLORS = {
    critical: '#EF4444',
    warning: '#F59E0B',
    info: '#3B82F6',
};

/**
 * StatusBadge - Large status indicator with glow effect
 */
const StatusBadge = ({ status, size = 'large', isDark }) => {
    const color = STATUS_COLORS[status] || STATUS_COLORS.unknown;
    const sizes = {
        small: { badge: 10, glow: 20 },
        medium: { badge: 14, glow: 28 },
        large: { badge: 18, glow: 36 },
    };
    const s = sizes[size];

    return (
        <Box
            sx={{
                position: 'relative',
                width: s.glow,
                height: s.glow,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
            }}
        >
            <Box
                sx={{
                    position: 'absolute',
                    width: s.glow,
                    height: s.glow,
                    borderRadius: '50%',
                    bgcolor: alpha(color, 0.2),
                    animation: status === 'online' ? 'pulse 2s ease-in-out infinite' : 'none',
                    '@keyframes pulse': {
                        '0%, 100%': { transform: 'scale(1)', opacity: 1 },
                        '50%': { transform: 'scale(1.2)', opacity: 0.6 },
                    },
                }}
            />
            <StatusIcon
                sx={{
                    fontSize: s.badge,
                    color: color,
                    filter: `drop-shadow(0 0 4px ${color})`,
                    zIndex: 1,
                }}
            />
        </Box>
    );
};

/**
 * MetricCard - Display a key metric with trend indicator
 */
const MetricCard = ({ label, value, trend, trendValue, icon: Icon, color, isDark }) => {
    const TrendIcon = trend === 'up' ? TrendingUpIcon : TrendingDownIcon;
    const trendColor = trend === 'up' ? '#22C55E' : '#EF4444';

    return (
        <Paper
            elevation={0}
            sx={{
                p: 2,
                borderRadius: 2,
                bgcolor: isDark ? alpha('#334155', 0.5) : alpha('#F3F4F6', 0.8),
                border: '1px solid',
                borderColor: isDark ? '#475569' : '#E5E7EB',
                flex: 1,
                minWidth: 120,
            }}
        >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                {Icon && (
                    <Icon
                        sx={{
                            fontSize: 18,
                            color: color || (isDark ? '#94A3B8' : '#6B7280'),
                        }}
                    />
                )}
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.secondary',
                        fontSize: '0.75rem',
                        fontWeight: 500,
                        textTransform: 'uppercase',
                        letterSpacing: '0.05em',
                    }}
                >
                    {label}
                </Typography>
            </Box>
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1 }}>
                <Typography
                    variant="h4"
                    sx={{
                        fontWeight: 700,
                        color: color || 'text.primary',
                        fontSize: '1.75rem',
                        lineHeight: 1,
                        fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    }}
                >
                    {value}
                </Typography>
                {trend && (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25 }}>
                        <TrendIcon sx={{ fontSize: 14, color: trendColor }} />
                        <Typography
                            variant="caption"
                            sx={{ color: trendColor, fontSize: '0.6875rem', fontWeight: 600 }}
                        >
                            {trendValue}
                        </Typography>
                    </Box>
                )}
            </Box>
        </Paper>
    );
};

/**
 * InfoChip - Compact label/value display for server details
 */
const InfoChip = ({ label, value, isDark, mono = false, capitalize = false }) => {
    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 0.75,
                px: 1,
                py: 0.5,
                borderRadius: 1,
                bgcolor: isDark ? alpha('#1E293B', 0.6) : alpha('#FFFFFF', 0.8),
                border: '1px solid',
                borderColor: isDark ? alpha('#475569', 0.4) : '#E5E7EB',
            }}
        >
            <Typography
                variant="caption"
                sx={{
                    color: 'text.disabled',
                    fontSize: '0.625rem',
                    fontWeight: 600,
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                }}
            >
                {label}
            </Typography>
            <Typography
                variant="body2"
                sx={{
                    color: 'text.primary',
                    fontSize: '0.75rem',
                    fontWeight: 500,
                    fontFamily: mono ? '"JetBrains Mono", "SF Mono", monospace' : 'inherit',
                    textTransform: capitalize ? 'capitalize' : 'none',
                }}
            >
                {value}
            </Typography>
        </Box>
    );
};

/**
 * AlertItem - Single alert entry with severity indicator
 */
const AlertItem = ({ alert, isDark }) => {
    const severityColor = SEVERITY_COLORS[alert.severity] || SEVERITY_COLORS.info;
    const SeverityIcon = alert.severity === 'critical' ? ErrorIcon : WarningIcon;

    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: 1.5,
                p: 1.5,
                borderRadius: 1.5,
                bgcolor: isDark ? alpha(severityColor, 0.08) : alpha(severityColor, 0.05),
                border: '1px solid',
                borderColor: alpha(severityColor, isDark ? 0.3 : 0.2),
                transition: 'all 0.15s ease',
                '&:hover': {
                    bgcolor: isDark ? alpha(severityColor, 0.12) : alpha(severityColor, 0.08),
                    transform: 'translateX(4px)',
                },
            }}
        >
            <SeverityIcon
                sx={{
                    fontSize: 18,
                    color: severityColor,
                    mt: 0.25,
                    flexShrink: 0,
                }}
            />
            <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography
                    variant="body2"
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.875rem',
                        lineHeight: 1.3,
                        mb: 0.5,
                    }}
                >
                    {alert.title}
                </Typography>
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.secondary',
                        fontSize: '0.75rem',
                        lineHeight: 1.4,
                        display: 'block',
                    }}
                >
                    {alert.description}
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 1 }}>
                    <Chip
                        label={alert.severity}
                        size="small"
                        sx={{
                            height: 18,
                            fontSize: '0.625rem',
                            fontWeight: 600,
                            textTransform: 'uppercase',
                            bgcolor: alpha(severityColor, 0.15),
                            color: severityColor,
                            '& .MuiChip-label': { px: 0.75 },
                        }}
                    />
                    <Typography
                        variant="caption"
                        sx={{
                            color: 'text.disabled',
                            fontSize: '0.6875rem',
                            display: 'flex',
                            alignItems: 'center',
                            gap: 0.5,
                        }}
                    >
                        <ScheduleIcon sx={{ fontSize: 12 }} />
                        {alert.time}
                    </Typography>
                    {alert.server && (
                        <Typography
                            variant="caption"
                            sx={{
                                color: 'text.disabled',
                                fontSize: '0.6875rem',
                                display: 'flex',
                                alignItems: 'center',
                                gap: 0.5,
                            }}
                        >
                            <ServerIcon sx={{ fontSize: 12 }} />
                            {alert.server}
                        </Typography>
                    )}
                </Box>
            </Box>
        </Box>
    );
};

/**
 * AlertsSection - Collapsible alerts list
 */
const AlertsSection = ({ alerts, isDark, loading }) => {
    const [expanded, setExpanded] = useState(true);

    if (loading) {
        return (
            <Box sx={{ mt: 3 }}>
                <Skeleton variant="text" width={120} height={24} />
                <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
                    {[1, 2, 3].map((i) => (
                        <Skeleton
                            key={i}
                            variant="rounded"
                            height={80}
                            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
                        />
                    ))}
                </Box>
            </Box>
        );
    }

    return (
        <Box sx={{ mt: 3 }}>
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                    cursor: 'pointer',
                    py: 0.5,
                    '&:hover': { opacity: 0.8 },
                }}
            >
                <AlertIcon sx={{ fontSize: 18, color: 'primary.main' }} />
                <Typography
                    variant="subtitle2"
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.875rem',
                    }}
                >
                    Active Alerts
                </Typography>
                <Chip
                    label={alerts.length}
                    size="small"
                    sx={{
                        height: 20,
                        fontSize: '0.6875rem',
                        fontWeight: 600,
                        bgcolor: alerts.length > 0
                            ? alpha(SEVERITY_COLORS.warning, 0.15)
                            : (isDark ? alpha('#22C55E', 0.15) : alpha('#22C55E', 0.1)),
                        color: alerts.length > 0 ? SEVERITY_COLORS.warning : '#22C55E',
                        '& .MuiChip-label': { px: 0.75 },
                    }}
                />
                <Box sx={{ flex: 1 }} />
                <IconButton size="small" sx={{ p: 0.25 }}>
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
            </Box>
            <Collapse in={expanded}>
                <Box
                    sx={{
                        mt: 1.5,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 1,
                    }}
                >
                    {alerts.length === 0 ? (
                        <Box
                            sx={{
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: 1,
                                py: 3,
                                borderRadius: 2,
                                bgcolor: isDark ? alpha('#22C55E', 0.08) : alpha('#22C55E', 0.05),
                                border: '1px solid',
                                borderColor: isDark ? alpha('#22C55E', 0.2) : alpha('#22C55E', 0.15),
                            }}
                        >
                            <HealthyIcon sx={{ fontSize: 20, color: '#22C55E' }} />
                            <Typography
                                variant="body2"
                                sx={{
                                    color: '#22C55E',
                                    fontWeight: 500,
                                }}
                            >
                                No active alerts
                            </Typography>
                        </Box>
                    ) : (
                        alerts.map((alert, index) => (
                            <AlertItem key={alert.id || index} alert={alert} isDark={isDark} />
                        ))
                    )}
                </Box>
            </Collapse>
        </Box>
    );
};

/**
 * SelectionHeader - Header showing what's currently selected
 */
const SelectionHeader = ({ selection, isDark }) => {
    const getIcon = () => {
        switch (selection.type) {
            case 'server':
                return ServerIcon;
            case 'cluster':
                return ClusterIcon;
            case 'estate':
                return EstateIcon;
            default:
                return InfoIcon;
        }
    };

    const getLabel = () => {
        switch (selection.type) {
            case 'server':
                return 'Server';
            case 'cluster':
                return 'Cluster';
            case 'estate':
                return 'Estate Overview';
            default:
                return 'Selection';
        }
    };

    const Icon = getIcon();

    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                mb: 3,
            }}
        >
            <Box
                sx={{
                    width: 48,
                    height: 48,
                    borderRadius: 2,
                    bgcolor: isDark ? alpha('#22B8CF', 0.15) : alpha('#15AABF', 0.1),
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                }}
            >
                <Icon sx={{ fontSize: 24, color: 'primary.main' }} />
            </Box>
            <Box sx={{ flex: 1 }}>
                <Typography
                    variant="overline"
                    sx={{
                        color: 'text.secondary',
                        fontSize: '0.6875rem',
                        fontWeight: 600,
                        letterSpacing: '0.08em',
                        lineHeight: 1,
                    }}
                >
                    {getLabel()}
                </Typography>
                <Typography
                    variant="h5"
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        lineHeight: 1.2,
                        mt: 0.25,
                    }}
                >
                    {selection.name}
                </Typography>
                {selection.subtitle && (
                    <Typography
                        variant="body2"
                        sx={{
                            color: 'text.secondary',
                            fontSize: '0.8125rem',
                            mt: 0.25,
                        }}
                    >
                        {selection.subtitle}
                    </Typography>
                )}
            </Box>
            <StatusBadge status={selection.status} isDark={isDark} />
        </Box>
    );
};

/**
 * StatusPanel - Main component showing status and alerts
 */
const StatusPanel = ({
    selection,
    mode = 'light',
}) => {
    const isDark = mode === 'dark';
    const { sessionToken: token } = useAuth();
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(true);

    // Calculate metrics based on selection type
    const metrics = useMemo(() => {
        if (!selection) return null;

        if (selection.type === 'server') {
            return {
                status: selection.status,
                servers: { total: 1, online: selection.status === 'online' ? 1 : 0 },
            };
        }

        if (selection.type === 'cluster') {
            const servers = selection.servers || [];
            const online = servers.filter(s => s.status === 'online').length;
            const warning = servers.filter(s => s.status === 'warning').length;
            const offline = servers.filter(s => s.status === 'offline').length;

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
                    const collectServers = (servers) => {
                        servers?.forEach(s => {
                            allServers.push(s);
                            if (s.children) collectServers(s.children);
                        });
                    };
                    collectServers(cluster.servers);
                });
            });

            const online = allServers.filter(s => s.status === 'online').length;
            const warning = allServers.filter(s => s.status === 'warning').length;
            const offline = allServers.filter(s => s.status === 'offline').length;

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
        if (!date) return '';
        const now = new Date();
        const then = new Date(date);
        const diffMs = now - then;
        const diffSecs = Math.floor(diffMs / 1000);
        const diffMins = Math.floor(diffSecs / 60);
        const diffHours = Math.floor(diffMins / 60);
        const diffDays = Math.floor(diffHours / 24);

        if (diffSecs < 60) return 'just now';
        if (diffMins < 60) return `${diffMins} min ago`;
        if (diffHours < 24) return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
        if (diffDays < 7) return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
        return then.toLocaleDateString();
    };

    // Transform API alerts to component format
    const transformAlerts = (apiAlerts) => {
        return apiAlerts.map(alert => ({
            id: alert.id,
            severity: alert.severity?.toLowerCase() || 'info',
            title: alert.title,
            description: alert.description,
            time: formatRelativeTime(alert.triggered_at),
            server: alert.server_name,
            connectionId: alert.connection_id,
        }));
    };

    // Fetch alerts based on selection
    useEffect(() => {
        const fetchAlerts = async () => {
            if (!token || !selection) {
                setAlerts([]);
                setLoading(false);
                return;
            }

            setLoading(true);
            try {
                // Build query params based on selection type
                let url = '/api/alerts?status=active&limit=10';
                if (selection.type === 'server' && selection.id) {
                    url += `&connection_id=${selection.id}`;
                } else if (selection.type === 'cluster' && selection.serverIds?.length) {
                    // For cluster, filter by multiple connection IDs
                    url += `&connection_ids=${selection.serverIds.join(',')}`;
                }
                // For estate, fetch all active alerts (no connection filter)

                const response = await fetch(url, {
                    headers: {
                        'Authorization': `Bearer ${token}`,
                    },
                });

                if (response.ok) {
                    const data = await response.json();
                    const transformedAlerts = transformAlerts(data.alerts || []);
                    setAlerts(transformedAlerts);
                } else {
                    setAlerts([]);
                }
            } catch (err) {
                console.error('Error fetching alerts:', err);
                setAlerts([]);
            } finally {
                setLoading(false);
            }
        };

        fetchAlerts();
    }, [token, selection]);

    if (!selection) {
        return (
            <Box
                sx={{
                    height: '100%',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    p: 4,
                }}
            >
                <Box
                    sx={{
                        width: 80,
                        height: 80,
                        borderRadius: 3,
                        bgcolor: isDark ? alpha('#334155', 0.5) : alpha('#F3F4F6', 0.8),
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        mb: 2,
                    }}
                >
                    <ServerIcon sx={{ fontSize: 36, color: 'text.disabled' }} />
                </Box>
                <Typography
                    variant="h6"
                    sx={{
                        color: 'text.secondary',
                        fontWeight: 500,
                        mb: 0.5,
                    }}
                >
                    Select a server to get started
                </Typography>
                <Typography
                    variant="body2"
                    sx={{
                        color: 'text.disabled',
                        textAlign: 'center',
                        maxWidth: 300,
                    }}
                >
                    Choose a database server, cluster, or view the entire estate from the navigation panel
                </Typography>
            </Box>
        );
    }

    return (
        <Box
            sx={{
                overflow: 'auto',
                p: 3,
            }}
        >
            {/* Content Card */}
            <Paper
                elevation={0}
                sx={{
                    p: 3,
                    borderRadius: 2,
                    bgcolor: isDark ? alpha('#1E293B', 0.5) : alpha('#FFFFFF', 0.9),
                    border: '1px solid',
                    borderColor: isDark ? alpha('#475569', 0.5) : '#E5E7EB',
                }}
            >
                {/* Selection Header */}
                <SelectionHeader selection={selection} isDark={isDark} />

                {/* Divider with gradient */}
                <Box
                    sx={{
                        height: 1,
                        background: isDark
                            ? 'linear-gradient(90deg, transparent, #475569 20%, #475569 80%, transparent)'
                            : 'linear-gradient(90deg, transparent, #E5E7EB 20%, #E5E7EB 80%, transparent)',
                        mb: 2,
                    }}
                />

                {/* Server Details - Compact horizontal layout for single server */}
                {selection.type === 'server' && (
                    <Box
                        sx={{
                            display: 'flex',
                            flexWrap: 'wrap',
                            gap: 1.5,
                            mb: 2,
                            p: 1.5,
                            borderRadius: 2,
                            bgcolor: isDark ? alpha('#334155', 0.3) : alpha('#F3F4F6', 0.6),
                            border: '1px solid',
                            borderColor: isDark ? alpha('#475569', 0.5) : '#E5E7EB',
                        }}
                    >
                        <InfoChip label="Host" value={selection.host || 'N/A'} isDark={isDark} mono />
                        <InfoChip label="Port" value={selection.port || 'N/A'} isDark={isDark} mono />
                        {selection.database && (
                            <InfoChip label="Database" value={selection.database} isDark={isDark} mono />
                        )}
                        {selection.username && (
                            <InfoChip label="User" value={selection.username} isDark={isDark} />
                        )}
                        {selection.role && (
                            <InfoChip
                                label="Role"
                                value={selection.role.replace(/_/g, ' ')}
                                isDark={isDark}
                                capitalize
                            />
                        )}
                        {selection.version && (
                            <InfoChip label="PostgreSQL" value={selection.version} isDark={isDark} mono />
                        )}
                        {selection.os && (
                            <InfoChip label="OS" value={selection.os} isDark={isDark} />
                        )}
                    </Box>
                )}

                {/* Metrics Grid for cluster/estate */}
                {metrics && (selection.type === 'cluster' || selection.type === 'estate') && (
                    <Box
                        sx={{
                            display: 'flex',
                            gap: 2,
                            flexWrap: 'wrap',
                            mb: 2,
                        }}
                    >
                        <MetricCard
                            label="Online"
                            value={metrics.servers.online}
                            icon={HealthyIcon}
                            color={STATUS_COLORS.online}
                            isDark={isDark}
                        />
                        <MetricCard
                            label="Warning"
                            value={metrics.servers.warning || 0}
                            icon={WarningIcon}
                            color={STATUS_COLORS.warning}
                            isDark={isDark}
                        />
                        <MetricCard
                            label="Offline"
                            value={metrics.servers.offline || 0}
                            icon={ErrorIcon}
                            color={STATUS_COLORS.offline}
                            isDark={isDark}
                        />
                        {selection.type === 'estate' && (
                            <>
                                <MetricCard
                                    label="Clusters"
                                    value={metrics.clusters}
                                    icon={ClusterIcon}
                                    isDark={isDark}
                                />
                                <MetricCard
                                    label="Groups"
                                    value={metrics.groups}
                                    icon={EstateIcon}
                                    isDark={isDark}
                                />
                            </>
                        )}
                    </Box>
                )}

                {/* Alerts Section */}
                <AlertsSection alerts={alerts} isDark={isDark} loading={loading} />
            </Paper>
        </Box>
    );
};

export default StatusPanel;
