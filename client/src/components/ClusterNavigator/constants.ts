/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { alpha, Theme } from '@mui/material';
import {
    Star as PrimaryIcon,
    Backup as StandbyIcon,
    AccountTree as CascadingIcon,
    Hub as SpockIcon,
    Publish as PublisherIcon,
    Download as SubscriberIcon,
    Storage as StandaloneIcon,
} from '@mui/icons-material';
import type { SvgIconComponent } from '@mui/icons-material';

export interface RoleConfig {
    label: string;
    color: string;
    darkColor: string;
}

export type ServerRole =
    | 'binary_primary'
    | 'binary_standby'
    | 'binary_cascading'
    | 'spock_node'
    | 'standalone'
    | 'logical_publisher'
    | 'logical_subscriber';

export type ClusterType = 'spock' | 'binary' | 'logical' | 'default';

interface ModeColors {
    dark: string;
    light: string;
}

interface ClusterTypeColor {
    border: ModeColors;
    bg: ModeColors;
}

/**
 * Role configuration with colors and labels for server role display.
 * Returns a config object keyed by role; requires a MUI theme instance.
 */
export const getRoleConfigs = (theme: Theme): Record<ServerRole, RoleConfig> => ({
    binary_primary: { label: 'Primary', color: theme.palette.primary.main, darkColor: theme.palette.primary.light },
    binary_standby: { label: 'Standby', color: theme.palette.secondary.main, darkColor: theme.palette.secondary.light },
    binary_cascading: { label: 'Cascade', color: theme.palette.custom.status.purple, darkColor: theme.palette.custom.status.purpleLight },
    spock_node: { label: 'Spock', color: theme.palette.warning.main, darkColor: theme.palette.warning.light },
    standalone: { label: 'Standalone', color: theme.palette.grey[500], darkColor: theme.palette.grey[400] },
    logical_publisher: { label: 'Publisher', color: theme.palette.success.main, darkColor: theme.palette.success.light },
    logical_subscriber: { label: 'Subscriber', color: theme.palette.info.main, darkColor: theme.palette.info.light },
});

/**
 * Role to icon component mapping
 */
export const ROLE_ICONS: Record<ServerRole, SvgIconComponent> = {
    binary_primary: PrimaryIcon,
    binary_standby: StandbyIcon,
    binary_cascading: CascadingIcon,
    spock_node: SpockIcon,
    standalone: StandaloneIcon,
    logical_publisher: PublisherIcon,
    logical_subscriber: SubscriberIcon,
};

/**
 * Cluster type color schemes for visual distinction.
 * Returns a config object keyed by cluster type; requires a MUI theme instance.
 */
export const getClusterTypeColors = (theme: Theme): Record<ClusterType, ClusterTypeColor> => ({
    spock: {
        border: { dark: alpha(theme.palette.warning.main, 0.5), light: alpha(theme.palette.warning.main, 0.4) },
        bg: { dark: alpha(theme.palette.warning.main, 0.08), light: alpha(theme.palette.warning.main, 0.05) },
    },
    binary: {
        border: { dark: alpha(theme.palette.primary.light, 0.4), light: alpha(theme.palette.primary.main, 0.35) },
        bg: { dark: alpha(theme.palette.primary.light, 0.06), light: alpha(theme.palette.primary.main, 0.04) },
    },
    logical: {
        border: { dark: alpha(theme.palette.custom.status.purple, 0.4), light: alpha(theme.palette.custom.status.purple, 0.35) },
        bg: { dark: alpha(theme.palette.custom.status.purple, 0.06), light: alpha(theme.palette.custom.status.purple, 0.04) },
    },
    default: {
        border: { dark: alpha(theme.palette.grey[600], 0.5), light: alpha(theme.palette.grey[300], 0.6) },
        bg: { dark: alpha(theme.palette.grey[800], 0.5), light: alpha(theme.palette.grey[50], 0.5) },
    },
});

/**
 * localStorage keys for persisting navigator state
 */
export const STORAGE_KEYS = {
    WIDTH: 'clusterNavigator.width',
    EXPANDED_CLUSTERS: 'clusterNavigator.expandedClusters',
} as const;
