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
 * Friendly display names for probes and alert rules.
 *
 * These mappings translate internal identifiers into human-readable
 * labels used throughout the UI.
 */

// ---------------------------------------------------------------------------
// Probe friendly names
// ---------------------------------------------------------------------------

export const FRIENDLY_PROBE_NAMES: Record<string, string> = {
    // Core database activity
    'pg_stat_activity': 'Database Activity',
    'pg_stat_database': 'Database Statistics',
    'pg_stat_database_conflicts': 'Database Conflicts',
    'pg_database': 'Database Catalog',

    // Replication
    'pg_stat_replication': 'Replication Status',
    'pg_replication_slots': 'Replication Slots',
    'pg_stat_recovery_prefetch': 'Recovery Prefetch',
    'pg_stat_subscription': 'Subscription Status',

    // Connection and security
    'pg_stat_connection_security': 'Connection Security',
    'pg_connectivity': 'Connectivity',

    // I/O and storage
    'pg_stat_io': 'I/O Statistics',
    'pg_stat_checkpointer': 'Checkpointer Statistics',
    'pg_stat_wal': 'WAL Statistics',
    'pg_statio_all_sequences': 'Sequence I/O',

    // Configuration
    'pg_settings': 'Server Settings',
    'pg_hba_file_rules': 'HBA Rules',
    'pg_ident_file_mappings': 'Ident Mappings',
    'pg_server_info': 'Server Information',
    'pg_node_role': 'Node Role',

    // Table, index, and function statistics
    'pg_stat_all_tables': 'Table Statistics',
    'pg_stat_all_indexes': 'Index Statistics',
    'pg_stat_user_functions': 'Function Statistics',
    'pg_stat_statements': 'Query Statistics',

    // Extensions
    'pg_extension': 'Extensions',

    // System probes
    'pg_sys_os_info': 'OS Information',
    'pg_sys_cpu_info': 'CPU Information',
    'pg_sys_cpu_usage_info': 'CPU Usage',
    'pg_sys_memory_info': 'Memory Usage',
    'pg_sys_io_analysis_info': 'I/O Analysis',
    'pg_sys_disk_info': 'Disk Usage',
    'pg_sys_load_avg_info': 'Load Average',
    'pg_sys_process_info': 'Process Information',
    'pg_sys_network_info': 'Network Statistics',
    'pg_sys_cpu_memory_by_process': 'Process Resources',
};

/**
 * Return a friendly display name for a probe.
 *
 * Falls back to title-casing the raw name when no mapping exists.
 */
export const getFriendlyProbeName = (name: string): string => {
    if (!name) {return name;}
    const friendly = FRIENDLY_PROBE_NAMES[name];
    if (friendly) {return friendly;}

    // Fallback: strip leading "pg_" / "pg_sys_", replace underscores,
    // and title-case each word.
    return name
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (c) => c.toUpperCase())
        .trim();
};

// ---------------------------------------------------------------------------
// Alert friendly names
// ---------------------------------------------------------------------------

export const FRIENDLY_ALERT_TITLES: Record<string, string> = {
    // Connection alerts
    'high_max_connections': 'High Max Connections',
    'connection_utilization': 'Connection Utilization',
    'connection_utilization_percent': 'Connection Utilization',

    // Replication alerts
    'replication_lag_bytes': 'Replication Lag',
    'replication_slot_inactive': 'Replication Slot Inactive',
    'replication_standby_disconnected': 'Standby Disconnected',
    'subscription_worker_down': 'Subscription Worker Down',

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

    // Staleness and anomaly alerts
    'metric_staleness': 'Metric Staleness',
    'session_count_anomaly': 'Session Count Anomaly',

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
    'pg_sys_disk_info.used_percent': 'Disk Usage',
};

/**
 * Return a friendly display name for an alert title or rule name.
 *
 * Handles connection-error prefixes, exact matches, partial matches,
 * metric-dot notation, and falls back to title-cased text.
 */
export const getFriendlyTitle = (title: string): string => {
    if (!title) {return 'Alert';}

    // Connection error alerts: preserve hostname as-is
    if (title.toLowerCase().startsWith('connection error:')) {
        return `Connection Error:${title.substring('connection error:'.length)}`;
    }

    // Check for exact match first
    const normalizedTitle = title.toLowerCase().trim();
    if (FRIENDLY_ALERT_TITLES[normalizedTitle]) {
        return FRIENDLY_ALERT_TITLES[normalizedTitle];
    }

    // Check for partial matches
    for (const [key, value] of Object.entries(FRIENDLY_ALERT_TITLES)) {
        if (normalizedTitle.includes(key) || normalizedTitle.startsWith(key)) {
            return value;
        }
    }

    // Metric names (contain dots like pg_stat_activity.count) - display as-is
    if (normalizedTitle.includes('.')) {
        return title.trim();
    }

    // Fallback: clean up the title by replacing underscores and capitalizing
    return title
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (char) => char.toUpperCase())
        .trim();
};
