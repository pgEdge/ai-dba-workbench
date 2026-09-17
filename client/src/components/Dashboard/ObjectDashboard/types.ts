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
 * Local types for the ObjectDashboard components.
 */

import type { MetricDataPoint, MetricSeries } from '../types';
import { buildMetricChartData } from '../metricsChart';
import { formatBytes, formatValue, formatNumber, formatTime } from '../../../utils/formatters';

/** Props shared by all object detail components */
export interface ObjectDetailProps {
    connectionId: number;
    databaseName: string;
    schemaName?: string;
    objectName: string;
}

/** Table detail data from pg_stat_all_tables */
export interface TableDetailData {
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

/** Index detail data from pg_stat_all_indexes */
export interface IndexDetailData {
    schemaname: string;
    relname: string;
    indexrelname: string;
    idx_scan: number;
    idx_tup_read: number;
    idx_tup_fetch: number;
    index_size: number;
}

/** Query detail data from pg_stat_statements */
export interface QueryDetailData {
    queryid: string;
    query: string;
    calls: number;
    total_exec_time: number;
    mean_exec_time: number;
    /** Fastest recorded execution, in milliseconds. */
    min_exec_time: number;
    /** Slowest recorded execution, in milliseconds. */
    max_exec_time: number;
    rows: number;
    shared_blks_hit: number;
    shared_blks_read: number;
    /**
     * Database role that executed the statement; empty when the
     * collector could not resolve the role OID to a name.
     */
    username: string;
    /**
     * Client address seen running the statement in the most recent
     * pg_stat_activity snapshot that caught it in flight. A backend
     * connected over a Unix-domain socket is reported as 'local',
     * since it has no network address; use `client_observed_at`,
     * rather than this field, to tell whether the query was ever
     * observed in flight at all.
     */
    client_addr: string | null;
    /** Resolved client hostname, when the server had one. */
    client_hostname: string | null;
    /**
     * RFC 3339 time of the snapshot in which the client was seen;
     * null if and only if no snapshot ever caught the query in
     * flight.
     */
    client_observed_at: string | null;
}

/**
 * Helper to extract sparkline-compatible data points from a
 * MetricSeries array returned by useMetrics.
 */
export const extractSparklineData = (
    data: MetricSeries[] | null,
    metricName: string,
): MetricDataPoint[] => {
    if (!data) { return []; }
    const series = data.find(s => s.metric === metricName);
    return series?.data ?? [];
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
    data: MetricSeries[] | null,
    metricName: string,
): number | null => {
    const points = extractSparklineData(data, metricName);
    for (let i = points.length - 1; i >= 0; i--) {
        if (points[i].value !== null) { return points[i].value; }
    }
    return null;
};

/**
 * Build chart data from metric series for the Chart component.
 *
 * Re-exported here so the object-level detail views keep importing it
 * from their own types module; the behaviour lives in `metricsChart`,
 * which anchors the x-axis to the queried window.
 */
export const buildChartData = buildMetricChartData;

export { formatBytes, formatValue, formatNumber, formatTime };

/**
 * Format a timestamp for display.
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
