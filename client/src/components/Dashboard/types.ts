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

/** Time range options for metric queries */
export type TimeRange = '1h' | '6h' | '24h' | '7d' | '30d';

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

/** Metric data point from query_metrics API */
export interface MetricDataPoint {
    time: string;
    value: number;
}

/** Metric series for charts */
export interface MetricSeries {
    name: string;
    metric: string;
    data: MetricDataPoint[];
    unit?: string;
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
    data: MetricDataPoint[];
    color?: string;
    height?: number;
    showArea?: boolean;
}

/** Props shared by all dashboard level components */
export interface BaseDashboardProps {
    selection: Record<string, unknown>;
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
    sparklineData?: MetricDataPoint[];
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
    timeRange: TimeRange;
    buckets?: number;
    aggregation?: 'avg' | 'sum' | 'min' | 'max' | 'last';
    metrics?: string[];
}
