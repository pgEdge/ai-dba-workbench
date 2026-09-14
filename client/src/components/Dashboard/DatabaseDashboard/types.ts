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
 * Local types for the DatabaseDashboard components.
 */

import type { MetricDataPoint } from '../types';
import { formatBytes, formatValue, formatNumber, formatTime } from '../../../utils/formatters';

/** Props shared by all database dashboard section components */
export interface DatabaseSectionProps {
    connectionId: number;
    databaseName: string;
}

/** Table leaderboard row from pg_stat_all_tables */
export interface TableLeaderboardRow {
    schemaname: string;
    relname: string;
    n_live_tup: number;
    n_dead_tup: number;
    seq_scan: number;
    seq_tup_read: number;
    idx_scan: number;
    idx_tup_fetch: number;
    n_tup_ins: number;
    n_tup_upd: number;
    n_tup_del: number;
    n_tup_hot_upd: number;
    table_size: number;
    last_vacuum?: string;
    last_autovacuum?: string;
    last_analyze?: string;
    last_autoanalyze?: string;
}

/** Index leaderboard row from pg_stat_all_indexes */
export interface IndexLeaderboardRow {
    schemaname: string;
    relname: string;
    indexrelname: string;
    idx_scan: number;
    idx_tup_read: number;
    idx_tup_fetch: number;
    index_size: number;
}

/** Leaderboard sort criteria for tables */
export type TableSortCriteria =
    | 'size'
    | 'seq_scans'
    | 'dead_tuples'
    | 'modifications';

/** Leaderboard sort criteria for indexes */
export type IndexSortCriteria = 'size' | 'scans' | 'unused';

/** Response shape for table/index listing queries */
export interface LeaderboardResponse<T> {
    rows: T[];
    total_count: number;
}

/**
 * Helper to extract sparkline-compatible data points from a
 * MetricSeries array returned by useMetrics.
 */
export const extractSparklineData = (
    data: Array<{
        name: string;
        metric: string;
        data: MetricDataPoint[];
    }> | null,
    metricName: string,
): MetricDataPoint[] => {
    if (!data) { return []; }
    const series = data.find(s => s.metric === metricName);
    return series?.data ?? [];
};

/**
 * Extract the latest value from a gauge-style metric series.
 *
 * Empty time buckets are filled with 0 by the backend, so the most
 * recent bucket often reads 0 simply because it has not been
 * collected yet; this scans backwards for the last non-zero value to
 * avoid showing a spurious 0. That fallback is wrong for rates, where
 * 0 is a genuine reading for an idle counter, so use
 * `extractLatestRate` for `_per_sec` metrics instead.
 */
export const extractLatestValue = (
    data: Array<{
        name: string;
        metric: string;
        data: MetricDataPoint[];
    }> | null,
    metricName: string,
): number | null => {
    const points = extractSparklineData(data, metricName);
    if (points.length === 0) { return null; }

    // Scan backwards to find the last non-zero value. This handles
    // the common case where the most recent time bucket has no data
    // yet (empty buckets are filled with 0 by the backend).
    for (let i = points.length - 1; i >= 0; i--) {
        if (points[i].value !== 0) {
            return points[i].value;
        }
    }

    return 0;
};

/**
 * Extract the latest value from a rate metric series, such as any
 * `_per_sec` metric. Unlike `extractLatestValue` this returns the
 * final data point whatever its value, because a genuine 0 means the
 * underlying counter was idle over the bucket and must be shown as 0.
 */
export const extractLatestRate = (
    data: Array<{
        name: string;
        metric: string;
        data: MetricDataPoint[];
    }> | null,
    metricName: string,
): number | null => {
    const points = extractSparklineData(data, metricName);
    if (points.length === 0) { return null; }
    return points[points.length - 1].value;
};

export { formatBytes, formatValue, formatNumber, formatTime };

/**
 * Format a timestamp for display in the vacuum status table.
 */
export const formatTimestamp = (ts: string | undefined): string => {
    if (!ts) { return 'Never'; }
    try {
        const date = new Date(ts);
        return date.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        });
    } catch {
        return ts;
    }
};

/**
 * Determine how long ago a timestamp occurred and return
 * a status color indicating freshness.
 */
export const getVacuumStatus = (
    ts: string | undefined
): 'good' | 'warning' | 'critical' => {
    if (!ts) { return 'critical'; }
    try {
        const date = new Date(ts);
        const now = new Date();
        const diffMs = now.getTime() - date.getTime();
        const diffDays = diffMs / (1000 * 60 * 60 * 24);
        if (diffDays <= 1) { return 'good'; }
        if (diffDays <= 7) { return 'warning'; }
        return 'critical';
    } catch {
        return 'critical';
    }
};
