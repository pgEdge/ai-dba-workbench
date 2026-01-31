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

import React, { useState, useEffect, useMemo, useCallback } from 'react';
import {
    Box,
    Typography,
    Paper,
    Chip,
    alpha,
    IconButton,
    Tooltip,
    Collapse,
    Skeleton,
    Button,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
} from '@mui/material';
import {
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
    Info as InfoIcon,
    CheckCircleOutline as AckIcon,
    Undo as UnackIcon,
    Psychology as AnalyzeIcon,
    TableChart as TableIcon,
} from '@mui/icons-material';
import { useAuth } from '../contexts/AuthContext';
import EventTimeline from './EventTimeline';
import AlertAnalysisDialog from './AlertAnalysisDialog';

// Map internal alert rule names to friendly display names
const FRIENDLY_ALERT_TITLES = {
    // Connection alerts
    'high_connection_count': 'High Connection Count',
    'connection_utilization': 'Connection Utilization',
    // Replication alerts
    'replication_lag_bytes': 'Replication Lag',
    'replication_slot_inactive': 'Replication Slot Inactive',
    // Resource alerts
    'disk_usage_percent': 'Disk Usage',
    'disk_usage_critical': 'Critical Disk Usage',
    'table_bloat_ratio': 'Table Bloat Ratio',
    'cpu_usage_high': 'High CPU Usage',
    'memory_usage_high': 'High Memory Usage',
    'load_average_high': 'High Load Average',
    // Query alerts
    'long_running_queries': 'Long Running Queries',
    'blocked_queries': 'Blocked Queries',
    'long_running_transaction': 'Long Running Transaction',
    'idle_in_transaction': 'Idle in Transaction',
    // Transaction alerts
    'transaction_wraparound': 'Transaction Wraparound',
    'deadlocks_detected': 'Deadlocks Detected',
    'lock_wait_time': 'Lock Wait Time',
    // Maintenance alerts
    'checkpoint_warning': 'Checkpoint Warning',
    'wal_archive_failed': 'WAL Archive Failed',
    'autovacuum_not_running': 'Autovacuum Not Running',
    'dead_tuple_ratio': 'High Dead Tuple Ratio',
    // Performance alerts
    'slow_query_count': 'Slow Query Count',
    'cache_hit_ratio_low': 'Low Cache Hit Ratio',
    'temp_files_created': 'Temporary Files Created',
};

// Get friendly title for an alert
const getFriendlyTitle = (title) => {
    if (!title) return 'Alert';
    // Connection error alerts: preserve hostname as-is
    if (title.toLowerCase().startsWith('connection error:')) {
        return 'Connection Error:' + title.substring('connection error:'.length);
    }
    // Check for exact match first (alert rule names are typically lowercase with underscores)
    const normalizedTitle = title.toLowerCase().trim();
    if (FRIENDLY_ALERT_TITLES[normalizedTitle]) {
        return FRIENDLY_ALERT_TITLES[normalizedTitle];
    }
    // Check for partial matches (handle cases where title might have additional text)
    for (const [key, value] of Object.entries(FRIENDLY_ALERT_TITLES)) {
        if (normalizedTitle.includes(key) || normalizedTitle.startsWith(key)) {
            return value;
        }
    }
    // Fallback: clean up the title by replacing underscores and capitalizing words
    return title
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (char) => char.toUpperCase())
        .trim();
};

// Format threshold info for display
const formatThresholdInfo = (alert) => {
    if (alert.alertType !== 'threshold' || !alert.metricValue || !alert.thresholdValue) {
        return null;
    }
    const value = typeof alert.metricValue === 'number'
        ? alert.metricValue.toLocaleString(undefined, { maximumFractionDigits: 2 })
        : alert.metricValue;
    const threshold = typeof alert.thresholdValue === 'number'
        ? alert.thresholdValue.toLocaleString(undefined, { maximumFractionDigits: 2 })
        : alert.thresholdValue;
    const unit = alert.metricUnit || '';
    const op = alert.operator === '>' ? 'exceeds' : alert.operator === '<' ? 'below' : 'at';
    return unit
        ? `${value} ${unit} ${op} threshold of ${threshold} ${unit}`
        : `${value} ${op} threshold of ${threshold}`;
};

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
 * HeaderStatusIndicator - Shows node health status with appropriate icon
 * Matches the ClusterNavigator's status indicator style but sized for header
 * - Red error icon for offline/down nodes
 * - Yellow warning icon with count for nodes with alerts
 * - Green checkmark for healthy nodes
 */
const HeaderStatusIndicator = ({ status, alertCount = 0, size = 'large', isDark }) => {
    const sizes = {
        small: 14,
        medium: 18,
        large: 22,
    };
    const fontSize = sizes[size];

    // Offline/down nodes - red error icon
    if (status === 'offline') {
        return (
            <Tooltip title="Offline" placement="left">
                <ErrorIcon
                    sx={{
                        fontSize,
                        color: '#EF4444',
                        filter: 'drop-shadow(0 0 3px #EF4444)',
                    }}
                />
            </Tooltip>
        );
    }

    // Nodes with alerts - yellow warning icon with count
    if (alertCount > 0) {
        return (
            <Tooltip title={`${alertCount} active alert${alertCount !== 1 ? 's' : ''}`} placement="left">
                <Box sx={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                    <WarningIcon
                        sx={{
                            fontSize,
                            color: '#F59E0B',
                            filter: 'drop-shadow(0 0 3px #F59E0B)',
                        }}
                    />
                    <Box
                        sx={{
                            position: 'absolute',
                            top: -5,
                            left: -7,
                            minWidth: 14,
                            height: 14,
                            px: 0.25,
                            borderRadius: '7px',
                            bgcolor: isDark ? '#64748B' : '#6B7280',
                            color: '#FFF',
                            fontSize: '0.5625rem',
                            fontWeight: 700,
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            lineHeight: 1,
                        }}
                    >
                        {alertCount > 99 ? '99+' : alertCount}
                    </Box>
                </Box>
            </Tooltip>
        );
    }

    // Healthy nodes - green checkmark
    return (
        <Tooltip title="Online" placement="left">
            <HealthyIcon
                sx={{
                    fontSize,
                    color: '#22C55E',
                    filter: 'drop-shadow(0 0 3px #22C55E)',
                }}
            />
        </Tooltip>
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
 * ServerInfoCard - Unified compact server information display
 * Combines connection details, server info, and replication status in a clean grid
 */
const ServerInfoCard = ({ selection, isDark }) => {
    // Combine all data items into a single array for the grid
    const allData = [
        { label: 'HOST', value: selection.host, mono: true },
        { label: 'PORT', value: selection.port, mono: true },
        { label: 'DATABASE', value: selection.database, mono: true },
        { label: 'USER', value: selection.username, mono: true },
        { label: 'POSTGRESQL', value: selection.version, mono: true },
        { label: 'OS', value: selection.os, mono: false },
        { label: 'ROLE', value: selection.role?.replace(/_/g, ' '), mono: false, capitalize: true },
    ].filter(item => item.value);

    const replicationData = selection.spockVersion || selection.spockNodeName ? {
        version: selection.spockVersion,
        nodeName: selection.spockNodeName,
    } : null;

    return (
        <Box
            sx={{
                borderRadius: 1.5,
                overflow: 'hidden',
                border: '1px solid',
                borderColor: isDark ? '#334155' : '#E2E8F0',
                bgcolor: isDark ? alpha('#1E293B', 0.4) : '#FFFFFF',
            }}
        >
            {/* Data grid - single row that wraps if needed */}
            <Box
                sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                }}
            >
                {allData.map((item, idx) => (
                    <Box
                        key={item.label}
                        sx={{
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 0.25,
                            px: 1.5,
                            py: 1,
                            borderRight: idx < allData.length - 1 ? '1px solid' : 'none',
                            borderColor: isDark ? '#334155' : '#E2E8F0',
                            minWidth: item.label === 'OS' ? 180 : 'auto',
                        }}
                    >
                        <Typography
                            sx={{
                                color: isDark ? '#64748B' : '#94A3B8',
                                fontSize: '0.5625rem',
                                fontWeight: 700,
                                textTransform: 'uppercase',
                                letterSpacing: '0.1em',
                                lineHeight: 1,
                            }}
                        >
                            {item.label}
                        </Typography>
                        <Typography
                            sx={{
                                color: 'text.primary',
                                fontSize: '0.8125rem',
                                fontWeight: 500,
                                fontFamily: item.mono ? '"JetBrains Mono", "SF Mono", monospace' : 'inherit',
                                lineHeight: 1.2,
                                textTransform: item.capitalize ? 'capitalize' : 'none',
                                whiteSpace: 'nowrap',
                            }}
                        >
                            {item.value}
                        </Typography>
                    </Box>
                ))}
            </Box>

            {/* Spock replication section - only shown if Spock is installed */}
            {replicationData && (
                <Box
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 2,
                        px: 1.5,
                        py: 0.75,
                        bgcolor: isDark ? alpha('#0EA5E9', 0.08) : alpha('#0EA5E9', 0.04),
                        borderTop: '1px solid',
                        borderColor: isDark ? alpha('#0EA5E9', 0.2) : alpha('#0EA5E9', 0.15),
                    }}
                >
                    <Box
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 0.75,
                        }}
                    >
                        <Box
                            sx={{
                                width: 6,
                                height: 6,
                                borderRadius: '50%',
                                bgcolor: '#0EA5E9',
                            }}
                        />
                        <Typography
                            sx={{
                                fontSize: '0.6875rem',
                                fontWeight: 600,
                                color: isDark ? '#38BDF8' : '#0284C7',
                                textTransform: 'uppercase',
                                letterSpacing: '0.05em',
                            }}
                        >
                            Spock Replication
                        </Typography>
                    </Box>
                    {replicationData.version && (
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                                color: 'text.secondary',
                            }}
                        >
                            v{replicationData.version}
                        </Typography>
                    )}
                    {replicationData.nodeName && (
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                                color: 'text.primary',
                                fontWeight: 500,
                            }}
                        >
                            {replicationData.nodeName}
                        </Typography>
                    )}
                </Box>
            )}
        </Box>
    );
};

/**
 * AlertItem - Compact alert entry with severity indicator and ack functionality
 */
const AlertItem = ({ alert, isDark, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const isAcknowledged = !!alert.acknowledgedAt;
    const baseColor = isAcknowledged ? '#64748B' : (SEVERITY_COLORS[alert.severity] || SEVERITY_COLORS.info);
    const SeverityIcon = alert.severity === 'critical' ? ErrorIcon : WarningIcon;
    const thresholdInfo = formatThresholdInfo(alert);
    const friendlyTitle = getFriendlyTitle(alert.title);

    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                px: 1.25,
                py: 0.75,
                borderRadius: 1,
                bgcolor: isAcknowledged
                    ? (isDark ? alpha('#64748B', 0.12) : alpha('#64748B', 0.06))
                    : (isDark ? alpha(baseColor, 0.08) : alpha(baseColor, 0.04)),
                border: '1px solid',
                borderColor: isAcknowledged
                    ? (isDark ? alpha('#64748B', 0.25) : alpha('#64748B', 0.2))
                    : alpha(baseColor, isDark ? 0.25 : 0.15),
            }}
        >
            {/* Severity indicator */}
            <SeverityIcon
                sx={{
                    fontSize: 16,
                    color: baseColor,
                    flexShrink: 0,
                }}
            />

            {/* Main content */}
            <Box sx={{ flex: 1, minWidth: 0 }}>
                {/* Title row */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            color: isAcknowledged ? 'text.secondary' : 'text.primary',
                            fontSize: '0.8125rem',
                            lineHeight: 1.2,
                        }}
                    >
                        {friendlyTitle}
                    </Typography>
                    {showServer && alert.server && (
                        <Chip
                            label={alert.server}
                            size="small"
                            sx={{
                                height: 16,
                                fontSize: '0.625rem',
                                bgcolor: isDark ? alpha('#64748B', 0.2) : alpha('#64748B', 0.1),
                                color: 'text.secondary',
                                '& .MuiChip-label': { px: 0.5 },
                            }}
                        />
                    )}
                    {alert.databaseName && (
                        <Chip
                            label={alert.databaseName}
                            size="small"
                            sx={{
                                height: 16,
                                fontSize: '0.625rem',
                                bgcolor: isDark ? alpha('#6366F1', 0.2) : alpha('#6366F1', 0.1),
                                color: isDark ? '#818CF8' : '#6366F1',
                                '& .MuiChip-label': { px: 0.5 },
                            }}
                        />
                    )}
                    {alert.objectName && (
                        <Chip
                            icon={<TableIcon sx={{ fontSize: '0.625rem !important' }} />}
                            label={alert.objectName}
                            size="small"
                            sx={{
                                height: 16,
                                fontSize: '0.625rem',
                                bgcolor: isDark ? alpha('#10B981', 0.2) : alpha('#10B981', 0.1),
                                color: isDark ? '#34D399' : '#059669',
                                '& .MuiChip-label': { px: 0.5 },
                                '& .MuiChip-icon': {
                                    color: 'inherit',
                                    ml: 0.25,
                                    mr: -0.25,
                                },
                            }}
                        />
                    )}
                </Box>

                {/* Threshold info or description */}
                {thresholdInfo ? (
                    <Typography
                        sx={{
                            color: 'text.secondary',
                            fontSize: '0.6875rem',
                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                            mt: 0.25,
                        }}
                    >
                        {thresholdInfo}
                    </Typography>
                ) : alert.description && (
                    <Typography
                        sx={{
                            color: 'text.secondary',
                            fontSize: '0.6875rem',
                            mt: 0.25,
                            wordBreak: 'break-word',
                        }}
                    >
                        {alert.description}
                    </Typography>
                )}

                {/* Ack info if acknowledged */}
                {isAcknowledged && (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.25 }}>
                        <Typography
                            sx={{
                                color: 'text.secondary',
                                fontSize: '0.625rem',
                                fontStyle: 'italic',
                            }}
                        >
                            Acked by {alert.acknowledgedBy}{alert.ackMessage ? `: ${alert.ackMessage}` : ''}
                        </Typography>
                        {alert.falsePositive && (
                            <Chip
                                label="False Positive"
                                size="small"
                                sx={{
                                    height: 14,
                                    fontSize: '0.5rem',
                                    fontWeight: 600,
                                    bgcolor: isDark ? alpha('#EF4444', 0.15) : alpha('#EF4444', 0.1),
                                    color: '#EF4444',
                                    '& .MuiChip-label': { px: 0.5 },
                                }}
                            />
                        )}
                    </Box>
                )}
            </Box>

            {/* Time and severity */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexShrink: 0 }}>
                <Typography
                    sx={{
                        color: 'text.disabled',
                        fontSize: '0.625rem',
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.25,
                    }}
                >
                    <ScheduleIcon sx={{ fontSize: 10 }} />
                    {alert.time}
                </Typography>
                <Chip
                    label={alert.severity}
                    size="small"
                    sx={{
                        height: 16,
                        fontSize: '0.5625rem',
                        fontWeight: 600,
                        textTransform: 'uppercase',
                        bgcolor: alpha(baseColor, 0.15),
                        color: baseColor,
                        '& .MuiChip-label': { px: 0.5 },
                    }}
                />
            </Box>

            {/* Analyze button */}
            <Tooltip title="Analyze with AI" placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        onAnalyze?.(alert);
                    }}
                    sx={{
                        p: 0.5,
                        color: isDark ? '#818CF8' : '#6366F1',
                        '&:hover': {
                            bgcolor: isDark ? alpha('#6366F1', 0.15) : alpha('#6366F1', 0.1),
                        },
                    }}
                >
                    <AnalyzeIcon sx={{ fontSize: 16 }} />
                </IconButton>
            </Tooltip>

            {/* Ack/Unack button */}
            <Tooltip title={isAcknowledged ? 'Restore to active' : 'Acknowledge'} placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        if (isAcknowledged) {
                            onUnacknowledge?.(alert.id);
                        } else {
                            onAcknowledge?.(alert);
                        }
                    }}
                    sx={{
                        p: 0.5,
                        color: isAcknowledged ? '#6B7280' : baseColor,
                        '&:hover': {
                            bgcolor: alpha(baseColor, 0.1),
                        },
                    }}
                >
                    {isAcknowledged ? (
                        <UnackIcon sx={{ fontSize: 16 }} />
                    ) : (
                        <AckIcon sx={{ fontSize: 16 }} />
                    )}
                </IconButton>
            </Tooltip>
        </Box>
    );
};

/**
 * GroupedAlertInstance - A single instance row within a grouped alert panel
 */
const GroupedAlertInstance = ({ alert, isDark, showServer, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const isAcknowledged = !!alert.acknowledgedAt;
    const baseColor = isAcknowledged ? '#64748B' : (SEVERITY_COLORS[alert.severity] || SEVERITY_COLORS.info);
    const thresholdInfo = formatThresholdInfo(alert);

    return (
        <Box
            sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                px: 1,
                py: 0.5,
                borderRadius: 0.5,
                bgcolor: isAcknowledged
                    ? (isDark ? alpha('#64748B', 0.08) : alpha('#64748B', 0.04))
                    : 'transparent',
                '&:hover': {
                    bgcolor: isDark ? alpha('#64748B', 0.12) : alpha('#64748B', 0.06),
                },
            }}
        >
            {/* Context chips (server, database, object) */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap', flex: 1, minWidth: 0 }}>
                {showServer && alert.server && (
                    <Chip
                        label={alert.server}
                        size="small"
                        sx={{
                            height: 16,
                            fontSize: '0.625rem',
                            bgcolor: isDark ? alpha('#64748B', 0.2) : alpha('#64748B', 0.1),
                            color: 'text.secondary',
                            '& .MuiChip-label': { px: 0.5 },
                        }}
                    />
                )}
                {alert.databaseName && (
                    <Chip
                        label={alert.databaseName}
                        size="small"
                        sx={{
                            height: 16,
                            fontSize: '0.625rem',
                            bgcolor: isDark ? alpha('#6366F1', 0.2) : alpha('#6366F1', 0.1),
                            color: isDark ? '#818CF8' : '#6366F1',
                            '& .MuiChip-label': { px: 0.5 },
                        }}
                    />
                )}
                {alert.objectName && (
                    <Chip
                        icon={<TableIcon sx={{ fontSize: '0.625rem !important' }} />}
                        label={alert.objectName}
                        size="small"
                        sx={{
                            height: 16,
                            fontSize: '0.625rem',
                            bgcolor: isDark ? alpha('#10B981', 0.2) : alpha('#10B981', 0.1),
                            color: isDark ? '#34D399' : '#059669',
                            '& .MuiChip-label': { px: 0.5 },
                            '& .MuiChip-icon': {
                                color: 'inherit',
                                ml: 0.25,
                                mr: -0.25,
                            },
                        }}
                    />
                )}
                {/* Threshold info */}
                {thresholdInfo && (
                    <Typography
                        sx={{
                            color: 'text.secondary',
                            fontSize: '0.625rem',
                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                        }}
                    >
                        {thresholdInfo}
                    </Typography>
                )}
            </Box>

            {/* Time */}
            <Typography
                sx={{
                    color: 'text.disabled',
                    fontSize: '0.5625rem',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.25,
                    flexShrink: 0,
                }}
            >
                <ScheduleIcon sx={{ fontSize: 10 }} />
                {alert.time}
            </Typography>

            {/* Analyze button */}
            <Tooltip title="Analyze with AI" placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        onAnalyze?.(alert);
                    }}
                    sx={{
                        p: 0.25,
                        color: isDark ? '#818CF8' : '#6366F1',
                        '&:hover': {
                            bgcolor: isDark ? alpha('#6366F1', 0.15) : alpha('#6366F1', 0.1),
                        },
                    }}
                >
                    <AnalyzeIcon sx={{ fontSize: 14 }} />
                </IconButton>
            </Tooltip>

            {/* Ack/Unack button */}
            <Tooltip title={isAcknowledged ? 'Restore to active' : 'Acknowledge'} placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        if (isAcknowledged) {
                            onUnacknowledge?.(alert.id);
                        } else {
                            onAcknowledge?.(alert);
                        }
                    }}
                    sx={{
                        p: 0.25,
                        color: isAcknowledged ? '#6B7280' : baseColor,
                        '&:hover': {
                            bgcolor: alpha(baseColor, 0.1),
                        },
                    }}
                >
                    {isAcknowledged ? (
                        <UnackIcon sx={{ fontSize: 14 }} />
                    ) : (
                        <AckIcon sx={{ fontSize: 14 }} />
                    )}
                </IconButton>
            </Tooltip>
        </Box>
    );
};

/**
 * GroupedAlertItem - Display a group of alerts with the same title in a single panel
 */
const GroupedAlertItem = ({ title, alerts, isDark, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const [expanded, setExpanded] = useState(true);

    // Determine highest severity in the group
    const highestSeverity = alerts.reduce((highest, alert) => {
        if (alert.severity === 'critical') return 'critical';
        if (alert.severity === 'warning' && highest !== 'critical') return 'warning';
        return highest;
    }, 'info');

    const baseColor = SEVERITY_COLORS[highestSeverity] || SEVERITY_COLORS.info;
    const SeverityIcon = highestSeverity === 'critical' ? ErrorIcon : WarningIcon;
    const friendlyTitle = getFriendlyTitle(title);

    return (
        <Box
            sx={{
                borderRadius: 1,
                bgcolor: isDark ? alpha(baseColor, 0.06) : alpha(baseColor, 0.03),
                border: '1px solid',
                borderColor: alpha(baseColor, isDark ? 0.2 : 0.12),
                overflow: 'hidden',
            }}
        >
            {/* Group header */}
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                    px: 1.25,
                    py: 0.75,
                    cursor: 'pointer',
                    bgcolor: isDark ? alpha(baseColor, 0.08) : alpha(baseColor, 0.05),
                    '&:hover': {
                        bgcolor: isDark ? alpha(baseColor, 0.12) : alpha(baseColor, 0.08),
                    },
                }}
            >
                {/* Severity indicator */}
                <SeverityIcon
                    sx={{
                        fontSize: 16,
                        color: baseColor,
                        flexShrink: 0,
                    }}
                />

                {/* Title */}
                <Typography
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.8125rem',
                        lineHeight: 1.2,
                        flex: 1,
                    }}
                >
                    {friendlyTitle}
                </Typography>

                {/* Instance count */}
                <Chip
                    label={`${alerts.length} instance${alerts.length !== 1 ? 's' : ''}`}
                    size="small"
                    sx={{
                        height: 18,
                        fontSize: '0.625rem',
                        fontWeight: 600,
                        bgcolor: alpha(baseColor, 0.15),
                        color: baseColor,
                        '& .MuiChip-label': { px: 0.75 },
                    }}
                />

                {/* Severity badge */}
                <Chip
                    label={highestSeverity}
                    size="small"
                    sx={{
                        height: 16,
                        fontSize: '0.5625rem',
                        fontWeight: 600,
                        textTransform: 'uppercase',
                        bgcolor: alpha(baseColor, 0.15),
                        color: baseColor,
                        '& .MuiChip-label': { px: 0.5 },
                    }}
                />

                {/* Expand/Collapse */}
                <IconButton size="small" sx={{ p: 0.25 }}>
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 16 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 16 }} />
                    )}
                </IconButton>
            </Box>

            {/* Instances list */}
            <Collapse in={expanded}>
                <Box
                    sx={{
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 0.25,
                        px: 0.5,
                        py: 0.5,
                    }}
                >
                    {alerts.map((alert) => (
                        <GroupedAlertInstance
                            key={alert.id}
                            alert={alert}
                            isDark={isDark}
                            showServer={showServer}
                            onAcknowledge={onAcknowledge}
                            onUnacknowledge={onUnacknowledge}
                            onAnalyze={onAnalyze}
                        />
                    ))}
                </Box>
            </Collapse>
        </Box>
    );
};

/**
 * AcknowledgeDialog - Dialog for entering ack reason and false positive flag
 */
const AcknowledgeDialog = ({ open, alert, onClose, onConfirm, isDark }) => {
    const [message, setMessage] = useState('');
    const [falsePositive, setFalsePositive] = useState(false);

    const handleConfirm = () => {
        onConfirm(alert?.id, message, falsePositive);
        setMessage('');
        setFalsePositive(false);
    };

    const handleClose = () => {
        setMessage('');
        setFalsePositive(false);
        onClose();
    };

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{
                sx: {
                    bgcolor: isDark ? '#1E293B' : '#FFFFFF',
                    backgroundImage: 'none',
                },
            }}
        >
            <DialogTitle sx={{ pb: 1 }}>
                Acknowledge Alert
            </DialogTitle>
            <DialogContent>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                    {alert ? getFriendlyTitle(alert.title) : 'Alert'}
                </Typography>
                <TextField
                    autoFocus
                    label="Reason"
                    placeholder="e.g., Investigating, Known issue, Scheduled maintenance"
                    fullWidth
                    multiline
                    rows={2}
                    value={message}
                    onChange={(e) => setMessage(e.target.value)}
                    variant="outlined"
                    size="small"
                    InputLabelProps={{ shrink: true }}
                    sx={{ mb: 2 }}
                />
                <Box
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 1,
                        p: 1.5,
                        borderRadius: 1,
                        bgcolor: isDark ? 'rgba(239, 68, 68, 0.08)' : 'rgba(239, 68, 68, 0.04)',
                        border: '1px solid',
                        borderColor: isDark ? 'rgba(239, 68, 68, 0.2)' : 'rgba(239, 68, 68, 0.15)',
                        cursor: 'pointer',
                    }}
                    onClick={() => setFalsePositive(!falsePositive)}
                >
                    <Box
                        sx={{
                            width: 18,
                            height: 18,
                            borderRadius: 0.5,
                            border: '2px solid',
                            borderColor: falsePositive ? '#EF4444' : (isDark ? '#64748B' : '#94A3B8'),
                            bgcolor: falsePositive ? '#EF4444' : 'transparent',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            flexShrink: 0,
                            transition: 'all 0.15s ease',
                        }}
                    >
                        {falsePositive && (
                            <HealthyIcon sx={{ fontSize: 14, color: '#FFF' }} />
                        )}
                    </Box>
                    <Box>
                        <Typography sx={{ fontSize: '0.8125rem', fontWeight: 500, color: 'text.primary' }}>
                            Mark as false positive
                        </Typography>
                        <Typography sx={{ fontSize: '0.6875rem', color: 'text.secondary' }}>
                            This helps improve alert accuracy over time
                        </Typography>
                    </Box>
                </Box>
            </DialogContent>
            <DialogActions sx={{ px: 3, pb: 2 }}>
                <Button onClick={handleClose} color="inherit" size="small">
                    Cancel
                </Button>
                <Button
                    onClick={handleConfirm}
                    variant="contained"
                    size="small"
                    startIcon={<AckIcon />}
                >
                    Acknowledge
                </Button>
            </DialogActions>
        </Dialog>
    );
};

/**
 * Group alerts by their title for consolidated display
 */
const groupAlertsByTitle = (alerts) => {
    return alerts.reduce((groups, alert) => {
        const title = alert.title || 'Unknown Alert';
        if (!groups[title]) {
            groups[title] = [];
        }
        groups[title].push(alert);
        return groups;
    }, {});
};

/**
 * AlertsSection - Collapsible alerts list with active/acknowledged separation
 * Groups alerts by title and renders grouped panels for multiple instances
 */
const AlertsSection = ({ alerts, isDark, loading, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const [expanded, setExpanded] = useState(true);
    const [ackExpanded, setAckExpanded] = useState(false);

    // Separate active and acknowledged alerts
    const activeAlerts = alerts.filter(a => !a.acknowledgedAt);
    const acknowledgedAlerts = alerts.filter(a => !!a.acknowledgedAt);

    // Group active alerts by title
    const groupedActiveAlerts = useMemo(() => groupAlertsByTitle(activeAlerts), [activeAlerts]);
    const groupedAcknowledgedAlerts = useMemo(() => groupAlertsByTitle(acknowledgedAlerts), [acknowledgedAlerts]);

    // Convert grouped object to sorted array of [title, alerts] pairs
    const sortedActiveGroups = useMemo(() => {
        return Object.entries(groupedActiveAlerts).sort((a, b) => {
            // Sort by highest severity first, then by count
            const getSeverityWeight = (alerts) => {
                if (alerts.some(a => a.severity === 'critical')) return 3;
                if (alerts.some(a => a.severity === 'warning')) return 2;
                return 1;
            };
            const severityDiff = getSeverityWeight(b[1]) - getSeverityWeight(a[1]);
            if (severityDiff !== 0) return severityDiff;
            return b[1].length - a[1].length;
        });
    }, [groupedActiveAlerts]);

    const sortedAcknowledgedGroups = useMemo(() => {
        return Object.entries(groupedAcknowledgedAlerts).sort((a, b) => b[1].length - a[1].length);
    }, [groupedAcknowledgedAlerts]);

    if (loading) {
        return (
            <Box sx={{ mt: 2 }}>
                <Skeleton variant="text" width={120} height={20} />
                <Box sx={{ mt: 1, display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                    {[1, 2].map((i) => (
                        <Skeleton
                            key={i}
                            variant="rounded"
                            height={48}
                            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
                        />
                    ))}
                </Box>
            </Box>
        );
    }

    // Render either a single AlertItem or a GroupedAlertItem depending on count
    const renderAlertGroup = (title, alertsInGroup) => {
        if (alertsInGroup.length === 1) {
            // Single alert - render as simple AlertItem
            return (
                <AlertItem
                    key={alertsInGroup[0].id}
                    alert={alertsInGroup[0]}
                    isDark={isDark}
                    showServer={showServer}
                    onAcknowledge={onAcknowledge}
                    onUnacknowledge={onUnacknowledge}
                    onAnalyze={onAnalyze}
                />
            );
        }
        // Multiple alerts - render as grouped panel
        return (
            <GroupedAlertItem
                key={title}
                title={title}
                alerts={alertsInGroup}
                isDark={isDark}
                showServer={showServer}
                onAcknowledge={onAcknowledge}
                onUnacknowledge={onUnacknowledge}
                onAnalyze={onAnalyze}
            />
        );
    };

    return (
        <Box sx={{ mt: 2 }}>
            {/* Active Alerts Header */}
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.75,
                    cursor: 'pointer',
                    py: 0.25,
                    '&:hover': { opacity: 0.8 },
                }}
            >
                <AlertIcon sx={{ fontSize: 16, color: 'primary.main' }} />
                <Typography
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.8125rem',
                    }}
                >
                    Active Alerts
                </Typography>
                <Chip
                    label={activeAlerts.length}
                    size="small"
                    sx={{
                        height: 18,
                        fontSize: '0.625rem',
                        fontWeight: 600,
                        bgcolor: activeAlerts.length > 0
                            ? alpha(SEVERITY_COLORS.warning, 0.15)
                            : (isDark ? alpha('#22C55E', 0.15) : alpha('#22C55E', 0.1)),
                        color: activeAlerts.length > 0 ? SEVERITY_COLORS.warning : '#22C55E',
                        '& .MuiChip-label': { px: 0.5 },
                    }}
                />
                {sortedActiveGroups.length > 0 && sortedActiveGroups.length !== activeAlerts.length && (
                    <Typography
                        sx={{
                            color: 'text.disabled',
                            fontSize: '0.625rem',
                        }}
                    >
                        ({sortedActiveGroups.length} type{sortedActiveGroups.length !== 1 ? 's' : ''})
                    </Typography>
                )}
                <Box sx={{ flex: 1 }} />
                <IconButton size="small" sx={{ p: 0.25 }}>
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 16 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 16 }} />
                    )}
                </IconButton>
            </Box>

            {/* Active Alerts List */}
            <Collapse in={expanded}>
                <Box
                    sx={{
                        mt: 1,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 0.5,
                    }}
                >
                    {activeAlerts.length === 0 ? (
                        <Box
                            sx={{
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: 0.75,
                                py: 2,
                                borderRadius: 1,
                                bgcolor: isDark ? alpha('#22C55E', 0.06) : alpha('#22C55E', 0.04),
                                border: '1px solid',
                                borderColor: isDark ? alpha('#22C55E', 0.15) : alpha('#22C55E', 0.1),
                            }}
                        >
                            <HealthyIcon sx={{ fontSize: 16, color: '#22C55E' }} />
                            <Typography
                                sx={{
                                    color: '#22C55E',
                                    fontWeight: 500,
                                    fontSize: '0.8125rem',
                                }}
                            >
                                No active alerts
                            </Typography>
                        </Box>
                    ) : (
                        sortedActiveGroups.map(([title, alertsInGroup]) =>
                            renderAlertGroup(title, alertsInGroup)
                        )
                    )}
                </Box>
            </Collapse>

            {/* Acknowledged Alerts Section */}
            {acknowledgedAlerts.length > 0 && (
                <>
                    <Box
                        onClick={() => setAckExpanded(!ackExpanded)}
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 0.75,
                            cursor: 'pointer',
                            py: 0.25,
                            mt: 1.5,
                            '&:hover': { opacity: 0.8 },
                        }}
                    >
                        <AckIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                        <Typography
                            sx={{
                                fontWeight: 500,
                                color: 'text.secondary',
                                fontSize: '0.75rem',
                            }}
                        >
                            Acknowledged
                        </Typography>
                        <Chip
                            label={acknowledgedAlerts.length}
                            size="small"
                            sx={{
                                height: 16,
                                fontSize: '0.5625rem',
                                fontWeight: 600,
                                bgcolor: isDark ? alpha('#64748B', 0.2) : alpha('#64748B', 0.1),
                                color: 'text.disabled',
                                '& .MuiChip-label': { px: 0.5 },
                            }}
                        />
                        <Box sx={{ flex: 1 }} />
                        <IconButton size="small" sx={{ p: 0.25 }}>
                            {ackExpanded ? (
                                <ExpandLessIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            ) : (
                                <ExpandMoreIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            )}
                        </IconButton>
                    </Box>

                    <Collapse in={ackExpanded}>
                        <Box
                            sx={{
                                mt: 0.75,
                                display: 'flex',
                                flexDirection: 'column',
                                gap: 0.5,
                            }}
                        >
                            {sortedAcknowledgedGroups.map(([title, alertsInGroup]) =>
                                renderAlertGroup(title, alertsInGroup)
                            )}
                        </Box>
                    </Collapse>
                </>
            )}
        </Box>
    );
};

/**
 * SelectionHeader - Header showing what's currently selected
 */
const SelectionHeader = ({ selection, alertCount = 0, isDark }) => {
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
            </Box>
            <HeaderStatusIndicator
                status={selection.status}
                alertCount={alertCount}
                isDark={isDark}
            />
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
    const { user } = useAuth();
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(false);
    const initialLoadDoneRef = React.useRef(false);
    const [ackDialogOpen, setAckDialogOpen] = useState(false);
    const [selectedAlertForAck, setSelectedAlertForAck] = useState(null);
    const [analysisDialogOpen, setAnalysisDialogOpen] = useState(false);
    const [analysisAlert, setAnalysisAlert] = useState(null);

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
            databaseName: alert.database_name,
            objectName: alert.object_name,
            // Threshold info
            alertType: alert.alert_type,
            metricValue: alert.metric_value,
            metricUnit: alert.metric_unit,
            thresholdValue: alert.threshold_value,
            operator: alert.operator,
            // Acknowledgment info
            acknowledgedAt: alert.acknowledged_at,
            acknowledgedBy: alert.acknowledged_by,
            ackMessage: alert.ack_message,
            falsePositive: alert.false_positive,
        }));
    };

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

    // Handle confirming acknowledgment
    const handleAckConfirm = async (alertId, message, falsePositive = false) => {
        if (!user || !alertId) return;

        try {
            const response = await fetch('/api/v1/alerts/acknowledge', {
                method: 'POST',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    alert_id: alertId,
                    message: message || '',
                    false_positive: falsePositive,
                }),
            });

            if (response.ok) {
                // Refresh alerts to show updated status
                fetchAlertsData();
            } else {
                const errorData = await response.json().catch(() => ({}));
                console.error('Failed to acknowledge alert:', errorData.error || response.statusText);
            }
        } catch (err) {
            console.error('Error acknowledging alert:', err);
        } finally {
            setAckDialogOpen(false);
            setSelectedAlertForAck(null);
        }
    };

    // Handle unacknowledging an alert
    const handleUnacknowledge = async (alertId) => {
        if (!user || !alertId) return;

        try {
            const response = await fetch(`/api/v1/alerts/acknowledge?alert_id=${alertId}`, {
                method: 'DELETE',
                credentials: 'include',
            });

            if (response.ok) {
                // Refresh alerts to show updated status
                fetchAlertsData();
            } else {
                console.error('Failed to unacknowledge alert');
            }
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

            const response = await fetch(url, {
                credentials: 'include',
            });

            if (response.ok) {
                const data = await response.json();
                const transformedAlerts = transformAlerts(data.alerts || []);
                setAlerts(transformedAlerts);
                initialLoadDoneRef.current = true;
            } else {
                setAlerts([]);
            }
        } catch (err) {
            console.error('Error fetching alerts:', err);
            setAlerts([]);
        } finally {
            setLoading(false);
        }
    }, [user, selection]);

    // Reset initial load state when selection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [selection?.type, selection?.id]);

    // Fetch alerts on selection change
    useEffect(() => {
        fetchAlertsData();
    }, [fetchAlertsData]);

    // Count only active (non-acknowledged) alerts for the header indicator
    const activeAlertCount = useMemo(() => {
        return alerts.filter(a => !a.acknowledgedAt).length;
    }, [alerts]);

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
            {/* Content Container */}
            <Box>
                {/* Selection Header */}
                <SelectionHeader selection={selection} alertCount={activeAlertCount} isDark={isDark} />

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

                {/* Server Info Card - Unified display for single server */}
                {selection.type === 'server' && (
                    <Box sx={{ mb: 2 }}>
                        <ServerInfoCard selection={selection} isDark={isDark} />
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

                {/* Event Timeline */}
                <EventTimeline
                    selection={selection}
                    mode={isDark ? 'dark' : 'light'}
                />

                {/* Alerts Section */}
                <AlertsSection
                    alerts={alerts}
                    isDark={isDark}
                    loading={loading}
                    showServer={selection.type !== 'server'}
                    onAcknowledge={handleAcknowledge}
                    onUnacknowledge={handleUnacknowledge}
                    onAnalyze={handleAnalyze}
                />
            </Box>

            {/* Acknowledge Dialog */}
            <AcknowledgeDialog
                open={ackDialogOpen}
                alert={selectedAlertForAck}
                onClose={() => {
                    setAckDialogOpen(false);
                    setSelectedAlertForAck(null);
                }}
                onConfirm={handleAckConfirm}
                isDark={isDark}
            />

            {/* Alert Analysis Dialog */}
            <AlertAnalysisDialog
                open={analysisDialogOpen}
                alert={analysisAlert}
                onClose={() => {
                    setAnalysisDialogOpen(false);
                    setAnalysisAlert(null);
                }}
                isDark={isDark}
            />
        </Box>
    );
};

export default StatusPanel;
