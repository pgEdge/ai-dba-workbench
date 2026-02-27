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

import { MetricDataPoint, MetricSeries } from '../types';
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
    table_size_pretty?: string;
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
    index_size_pretty?: string;
}

/** Query detail data from pg_stat_statements */
export interface QueryDetailData {
    queryid: string;
    query: string;
    calls: number;
    total_exec_time: number;
    mean_exec_time: number;
    rows: number;
    shared_blks_hit: number;
    shared_blks_read: number;
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
 * Extract the latest value from a metric series.
 */
export const extractLatestValue = (
    data: MetricSeries[] | null,
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
 * Build chart data from metric series for the Chart component.
 */
export const buildChartData = (
    series: MetricSeries[] | null,
    metricNames: string[],
    displayNames?: string[],
) => {
    if (!series) { return null; }

    const matchedSeries = metricNames.map((metric, idx) => {
        const found = series.find(s => s.metric === metric);
        return {
            name: displayNames?.[idx] ?? metric,
            data: found?.data.map(d => d.value) ?? [],
            categories: found?.data.map(d => d.time) ?? [],
        };
    });

    if (matchedSeries.every(s => s.data.length === 0)) {
        return null;
    }

    const categories = matchedSeries.find(
        s => s.categories.length > 0
    )?.categories ?? [];

    return {
        categories,
        series: matchedSeries.map(s => ({
            name: s.name,
            data: s.data,
        })),
    };
};

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
