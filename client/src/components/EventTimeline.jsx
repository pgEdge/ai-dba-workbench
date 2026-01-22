/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo, useCallback, useEffect, memo } from 'react';
import {
    Box,
    Typography,
    Chip,
    IconButton,
    Tooltip,
    Collapse,
    Skeleton,
    alpha,
    ToggleButton,
    ToggleButtonGroup,
} from '@mui/material';
import {
    Settings as SettingsIcon,
    Security as SecurityIcon,
    AccountCircle as AccountCircleIcon,
    PowerSettingsNew as PowerSettingsNewIcon,
    Warning as WarningIcon,
    Error as ErrorIcon,
    CheckCircle as CheckCircleIcon,
    CheckCircleOutline as CheckCircleOutlineIcon,
    Extension as ExtensionIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Timeline as TimelineIcon,
    EventNote as EventNoteIcon,
    Close as CloseIcon,
} from '@mui/icons-material';
import { useTimelineEvents } from '../hooks/useTimelineEvents';

// Event type configuration with icons and colors
const EVENT_TYPE_CONFIG = {
    config_change: {
        icon: SettingsIcon,
        color: '#15AABF',
        label: 'Config',
    },
    hba_change: {
        icon: SecurityIcon,
        color: '#3B82F6',
        label: 'HBA',
    },
    ident_change: {
        icon: AccountCircleIcon,
        color: '#3B82F6',
        label: 'Ident',
    },
    restart: {
        icon: PowerSettingsNewIcon,
        color: '#F59E0B',
        label: 'Restart',
    },
    alert_fired: {
        icon: WarningIcon,
        color: '#F59E0B',
        label: 'Alert',
        getSeverityColor: (severity) => {
            return severity === 'critical' ? '#EF4444' : '#F59E0B';
        },
        getSeverityIcon: (severity) => {
            return severity === 'critical' ? ErrorIcon : WarningIcon;
        },
    },
    alert_cleared: {
        icon: CheckCircleIcon,
        color: '#22C55E',
        label: 'Cleared',
    },
    alert_acknowledged: {
        icon: CheckCircleOutlineIcon,
        color: '#8B5CF6',
        label: 'Acked',
    },
    extension_change: {
        icon: ExtensionIcon,
        color: '#06B6D4',
        label: 'Extension',
    },
};

// All event types for filter
const ALL_EVENT_TYPES = Object.keys(EVENT_TYPE_CONFIG);

// Time range options
const TIME_RANGE_OPTIONS = [
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
];

// localStorage key for persisting time range preference
const TIME_RANGE_STORAGE_KEY = 'timeline-time-range';

// Get initial time range from localStorage or use default
const getInitialTimeRange = () => {
    try {
        const stored = localStorage.getItem(TIME_RANGE_STORAGE_KEY);
        if (stored && TIME_RANGE_OPTIONS.some((opt) => opt.value === stored)) {
            return stored;
        }
    } catch {
        // localStorage not available
    }
    return '24h';
};

/**
 * Get event configuration with potential severity override
 */
const getEventConfig = (event) => {
    const config = EVENT_TYPE_CONFIG[event.event_type] || EVENT_TYPE_CONFIG.config_change;

    // Handle severity-based color and icon for alerts
    if (event.event_type === 'alert_fired' && config.getSeverityColor) {
        const severity = event.details?.severity;
        return {
            ...config,
            color: config.getSeverityColor(severity),
            icon: config.getSeverityIcon(severity),
        };
    }

    return config;
};

/**
 * Format timestamp for display
 */
const formatEventTime = (timestamp) => {
    if (!timestamp) return '';
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now - date;
    const diffMins = Math.floor(diffMs / (1000 * 60));
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) return 'just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;

    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
};

/**
 * Format full timestamp for detail view
 */
const formatFullTime = (timestamp) => {
    if (!timestamp) return '';
    const date = new Date(timestamp);
    return date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
};

/**
 * Calculate position of event on timeline as percentage
 */
const calculatePosition = (eventTime, startTime, endTime) => {
    const eventTs = new Date(eventTime).getTime();
    const startTs = startTime.getTime();
    const endTs = endTime.getTime();
    const range = endTs - startTs;
    if (range <= 0) return 0;
    const position = ((eventTs - startTs) / range) * 100;
    return Math.max(0, Math.min(100, position));
};

/**
 * Get time range boundaries
 */
const getTimeRangeBounds = (timeRange) => {
    const now = new Date();
    let startTime;

    switch (timeRange) {
        case '1h':
            startTime = new Date(now.getTime() - 60 * 60 * 1000);
            break;
        case '6h':
            startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000);
            break;
        case '24h':
            startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
            break;
        case '7d':
            startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            break;
        case '30d':
            startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
            break;
        default:
            startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
    }

    return { startTime, endTime: now };
};

/**
 * Cluster nearby events
 */
const clusterEvents = (events, startTime, endTime, minDistancePercent = 2) => {
    if (!events || events.length === 0) return [];

    // Sort by timestamp
    const sorted = [...events].sort(
        (a, b) => new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime()
    );

    const clusters = [];
    let currentCluster = null;

    sorted.forEach((event) => {
        const position = calculatePosition(event.occurred_at, startTime, endTime);

        if (!currentCluster) {
            currentCluster = {
                events: [event],
                position,
                startPosition: position,
            };
        } else if (position - currentCluster.position < minDistancePercent) {
            // Add to current cluster
            currentCluster.events.push(event);
            // Update position to average
            currentCluster.position =
                (currentCluster.startPosition + position) / 2;
        } else {
            // Start new cluster
            clusters.push(currentCluster);
            currentCluster = {
                events: [event],
                position,
                startPosition: position,
            };
        }
    });

    if (currentCluster) {
        clusters.push(currentCluster);
    }

    return clusters;
};

/**
 * Generate time axis markers
 */
const generateTimeMarkers = (startTime, endTime, count = 5) => {
    const markers = [];
    const range = endTime.getTime() - startTime.getTime();
    const step = range / (count - 1);

    for (let i = 0; i < count; i++) {
        const time = new Date(startTime.getTime() + step * i);
        const position = (i / (count - 1)) * 100;

        let label;
        if (range <= 60 * 60 * 1000) {
            // 1 hour or less - show time
            label = time.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
        } else if (range <= 24 * 60 * 60 * 1000) {
            // 24 hours or less - show time
            label = time.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
        } else {
            // More than 24 hours - show date and time
            label = time.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
        }

        markers.push({ position, label, time });
    }

    return markers;
};

/**
 * EventMarker - Single event or cluster marker on the timeline
 */
const EventMarker = memo(({ cluster, isDark, showServer, onClick }) => {
    const isCluster = cluster.events.length > 1;
    const primaryEvent = cluster.events[0];
    const config = getEventConfig(primaryEvent);
    const EventIcon = config.icon;

    // For clusters, determine if there are mixed types or severities
    const hasMixedTypes = isCluster && new Set(cluster.events.map(e => e.event_type)).size > 1;
    const hasCritical = cluster.events.some(
        e => e.event_type === 'alert_fired' && e.details?.severity === 'critical'
    );

    const markerColor = hasCritical ? '#EF4444' : config.color;

    return (
        <Tooltip
            title={
                <Box sx={{ p: 0.5 }}>
                    {isCluster ? (
                        <>
                            <Typography sx={{ fontSize: '0.75rem', fontWeight: 600 }}>
                                {cluster.events.length} events
                            </Typography>
                            {cluster.events.slice(0, 3).map((e, i) => (
                                <Typography key={i} sx={{ fontSize: '0.6875rem', color: 'grey.300' }}>
                                    {e.title}
                                </Typography>
                            ))}
                            {cluster.events.length > 3 && (
                                <Typography sx={{ fontSize: '0.6875rem', color: 'grey.400' }}>
                                    +{cluster.events.length - 3} more
                                </Typography>
                            )}
                        </>
                    ) : (
                        <>
                            <Typography sx={{ fontSize: '0.75rem', fontWeight: 600 }}>
                                {primaryEvent.title}
                            </Typography>
                            <Typography sx={{ fontSize: '0.6875rem', color: 'grey.300' }}>
                                {formatEventTime(primaryEvent.occurred_at)}
                            </Typography>
                            {showServer && primaryEvent.server_name && (
                                <Typography sx={{ fontSize: '0.6875rem', color: 'grey.400' }}>
                                    {primaryEvent.server_name}
                                </Typography>
                            )}
                        </>
                    )}
                </Box>
            }
            placement="top"
            arrow
        >
            <Box
                onClick={(e) => onClick(e, cluster)}
                sx={{
                    position: 'absolute',
                    left: `${cluster.position}%`,
                    top: '50%',
                    transform: 'translate(-50%, -50%)',
                    cursor: 'pointer',
                    zIndex: 10,
                    '&:hover': {
                        zIndex: 20,
                        '& > div': {
                            transform: 'scale(1.2)',
                        },
                    },
                }}
            >
                <Box
                    sx={{
                        position: 'relative',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: isCluster ? 28 : 24,
                        height: isCluster ? 28 : 24,
                        borderRadius: '50%',
                        bgcolor: isDark ? alpha(markerColor, 0.2) : alpha(markerColor, 0.15),
                        border: '2px solid',
                        borderColor: markerColor,
                        transition: 'transform 0.15s ease',
                    }}
                >
                    {hasMixedTypes ? (
                        <EventNoteIcon sx={{ fontSize: 14, color: markerColor }} />
                    ) : (
                        <EventIcon sx={{ fontSize: 14, color: markerColor }} />
                    )}
                    {isCluster && (
                        <Box
                            sx={{
                                position: 'absolute',
                                top: -6,
                                right: -6,
                                minWidth: 16,
                                height: 16,
                                px: 0.5,
                                borderRadius: '8px',
                                bgcolor: isDark ? '#475569' : '#64748B',
                                color: '#FFF',
                                fontSize: '0.5625rem',
                                fontWeight: 700,
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                lineHeight: 1,
                            }}
                        >
                            {cluster.events.length > 99 ? '99+' : cluster.events.length}
                        </Box>
                    )}
                </Box>
            </Box>
        </Tooltip>
    );
});

EventMarker.displayName = 'EventMarker';

/**
 * ExpandableList - A list that can be expanded to show all items
 */
const ExpandableList = memo(({ items, initialLimit, renderItem, isDark, emptyText }) => {
    const [showAll, setShowAll] = useState(false);
    const totalCount = items?.length || 0;
    const hasMore = totalCount > initialLimit;
    const displayItems = showAll ? items : items?.slice(0, initialLimit);

    if (!items || items.length === 0) {
        return (
            <Typography sx={{ color: 'text.secondary', fontSize: '0.75rem' }}>
                {emptyText || '0 items'}
            </Typography>
        );
    }

    return (
        <>
            {displayItems.map((item, i) => renderItem(item, i, displayItems.length))}
            {hasMore && (
                <Box
                    onClick={() => setShowAll(!showAll)}
                    sx={{
                        mt: 0.5,
                        pt: 0.5,
                        cursor: 'pointer',
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.5,
                        color: 'primary.main',
                        fontSize: '0.6875rem',
                        fontWeight: 500,
                        '&:hover': {
                            textDecoration: 'underline',
                        },
                    }}
                >
                    {showAll ? (
                        <>
                            <ExpandLessIcon sx={{ fontSize: 14 }} />
                            Show less
                        </>
                    ) : (
                        <>
                            <ExpandMoreIcon sx={{ fontSize: 14 }} />
                            Show all {totalCount} items (+{totalCount - initialLimit} more)
                        </>
                    )}
                </Box>
            )}
        </>
    );
});

ExpandableList.displayName = 'ExpandableList';

/**
 * ConfigChangeDetails - Shows configuration change details with expandable list
 */
const ConfigChangeDetails = memo(({ details, isDark }) => {
    const settings = details?.settings || [];
    const count = details?.setting_count || settings.length || 0;

    return (
        <Box sx={{ mt: 1 }}>
            <Typography
                sx={{
                    fontSize: '0.6875rem',
                    fontWeight: 600,
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    mb: 0.5,
                }}
            >
                Settings ({count})
            </Typography>
            <Box
                sx={{
                    p: 1,
                    borderRadius: 1,
                    bgcolor: isDark ? alpha('#334155', 0.5) : '#F3F4F6',
                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    fontSize: '0.75rem',
                    maxHeight: 300,
                    overflow: 'auto',
                }}
            >
                <ExpandableList
                    items={settings}
                    initialLimit={10}
                    isDark={isDark}
                    emptyText={`${count} settings`}
                    renderItem={(setting, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={{
                                    color: 'primary.main',
                                    fontWeight: 600,
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                }}
                            >
                                {setting.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={{ color: 'text.secondary', fontFamily: 'inherit', fontSize: 'inherit' }}
                            >
                                {' = '}{setting.value}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

ConfigChangeDetails.displayName = 'ConfigChangeDetails';

/**
 * ExtensionChangeDetails - Shows extension change details
 */
const ExtensionChangeDetails = memo(({ details, isDark }) => {
    const extensions = details?.extensions || [];
    const count = details?.extension_count || extensions.length || 0;

    return (
        <Box sx={{ mt: 1 }}>
            <Typography
                sx={{
                    fontSize: '0.6875rem',
                    fontWeight: 600,
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    mb: 0.5,
                }}
            >
                Extensions ({count})
            </Typography>
            <Box
                sx={{
                    p: 1,
                    borderRadius: 1,
                    bgcolor: isDark ? alpha('#334155', 0.5) : '#F3F4F6',
                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    fontSize: '0.75rem',
                    maxHeight: 300,
                    overflow: 'auto',
                }}
            >
                <ExpandableList
                    items={extensions}
                    initialLimit={10}
                    isDark={isDark}
                    emptyText={`${count} extensions`}
                    renderItem={(ext, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={{
                                    color: '#06B6D4',
                                    fontWeight: 600,
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                }}
                            >
                                {ext.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={{ color: 'text.secondary', fontFamily: 'inherit', fontSize: 'inherit' }}
                            >
                                {' v'}{ext.version}
                            </Typography>
                            {ext.database && (
                                <Typography
                                    component="span"
                                    sx={{ color: 'text.disabled', fontFamily: 'inherit', fontSize: 'inherit' }}
                                >
                                    {' in '}{ext.database}
                                </Typography>
                            )}
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

ExtensionChangeDetails.displayName = 'ExtensionChangeDetails';

/**
 * HbaChangeDetails - Shows HBA rule change details with expandable list
 */
const HbaChangeDetails = memo(({ details, isDark }) => {
    const rules = details?.rules || [];
    const count = details?.rule_count || rules.length || 0;

    return (
        <Box sx={{ mt: 1 }}>
            <Typography
                sx={{
                    fontSize: '0.6875rem',
                    fontWeight: 600,
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    mb: 0.5,
                }}
            >
                HBA Rules ({count})
            </Typography>
            <Box
                sx={{
                    p: 1,
                    borderRadius: 1,
                    bgcolor: isDark ? alpha('#334155', 0.5) : '#F3F4F6',
                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    fontSize: '0.6875rem',
                    maxHeight: 300,
                    overflow: 'auto',
                }}
            >
                <ExpandableList
                    items={rules}
                    initialLimit={8}
                    isDark={isDark}
                    emptyText={`${count} rules`}
                    renderItem={(rule, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography
                                sx={{
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                    color: 'text.primary',
                                }}
                            >
                                {rule.type} {rule.database} {rule.user_name} {rule.address || ''} {rule.auth_method}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

HbaChangeDetails.displayName = 'HbaChangeDetails';

/**
 * IdentChangeDetails - Shows ident mapping change details with expandable list
 */
const IdentChangeDetails = memo(({ details, isDark }) => {
    const mappings = details?.mappings || [];
    const count = details?.mapping_count || mappings.length || 0;

    return (
        <Box sx={{ mt: 1 }}>
            <Typography
                sx={{
                    fontSize: '0.6875rem',
                    fontWeight: 600,
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    mb: 0.5,
                }}
            >
                Ident Mappings ({count})
            </Typography>
            <Box
                sx={{
                    p: 1,
                    borderRadius: 1,
                    bgcolor: isDark ? alpha('#334155', 0.5) : '#F3F4F6',
                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    fontSize: '0.6875rem',
                    maxHeight: 300,
                    overflow: 'auto',
                }}
            >
                <ExpandableList
                    items={mappings}
                    initialLimit={8}
                    isDark={isDark}
                    emptyText={`${count} mappings`}
                    renderItem={(mapping, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography
                                sx={{
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                    color: 'text.primary',
                                }}
                            >
                                {mapping.map_name}: {mapping.sys_name} → {mapping.pg_username}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

IdentChangeDetails.displayName = 'IdentChangeDetails';

/**
 * AlertDetails - Shows alert fired/cleared/acknowledged details
 */
const AlertDetails = memo(({ details, config }) => {
    return (
        <Box sx={{ mt: 1 }}>
            {/* Acknowledged by info */}
            {details?.acknowledged_by && (
                <Box sx={{ mb: 1 }}>
                    <Typography
                        sx={{
                            fontSize: '0.6875rem',
                            fontWeight: 600,
                            color: 'text.secondary',
                            textTransform: 'uppercase',
                            letterSpacing: '0.05em',
                            mb: 0.25,
                        }}
                    >
                        Acknowledged By
                    </Typography>
                    <Typography
                        sx={{
                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                            fontSize: '0.8125rem',
                            fontWeight: 500,
                            color: 'text.primary',
                        }}
                    >
                        {details.acknowledged_by}
                        {details.false_positive && (
                            <Chip
                                label="False Positive"
                                size="small"
                                sx={{
                                    ml: 1,
                                    height: 16,
                                    fontSize: '0.5625rem',
                                    fontWeight: 600,
                                    textTransform: 'uppercase',
                                    bgcolor: alpha('#F59E0B', 0.15),
                                    color: '#F59E0B',
                                    '& .MuiChip-label': { px: 0.5 },
                                }}
                            />
                        )}
                    </Typography>
                    {details.message && (
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                color: 'text.secondary',
                                mt: 0.5,
                                fontStyle: 'italic',
                            }}
                        >
                            "{details.message}"
                        </Typography>
                    )}
                </Box>
            )}
            {details?.metric_value !== undefined && (
                <Box sx={{ mb: 1 }}>
                    <Typography
                        sx={{
                            fontSize: '0.6875rem',
                            fontWeight: 600,
                            color: 'text.secondary',
                            textTransform: 'uppercase',
                            letterSpacing: '0.05em',
                            mb: 0.25,
                        }}
                    >
                        Metric Value
                    </Typography>
                    <Typography
                        sx={{
                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                            fontSize: '0.875rem',
                            fontWeight: 600,
                            color: config.color,
                        }}
                    >
                        {details.metric_value}
                        {details.threshold_value !== undefined && (
                            <Typography
                                component="span"
                                sx={{
                                    fontWeight: 400,
                                    color: 'text.secondary',
                                    fontSize: '0.75rem',
                                }}
                            >
                                {' '}/ threshold: {details.threshold_value}
                            </Typography>
                        )}
                    </Typography>
                </Box>
            )}
            {/* Show severity chip - use original_severity for acks, severity otherwise */}
            {(details?.severity || details?.original_severity) && (
                <Chip
                    label={details.original_severity || details.severity}
                    size="small"
                    sx={{
                        height: 18,
                        fontSize: '0.5625rem',
                        fontWeight: 600,
                        textTransform: 'uppercase',
                        bgcolor: alpha(config.color, 0.15),
                        color: config.color,
                        '& .MuiChip-label': { px: 0.75 },
                    }}
                />
            )}
        </Box>
    );
});

AlertDetails.displayName = 'AlertDetails';

/**
 * RestartDetails - Shows server restart details
 */
const RestartDetails = memo(({ details, isDark }) => {
    if (!details?.previous_timeline && !details?.old_timeline_id) {
        return null;
    }

    return (
        <Box sx={{ mt: 1 }}>
            <Box
                sx={{
                    p: 1,
                    borderRadius: 1,
                    bgcolor: isDark ? alpha('#334155', 0.5) : '#F3F4F6',
                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                    fontSize: '0.75rem',
                }}
            >
                <Typography sx={{ fontSize: '0.6875rem', color: 'text.secondary', mb: 0.25 }}>
                    Timeline ID
                </Typography>
                <Typography sx={{ fontFamily: 'inherit', fontSize: 'inherit' }}>
                    {details.previous_timeline || details.old_timeline_id} {'->'} {details.new_timeline || details.new_timeline_id}
                </Typography>
            </Box>
        </Box>
    );
});

RestartDetails.displayName = 'RestartDetails';

/**
 * EventDetails - Renders the appropriate details component based on event type
 */
const EventDetails = memo(({ event, config, isDark }) => {
    if (!event.details) return null;

    switch (event.event_type) {
        case 'config_change':
            return <ConfigChangeDetails details={event.details} isDark={isDark} />;
        case 'extension_change':
            return <ExtensionChangeDetails details={event.details} isDark={isDark} />;
        case 'hba_change':
            return <HbaChangeDetails details={event.details} isDark={isDark} />;
        case 'ident_change':
            return <IdentChangeDetails details={event.details} isDark={isDark} />;
        case 'alert_fired':
        case 'alert_cleared':
        case 'alert_acknowledged':
            return <AlertDetails details={event.details} config={config} />;
        case 'restart':
            return <RestartDetails details={event.details} isDark={isDark} />;
        default:
            return null;
    }
});

EventDetails.displayName = 'EventDetails';

/**
 * Single Event Card in the detail popover
 */
const SingleEventCard = memo(({ event, isDark, isCompact = false }) => {
    const config = getEventConfig(event);
    const EventIcon = config.icon;

    return (
        <Box sx={{ mb: isCompact ? 1.5 : 0 }}>
            {/* Header */}
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
                <Box
                    sx={{
                        width: isCompact ? 24 : 32,
                        height: isCompact ? 24 : 32,
                        borderRadius: 1,
                        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        flexShrink: 0,
                    }}
                >
                    <EventIcon sx={{ fontSize: isCompact ? 14 : 18, color: config.color }} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            fontSize: isCompact ? '0.8125rem' : '0.875rem',
                            color: 'text.primary',
                            lineHeight: 1.3,
                        }}
                    >
                        {event.title}
                    </Typography>
                    <Typography
                        sx={{
                            fontSize: '0.6875rem',
                            color: 'text.secondary',
                            mt: 0.25,
                        }}
                    >
                        {formatFullTime(event.occurred_at)}
                        {event.server_name && (
                            <Typography
                                component="span"
                                sx={{ color: 'text.disabled', fontSize: 'inherit' }}
                            >
                                {' · '}{event.server_name}
                            </Typography>
                        )}
                    </Typography>
                </Box>
            </Box>

            {/* Summary */}
            {event.summary && (
                <Typography
                    sx={{
                        mt: 0.75,
                        fontSize: '0.75rem',
                        color: 'text.secondary',
                        lineHeight: 1.4,
                        pl: isCompact ? 4 : 5.5,
                    }}
                >
                    {event.summary}
                </Typography>
            )}

            {/* Type-specific details */}
            <Box sx={{ pl: isCompact ? 4 : 5.5 }}>
                <EventDetails event={event} config={config} isDark={isDark} />
            </Box>
        </Box>
    );
});

SingleEventCard.displayName = 'SingleEventCard';

/**
 * CollapsibleEventCard - A single event card that can be expanded/collapsed
 */
const CollapsibleEventCard = memo(({ event, isDark, defaultExpanded = true }) => {
    const [expanded, setExpanded] = useState(defaultExpanded);
    const config = getEventConfig(event);
    const EventIcon = config.icon;

    return (
        <Box
            sx={{
                borderRadius: 1,
                bgcolor: isDark ? alpha('#334155', 0.3) : alpha('#F9FAFB', 0.8),
                border: '1px solid',
                borderColor: isDark ? alpha('#334155', 0.5) : '#E5E7EB',
                flexShrink: 0,
            }}
        >
            {/* Collapsible header */}
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                    p: 1.5,
                    cursor: 'pointer',
                    '&:hover': {
                        bgcolor: isDark ? alpha('#334155', 0.2) : alpha('#E5E7EB', 0.3),
                    },
                }}
            >
                <Box
                    sx={{
                        width: 28,
                        height: 28,
                        borderRadius: 1,
                        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        flexShrink: 0,
                    }}
                >
                    <EventIcon sx={{ fontSize: 16, color: config.color }} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            fontSize: '0.8125rem',
                            color: 'text.primary',
                            lineHeight: 1.3,
                        }}
                    >
                        {event.title}
                    </Typography>
                    <Typography
                        sx={{
                            fontSize: '0.6875rem',
                            color: 'text.secondary',
                        }}
                    >
                        {formatFullTime(event.occurred_at)}
                        {event.server_name && (
                            <Typography
                                component="span"
                                sx={{ color: 'text.disabled', fontSize: 'inherit' }}
                            >
                                {' · '}{event.server_name}
                            </Typography>
                        )}
                    </Typography>
                </Box>
                <IconButton
                    size="small"
                    sx={{
                        p: 0.25,
                        color: 'text.secondary',
                    }}
                >
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
            </Box>

            {/* Collapsible content */}
            <Collapse in={expanded}>
                <Box sx={{ px: 1.5, pb: 1.5 }}>
                    {/* Summary */}
                    {event.summary && (
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                color: 'text.secondary',
                                lineHeight: 1.4,
                                mb: 1,
                            }}
                        >
                            {event.summary}
                        </Typography>
                    )}

                    {/* Type-specific details */}
                    <EventDetails event={event} config={config} isDark={isDark} />
                </Box>
            </Collapse>
        </Box>
    );
});

CollapsibleEventCard.displayName = 'CollapsibleEventCard';

/**
 * EventDetailPanel - Shows detailed information about an event or cluster in an inline panel
 */
const EventDetailPanel = memo(({ events, onClose, isDark }) => {
    if (!events || events.length === 0) return null;

    const isCluster = events.length > 1;
    const primaryEvent = events[0];

    // Count events by type for cluster header
    const typeCounts = useMemo(() => {
        if (!isCluster) return null;
        const counts = {};
        events.forEach(e => {
            const typeConfig = EVENT_TYPE_CONFIG[e.event_type];
            const label = typeConfig?.label || e.event_type;
            counts[label] = (counts[label] || 0) + 1;
        });
        return Object.entries(counts).map(([label, count]) => `${count} ${label}`).join(', ');
    }, [events, isCluster]);

    return (
        <Box
            sx={{
                mt: 2,
                p: 2,
                borderRadius: 1.5,
                bgcolor: isDark ? alpha('#1E293B', 0.6) : '#FFFFFF',
                border: '1px solid',
                borderColor: isDark ? '#334155' : '#E5E7EB',
                boxShadow: isDark
                    ? 'inset 0 1px 2px rgba(0, 0, 0, 0.2)'
                    : 'inset 0 1px 2px rgba(0, 0, 0, 0.05)',
            }}
        >
            {/* Panel header with close button */}
            <Box
                sx={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    justifyContent: 'space-between',
                    mb: 2,
                    pb: 1.5,
                    borderBottom: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                }}
            >
                <Box>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            fontSize: '0.875rem',
                            color: 'text.primary',
                        }}
                    >
                        {isCluster ? `${events.length} Events` : 'Event Details'}
                    </Typography>
                    {isCluster && (
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                color: 'text.secondary',
                                mt: 0.25,
                            }}
                        >
                            {typeCounts}
                        </Typography>
                    )}
                </Box>
                <IconButton
                    size="small"
                    onClick={onClose}
                    sx={{
                        p: 0.5,
                        color: 'text.secondary',
                        '&:hover': {
                            bgcolor: isDark ? alpha('#334155', 0.5) : alpha('#E5E7EB', 0.5),
                        },
                    }}
                >
                    <CloseIcon sx={{ fontSize: 18 }} />
                </IconButton>
            </Box>

            {/* Event content - stacked vertically */}
            {isCluster ? (
                <Box
                    sx={{
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 1.5,
                        maxHeight: 400,
                        overflow: 'auto',
                        pb: 0.5,
                    }}
                >
                    {events.map((event, index) => (
                        <CollapsibleEventCard
                            key={event.id || index}
                            event={event}
                            isDark={isDark}
                            defaultExpanded={index === 0}
                        />
                    ))}
                </Box>
            ) : (
                <SingleEventCard event={primaryEvent} isDark={isDark} isCompact={false} />
            )}
        </Box>
    );
});

EventDetailPanel.displayName = 'EventDetailPanel';

/**
 * TimelineCanvas - The actual timeline visualization
 */
const TimelineCanvas = memo(({ events, timeRange, isDark, showServer, onEventClick }) => {
    const { startTime, endTime } = useMemo(() => getTimeRangeBounds(timeRange), [timeRange]);
    const timeMarkers = useMemo(() => generateTimeMarkers(startTime, endTime), [startTime, endTime]);
    const eventClusters = useMemo(
        () => clusterEvents(events, startTime, endTime),
        [events, startTime, endTime]
    );

    return (
        <Box
            sx={{
                position: 'relative',
                height: 80,
                mt: 1,
                mx: 1,
            }}
        >
            {/* Time axis */}
            <Box
                sx={{
                    position: 'absolute',
                    bottom: 0,
                    left: 0,
                    right: 0,
                    height: 20,
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'flex-end',
                }}
            >
                {timeMarkers.map((marker, i) => (
                    <Box
                        key={i}
                        sx={{
                            position: 'absolute',
                            left: `${marker.position}%`,
                            transform: 'translateX(-50%)',
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                        }}
                    >
                        <Box
                            sx={{
                                width: 1,
                                height: 4,
                                bgcolor: isDark ? '#475569' : '#D1D5DB',
                                mb: 0.25,
                            }}
                        />
                        <Typography
                            sx={{
                                fontSize: '0.5625rem',
                                color: 'text.disabled',
                                whiteSpace: 'nowrap',
                            }}
                        >
                            {marker.label}
                        </Typography>
                    </Box>
                ))}
            </Box>

            {/* Timeline track */}
            <Box
                sx={{
                    position: 'absolute',
                    top: 24,
                    left: 0,
                    right: 0,
                    height: 32,
                    borderRadius: 2,
                    bgcolor: isDark ? alpha('#334155', 0.3) : alpha('#E5E7EB', 0.5),
                    border: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                }}
            >
                {/* Event markers */}
                {eventClusters.map((cluster, i) => (
                    <EventMarker
                        key={i}
                        cluster={cluster}
                        isDark={isDark}
                        showServer={showServer}
                        onClick={onEventClick}
                    />
                ))}
            </Box>
        </Box>
    );
});

TimelineCanvas.displayName = 'TimelineCanvas';

/**
 * TimelineHeader - Collapsible header with controls
 */
const TimelineHeader = memo(({
    expanded,
    onExpandToggle,
    eventCount,
    timeRange,
    onTimeRangeChange,
    eventTypes,
    onEventTypesChange,
    isDark,
}) => {
    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                flexWrap: 'wrap',
            }}
        >
            {/* Title with expand toggle */}
            <Box
                onClick={onExpandToggle}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.75,
                    cursor: 'pointer',
                    '&:hover': { opacity: 0.8 },
                }}
            >
                <TimelineIcon sx={{ fontSize: 16, color: 'primary.main' }} />
                <Typography
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.8125rem',
                    }}
                >
                    Event Timeline
                </Typography>
                <Chip
                    label={eventCount}
                    size="small"
                    sx={{
                        height: 18,
                        fontSize: '0.625rem',
                        fontWeight: 600,
                        bgcolor: eventCount > 0
                            ? alpha('#15AABF', isDark ? 0.15 : 0.1)
                            : (isDark ? alpha('#64748B', 0.2) : alpha('#64748B', 0.1)),
                        color: eventCount > 0 ? 'primary.main' : 'text.secondary',
                        '& .MuiChip-label': { px: 0.5 },
                    }}
                />
                <IconButton size="small" sx={{ p: 0.25 }}>
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 16 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 16 }} />
                    )}
                </IconButton>
            </Box>

            <Box sx={{ flex: 1 }} />

            {/* Time range selector */}
            <ToggleButtonGroup
                value={timeRange}
                exclusive
                onChange={(e, value) => value && onTimeRangeChange(value)}
                size="small"
                sx={{
                    height: 24,
                    border: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                    borderRadius: 1,
                    '& .MuiToggleButton-root': {
                        px: 1,
                        py: 0,
                        fontSize: '0.625rem',
                        fontWeight: 600,
                        textTransform: 'none',
                        color: 'text.secondary',
                        border: 'none',
                        borderRadius: 0,
                        '&.Mui-selected': {
                            bgcolor: isDark ? alpha('#22B8CF', 0.15) : alpha('#15AABF', 0.1),
                            color: 'primary.main',
                            '&:hover': {
                                bgcolor: isDark ? alpha('#22B8CF', 0.2) : alpha('#15AABF', 0.15),
                            },
                        },
                        '&:hover': {
                            bgcolor: isDark ? alpha('#334155', 0.5) : alpha('#E5E7EB', 0.5),
                        },
                        '&:first-of-type': {
                            borderTopLeftRadius: 3,
                            borderBottomLeftRadius: 3,
                        },
                        '&:last-of-type': {
                            borderTopRightRadius: 3,
                            borderBottomRightRadius: 3,
                        },
                    },
                }}
            >
                {TIME_RANGE_OPTIONS.map((option) => (
                    <ToggleButton key={option.value} value={option.value}>
                        {option.label}
                    </ToggleButton>
                ))}
            </ToggleButtonGroup>

            {/* Event type filter chips */}
            <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                {Object.entries(EVENT_TYPE_CONFIG).map(([type, config]) => {
                    const isSelected = eventTypes.includes('all') || eventTypes.includes(type);
                    return (
                        <Chip
                            key={type}
                            label={config.label}
                            size="small"
                            onClick={() => {
                                if (eventTypes.includes('all')) {
                                    // Switching from 'all' - select only this type
                                    onEventTypesChange([type]);
                                } else if (isSelected && eventTypes.length === 1) {
                                    // Last one selected - switch back to all
                                    onEventTypesChange(['all']);
                                } else if (isSelected) {
                                    // Deselect this type
                                    onEventTypesChange(eventTypes.filter(t => t !== type));
                                } else {
                                    // Select this type
                                    const newTypes = [...eventTypes, type];
                                    if (newTypes.length === ALL_EVENT_TYPES.length) {
                                        onEventTypesChange(['all']);
                                    } else {
                                        onEventTypesChange(newTypes);
                                    }
                                }
                            }}
                            sx={{
                                height: 20,
                                fontSize: '0.5625rem',
                                fontWeight: 500,
                                cursor: 'pointer',
                                bgcolor: isSelected
                                    ? alpha(config.color, isDark ? 0.2 : 0.15)
                                    : 'transparent',
                                color: isSelected ? config.color : 'text.disabled',
                                border: '1px solid',
                                borderColor: isSelected
                                    ? alpha(config.color, 0.4)
                                    : (isDark ? '#334155' : '#E5E7EB'),
                                '& .MuiChip-label': { px: 0.5 },
                                '&:hover': {
                                    bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
                                },
                            }}
                        />
                    );
                })}
            </Box>
        </Box>
    );
});

TimelineHeader.displayName = 'TimelineHeader';

/**
 * LoadingSkeleton - Skeleton state while loading
 */
const LoadingSkeleton = ({ isDark }) => (
    <Box sx={{ mt: 1, mx: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Skeleton variant="circular" width={16} height={16} />
            <Skeleton variant="text" width={100} height={20} />
            <Skeleton variant="rounded" width={24} height={18} />
        </Box>
        <Skeleton
            variant="rounded"
            height={32}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
        />
        <Box sx={{ display: 'flex', justifyContent: 'space-between', mt: 0.5 }}>
            {[1, 2, 3, 4, 5].map((i) => (
                <Skeleton key={i} variant="text" width={40} height={14} />
            ))}
        </Box>
    </Box>
);

/**
 * EmptyState - Shown when no events found
 */
const EmptyState = ({ isDark }) => (
    <Box
        sx={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            py: 3,
            borderRadius: 1,
            bgcolor: isDark ? alpha('#334155', 0.2) : alpha('#F3F4F6', 0.5),
            border: '1px dashed',
            borderColor: isDark ? '#334155' : '#E5E7EB',
            mt: 1,
        }}
    >
        <TimelineIcon sx={{ fontSize: 24, color: 'text.disabled', mb: 0.5 }} />
        <Typography
            sx={{
                color: 'text.secondary',
                fontSize: '0.8125rem',
                fontWeight: 500,
            }}
        >
            No events in this time range
        </Typography>
        <Typography
            sx={{
                color: 'text.disabled',
                fontSize: '0.75rem',
            }}
        >
            Try expanding the time range or adjusting filters
        </Typography>
    </Box>
);

/**
 * EventTimeline - Main component for displaying server events on a timeline
 */
const EventTimeline = ({ selection, mode = 'light' }) => {
    const isDark = mode === 'dark';

    // Internal state
    const [timeRange, setTimeRange] = useState(getInitialTimeRange);
    const [eventTypes, setEventTypes] = useState(['all']);
    const [expanded, setExpanded] = useState(true);
    const [selectedEvents, setSelectedEvents] = useState(null);

    // Persist time range preference to localStorage
    useEffect(() => {
        try {
            localStorage.setItem(TIME_RANGE_STORAGE_KEY, timeRange);
        } catch {
            // localStorage not available
        }
    }, [timeRange]);

    // Close detail panel when selection changes
    useEffect(() => {
        setSelectedEvents(null);
    }, [selection?.type, selection?.id, selection?.serverIds?.join(',')]);

    // Determine connection ID(s) based on selection - memoize to prevent unnecessary re-fetches
    const connectionId = selection?.type === 'server' ? selection.id : null;
    const connectionIds = useMemo(
        () => (selection?.type !== 'server' ? selection?.serverIds : null),
        // Only recreate when serverIds actually changes (by value, not reference)
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [selection?.type, selection?.serverIds?.join(',')]
    );

    // Fetch events using the hook
    const { events, loading, totalCount } = useTimelineEvents({
        connectionId,
        connectionIds,
        timeRange,
        eventTypes: eventTypes.includes('all') ? ['all'] : eventTypes,
        enabled: Boolean(selection),
    });

    // Handle event click - shows all events in cluster in the detail panel
    const handleEventClick = useCallback((e, cluster) => {
        setSelectedEvents(cluster.events);
    }, []);

    // Handle panel close
    const handlePanelClose = useCallback(() => {
        setSelectedEvents(null);
    }, []);

    // Show server name when not viewing a single server
    const showServer = selection?.type !== 'server';

    if (!selection) {
        return null;
    }

    return (
        <Box
            sx={{
                mt: 2,
                p: 1.5,
                borderRadius: 1.5,
                bgcolor: isDark ? alpha('#1E293B', 0.4) : alpha('#F9FAFB', 0.8),
                border: '1px solid',
                borderColor: isDark ? '#334155' : '#E2E8F0',
            }}
        >
            <TimelineHeader
                expanded={expanded}
                onExpandToggle={() => setExpanded(!expanded)}
                eventCount={totalCount}
                timeRange={timeRange}
                onTimeRangeChange={setTimeRange}
                eventTypes={eventTypes}
                onEventTypesChange={setEventTypes}
                isDark={isDark}
            />

            <Collapse in={expanded}>
                {loading ? (
                    <LoadingSkeleton isDark={isDark} />
                ) : events.length === 0 ? (
                    <EmptyState isDark={isDark} />
                ) : (
                    <>
                        <TimelineCanvas
                            events={events}
                            timeRange={timeRange}
                            isDark={isDark}
                            showServer={showServer}
                            onEventClick={handleEventClick}
                        />
                        <EventDetailPanel
                            events={selectedEvents}
                            onClose={handlePanelClose}
                            isDark={isDark}
                        />
                    </>
                )}
            </Collapse>
        </Box>
    );
};

export default memo(EventTimeline);
