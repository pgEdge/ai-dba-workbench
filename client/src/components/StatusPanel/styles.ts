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
 * Shared style constants for StatusPanel sub-components.
 *
 * Global, app-wide design tokens (icon sizes, metric label/value
 * typography, server-info label/value typography, alert text variants,
 * etc.) live in `client/src/theme/tokens.ts` and should be imported
 * from `'../../theme'`. This file contains only StatusPanel-specific
 * layout and helpers.
 */

import type { Theme } from '@mui/material/styles';
import { alpha } from '@mui/material';
import { getFriendlyTitle } from '../../utils/friendlyNames';
import { MONO_CAPTION_SX } from '../../theme';

// Re-export so existing imports from this module continue to work
export { getFriendlyTitle } from '../../utils/friendlyNames';

/**
 * Return true when an alert has a `lastUpdated` timestamp that is
 * meaningfully different from its `triggeredAt` timestamp. Used to
 * decide whether to surface a separate "Last updated" line in the
 * alert display; reactivated alerts will have the two timestamps
 * diverge, freshly-triggered alerts will not.
 */
export const hasDistinctLastUpdated = (alert) => {
    if (!alert?.lastUpdated || !alert?.triggeredAt) {
        return false;
    }
    const triggered = new Date(alert.triggeredAt).getTime();
    const updated = new Date(alert.lastUpdated).getTime();
    if (Number.isNaN(triggered) || Number.isNaN(updated)) {
        return false;
    }
    // Treat timestamps within one second of each other as "the same"
    // since the backend writes them at effectively the same instant
    // when an alert is first created.
    return Math.abs(updated - triggered) >= 1000;
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

// Static layout styles
export const FLEX_1_MIN0_SX = { flex: 1, minWidth: 0 };
export const EXPAND_BUTTON_SX = { p: 0.25 };

// MetricCard static styles
export const METRIC_TREND_CONTAINER_SX = { display: 'flex', alignItems: 'center', gap: 0.25 };

// ServerInfoCard static styles
export const SERVER_INFO_WRAPPER_SX = {
    display: 'flex',
    flexWrap: 'wrap',
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
    ...MONO_CAPTION_SX,
    color: 'text.secondary',
};

export const SPOCK_NODE_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.primary',
    fontWeight: 500,
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
    ...MONO_CAPTION_SX,
    color: 'text.secondary',
};

// GroupedAlertItem static styles
export const GROUP_TITLE_SX = {
    fontWeight: 600,
    color: 'text.primary',
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
export const ACK_FALSE_POSITIVE_TITLE_SX = { fontWeight: 500, color: 'text.primary' };
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
