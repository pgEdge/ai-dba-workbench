/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

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
    DoNotDisturb as DoNotDisturbIcon,
    DoNotDisturbOff as DoNotDisturbOffIcon,
} from '@mui/icons-material';
import type { FilterChipEntry } from './types';

/**
 * Event type configuration with icons and theme-based color keys
 */
export const EVENT_TYPE_CONFIG = {
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
export const ALL_EVENT_TYPES = Object.keys(EVENT_TYPE_CONFIG);

// Filter chip definitions -- groups related event types under a single chip.
// Each entry maps a chip key to its display label, theme color key, and
// the underlying event types it controls.
export const FILTER_CHIPS: Record<string, FilterChipEntry> = {
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
export const TIME_RANGE_OPTIONS = [
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
];

// localStorage key for persisting time range preference
export const TIME_RANGE_STORAGE_KEY = 'timeline-time-range';

// Get initial time range from localStorage or use default
export const getInitialTimeRange = () => {
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
