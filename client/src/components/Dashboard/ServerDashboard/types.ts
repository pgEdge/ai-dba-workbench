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
 * Local types for the ServerDashboard components.
 */

import type { MetricDataPoint } from '../types';

/** Props shared by all server dashboard section components */
export interface ServerSectionProps {
    connectionId: number;
    connectionName?: string;
}

/**
 * Cache hit ratio over the requested range, as sent by the
 * performance-summary and database-summaries endpoints. The server
 * computes it from per-interval deltas of the pg_stat_database block
 * counters, differenced per database before summing. Both `current`
 * and a bucket `value` are null when no block access happened in that
 * interval; a null bucket is drawn as a gap rather than as 0%.
 */
export interface CacheHitRatioData {
    current: number | null;
    time_series: { time: string; value: number | null }[];
}

/**
 * The part of a performance-summary response the server dashboard
 * reads. The endpoint returns one entry per requested connection.
 */
export interface ServerCacheHitSummary {
    connections?: {
        connection_id: number;
        cache_hit_ratio?: CacheHitRatioData;
    }[];
}

/** Database summary card data from the database-summaries API */
export interface DatabaseSummary {
    database_name: string;
    size_bytes: number;
    size_pretty: string;
    cache_hit_ratio: CacheHitRatioData;
    transaction_rate: number;
    dead_tuple_ratio: number;
    active_connections: number;
}

/** Performance summary response for a single server */
export interface ServerPerformanceSummary {
    databases: DatabaseSummary[];
}

/** Top query row from pg_stat_statements */
export interface TopQueryRow {
    query: string;
    queryid: string;
    calls: number;
    total_exec_time: number;
    mean_exec_time: number;
    rows: number;
    shared_blks_hit: number;
    shared_blks_read: number;
    database_name: string;
    /**
     * Last observed client, from the pg_stat_activity snapshot join;
     * each is null when the client was never observed. Optional
     * because this section does not render them.
     */
    client_addr?: string | null;
    client_hostname?: string | null;
    client_observed_at?: string | null;
}

/** The groupings supported by the connection-groups endpoint */
export type ConnectionGroupBy = 'user' | 'client' | 'database';

/**
 * A single connection grouping from the latest collected snapshot.
 * `client_hostname` is only ever populated for the client grouping,
 * and is null when no reverse lookup was recorded.
 */
export interface ConnectionGroupRow {
    group_label: string;
    client_hostname: string | null;
    total: number;
    active: number;
    idle: number;
    idle_in_transaction: number;
    other: number;
}

/**
 * Connection groupings response. `collected_at` is null when no
 * snapshot exists within the requested period, in which case
 * `groups` is empty.
 */
export interface ConnectionGroupsResponse {
    collected_at: string | null;
    groups: ConnectionGroupRow[];
}

/**
 * Helper to extract sparkline-compatible data points from a
 * MetricSeries array returned by useMetrics.
 */
export const extractSparklineData = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
    metricName: string,
): MetricDataPoint[] => {
    if (!data) { return []; }
    const series = data.find(s => s.metric === metricName);
    return series?.data ?? [];
};

/**
 * Check whether a metric series contains any non-zero data points.
 * Returns false when all values are zero or null, which typically
 * indicates the underlying extension is not installed.
 */
export const hasNonZeroData = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
    metricName: string,
): boolean => {
    const points = extractSparklineData(data, metricName);
    return points.some(p => p.value !== null && p.value !== 0);
};

/**
 * Extract the latest value from a gauge-style metric series.
 *
 * The backend reports a bucket with no reading as `null`, so this
 * scans backwards for the last non-null point and returns it, a
 * genuine 0 included. Zeros used to be skipped when the backend filled
 * empty buckets with 0, but a missing reading is now null, so a
 * trailing 0 is a real reading and skipping it would show a stale
 * value. An all-null or empty series yields null.
 */
export const extractLatestValue = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
    metricName: string,
): number | null => {
    const points = extractSparklineData(data, metricName);
    for (let i = points.length - 1; i >= 0; i--) {
        if (points[i].value !== null) { return points[i].value; }
    }
    return null;
};

/**
 * Extract the latest value from a rate metric series, such as any
 * `_per_sec` metric. Like `extractLatestValue` this returns the final
 * non-null data point whatever its value, because a genuine 0 means
 * the underlying counter was idle over the bucket and must be shown
 * as 0. Null buckets (a collector gap, a counter reset or a rate that
 * cannot be derived) are skipped; an all-null series yields null. It
 * is kept as a separate name so call sites say which kind of metric
 * they read.
 */
export const extractLatestRate = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
    metricName: string,
): number | null => {
    const points = extractSparklineData(data, metricName);
    for (let i = points.length - 1; i >= 0; i--) {
        if (points[i].value !== null) { return points[i].value; }
    }
    return null;
};
