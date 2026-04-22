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
 * TypeScript interfaces for the PerformanceTiles feature
 */

export interface XidAgeEntry {
    database_name: string;
    age: number;
    percent: number;
}

export interface CacheHitTimeSeries {
    time: string;
    value: number;
}

export interface CacheHitRatio {
    current: number;
    time_series: CacheHitTimeSeries[];
}

export interface TransactionTimeSeries {
    time: string;
    commits_per_sec: number;
    rollback_percent: number;
}

export interface TransactionData {
    commits_per_sec: number;
    rollback_percent: number;
    time_series: TransactionTimeSeries[];
}

export interface CheckpointTimeSeries {
    time: string;
    write_time_ms: number;
    sync_time_ms: number;
}

export interface CheckpointData {
    time_series: CheckpointTimeSeries[];
}

export interface ConnectionPerformance {
    connection_id: number;
    connection_name: string;
    xid_age: XidAgeEntry[];
    cache_hit_ratio: CacheHitRatio;
    transactions: TransactionData;
    checkpoints: CheckpointData;
}

export interface AggregateData {
    cache_hit_ratio: number;
    commits_per_sec: number;
    rollback_percent: number;
}

export interface PerformanceSummaryData {
    time_range: string;
    connections: ConnectionPerformance[];
    aggregate?: AggregateData;
}

export interface PerformanceTilesProps {
    selection: Record<string, unknown>;
}

/**
 * Per-database cache hit ratio data from the database-summaries endpoint.
 * Used by CacheHitTile to show per-database series in single-server view.
 */
export interface DatabaseCacheHitData {
    database_name: string;
    cache_hit_ratio: CacheHitRatio;
}

/**
 * Response structure from /api/v1/metrics/database-summaries.
 * We only use the fields relevant to cache hit ratio.
 */
export interface DatabaseSummariesResponse {
    databases: Array<{
        database_name: string;
        cache_hit_ratio: CacheHitRatio;
    }>;
}
