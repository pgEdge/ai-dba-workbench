/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { alpha } from '@mui/material';
import {
    Star as PrimaryIcon,
    Backup as StandbyIcon,
    AccountTree as CascadingIcon,
    Hub as SpockIcon,
    Publish as PublisherIcon,
    Download as SubscriberIcon,
    Storage as StandaloneIcon,
} from '@mui/icons-material';

/**
 * Role configuration with colors and labels for server role display
 */
export const ROLE_CONFIGS = {
    binary_primary: { label: 'Primary', color: '#15AABF', darkColor: '#22B8CF' },
    binary_standby: { label: 'Standby', color: '#6366F1', darkColor: '#818CF8' },
    binary_cascading: { label: 'Cascade', color: '#8B5CF6', darkColor: '#A78BFA' },
    spock_node: { label: 'Spock', color: '#F59E0B', darkColor: '#FBBF24' },
    standalone: { label: 'Standalone', color: '#6B7280', darkColor: '#94A3B8' },
    logical_publisher: { label: 'Publisher', color: '#22C55E', darkColor: '#4ADE80' },
    logical_subscriber: { label: 'Subscriber', color: '#3B82F6', darkColor: '#60A5FA' },
};

/**
 * Role to icon component mapping
 */
export const ROLE_ICONS = {
    binary_primary: PrimaryIcon,
    binary_standby: StandbyIcon,
    binary_cascading: CascadingIcon,
    spock_node: SpockIcon,
    standalone: StandaloneIcon,
    logical_publisher: PublisherIcon,
    logical_subscriber: SubscriberIcon,
};

/**
 * Cluster type color schemes for visual distinction
 */
export const CLUSTER_TYPE_COLORS = {
    spock: {
        border: { dark: alpha('#F59E0B', 0.5), light: alpha('#F59E0B', 0.4) },
        bg: { dark: alpha('#F59E0B', 0.08), light: alpha('#F59E0B', 0.05) },
    },
    binary: {
        border: { dark: alpha('#22B8CF', 0.4), light: alpha('#15AABF', 0.35) },
        bg: { dark: alpha('#22B8CF', 0.06), light: alpha('#15AABF', 0.04) },
    },
    logical: {
        border: { dark: alpha('#8B5CF6', 0.4), light: alpha('#8B5CF6', 0.35) },
        bg: { dark: alpha('#8B5CF6', 0.06), light: alpha('#8B5CF6', 0.04) },
    },
    default: {
        border: { dark: alpha('#475569', 0.5), light: alpha('#D1D5DB', 0.6) },
        bg: { dark: alpha('#1E293B', 0.5), light: alpha('#F9FAFB', 0.5) },
    },
};

/**
 * localStorage keys for persisting navigator state
 */
export const STORAGE_KEYS = {
    WIDTH: 'clusterNavigator.width',
    EXPANDED_CLUSTERS: 'clusterNavigator.expandedClusters',
};
