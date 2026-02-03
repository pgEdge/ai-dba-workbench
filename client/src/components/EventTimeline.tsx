/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
    useTheme,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
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
    DoNotDisturb as DoNotDisturbIcon,
    DoNotDisturbOff as DoNotDisturbOffIcon,
} from '@mui/icons-material';
import { useTimelineEvents } from '../hooks/useTimelineEvents';

// Event type configuration with icons and theme-based color keys
const EVENT_TYPE_CONFIG = {
    config_change: {
        icon: SettingsIcon,
        colorKey: 'primary.main',
        label: 'Config',
    },
    hba_change: {
        icon: SecurityIcon,
        colorKey: 'custom.status.sky',
        label: 'HBA',
    },
    ident_change: {
        icon: AccountCircleIcon,
        colorKey: 'info.main',
        label: 'Ident',
    },
    restart: {
        icon: PowerSettingsNewIcon,
        colorKey: 'warning.main',
        label: 'Restart',
    },
    alert_fired: {
        icon: WarningIcon,
        colorKey: 'error.main',
        label: 'Alert',
        getSeverityColorKey: (severity) => {
            return severity === 'critical' ? 'error.main' : 'warning.main';
        },
        getSeverityIcon: (severity) => {
            return severity === 'critical' ? ErrorIcon : WarningIcon;
        },
    },
    alert_cleared: {
        icon: CheckCircleIcon,
        colorKey: 'success.main',
        label: 'Cleared',
    },
    alert_acknowledged: {
        icon: CheckCircleOutlineIcon,
        colorKey: 'custom.status.purple',
        label: 'Acked',
    },
    extension_change: {
        icon: ExtensionIcon,
        colorKey: 'custom.status.cyan',
        label: 'Extension',
    },
    blackout_started: {
        icon: DoNotDisturbIcon,
        colorKey: 'custom.status.skyDark',
        label: 'Blackout',
    },
    blackout_ended: {
        icon: DoNotDisturbOffIcon,
        colorKey: 'custom.status.skyDark',
        label: 'Blk End',
    },
};

// All event types for filter
const ALL_EVENT_TYPES = Object.keys(EVENT_TYPE_CONFIG);

// Filter chip definitions — groups related event types under a single chip.
// Each entry maps a chip key to its display label, theme color key, and
// the underlying event types it controls.
const FILTER_CHIPS: Record<string, { label: string; colorKey: string; types: string[] }> = {
    config_change: { label: 'Config', colorKey: 'primary.main', types: ['config_change'] },
    hba_change: { label: 'HBA', colorKey: 'custom.status.sky', types: ['hba_change'] },
    ident_change: { label: 'Ident', colorKey: 'info.main', types: ['ident_change'] },
    restart: { label: 'Restart', colorKey: 'warning.main', types: ['restart'] },
    alert_fired: { label: 'Alert', colorKey: 'error.main', types: ['alert_fired'] },
    alert_cleared: { label: 'Cleared', colorKey: 'success.main', types: ['alert_cleared'] },
    alert_acknowledged: { label: 'Acked', colorKey: 'custom.status.purple', types: ['alert_acknowledged'] },
    extension_change: { label: 'Extension', colorKey: 'custom.status.cyan', types: ['extension_change'] },
    blackouts: { label: 'Blackouts', colorKey: 'custom.status.skyDark', types: ['blackout_started', 'blackout_ended'] },
};

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
 * Resolve a dotted path like 'primary.main' from the theme palette
 */
const resolveColor = (palette, colorKey: string): string => {
    const parts = colorKey.split('.');
    let value = palette;
    for (const part of parts) {
        value = value?.[part];
    }
    return typeof value === 'string' ? value : palette?.primary?.main ?? '#1976d2';
};

/**
 * Get event configuration with potential severity override, resolved against theme
 */
const getEventConfig = (event, palette) => {
    const config = EVENT_TYPE_CONFIG[event.event_type] || EVENT_TYPE_CONFIG.config_change;

    let colorKey = config.colorKey;
    let icon = config.icon;

    // Handle severity-based color and icon for alerts
    if (event.event_type === 'alert_fired' && config.getSeverityColorKey) {
        const severity = event.details?.severity;
        colorKey = config.getSeverityColorKey(severity);
        icon = config.getSeverityIcon(severity);
    }

    return {
        ...config,
        icon,
        colorKey,
        color: palette ? resolveColor(palette, colorKey) : colorKey,
    };
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

// ---- Extracted static style constants ----

const tooltipPaddingSx = { p: 0.5 };

const tooltipClusterTitleSx = { fontSize: '0.75rem', fontWeight: 600 };

const tooltipClusterItemSx = { fontSize: '0.6875rem', color: 'grey.300' };

const tooltipClusterMoreSx = { fontSize: '0.6875rem', color: 'grey.400' };

const tooltipSingleTitleSx = { fontSize: '0.75rem', fontWeight: 600 };

const tooltipSingleTimeSx = { fontSize: '0.6875rem', color: 'grey.300' };

const tooltipSingleServerSx = { fontSize: '0.6875rem', color: 'grey.400' };

const markerIconSx = { fontSize: 14 };

const clusterBadgeBaseSx = {
    position: 'absolute',
    top: -6,
    right: -6,
    minWidth: 16,
    height: 16,
    px: 0.5,
    borderRadius: '8px',
    color: 'common.white',
    fontSize: '0.5625rem',
    fontWeight: 700,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    lineHeight: 1,
};

const getClusterBadgeSx = (theme: Theme) => ({
    ...clusterBadgeBaseSx,
    bgcolor: theme.palette.grey[600],
});

const sectionLabelSx = {
    fontSize: '0.6875rem',
    fontWeight: 600,
    color: 'text.secondary',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    mb: 0.5,
};

const sectionLabelShortSx = {
    ...sectionLabelSx,
    mb: 0.25,
};

const getCodeBlockSx = (theme: Theme) => ({
    p: 1,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.grey[100],
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.75rem',
    maxHeight: 300,
    overflow: 'auto',
});

const getCodeBlockSmallSx = (theme: Theme) => ({
    ...getCodeBlockSx(theme),
    fontSize: '0.6875rem',
});

const settingNameSx = {
    color: 'primary.main',
    fontWeight: 600,
    fontFamily: 'inherit',
    fontSize: 'inherit',
};

const settingValueSx = {
    color: 'text.secondary',
    fontFamily: 'inherit',
    fontSize: 'inherit',
};

const hbaRuleTextSx = {
    fontFamily: 'inherit',
    fontSize: 'inherit',
    color: 'text.primary',
};

const falsePositiveChipSx = (theme: Theme) => ({
    ml: 1,
    height: 16,
    fontSize: '0.5625rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    bgcolor: alpha(theme.palette.warning.main, 0.15),
    color: theme.palette.warning.main,
    '& .MuiChip-label': { px: 0.5 },
});

const metricValueMonoSx = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
    fontWeight: 600,
};

const metricUnitSx = {
    fontWeight: 400,
    color: 'text.secondary',
    fontSize: '0.75rem',
    ml: 0.5,
};

const thresholdSx = {
    fontWeight: 400,
    color: 'text.secondary',
    fontSize: '0.75rem',
};

const severityChipSx = (color) => ({
    height: 18,
    fontSize: '0.5625rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    bgcolor: alpha(color, 0.15),
    color: color,
    '& .MuiChip-label': { px: 0.75 },
});

const restartCodeBlockSx = (theme: Theme) => ({
    p: 1,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.grey[100],
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.75rem',
});

const databaseNameSx = (theme: Theme) => ({
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.8125rem',
    fontWeight: 500,
    color: theme.palette.secondary.main,
});

const ackNameSx = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.8125rem',
    fontWeight: 500,
    color: 'text.primary',
};

const ackMessageSx = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    mt: 0.5,
    fontStyle: 'italic',
};

const expandableShowMoreBaseSx = {
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
};

const expandIconSmallSx = { fontSize: 14 };

const getCollapsibleCardSx = (theme: Theme) => ({
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.3)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.divider,
    flexShrink: 0,
});

const getCollapsibleHeaderHoverSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    p: 1.5,
    cursor: 'pointer',
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.2)
            : alpha(theme.palette.divider, 0.3),
    },
});

const collapsibleTitleSx = {
    fontWeight: 600,
    fontSize: '0.8125rem',
    color: 'text.primary',
    lineHeight: 1.3,
};

const collapsibleTimeSx = {
    fontSize: '0.6875rem',
    color: 'text.secondary',
};

const serverDisabledSx = { color: 'text.disabled', fontSize: 'inherit' };

const collapseToggleSx = {
    p: 0.25,
    color: 'text.secondary',
};

const collapseContentSx = { px: 1.5, pb: 1.5 };

const summarySx = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    lineHeight: 1.4,
    mb: 1,
};

const getDetailPanelSx = (theme: Theme) => ({
    mt: 2,
    p: 2,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.6)
        : theme.palette.background.paper,
    border: '1px solid',
    borderColor: theme.palette.divider,
    boxShadow: theme.palette.mode === 'dark'
        ? 'inset 0 1px 2px rgba(0, 0, 0, 0.2)'
        : 'inset 0 1px 2px rgba(0, 0, 0, 0.05)',
});

const getDetailPanelHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    mb: 2,
    pb: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
});

const detailPanelTitleSx = {
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.primary',
};

const detailPanelSubtitleSx = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    mt: 0.25,
};

const getCloseButtonSx = (theme: Theme) => ({
    p: 0.5,
    color: 'text.secondary',
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.5)
            : alpha(theme.palette.divider, 0.5),
    },
});

const closeIconSx = { fontSize: 18 };

const clusterListSx = {
    display: 'flex',
    flexDirection: 'column',
    gap: 1.5,
    maxHeight: 400,
    overflow: 'auto',
    pb: 0.5,
};

const timelineCanvasContainerSx = {
    position: 'relative',
    height: 80,
    mt: 1,
    mx: 1,
};

const timeAxisSx = {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    height: 20,
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'flex-end',
};

const getTickMarkSx = (theme: Theme) => ({
    width: 1,
    height: 4,
    bgcolor: theme.palette.grey[theme.palette.mode === 'dark' ? 600 : 300],
    mb: 0.25,
});

const tickLabelSx = {
    fontSize: '0.5625rem',
    color: 'text.secondary',
    whiteSpace: 'nowrap',
};

const getTimelineTrackSx = (theme: Theme) => ({
    position: 'absolute',
    top: 24,
    left: 0,
    right: 0,
    height: 32,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.3)
        : alpha(theme.palette.divider, 0.5),
    border: '1px solid',
    borderColor: theme.palette.divider,
});

const headerContainerSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    flexWrap: 'wrap',
};

const headerTitleGroupSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    '&:hover': { opacity: 0.8 },
};

const headerTitleIconSx = { fontSize: 16, color: 'primary.main' };

const headerTitleTextSx = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '0.8125rem',
};

const headerExpandSx = { p: 0.25 };

const expandIconMedSx = { fontSize: 16 };

const filterChipsSx = { display: 'flex', gap: 0.5, flexWrap: 'wrap' };

const getToggleGroupSx = (theme: Theme) => ({
    height: 24,
    border: '1px solid',
    borderColor: theme.palette.divider,
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
            bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1),
            color: 'primary.main',
            '&:hover': {
                bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.2 : 0.15),
            },
        },
        '&:hover': {
            bgcolor: theme.palette.mode === 'dark'
                ? alpha(theme.palette.grey[700], 0.5)
                : alpha(theme.palette.divider, 0.5),
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
});

const getEventCountChipSx = (theme, eventCount) => ({
    height: 18,
    fontSize: '0.625rem',
    fontWeight: 600,
    bgcolor: eventCount > 0
        ? alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1)
        : alpha(theme.palette.grey[500], theme.palette.mode === 'dark' ? 0.2 : 0.1),
    color: eventCount > 0 ? 'primary.main' : 'text.secondary',
    '& .MuiChip-label': { px: 0.5 },
});

const getFilterChipSx = (theme, isSelected, color) => ({
    height: 20,
    fontSize: '0.5625rem',
    fontWeight: 500,
    cursor: 'pointer',
    bgcolor: isSelected
        ? alpha(color, theme.palette.mode === 'dark' ? 0.2 : 0.15)
        : 'transparent',
    color: isSelected ? color : 'text.disabled',
    border: '1px solid',
    borderColor: isSelected
        ? alpha(color, 0.4)
        : theme.palette.divider,
    '& .MuiChip-label': { px: 0.5 },
    '&:hover': {
        bgcolor: alpha(color, theme.palette.mode === 'dark' ? 0.15 : 0.1),
    },
});

const loadingSkeletonRowSx = { display: 'flex', alignItems: 'center', gap: 1, mb: 1 };

const loadingSkeletonBarSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

const loadingSkeletonTimeSx = { display: 'flex', justifyContent: 'space-between', mt: 0.5 };

const getEmptyStateSx = (theme: Theme) => ({
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    py: 3,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.2)
        : alpha(theme.palette.grey[100], 0.5),
    border: '1px dashed',
    borderColor: theme.palette.divider,
    mt: 1,
});

const emptyStateTitleSx = {
    color: 'text.secondary',
    fontSize: '0.8125rem',
    fontWeight: 500,
};

const emptyStateSubtitleSx = {
    color: 'text.disabled',
    fontSize: '0.75rem',
};

const getOuterContainerSx = (theme: Theme) => ({
    mt: 2,
    p: 1.5,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.4)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: theme.palette.divider,
});

// ---- End extracted style constants ----

/**
 * EventMarker - Single event or cluster marker on the timeline
 */
const EventMarker = memo(({ cluster, showServer, onClick }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const isCluster = cluster.events.length > 1;
    const primaryEvent = cluster.events[0];
    const config = getEventConfig(primaryEvent, theme.palette);
    const EventIcon = config.icon;

    // For clusters, determine if there are mixed types or severities
    const hasMixedTypes = isCluster && new Set(cluster.events.map(e => e.event_type)).size > 1;
    const hasCritical = cluster.events.some(
        e => e.event_type === 'alert_fired' && e.details?.severity === 'critical'
    );

    const markerColor = hasCritical ? theme.palette.error.main : config.color;

    const markerOuterSx = useMemo(() => ({
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
    }), [cluster.position]);

    const markerInnerSx = useMemo(() => ({
        position: 'relative',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: isCluster ? 28 : 24,
        height: isCluster ? 28 : 24,
        borderRadius: '50%',
        bgcolor: alpha(markerColor, isDark ? 0.2 : 0.15),
        border: '2px solid',
        borderColor: markerColor,
        transition: 'transform 0.15s ease',
    }), [isCluster, markerColor, isDark]);

    const iconColorSx = useMemo(() => ({ fontSize: 14, color: markerColor }), [markerColor]);

    const badgeSx = useMemo(() => getClusterBadgeSx(theme), [theme]);

    return (
        <Tooltip
            title={
                <Box sx={tooltipPaddingSx}>
                    {isCluster ? (
                        <>
                            <Typography sx={tooltipClusterTitleSx}>
                                {cluster.events.length} events
                            </Typography>
                            {cluster.events.slice(0, 3).map((e, i) => (
                                <Typography key={i} sx={tooltipClusterItemSx}>
                                    {e.title}
                                </Typography>
                            ))}
                            {cluster.events.length > 3 && (
                                <Typography sx={tooltipClusterMoreSx}>
                                    +{cluster.events.length - 3} more
                                </Typography>
                            )}
                        </>
                    ) : (
                        <>
                            <Typography sx={tooltipSingleTitleSx}>
                                {primaryEvent.title}
                            </Typography>
                            <Typography sx={tooltipSingleTimeSx}>
                                {formatEventTime(primaryEvent.occurred_at)}
                            </Typography>
                            {showServer && primaryEvent.server_name && (
                                <Typography sx={tooltipSingleServerSx}>
                                    {primaryEvent.server_name}
                                    {primaryEvent.details?.database_name && ` / ${primaryEvent.details.database_name}`}
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
                sx={markerOuterSx}
            >
                <Box sx={markerInnerSx}>
                    {hasMixedTypes ? (
                        <EventNoteIcon sx={iconColorSx} />
                    ) : (
                        <EventIcon sx={iconColorSx} />
                    )}
                    {isCluster && (
                        <Box sx={badgeSx}>
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
const ExpandableList = memo(({ items, initialLimit, renderItem, emptyText }) => {
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
                    sx={expandableShowMoreBaseSx}
                >
                    {showAll ? (
                        <>
                            <ExpandLessIcon sx={expandIconSmallSx} />
                            Show less
                        </>
                    ) : (
                        <>
                            <ExpandMoreIcon sx={expandIconSmallSx} />
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
const ConfigChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const settings = details?.settings || [];
    const count = details?.setting_count || settings.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Settings ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={settings}
                    initialLimit={10}
                    emptyText={`${count} settings`}
                    renderItem={(setting, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={settingNameSx}
                            >
                                {setting.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={settingValueSx}
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
const ExtensionChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const extensions = details?.extensions || [];
    const count = details?.extension_count || extensions.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Extensions ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={extensions}
                    initialLimit={10}
                    emptyText={`${count} extensions`}
                    renderItem={(ext, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={{
                                    color: theme.palette.custom.status.cyan,
                                    fontWeight: 600,
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                }}
                            >
                                {ext.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={settingValueSx}
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
const HbaChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const rules = details?.rules || [];
    const count = details?.rule_count || rules.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSmallSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                HBA Rules ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={rules}
                    initialLimit={8}
                    emptyText={`${count} rules`}
                    renderItem={(rule, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography sx={hbaRuleTextSx}>
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
const IdentChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const mappings = details?.mappings || [];
    const count = details?.mapping_count || mappings.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSmallSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Ident Mappings ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={mappings}
                    initialLimit={8}
                    emptyText={`${count} mappings`}
                    renderItem={(mapping, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography sx={hbaRuleTextSx}>
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
    const theme = useTheme();

    const dbNameSx = useMemo(() => databaseNameSx(theme), [theme]);
    const fpChipSx = useMemo(() => falsePositiveChipSx(theme), [theme]);
    const sevChipSx = useMemo(() => severityChipSx(config.color), [config.color]);

    return (
        <Box sx={{ mt: 1 }}>
            {/* Database name if present */}
            {details?.database_name && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Database
                    </Typography>
                    <Typography sx={dbNameSx}>
                        {details.database_name}
                    </Typography>
                </Box>
            )}
            {/* Acknowledged by info */}
            {details?.acknowledged_by && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Acknowledged By
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.acknowledged_by}
                        {details.false_positive && (
                            <Chip
                                label="False Positive"
                                size="small"
                                sx={fpChipSx}
                            />
                        )}
                    </Typography>
                    {details.message && (
                        <Typography sx={ackMessageSx}>
                            "{details.message}"
                        </Typography>
                    )}
                </Box>
            )}
            {details?.metric_value !== undefined && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Metric Value
                    </Typography>
                    <Typography
                        sx={{
                            ...metricValueMonoSx,
                            color: config.color,
                        }}
                    >
                        {details.metric_value}
                        {details.metric_unit && (
                            <Typography
                                component="span"
                                sx={metricUnitSx}
                            >
                                {details.metric_unit}
                            </Typography>
                        )}
                        {details.threshold_value !== undefined && (
                            <Typography
                                component="span"
                                sx={thresholdSx}
                            >
                                {' '}/ threshold: {details.threshold_value}
                                {details.metric_unit && ` ${details.metric_unit}`}
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
                    sx={sevChipSx}
                />
            )}
        </Box>
    );
});

AlertDetails.displayName = 'AlertDetails';

/**
 * RestartDetails - Shows server restart details
 */
const RestartDetails = memo(({ details }) => {
    const theme = useTheme();

    if (!details?.previous_timeline && !details?.old_timeline_id) {
        return null;
    }

    const codeBlockSx = useMemo(() => restartCodeBlockSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Box sx={codeBlockSx}>
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
 * BlackoutDetails - Shows blackout started/ended details
 */
const BlackoutDetails = memo(({ details, eventType }) => {
    return (
        <Box sx={{ mt: 1 }}>
            {details?.scope && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Scope
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.scope}
                    </Typography>
                </Box>
            )}
            {details?.reason && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Reason
                    </Typography>
                    <Typography sx={{ fontSize: '0.75rem', color: 'text.secondary' }}>
                        {details.reason}
                    </Typography>
                </Box>
            )}
            {details?.created_by && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Created By
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.created_by}
                    </Typography>
                </Box>
            )}
            {eventType === 'blackout_started' && details?.end_time && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        End Time
                    </Typography>
                    <Typography sx={{ fontSize: '0.75rem', color: 'text.secondary' }}>
                        {formatFullTime(details.end_time)}
                    </Typography>
                </Box>
            )}
        </Box>
    );
});

BlackoutDetails.displayName = 'BlackoutDetails';

/**
 * EventDetails - Renders the appropriate details component based on event type
 */
const EventDetails = memo(({ event, config }) => {
    if (!event.details) return null;

    switch (event.event_type) {
        case 'config_change':
            return <ConfigChangeDetails details={event.details} />;
        case 'extension_change':
            return <ExtensionChangeDetails details={event.details} />;
        case 'hba_change':
            return <HbaChangeDetails details={event.details} />;
        case 'ident_change':
            return <IdentChangeDetails details={event.details} />;
        case 'alert_fired':
        case 'alert_cleared':
        case 'alert_acknowledged':
            return <AlertDetails details={event.details} config={config} />;
        case 'restart':
            return <RestartDetails details={event.details} />;
        case 'blackout_started':
        case 'blackout_ended':
            return <BlackoutDetails details={event.details} eventType={event.event_type} />;
        default:
            return null;
    }
});

EventDetails.displayName = 'EventDetails';

/**
 * Single Event Card in the detail popover
 */
const SingleEventCard = memo(({ event, isCompact = false }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const config = getEventConfig(event, theme.palette);
    const EventIcon = config.icon;

    const iconBoxSx = useMemo(() => ({
        width: isCompact ? 24 : 32,
        height: isCompact ? 24 : 32,
        borderRadius: 1,
        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
    }), [isCompact, config.color, isDark]);

    const iconSx = useMemo(
        () => ({ fontSize: isCompact ? 14 : 18, color: config.color }),
        [isCompact, config.color]
    );

    return (
        <Box sx={{ mb: isCompact ? 1.5 : 0 }}>
            {/* Header */}
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
                <Box sx={iconBoxSx}>
                    <EventIcon sx={iconSx} />
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
                                sx={serverDisabledSx}
                            >
                                {' · '}{event.server_name}
                                {event.details?.database_name && ` / ${event.details.database_name}`}
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
                <EventDetails event={event} config={config} />
            </Box>
        </Box>
    );
});

SingleEventCard.displayName = 'SingleEventCard';

/**
 * CollapsibleEventCard - A single event card that can be expanded/collapsed
 */
const CollapsibleEventCard = memo(({ event, defaultExpanded = true }) => {
    const [expanded, setExpanded] = useState(defaultExpanded);
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const config = getEventConfig(event, theme.palette);
    const EventIcon = config.icon;

    const cardSx = useMemo(() => getCollapsibleCardSx(theme), [theme]);
    const headerSx = useMemo(() => getCollapsibleHeaderHoverSx(theme), [theme]);

    const iconBoxSx = useMemo(() => ({
        width: 28,
        height: 28,
        borderRadius: 1,
        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
    }), [config.color, isDark]);

    return (
        <Box sx={cardSx}>
            {/* Collapsible header */}
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={headerSx}
            >
                <Box sx={iconBoxSx}>
                    <EventIcon sx={{ fontSize: 16, color: config.color }} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography sx={collapsibleTitleSx}>
                        {event.title}
                    </Typography>
                    <Typography sx={collapsibleTimeSx}>
                        {formatFullTime(event.occurred_at)}
                        {event.server_name && (
                            <Typography
                                component="span"
                                sx={serverDisabledSx}
                            >
                                {' · '}{event.server_name}
                                {event.details?.database_name && ` / ${event.details.database_name}`}
                            </Typography>
                        )}
                    </Typography>
                </Box>
                <IconButton
                    size="small"
                    sx={collapseToggleSx}
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
                <Box sx={collapseContentSx}>
                    {/* Summary */}
                    {event.summary && (
                        <Typography sx={summarySx}>
                            {event.summary}
                        </Typography>
                    )}

                    {/* Type-specific details */}
                    <EventDetails event={event} config={config} />
                </Box>
            </Collapse>
        </Box>
    );
});

CollapsibleEventCard.displayName = 'CollapsibleEventCard';

/**
 * EventDetailPanel - Shows detailed information about an event or cluster in an inline panel
 */
const EventDetailPanel = memo(({ events, onClose }) => {
    const theme = useTheme();

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

    const panelSx = useMemo(() => getDetailPanelSx(theme), [theme]);
    const panelHeaderSx = useMemo(() => getDetailPanelHeaderSx(theme), [theme]);
    const closeBtnSx = useMemo(() => getCloseButtonSx(theme), [theme]);

    return (
        <Box sx={panelSx}>
            {/* Panel header with close button */}
            <Box sx={panelHeaderSx}>
                <Box>
                    <Typography sx={detailPanelTitleSx}>
                        {isCluster ? `${events.length} Events` : 'Event Details'}
                    </Typography>
                    {isCluster && (
                        <Typography sx={detailPanelSubtitleSx}>
                            {typeCounts}
                        </Typography>
                    )}
                </Box>
                <IconButton
                    size="small"
                    onClick={onClose}
                    sx={closeBtnSx}
                >
                    <CloseIcon sx={closeIconSx} />
                </IconButton>
            </Box>

            {/* Event content - stacked vertically */}
            {isCluster ? (
                <Box sx={clusterListSx}>
                    {events.map((event, index) => (
                        <CollapsibleEventCard
                            key={event.id || index}
                            event={event}
                            defaultExpanded={index === 0}
                        />
                    ))}
                </Box>
            ) : (
                <SingleEventCard event={primaryEvent} isCompact={false} />
            )}
        </Box>
    );
});

EventDetailPanel.displayName = 'EventDetailPanel';

/**
 * TimelineCanvas - The actual timeline visualization
 */
const TimelineCanvas = memo(({ events, timeRange, showServer, onEventClick }) => {
    const theme = useTheme();
    const { startTime, endTime } = useMemo(() => getTimeRangeBounds(timeRange), [timeRange]);
    const timeMarkers = useMemo(() => generateTimeMarkers(startTime, endTime), [startTime, endTime]);
    const eventClusters = useMemo(
        () => clusterEvents(events, startTime, endTime),
        [events, startTime, endTime]
    );

    const tickSx = useMemo(() => getTickMarkSx(theme), [theme]);
    const trackSx = useMemo(() => getTimelineTrackSx(theme), [theme]);

    return (
        <Box sx={timelineCanvasContainerSx}>
            {/* Time axis */}
            <Box sx={timeAxisSx}>
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
                        <Box sx={tickSx} />
                        <Typography sx={tickLabelSx}>
                            {marker.label}
                        </Typography>
                    </Box>
                ))}
            </Box>

            {/* Timeline track */}
            <Box sx={trackSx}>
                {/* Event markers */}
                {eventClusters.map((cluster, i) => (
                    <EventMarker
                        key={i}
                        cluster={cluster}
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
}) => {
    const theme = useTheme();

    const toggleGroupSx = useMemo(() => getToggleGroupSx(theme), [theme]);
    const countChipSx = useMemo(() => getEventCountChipSx(theme, eventCount), [theme, eventCount]);

    return (
        <Box sx={headerContainerSx}>
            {/* Title with expand toggle */}
            <Box
                onClick={onExpandToggle}
                sx={headerTitleGroupSx}
            >
                <TimelineIcon sx={headerTitleIconSx} />
                <Typography sx={headerTitleTextSx}>
                    Event Timeline
                </Typography>
                <Chip
                    label={eventCount}
                    size="small"
                    sx={countChipSx}
                />
                <IconButton size="small" sx={headerExpandSx}>
                    {expanded ? (
                        <ExpandLessIcon sx={expandIconMedSx} />
                    ) : (
                        <ExpandMoreIcon sx={expandIconMedSx} />
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
                sx={toggleGroupSx}
            >
                {TIME_RANGE_OPTIONS.map((option) => (
                    <ToggleButton key={option.value} value={option.value}>
                        {option.label}
                    </ToggleButton>
                ))}
            </ToggleButtonGroup>

            {/* Event type filter chips */}
            <Box sx={filterChipsSx}>
                {Object.entries(FILTER_CHIPS).map(([chipKey, chipConfig]) => {
                    const chipTypes = chipConfig.types;
                    const isSelected = eventTypes.includes('all') ||
                        chipTypes.every(t => eventTypes.includes(t));
                    const color = resolveColor(theme.palette, chipConfig.colorKey);
                    return (
                        <Chip
                            key={chipKey}
                            label={chipConfig.label}
                            size="small"
                            onClick={() => {
                                if (eventTypes.includes('all')) {
                                    // Switching from 'all' - select only this chip's types
                                    onEventTypesChange([...chipTypes]);
                                } else if (isSelected) {
                                    // Deselect this chip's types
                                    const remaining = eventTypes.filter(t => !chipTypes.includes(t));
                                    if (remaining.length === 0) {
                                        // Last chip selected - switch back to all
                                        onEventTypesChange(['all']);
                                    } else {
                                        onEventTypesChange(remaining);
                                    }
                                } else {
                                    // Select this chip's types
                                    const newTypes = [...eventTypes, ...chipTypes];
                                    if (newTypes.length >= ALL_EVENT_TYPES.length) {
                                        onEventTypesChange(['all']);
                                    } else {
                                        onEventTypesChange(newTypes);
                                    }
                                }
                            }}
                            sx={getFilterChipSx(theme, isSelected, color)}
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
const LoadingSkeleton = () => {
    const theme = useTheme();
    const barSx = useMemo(() => loadingSkeletonBarSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1, mx: 1 }}>
            <Box sx={loadingSkeletonRowSx}>
                <Skeleton variant="circular" width={16} height={16} />
                <Skeleton variant="text" width={100} height={20} />
                <Skeleton variant="rounded" width={24} height={18} />
            </Box>
            <Skeleton
                variant="rounded"
                height={32}
                sx={barSx}
            />
            <Box sx={loadingSkeletonTimeSx}>
                {[1, 2, 3, 4, 5].map((i) => (
                    <Skeleton key={i} variant="text" width={40} height={14} />
                ))}
            </Box>
        </Box>
    );
};

/**
 * EmptyState - Shown when no events found
 */
const EmptyState = () => {
    const theme = useTheme();
    const containerSx = useMemo(() => getEmptyStateSx(theme), [theme]);

    return (
        <Box sx={containerSx}>
            <TimelineIcon sx={{ fontSize: 24, color: 'text.disabled', mb: 0.5 }} />
            <Typography sx={emptyStateTitleSx}>
                No events in this time range
            </Typography>
            <Typography sx={emptyStateSubtitleSx}>
                Try expanding the time range or adjusting filters
            </Typography>
        </Box>
    );
};

/**
 * EventTimeline - Main component for displaying server events on a timeline
 */
interface EventTimelineProps {
    selection: Record<string, unknown> | null;
    mode?: string;
}

const EventTimeline: React.FC<EventTimelineProps> = ({ selection, mode = 'light' }) => {
    const theme = useTheme();

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

    // Memoize the eventTypes array to prevent creating new references on each render
    // This is critical to avoid infinite re-fetch loops in useTimelineEvents
    const memoizedEventTypes = useMemo(
        () => (eventTypes.includes('all') ? ['all'] : eventTypes),
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [eventTypes.join(',')]
    );

    // Fetch events using the hook
    const { events, loading, totalCount } = useTimelineEvents({
        connectionId,
        connectionIds,
        timeRange,
        eventTypes: memoizedEventTypes,
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

    const outerSx = useMemo(() => getOuterContainerSx(theme), [theme]);

    if (!selection) {
        return null;
    }

    return (
        <Box sx={outerSx}>
            <TimelineHeader
                expanded={expanded}
                onExpandToggle={() => setExpanded(!expanded)}
                eventCount={totalCount}
                timeRange={timeRange}
                onTimeRangeChange={setTimeRange}
                eventTypes={eventTypes}
                onEventTypesChange={setEventTypes}
            />

            <Collapse in={expanded}>
                {loading ? (
                    <LoadingSkeleton />
                ) : events.length === 0 ? (
                    <EmptyState />
                ) : (
                    <>
                        <TimelineCanvas
                            events={events}
                            timeRange={timeRange}
                            showServer={showServer}
                            onEventClick={handleEventClick}
                        />
                        <EventDetailPanel
                            events={selectedEvents}
                            onClose={handlePanelClose}
                        />
                    </>
                )}
            </Collapse>
        </Box>
    );
};

export default memo(EventTimeline);
