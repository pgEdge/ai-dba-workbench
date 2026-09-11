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
 * Cache hit ratio helpers shared by the dashboard sections that derive
 * the ratio client-side from the `blks_hit_per_sec` and
 * `blks_read_per_sec` metrics of `/api/v1/metrics/query`.
 *
 * The ratio is computed per bucket from per-interval rates, never from
 * lifetime counters, so it reflects the current interval rather than a
 * since-`stats_reset` average. A bucket with no block access at all has
 * no ratio and is represented as null, which the charts draw as a gap;
 * it is never reported as 0% (a catastrophic miss rate) or 100%.
 */

import type { MetricSeries, SparklinePoint } from './types';

/** Metric names requested for the cache hit ratio KPI and chart. */
export const BLKS_HIT_PER_SEC = 'blks_hit_per_sec';
export const BLKS_READ_PER_SEC = 'blks_read_per_sec';
export const CACHE_HIT_METRICS = [BLKS_HIT_PER_SEC, BLKS_READ_PER_SEC];

/**
 * Caveat appended to cache hit ratio descriptions. `blks_hit` counts
 * shared_buffers hits only; a block read may still be served from the
 * operating system page cache at memory speed, so a lower ratio does
 * not by itself mean slow I/O.
 */
export const CACHE_HIT_CAVEAT =
    'blks_hit counts shared_buffers hits only; a block read may still be '
    + 'served from the OS page cache at memory speed, so a lower ratio '
    + 'does not by itself mean slow I/O.';

/**
 * Ratio of hits to total block accesses as a percentage, or null when
 * there were no block accesses (or either input is missing).
 */
export const cacheHitRatio = (
    hit: number | null | undefined,
    read: number | null | undefined,
): number | null => {
    if (typeof hit !== 'number' || typeof read !== 'number') { return null; }
    if (!Number.isFinite(hit) || !Number.isFinite(read)) { return null; }
    const total = hit + read;
    if (total <= 0) { return null; }
    return (hit / total) * 100;
};

/**
 * Build per-bucket cache hit ratio points from the hit and read rate
 * series of a metrics query. Buckets are paired by index and truncated
 * to the shorter series; an idle bucket yields a null value.
 */
export const buildCacheHitRatioPoints = (
    series: MetricSeries[] | null,
): SparklinePoint[] => {
    if (!series) { return []; }
    const hitSeries = series.find(s => s.metric === BLKS_HIT_PER_SEC);
    const readSeries = series.find(s => s.metric === BLKS_READ_PER_SEC);
    if (!hitSeries || !readSeries) { return []; }

    const len = Math.min(hitSeries.data.length, readSeries.data.length);
    const points: SparklinePoint[] = [];
    for (let i = 0; i < len; i++) {
        points.push({
            time: hitSeries.data[i].time,
            value: cacheHitRatio(
                hitSeries.data[i].value,
                readSeries.data[i].value,
            ),
        });
    }
    return points;
};

/**
 * The most recent bucket that has a ratio, or null when every bucket
 * was idle. Unlike `extractLatestValue` this does not skip zeros: a
 * genuine 0% ratio is a real value and must be reported.
 */
export const latestCacheHitRatio = (
    points: SparklinePoint[],
): number | null => {
    for (let i = points.length - 1; i >= 0; i--) {
        const value = points[i].value;
        if (value !== null) { return value; }
    }
    return null;
};
