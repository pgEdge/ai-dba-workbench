/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared style constants for StatusPanel sub-components
 */

import { Theme } from '@mui/material/styles';
import { alpha } from '@mui/material';

// Map internal alert rule names to friendly display names
export const FRIENDLY_ALERT_TITLES = {
    // Connection alerts
    'high_max_connections': 'High Max Connections',
    'connection_utilization': 'Connection Utilization',
    'connection_utilization_percent': 'Connection Utilization',
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
    // Anomaly metric names
    'pg_stat_activity.count': 'Active Backends',
    'pg_stat_activity.idle_in_transaction_seconds': 'Idle in Transaction',
    'pg_stat_activity.max_query_duration_seconds': 'Long Running Query',
    'pg_stat_activity.max_xact_duration_seconds': 'Long Running Transaction',
    'pg_stat_all_tables.dead_tuple_percent': 'Dead Tuple Ratio',
    'pg_stat_database.cache_hit_ratio': 'Cache Hit Ratio',
    'pg_stat_database.deadlocks_delta': 'Deadlocks',
    'pg_stat_database.temp_files_delta': 'Temporary Files',
    'pg_sys_memory_info.used_percent': 'Memory Usage',
};

// Get friendly title for an alert
export const getFriendlyTitle = (title) => {
    if (!title) {return 'Alert';}
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
    // Metric names (contain dots like pg_stat_activity.count) — display as-is
    if (normalizedTitle.includes('.')) {
        return title.trim();
    }
    // Fallback: clean up the title by replacing underscores and capitalizing words
    return title
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (char) => char.toUpperCase())
        .trim();
};

// Format threshold info for display
export const formatThresholdInfo = (alert) => {
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

// Shared section panel container style (used by AlertsSection, Monitoring, etc.)
export const getSectionPanelSx = (theme: Theme) => ({
    mt: 2,
    p: 1.5,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.4)
        : theme.palette.grey[100],
    border: '1px solid',
    borderColor: theme.palette.divider,
});

// Shared small chip label style
export const CHIP_LABEL_SX = { px: 0.5 };
export const CHIP_LABEL_075_SX = { px: 0.75 };

// HeaderStatusIndicator sizes
export const INDICATOR_SIZES = {
    small: 14,
    medium: 18,
    large: 22,
};

// Static layout styles
export const FLEX_1_MIN0_SX = { flex: 1, minWidth: 0 };
export const EXPAND_BUTTON_SX = { p: 0.25 };
export const ICON_16_SX = { fontSize: 16 };
export const ICON_14_SX = { fontSize: 14 };
export const ICON_10_SX = { fontSize: 10 };

// MetricCard static styles
export const METRIC_LABEL_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

export const METRIC_VALUE_BASE_SX = {
    fontWeight: 700,
    fontSize: '1.75rem',
    lineHeight: 1,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

export const METRIC_TREND_CONTAINER_SX = { display: 'flex', alignItems: 'center', gap: 0.25 };

// ServerInfoCard static styles
export const SERVER_INFO_WRAPPER_SX = {
    display: 'flex',
    flexWrap: 'wrap',
};

export const SERVER_INFO_LABEL_BASE_SX = {
    fontSize: '0.875rem',
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.1em',
    lineHeight: 1,
};

export const SERVER_INFO_VALUE_BASE_SX = {
    color: 'text.primary',
    fontSize: '1rem',
    fontWeight: 500,
    lineHeight: 1.2,
    whiteSpace: 'nowrap',
};

export const SPOCK_DOT_SX = {
    width: 6,
    height: 6,
    borderRadius: '50%',
};

export const SPOCK_LABEL_BASE_SX = {
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

export const SPOCK_VERSION_SX = {
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.secondary',
};

export const SPOCK_NODE_SX = {
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.primary',
    fontWeight: 500,
};

// AlertItem static styles
export const ALERT_TITLE_BASE_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    lineHeight: 1.2,
};

export const ALERT_THRESHOLD_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    mt: 0.25,
};

export const ALERT_DESCRIPTION_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

export const ALERT_ACK_TEXT_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontStyle: 'italic',
};

export const ALERT_TIME_SX = {
    color: 'text.disabled',
    fontSize: '0.875rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
};

export const SEVERITY_CHIP_BASE_SX = {
    height: 16,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
};

export const ALERT_TYPE_CHIP_BASE_SX = {
    height: 16,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'capitalize',
};

// GroupedAlertInstance static styles
export const INSTANCE_TIME_SX = {
    color: 'text.disabled',
    fontSize: '0.875rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
    flexShrink: 0,
};

export const INSTANCE_THRESHOLD_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

// GroupedAlertItem static styles
export const GROUP_TITLE_SX = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1rem',
    lineHeight: 1.2,
    flex: 1,
};

export const GROUP_INSTANCES_LIST_SX = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.25,
    px: 0.5,
    py: 0.5,
};

// AcknowledgeDialog static styles
export const ACK_DIALOG_TITLE_SX = { pb: 1 };
export const ACK_DIALOG_ACTIONS_SX = { px: 3, pb: 2 };
export const ACK_FALSE_POSITIVE_TITLE_SX = { fontSize: '1rem', fontWeight: 500, color: 'text.primary' };
export const ACK_FALSE_POSITIVE_DESC_SX = { fontSize: '0.875rem', color: 'text.secondary' };

// AlertsSection static styles
export const ALERTS_SECTION_MT_SX = { mt: 2 };
export const ALERTS_HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    py: 0.25,
    '&:hover': { opacity: 0.8 },
};

export const ALERTS_TITLE_SX = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1rem',
};

export const ALERTS_TYPE_COUNT_SX = {
    color: 'text.disabled',
    fontSize: '0.875rem',
};

export const ACTIVE_LIST_SX = {
    mt: 1,
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

export const NO_ALERTS_TEXT_BASE_SX = {
    fontWeight: 500,
    fontSize: '1rem',
};

export const ACK_HEADER_BASE_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    py: 0.25,
    mt: 1.5,
    '&:hover': { opacity: 0.8 },
};

export const ACK_TITLE_SX = {
    fontWeight: 500,
    color: 'text.secondary',
    fontSize: '0.875rem',
};

export const ACK_LIST_SX = {
    mt: 0.75,
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

// SelectionHeader static styles
export const HEADER_CONTAINER_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    mb: 3,
};

export const HEADER_ICON_BOX_BASE_SX = {
    width: 48,
    height: 48,
    borderRadius: 2,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
};

export const HEADER_LABEL_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontWeight: 600,
    letterSpacing: '0.08em',
    lineHeight: 1,
};

export const HEADER_NAME_SX = {
    fontWeight: 600,
    color: 'text.primary',
    lineHeight: 1.2,
    mt: 0.25,
};

// StatusPanel static styles
export const EMPTY_STATE_CONTAINER_SX = {
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    p: 4,
};

export const EMPTY_STATE_TITLE_SX = {
    color: 'text.secondary',
    fontWeight: 500,
    mb: 0.5,
};

export const EMPTY_STATE_DESC_SX = {
    color: 'text.disabled',
    textAlign: 'center',
    maxWidth: 300,
};

export const PANEL_ROOT_SX = {
    overflow: 'auto',
    p: 3,
};

export const METRICS_GRID_SX = {
    display: 'flex',
    gap: 2,
    flexWrap: 'wrap',
    mb: 2,
};

// Theme-dependent style getters
export const getStatusColors = (theme: Theme) => ({
    online: theme.palette.success.main,
    warning: theme.palette.warning.main,
    offline: theme.palette.error.main,
    unknown: theme.palette.grey[500],
});

export const getSeverityColors = (theme: Theme) => ({
    critical: theme.palette.error.main,
    warning: theme.palette.warning.main,
    info: theme.palette.info.main,
});

export const getAlertTypeColor = (theme: Theme, alertType: string) => {
    return alertType === 'anomaly'
        ? theme.palette.secondary.main
        : theme.palette.info.main;
};

/**
 * Group alerts by their title and severity for consolidated display.
 * The grouping key combines title and severity (e.g. "High CPU::critical")
 * so alerts of different severities appear as separate groups.
 */
export const groupAlertsByTitleAndSeverity = (alerts) => {
    return alerts.reduce((groups, alert) => {
        const title = getFriendlyTitle(alert.title);
        const key = `${title}::${alert.severity || 'info'}`;
        if (!groups[key]) {
            groups[key] = [];
        }
        groups[key].push(alert);
        return groups;
    }, {});
};
