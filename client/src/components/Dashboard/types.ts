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
 * TypeScript interfaces for the Dashboard monitoring system
 */

import type { Selection } from '../../types/selection';

/**
 * Time range options for metric queries. The 'custom' member denotes an
 * arbitrary window whose bounds are carried by the customStart and
 * customEnd fields of TimeRangeState.
 */
export type TimeRange = '1h' | '6h' | '24h' | '7d' | '30d' | 'custom';

/** Dashboard hierarchy levels */
export type DashboardLevel = 'estate' | 'cluster' | 'server' | 'database' | 'object';

/** Object types that can be drilled into */
export type ObjectType = 'table' | 'index' | 'query';

/** Overlay entry in the overlay stack */
export interface OverlayEntry {
    level: DashboardLevel;
    title: string;
    entityId: string | number;
    entityName: string;
    objectType?: ObjectType;
    /** For database level: which connection and database */
    connectionId?: number;
    connectionName?: string;
    databaseName?: string;
    /** For object level: parent database info */
    schemaName?: string;
    objectName?: string;
}

/** Time range state managed by DashboardContext */
export interface TimeRangeState {
    range: TimeRange;
    /** Optional custom range (ISO strings) */
    customStart?: string;
    customEnd?: string;
}

/** Auto-refresh configuration */
export interface AutoRefreshConfig {
    enabled: boolean;
    intervalMs: number;
}

/**
 * Metric data point from the metrics query API. The server emits every
 * bucket for every series in a response, so all series share their
 * bucket times; a bucket with no value (a collector gap, a counter
 * reset or a rate that cannot be derived) carries `null` rather than
 * being omitted or filled with 0.
 */
export interface MetricDataPoint {
    time: string;
    value: number | null;
    /**
     * True when the value is the last observation carried forward
     * rather than a sample collected in this bucket. Omitted when the
     * point is an observed reading.
     */
    filled?: boolean;
}

/**
 * A point accepted by Sparkline and KpiTile. Unlike MetricDataPoint
 * the value may be null, which is drawn as a gap; MetricDataPoint is
 * assignable to it, so callers with complete series need no change.
 */
export interface SparklinePoint {
    time: string;
    value: number | null;
}

/** Metric series for charts */
export interface MetricSeries {
    name: string;
    metric: string;
    data: MetricDataPoint[];
    unit?: string;
}

/**
 * The window a metrics query actually covered, as reported by the
 * server. Charts anchor their x-axis to this rather than to the data,
 * so that changing the range changes the axis even on an instance whose
 * history is shorter than the range asked for.
 */
export interface MetricsWindow {
    /** Inclusive start of the window (RFC 3339). */
    start: string;
    /** Exclusive end of the window (RFC 3339). */
    end: string;
    /** Width of one bucket in seconds, as used by the server's SQL. */
    bucketSeconds: number;
}

/**
 * Envelope returned by GET /api/v1/metrics/query in time-series mode.
 * The latest-rows mode of the same endpoint (a request carrying `limit`
 * or `order_by`) still returns flat rows and is unaffected.
 */
export interface MetricsQueryResult {
    probe_name: string;
    connection_ids: number[];
    time_range: string;
    time_start: string;
    time_end: string;
    bucket_seconds: number;
    buckets: number;
    aggregation: string;
    series: MetricSeries[] | null;
}

/** Baseline data from get_metric_baselines */
export interface MetricBaseline {
    metric: string;
    mean: number;
    stddev: number;
    min: number;
    max: number;
    p50: number;
    p95: number;
    p99: number;
}

/** Sparkline props for embedding in tiles */
export interface SparklineProps {
    data: SparklinePoint[];
    color?: string;
    height?: number;
    showArea?: boolean;
}

/** Props shared by all dashboard level components */
export interface BaseDashboardProps {
    selection: Selection;
}

/** Section in a collapsible dashboard */
export interface DashboardSection {
    id: string;
    title: string;
    defaultExpanded?: boolean;
}

/** KPI tile data */
export interface KpiTileData {
    label: string;
    value: number | string;
    unit?: string;
    trend?: 'up' | 'down' | 'flat';
    trendValue?: string;
    /**
     * Optional supporting figure rendered as a small line beneath the
     * headline value, for a second reading that belongs with the same
     * metric (for example the available memory behind a usage
     * percentage). Unlike `trendValue` it carries no direction, colour
     * or icon, and it is folded into the tile's `aria-label`. Omit it
     * entirely when the underlying figure is unavailable, rather than
     * passing a placeholder.
     */
    secondaryText?: string;
    sparklineData?: SparklinePoint[];
    status?: 'good' | 'warning' | 'critical';
}

/** Leaderboard entry for database dashboard */
export interface LeaderboardEntry {
    name: string;
    schemaName?: string;
    value: number;
    unit?: string;
    secondaryValue?: number;
    secondaryUnit?: string;
}

/** Metric query parameters */
export interface MetricQueryParams {
    probeName: string;
    connectionId?: number;
    connectionIds?: number[];
    databaseName?: string;
    schemaName?: string;
    tableName?: string;
    indexName?: string;
    /**
     * Filesystem mount point, for probes such as pg_sys_disk_info that
     * record one row per mounted filesystem. Without it the query
     * aggregates across every mount, which describes no real volume.
     */
    mountPoint?: string;
    /**
     * pg_stat_statements query identifier. Carried as a string because
     * query identifiers are 64-bit values that JavaScript numbers cannot
     * represent exactly.
     */
    queryId?: string;
    timeRange: TimeRange;
    buckets?: number;
    aggregation?: 'avg' | 'sum' | 'min' | 'max' | 'last';
    metrics?: string[];
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

/**
 * Props for the shared Top Queries section.
 *
 * The section serves both the server dashboard, where it spans every
 * database on the connection and offers a database filter, and the
 * database dashboard, where `databaseName` pins it to the one database
 * the dashboard is already scoped to. When pinned, the filter control
 * and the Database column both disappear, because neither has anything
 * left to say.
 */
export interface TopQueriesSectionProps {
    connectionId: number;
    connectionName?: string;
    databaseName?: string;
}
