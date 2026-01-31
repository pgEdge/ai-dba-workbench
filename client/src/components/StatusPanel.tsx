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
import { useTheme, Theme } from '@mui/material/styles';
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
    DarkMode as BlackoutIcon,
} from '@mui/icons-material';
import { useAuth } from '../contexts/AuthContext';
import EventTimeline from './EventTimeline';
import BlackoutPanel from './BlackoutPanel';
import AlertAnalysisDialog from './AlertAnalysisDialog';
import BlackoutManagementDialog from './BlackoutManagementDialog';

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

// ---- Static style constants (Issue 23) ----

// Shared small chip label style
const CHIP_LABEL_SX = { px: 0.5 };
const CHIP_LABEL_075_SX = { px: 0.75 };

// HeaderStatusIndicator sizes
const INDICATOR_SIZES = {
    small: 14,
    medium: 18,
    large: 22,
};

// Static layout styles
const FLEX_CENTER_SX = { display: 'flex', alignItems: 'center' };
const FLEX_COL_SX = { display: 'flex', flexDirection: 'column' };
const FLEX_1_MIN0_SX = { flex: 1, minWidth: 0 };
const FLEX_SHRINK_0_SX = { flexShrink: 0 };
const EXPAND_BUTTON_SX = { p: 0.25 };
const ICON_16_SX = { fontSize: 16 };
const ICON_14_SX = { fontSize: 14 };
const ICON_10_SX = { fontSize: 10 };

// MetricCard static styles
const METRIC_LABEL_SX = {
    color: 'text.secondary',
    fontSize: '0.75rem',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

const METRIC_VALUE_BASE_SX = {
    fontWeight: 700,
    fontSize: '1.75rem',
    lineHeight: 1,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

const METRIC_TREND_CONTAINER_SX = { display: 'flex', alignItems: 'center', gap: 0.25 };

// ServerInfoCard static styles
const SERVER_INFO_WRAPPER_SX = {
    display: 'flex',
    flexWrap: 'wrap',
};

const SERVER_INFO_LABEL_BASE_SX = {
    fontSize: '0.5625rem',
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.1em',
    lineHeight: 1,
};

const SERVER_INFO_VALUE_BASE_SX = {
    color: 'text.primary',
    fontSize: '0.8125rem',
    fontWeight: 500,
    lineHeight: 1.2,
    whiteSpace: 'nowrap',
};

const SPOCK_DOT_SX = {
    width: 6,
    height: 6,
    borderRadius: '50%',
};

const SPOCK_LABEL_BASE_SX = {
    fontSize: '0.6875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

const SPOCK_VERSION_SX = {
    fontSize: '0.75rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.secondary',
};

const SPOCK_NODE_SX = {
    fontSize: '0.75rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.primary',
    fontWeight: 500,
};

// AlertItem static styles
const ALERT_TITLE_BASE_SX = {
    fontWeight: 600,
    fontSize: '0.8125rem',
    lineHeight: 1.2,
};

const ALERT_THRESHOLD_SX = {
    color: 'text.secondary',
    fontSize: '0.6875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    mt: 0.25,
};

const ALERT_DESCRIPTION_SX = {
    color: 'text.secondary',
    fontSize: '0.6875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

const ALERT_ACK_TEXT_SX = {
    color: 'text.secondary',
    fontSize: '0.625rem',
    fontStyle: 'italic',
};

const ALERT_TIME_SX = {
    color: 'text.disabled',
    fontSize: '0.625rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
};

const SEVERITY_CHIP_BASE_SX = {
    height: 16,
    fontSize: '0.5625rem',
    fontWeight: 600,
    textTransform: 'uppercase',
};

// GroupedAlertInstance static styles
const INSTANCE_TIME_SX = {
    color: 'text.disabled',
    fontSize: '0.5625rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
    flexShrink: 0,
};

const INSTANCE_THRESHOLD_SX = {
    color: 'text.secondary',
    fontSize: '0.625rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

// GroupedAlertItem static styles
const GROUP_TITLE_SX = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '0.8125rem',
    lineHeight: 1.2,
    flex: 1,
};

const GROUP_INSTANCES_LIST_SX = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.25,
    px: 0.5,
    py: 0.5,
};

// AcknowledgeDialog static styles
const ACK_DIALOG_TITLE_SX = { pb: 1 };
const ACK_DIALOG_ACTIONS_SX = { px: 3, pb: 2 };
const ACK_FALSE_POSITIVE_TITLE_SX = { fontSize: '0.8125rem', fontWeight: 500, color: 'text.primary' };
const ACK_FALSE_POSITIVE_DESC_SX = { fontSize: '0.6875rem', color: 'text.secondary' };

// AlertsSection static styles
const ALERTS_SECTION_MT_SX = { mt: 2 };
const ALERTS_HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    py: 0.25,
    '&:hover': { opacity: 0.8 },
};

const ALERTS_TITLE_SX = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '0.8125rem',
};

const ALERTS_TYPE_COUNT_SX = {
    color: 'text.disabled',
    fontSize: '0.625rem',
};

const ACTIVE_LIST_SX = {
    mt: 1,
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

const NO_ALERTS_TEXT_BASE_SX = {
    fontWeight: 500,
    fontSize: '0.8125rem',
};

const ACK_HEADER_BASE_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    py: 0.25,
    mt: 1.5,
    '&:hover': { opacity: 0.8 },
};

const ACK_TITLE_SX = {
    fontWeight: 500,
    color: 'text.secondary',
    fontSize: '0.75rem',
};

const ACK_LIST_SX = {
    mt: 0.75,
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

// SelectionHeader static styles
const HEADER_CONTAINER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    mb: 3,
};

const HEADER_ICON_BOX_BASE_SX = {
    width: 48,
    height: 48,
    borderRadius: 2,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
};

const HEADER_LABEL_SX = {
    color: 'text.secondary',
    fontSize: '0.6875rem',
    fontWeight: 600,
    letterSpacing: '0.08em',
    lineHeight: 1,
};

const HEADER_NAME_SX = {
    fontWeight: 600,
    color: 'text.primary',
    lineHeight: 1.2,
    mt: 0.25,
};

// StatusPanel static styles
const EMPTY_STATE_CONTAINER_SX = {
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    p: 4,
};

const EMPTY_STATE_TITLE_SX = {
    color: 'text.secondary',
    fontWeight: 500,
    mb: 0.5,
};

const EMPTY_STATE_DESC_SX = {
    color: 'text.disabled',
    textAlign: 'center',
    maxWidth: 300,
};

const PANEL_ROOT_SX = {
    overflow: 'auto',
    p: 3,
};

const METRICS_GRID_SX = {
    display: 'flex',
    gap: 2,
    flexWrap: 'wrap',
    mb: 2,
};

// ---- Theme-dependent style getters (Issue 22 + 23) ----

const getStatusColors = (theme: Theme) => ({
    online: theme.palette.success.main,
    warning: theme.palette.warning.main,
    offline: theme.palette.error.main,
    unknown: theme.palette.grey[500],
});

const getSeverityColors = (theme: Theme) => ({
    critical: theme.palette.error.main,
    warning: theme.palette.warning.main,
    info: theme.palette.info.main,
});

/**
 * HeaderStatusIndicator - Shows node health status with appropriate icon
 * Matches the ClusterNavigator's status indicator style but sized for header
 */
const HeaderStatusIndicator = ({ status, alertCount = 0, size = 'large' }) => {
    const theme = useTheme();
    const fontSize = INDICATOR_SIZES[size];

    const offlineIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.error.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.error.main})`,
    }), [fontSize, theme.palette.error.main]);

    const warningIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.warning.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.warning.main})`,
    }), [fontSize, theme.palette.warning.main]);

    const badgeSx = useMemo(() => ({
        position: 'absolute',
        top: -5,
        left: -7,
        minWidth: 14,
        height: 14,
        px: 0.25,
        borderRadius: '7px',
        bgcolor: theme.palette.grey[500],
        color: 'common.white',
        fontSize: '0.5625rem',
        fontWeight: 700,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        lineHeight: 1,
    }), [theme.palette.grey]);

    const healthyIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.success.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.success.main})`,
    }), [fontSize, theme.palette.success.main]);

    // Offline/down nodes - red error icon
    if (status === 'offline') {
        return (
            <Tooltip title="Offline" placement="left">
                <ErrorIcon sx={offlineIconSx} />
            </Tooltip>
        );
    }

    // Nodes with alerts - yellow warning icon with count
    if (alertCount > 0) {
        return (
            <Tooltip title={`${alertCount} active alert${alertCount !== 1 ? 's' : ''}`} placement="left">
                <Box sx={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                    <WarningIcon sx={warningIconSx} />
                    <Box sx={badgeSx}>
                        {alertCount > 99 ? '99+' : alertCount}
                    </Box>
                </Box>
            </Tooltip>
        );
    }

    // Healthy nodes - green checkmark
    return (
        <Tooltip title="Online" placement="left">
            <HealthyIcon sx={healthyIconSx} />
        </Tooltip>
    );
};

/**
 * MetricCard - Display a key metric with trend indicator
 */
const MetricCard = ({ label, value, trend, trendValue, icon: Icon, color }) => {
    const theme = useTheme();
    const TrendIcon = trend === 'up' ? TrendingUpIcon : TrendingDownIcon;
    const trendColor = trend === 'up' ? theme.palette.success.main : theme.palette.error.main;

    const paperSx = useMemo(() => ({
        p: 2,
        borderRadius: 2,
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[800], 0.8)
            : alpha(theme.palette.grey[100], 0.8),
        border: '1px solid',
        borderColor: theme.palette.divider,
        flex: 1,
        minWidth: 120,
    }), [theme]);

    const iconSx = useMemo(() => ({
        fontSize: 18,
        color: color || theme.palette.grey[500],
    }), [color, theme.palette.grey]);

    const valueSx = useMemo(() => ({
        ...METRIC_VALUE_BASE_SX,
        color: color || 'text.primary',
    }), [color]);

    return (
        <Paper elevation={0} sx={paperSx}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                {Icon && <Icon sx={iconSx} />}
                <Typography variant="caption" sx={METRIC_LABEL_SX}>
                    {label}
                </Typography>
            </Box>
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1 }}>
                <Typography variant="h4" sx={valueSx}>
                    {value}
                </Typography>
                {trend && (
                    <Box sx={METRIC_TREND_CONTAINER_SX}>
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
 */
const ServerInfoCard = ({ selection }) => {
    const theme = useTheme();

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

    const containerSx = useMemo(() => ({
        borderRadius: 1.5,
        overflow: 'hidden',
        border: '1px solid',
        borderColor: theme.palette.divider,
        bgcolor: theme.palette.background.paper,
    }), [theme]);

    const labelSx = useMemo(() => ({
        ...SERVER_INFO_LABEL_BASE_SX,
        color: theme.palette.grey[500],
    }), [theme.palette.grey]);

    const spockSectionSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        px: 1.5,
        py: 0.75,
        bgcolor: alpha(theme.palette.custom.status.sky, 0.06),
        borderTop: '1px solid',
        borderColor: alpha(theme.palette.custom.status.sky, 0.18),
    }), [theme]);

    const spockDotSx = useMemo(() => ({
        ...SPOCK_DOT_SX,
        bgcolor: theme.palette.custom.status.sky,
    }), [theme.palette.custom.status]);

    const spockLabelSx = useMemo(() => ({
        ...SPOCK_LABEL_BASE_SX,
        color: theme.palette.custom.status.skyDark,
    }), [theme.palette.custom.status]);

    return (
        <Box sx={containerSx}>
            {/* Data grid */}
            <Box sx={SERVER_INFO_WRAPPER_SX}>
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
                            borderColor: theme.palette.divider,
                            minWidth: item.label === 'OS' ? 180 : 'auto',
                        }}
                    >
                        <Typography sx={labelSx}>
                            {item.label}
                        </Typography>
                        <Typography
                            sx={{
                                ...SERVER_INFO_VALUE_BASE_SX,
                                fontFamily: item.mono ? '"JetBrains Mono", "SF Mono", monospace' : 'inherit',
                                textTransform: item.capitalize ? 'capitalize' : 'none',
                            }}
                        >
                            {item.value}
                        </Typography>
                    </Box>
                ))}
            </Box>

            {/* Spock replication section */}
            {replicationData && (
                <Box sx={spockSectionSx}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                        <Box sx={spockDotSx} />
                        <Typography sx={spockLabelSx}>
                            Spock Replication
                        </Typography>
                    </Box>
                    {replicationData.version && (
                        <Typography sx={SPOCK_VERSION_SX}>
                            v{replicationData.version}
                        </Typography>
                    )}
                    {replicationData.nodeName && (
                        <Typography sx={SPOCK_NODE_SX}>
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
const AlertItem = ({ alert, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const isAcknowledged = !!alert.acknowledgedAt;
    const baseColor = isAcknowledged ? theme.palette.grey[500] : (severityColors[alert.severity] || severityColors.info);
    const SeverityIcon = alert.severity === 'critical' ? ErrorIcon : WarningIcon;
    const thresholdInfo = formatThresholdInfo(alert);
    const friendlyTitle = getFriendlyTitle(alert.title);

    const containerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        borderRadius: 1,
        bgcolor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.08)
            : alpha(baseColor, 0.05),
        border: '1px solid',
        borderColor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.22)
            : alpha(baseColor, 0.18),
    }), [isAcknowledged, baseColor, theme]);

    const severityIconSx = useMemo(() => ({
        fontSize: 16,
        color: baseColor,
        flexShrink: 0,
    }), [baseColor]);

    const dbChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.secondary.main, 0.15),
        color: theme.palette.secondary.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.secondary]);

    const objectChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.custom.status.online, 0.15),
        color: theme.palette.custom.status.connected,
        '& .MuiChip-label': CHIP_LABEL_SX,
        '& .MuiChip-icon': {
            color: 'inherit',
            ml: 0.25,
            mr: -0.25,
        },
    }), [theme.palette.custom.status]);

    const falsePositiveChipSx = useMemo(() => ({
        height: 14,
        fontSize: '0.5rem',
        fontWeight: 600,
        bgcolor: alpha(theme.palette.error.main, 0.12),
        color: theme.palette.error.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.error]);

    const severityChipSx = useMemo(() => ({
        ...SEVERITY_CHIP_BASE_SX,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [baseColor]);

    const analyzeButtonSx = useMemo(() => ({
        p: 0.5,
        color: theme.palette.secondary.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.secondary.main, 0.12),
        },
    }), [theme.palette.secondary]);

    const ackButtonSx = useMemo(() => ({
        p: 0.5,
        color: isAcknowledged ? theme.palette.grey[500] : baseColor,
        '&:hover': {
            bgcolor: alpha(baseColor, 0.1),
        },
    }), [isAcknowledged, baseColor, theme.palette.grey]);

    const serverChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    return (
        <Box sx={containerSx}>
            {/* Severity indicator */}
            <SeverityIcon sx={severityIconSx} />

            {/* Main content */}
            <Box sx={FLEX_1_MIN0_SX}>
                {/* Title row */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                    <Typography
                        sx={{
                            ...ALERT_TITLE_BASE_SX,
                            color: isAcknowledged ? 'text.secondary' : 'text.primary',
                        }}
                    >
                        {friendlyTitle}
                    </Typography>
                    {showServer && alert.server && (
                        <Chip label={alert.server} size="small" sx={serverChipSx} />
                    )}
                    {alert.databaseName && (
                        <Chip label={alert.databaseName} size="small" sx={dbChipSx} />
                    )}
                    {alert.objectName && (
                        <Chip
                            icon={<TableIcon sx={{ fontSize: '0.625rem !important' }} />}
                            label={alert.objectName}
                            size="small"
                            sx={objectChipSx}
                        />
                    )}
                </Box>

                {/* Threshold info or description */}
                {thresholdInfo ? (
                    <Typography sx={ALERT_THRESHOLD_SX}>
                        {thresholdInfo}
                    </Typography>
                ) : alert.description && (
                    <Typography sx={ALERT_DESCRIPTION_SX}>
                        {alert.description}
                    </Typography>
                )}

                {/* Ack info if acknowledged */}
                {isAcknowledged && (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.25 }}>
                        <Typography sx={ALERT_ACK_TEXT_SX}>
                            Acked by {alert.acknowledgedBy}{alert.ackMessage ? `: ${alert.ackMessage}` : ''}
                        </Typography>
                        {alert.falsePositive && (
                            <Chip label="False Positive" size="small" sx={falsePositiveChipSx} />
                        )}
                    </Box>
                )}
            </Box>

            {/* Time and severity */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexShrink: 0 }}>
                <Typography sx={ALERT_TIME_SX}>
                    <ScheduleIcon sx={ICON_10_SX} />
                    {alert.time}
                </Typography>
                <Chip label={alert.severity} size="small" sx={severityChipSx} />
            </Box>

            {/* Analyze button */}
            <Tooltip title="Analyze with AI" placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        onAnalyze?.(alert);
                    }}
                    sx={analyzeButtonSx}
                >
                    <AnalyzeIcon sx={ICON_16_SX} />
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
                    sx={ackButtonSx}
                >
                    {isAcknowledged ? (
                        <UnackIcon sx={ICON_16_SX} />
                    ) : (
                        <AckIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Tooltip>
        </Box>
    );
};

/**
 * GroupedAlertInstance - A single instance row within a grouped alert panel
 */
const GroupedAlertInstance = ({ alert, showServer, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const isAcknowledged = !!alert.acknowledgedAt;
    const baseColor = isAcknowledged ? theme.palette.grey[500] : (severityColors[alert.severity] || severityColors.info);
    const thresholdInfo = formatThresholdInfo(alert);

    const containerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1,
        py: 0.5,
        borderRadius: 0.5,
        bgcolor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.06)
            : 'transparent',
        '&:hover': {
            bgcolor: alpha(theme.palette.grey[500], 0.08),
        },
    }), [isAcknowledged, theme]);

    const serverChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    const dbChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.secondary.main, 0.15),
        color: theme.palette.secondary.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.secondary]);

    const objectChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.625rem',
        bgcolor: alpha(theme.palette.custom.status.online, 0.15),
        color: theme.palette.custom.status.connected,
        '& .MuiChip-label': CHIP_LABEL_SX,
        '& .MuiChip-icon': {
            color: 'inherit',
            ml: 0.25,
            mr: -0.25,
        },
    }), [theme.palette.custom.status]);

    const analyzeButtonSx = useMemo(() => ({
        p: 0.25,
        color: theme.palette.secondary.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.secondary.main, 0.12),
        },
    }), [theme.palette.secondary]);

    const ackButtonSx = useMemo(() => ({
        p: 0.25,
        color: isAcknowledged ? theme.palette.grey[500] : baseColor,
        '&:hover': {
            bgcolor: alpha(baseColor, 0.1),
        },
    }), [isAcknowledged, baseColor, theme.palette.grey]);

    return (
        <Box sx={containerSx}>
            {/* Context chips */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap', flex: 1, minWidth: 0 }}>
                {showServer && alert.server && (
                    <Chip label={alert.server} size="small" sx={serverChipSx} />
                )}
                {alert.databaseName && (
                    <Chip label={alert.databaseName} size="small" sx={dbChipSx} />
                )}
                {alert.objectName && (
                    <Chip
                        icon={<TableIcon sx={{ fontSize: '0.625rem !important' }} />}
                        label={alert.objectName}
                        size="small"
                        sx={objectChipSx}
                    />
                )}
                {thresholdInfo && (
                    <Typography sx={INSTANCE_THRESHOLD_SX}>
                        {thresholdInfo}
                    </Typography>
                )}
            </Box>

            {/* Time */}
            <Typography sx={INSTANCE_TIME_SX}>
                <ScheduleIcon sx={ICON_10_SX} />
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
                    sx={analyzeButtonSx}
                >
                    <AnalyzeIcon sx={ICON_14_SX} />
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
                    sx={ackButtonSx}
                >
                    {isAcknowledged ? (
                        <UnackIcon sx={ICON_14_SX} />
                    ) : (
                        <AckIcon sx={ICON_14_SX} />
                    )}
                </IconButton>
            </Tooltip>
        </Box>
    );
};

/**
 * GroupedAlertItem - Display a group of alerts with the same title in a single panel
 */
const GroupedAlertItem = ({ title, alerts, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const [expanded, setExpanded] = useState(true);

    // Determine highest severity in the group
    const highestSeverity = alerts.reduce((highest, alert) => {
        if (alert.severity === 'critical') return 'critical';
        if (alert.severity === 'warning' && highest !== 'critical') return 'warning';
        return highest;
    }, 'info');

    const baseColor = severityColors[highestSeverity] || severityColors.info;
    const SeverityIcon = highestSeverity === 'critical' ? ErrorIcon : WarningIcon;
    const friendlyTitle = getFriendlyTitle(title);

    const containerSx = useMemo(() => ({
        borderRadius: 1,
        bgcolor: alpha(baseColor, 0.04),
        border: '1px solid',
        borderColor: alpha(baseColor, 0.15),
        overflow: 'hidden',
    }), [baseColor]);

    const headerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        cursor: 'pointer',
        bgcolor: alpha(baseColor, 0.06),
        '&:hover': {
            bgcolor: alpha(baseColor, 0.1),
        },
    }), [baseColor]);

    const countChipSx = useMemo(() => ({
        height: 18,
        fontSize: '0.625rem',
        fontWeight: 600,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_075_SX,
    }), [baseColor]);

    const severityChipSx = useMemo(() => ({
        ...SEVERITY_CHIP_BASE_SX,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [baseColor]);

    return (
        <Box sx={containerSx}>
            {/* Group header */}
            <Box onClick={() => setExpanded(!expanded)} sx={headerSx}>
                <SeverityIcon sx={{ fontSize: 16, color: baseColor, flexShrink: 0 }} />
                <Typography sx={GROUP_TITLE_SX}>
                    {friendlyTitle}
                </Typography>
                <Chip
                    label={`${alerts.length} instance${alerts.length !== 1 ? 's' : ''}`}
                    size="small"
                    sx={countChipSx}
                />
                <Chip label={highestSeverity} size="small" sx={severityChipSx} />
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {expanded ? (
                        <ExpandLessIcon sx={ICON_16_SX} />
                    ) : (
                        <ExpandMoreIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Box>

            {/* Instances list */}
            <Collapse in={expanded}>
                <Box sx={GROUP_INSTANCES_LIST_SX}>
                    {alerts.map((alert) => (
                        <GroupedAlertInstance
                            key={alert.id}
                            alert={alert}
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
const AcknowledgeDialog = ({ open, alert, onClose, onConfirm }) => {
    const theme = useTheme();
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

    const dialogPaperSx = useMemo(() => ({
        bgcolor: theme.palette.background.paper,
        backgroundImage: 'none',
    }), [theme.palette.background.paper]);

    const falsePositiveBoxSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        p: 1.5,
        borderRadius: 1,
        bgcolor: alpha(theme.palette.error.main, 0.06),
        border: '1px solid',
        borderColor: alpha(theme.palette.error.main, 0.18),
        cursor: 'pointer',
    }), [theme.palette.error]);

    const checkboxSx = useMemo(() => ({
        width: 18,
        height: 18,
        borderRadius: 0.5,
        border: '2px solid',
        borderColor: falsePositive ? theme.palette.error.main : theme.palette.grey[400],
        bgcolor: falsePositive ? theme.palette.error.main : 'transparent',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
        transition: 'all 0.15s ease',
    }), [falsePositive, theme]);

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{ sx: dialogPaperSx }}
        >
            <DialogTitle sx={ACK_DIALOG_TITLE_SX}>
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
                    sx={falsePositiveBoxSx}
                    onClick={() => setFalsePositive(!falsePositive)}
                >
                    <Box sx={checkboxSx}>
                        {falsePositive && (
                            <HealthyIcon sx={{ fontSize: 14, color: 'common.white' }} />
                        )}
                    </Box>
                    <Box>
                        <Typography sx={ACK_FALSE_POSITIVE_TITLE_SX}>
                            Mark as false positive
                        </Typography>
                        <Typography sx={ACK_FALSE_POSITIVE_DESC_SX}>
                            This helps improve alert accuracy over time
                        </Typography>
                    </Box>
                </Box>
            </DialogContent>
            <DialogActions sx={ACK_DIALOG_ACTIONS_SX}>
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
 */
const AlertsSection = ({ alerts, loading, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
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

    const skeletonSx = useMemo(() => ({
        bgcolor: theme.palette.divider,
    }), [theme.palette.divider]);

    const activeCountChipSx = useMemo(() => ({
        height: 18,
        fontSize: '0.625rem',
        fontWeight: 600,
        bgcolor: activeAlerts.length > 0
            ? alpha(severityColors.warning, 0.15)
            : alpha(theme.palette.success.main, 0.12),
        color: activeAlerts.length > 0 ? severityColors.warning : theme.palette.success.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [activeAlerts.length, severityColors, theme.palette.success]);

    const noAlertsBoxSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 0.75,
        py: 2,
        borderRadius: 1,
        bgcolor: alpha(theme.palette.success.main, 0.05),
        border: '1px solid',
        borderColor: alpha(theme.palette.success.main, 0.12),
    }), [theme.palette.success]);

    const noAlertsTextSx = useMemo(() => ({
        ...NO_ALERTS_TEXT_BASE_SX,
        color: theme.palette.success.main,
    }), [theme.palette.success]);

    const ackCountChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.5625rem',
        fontWeight: 600,
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.disabled',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    if (loading) {
        return (
            <Box sx={ALERTS_SECTION_MT_SX}>
                <Skeleton variant="text" width={120} height={20} />
                <Box sx={{ mt: 1, display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                    {[1, 2].map((i) => (
                        <Skeleton
                            key={i}
                            variant="rounded"
                            height={48}
                            sx={skeletonSx}
                        />
                    ))}
                </Box>
            </Box>
        );
    }

    // Render either a single AlertItem or a GroupedAlertItem depending on count
    const renderAlertGroup = (title, alertsInGroup) => {
        if (alertsInGroup.length === 1) {
            return (
                <AlertItem
                    key={alertsInGroup[0].id}
                    alert={alertsInGroup[0]}
                    showServer={showServer}
                    onAcknowledge={onAcknowledge}
                    onUnacknowledge={onUnacknowledge}
                    onAnalyze={onAnalyze}
                />
            );
        }
        return (
            <GroupedAlertItem
                key={title}
                title={title}
                alerts={alertsInGroup}
                showServer={showServer}
                onAcknowledge={onAcknowledge}
                onUnacknowledge={onUnacknowledge}
                onAnalyze={onAnalyze}
            />
        );
    };

    return (
        <Box sx={ALERTS_SECTION_MT_SX}>
            {/* Active Alerts Header */}
            <Box onClick={() => setExpanded(!expanded)} sx={ALERTS_HEADER_SX}>
                <AlertIcon sx={{ fontSize: 16, color: 'primary.main' }} />
                <Typography sx={ALERTS_TITLE_SX}>
                    Active Alerts
                </Typography>
                <Chip label={activeAlerts.length} size="small" sx={activeCountChipSx} />
                {sortedActiveGroups.length > 0 && sortedActiveGroups.length !== activeAlerts.length && (
                    <Typography sx={ALERTS_TYPE_COUNT_SX}>
                        ({sortedActiveGroups.length} type{sortedActiveGroups.length !== 1 ? 's' : ''})
                    </Typography>
                )}
                <Box sx={{ flex: 1 }} />
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {expanded ? (
                        <ExpandLessIcon sx={ICON_16_SX} />
                    ) : (
                        <ExpandMoreIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Box>

            {/* Active Alerts List */}
            <Collapse in={expanded}>
                <Box sx={ACTIVE_LIST_SX}>
                    {activeAlerts.length === 0 ? (
                        <Box sx={noAlertsBoxSx}>
                            <HealthyIcon sx={{ fontSize: 16, color: theme.palette.success.main }} />
                            <Typography sx={noAlertsTextSx}>
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
                    <Box onClick={() => setAckExpanded(!ackExpanded)} sx={ACK_HEADER_BASE_SX}>
                        <AckIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                        <Typography sx={ACK_TITLE_SX}>
                            Acknowledged
                        </Typography>
                        <Chip label={acknowledgedAlerts.length} size="small" sx={ackCountChipSx} />
                        <Box sx={{ flex: 1 }} />
                        <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                            {ackExpanded ? (
                                <ExpandLessIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            ) : (
                                <ExpandMoreIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            )}
                        </IconButton>
                    </Box>

                    <Collapse in={ackExpanded}>
                        <Box sx={ACK_LIST_SX}>
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
const SelectionHeader = ({ selection, alertCount = 0, onBlackoutClick }) => {
    const theme = useTheme();

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

    const iconBoxSx = useMemo(() => ({
        ...HEADER_ICON_BOX_BASE_SX,
        bgcolor: alpha(theme.palette.primary.main, 0.12),
    }), [theme.palette.primary]);

    return (
        <Box sx={HEADER_CONTAINER_SX}>
            <Box sx={iconBoxSx}>
                <Icon sx={{ fontSize: 24, color: 'primary.main' }} />
            </Box>
            <Box sx={{ flex: 1 }}>
                <Typography variant="overline" sx={HEADER_LABEL_SX}>
                    {getLabel()}
                </Typography>
                <Typography variant="h5" sx={HEADER_NAME_SX}>
                    {selection.name}
                </Typography>
            </Box>
            <Tooltip title="Blackout management" placement="bottom">
                <IconButton
                    size="small"
                    onClick={onBlackoutClick}
                    sx={{ color: 'text.secondary', mr: 1 }}
                >
                    <BlackoutIcon sx={{ fontSize: 20 }} />
                </IconButton>
            </Tooltip>
            <HeaderStatusIndicator
                status={selection.status}
                alertCount={alertCount}
            />
        </Box>
    );
};

/**
 * StatusPanel - Main component showing status and alerts
 */
interface StatusPanelProps {
    selection: Record<string, unknown> | null;
    mode?: string;
}

const StatusPanel: React.FC<StatusPanelProps> = ({
    selection,
    mode = 'light',
}) => {
    const theme = useTheme();
    const isDark = mode === 'dark';
    const { user } = useAuth();
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(false);
    const initialLoadDoneRef = React.useRef(false);
    const [blackoutMgmtOpen, setBlackoutMgmtOpen] = useState(false);
    const [ackDialogOpen, setAckDialogOpen] = useState(false);
    const [selectedAlertForAck, setSelectedAlertForAck] = useState(null);
    const [analysisDialogOpen, setAnalysisDialogOpen] = useState(false);
    const [analysisAlert, setAnalysisAlert] = useState(null);

    const statusColors = useMemo(() => getStatusColors(theme), [theme]);

    // Calculate metrics based on selection type
    const metrics = useMemo(() => {
        if (!selection) return null;

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
                    const collectServers = (servers) => {
                        servers?.forEach(s => {
                            allServers.push(s);
                            if (s.children) collectServers(s.children);
                        });
                    };
                    collectServers(cluster.servers);
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
                <SelectionHeader selection={selection} alertCount={activeAlertCount} onBlackoutClick={() => setBlackoutMgmtOpen(true)} />

                {/* Divider with gradient */}
                <Box sx={dividerSx} />

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
                    mode={isDark ? 'dark' : 'light'}
                />

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

            {/* Blackout Management Dialog */}
            <BlackoutManagementDialog
                open={blackoutMgmtOpen}
                onClose={() => setBlackoutMgmtOpen(false)}
                selection={selection}
            />
        </Box>
    );
};

export default StatusPanel;
