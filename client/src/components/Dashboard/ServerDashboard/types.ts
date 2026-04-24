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

/** Database summary card data from the performance-summary API */
export interface DatabaseSummary {
    database_name: string;
    size_bytes: number;
    size_pretty: string;
    cache_hit_ratio: {
        current: number;
        time_series: { time: string; value: number }[];
    };
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
 * Returns false when all values are zero, which typically indicates
 * the underlying extension is not installed.
 */
export const hasNonZeroData = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
    metricName: string,
): boolean => {
    const points = extractSparklineData(data, metricName);
    return points.some(p => p.value !== 0);
};

/**
 * Extract the latest value from a metric series.
 */
export const extractLatestValue = (
    data: { name: string; metric: string; data: MetricDataPoint[] }[] | null,
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
