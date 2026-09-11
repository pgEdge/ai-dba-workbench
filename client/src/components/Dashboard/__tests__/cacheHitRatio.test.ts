/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import {
    BLKS_HIT_PER_SEC,
    BLKS_READ_PER_SEC,
    CACHE_HIT_CAVEAT,
    CACHE_HIT_METRICS,
    buildCacheHitRatioPoints,
    cacheHitRatio,
    latestCacheHitRatio,
} from '../cacheHitRatio';
import type { MetricSeries } from '../types';

/** Bucket timestamp for the given minute on the shared grid. */
const at = (minute: number): string => `2024-01-01T00:0${minute}:00Z`;

/**
 * Build a MetricSeries for the given metric and values, starting at
 * the given bucket of the shared grid.
 */
const series = (
    metric: string,
    values: number[],
    startMinute = 0,
): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: at(startMinute + idx),
        value,
    })),
});

describe('cacheHitRatio', () => {
    it('requests the per-second derived metrics', () => {
        expect(CACHE_HIT_METRICS).toEqual([BLKS_HIT_PER_SEC, BLKS_READ_PER_SEC]);
        expect(BLKS_HIT_PER_SEC).toBe('blks_hit_per_sec');
        expect(BLKS_READ_PER_SEC).toBe('blks_read_per_sec');
    });

    it('states the page-cache caveat', () => {
        expect(CACHE_HIT_CAVEAT).toContain('shared_buffers');
        expect(CACHE_HIT_CAVEAT).toContain('OS page cache');
    });

    it('computes hit / (hit + read) as a percentage', () => {
        expect(cacheHitRatio(95, 5)).toBeCloseTo(95);
        expect(cacheHitRatio(1, 3)).toBeCloseTo(25);
    });

    it('reports a genuine 0% and 100% ratio', () => {
        expect(cacheHitRatio(0, 10)).toBe(0);
        expect(cacheHitRatio(10, 0)).toBe(100);
    });

    it('is null for an idle interval, never 0 or 100', () => {
        expect(cacheHitRatio(0, 0)).toBeNull();
    });

    it('is null when either input is missing or not finite', () => {
        expect(cacheHitRatio(null, 5)).toBeNull();
        expect(cacheHitRatio(5, undefined)).toBeNull();
        expect(cacheHitRatio(Number.NaN, 5)).toBeNull();
        expect(cacheHitRatio(5, Number.POSITIVE_INFINITY)).toBeNull();
    });

    it('is null when the hit rate is negative', () => {
        expect(cacheHitRatio(-1, 5)).toBeNull();
    });

    it('is null when the read rate is negative', () => {
        expect(cacheHitRatio(5, -1)).toBeNull();
        expect(cacheHitRatio(10, -5)).toBeNull();
    });
});

describe('buildCacheHitRatioPoints', () => {
    it('returns no points without data or without both series', () => {
        expect(buildCacheHitRatioPoints(null)).toEqual([]);
        expect(buildCacheHitRatioPoints([])).toEqual([]);
        expect(buildCacheHitRatioPoints([
            series(BLKS_READ_PER_SEC, [1, 2]),
        ])).toEqual([]);
        expect(buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [1, 2]),
        ])).toEqual([]);
    });

    it('joins buckets by time and makes idle buckets null', () => {
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [90, 0, 50]),
            series(BLKS_READ_PER_SEC, [10, 0, 50]),
        ]);

        expect(points.map(p => p.time)).toEqual([at(0), at(1), at(2)]);
        expect(points[0].value).toBeCloseTo(90);
        expect(points[1].value).toBeNull();
        expect(points[2].value).toBeCloseTo(50);
    });

    it('produces one point per bucket for identical grids', () => {
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [90, 95, 99]),
            series(BLKS_READ_PER_SEC, [10, 5, 1]),
        ]);

        expect(points).toEqual([
            { time: at(0), value: 90 },
            { time: at(1), value: 95 },
            { time: at(2), value: 99 },
        ]);
    });

    it('pairs by time when the series start at different buckets', () => {
        // The metrics layer drops leading empty buckets per metric, so
        // the read series may start later than the hit series.
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [100, 90, 50]),
            series(BLKS_READ_PER_SEC, [10, 50], 1),
        ]);

        expect(points.map(p => p.time)).toEqual([at(0), at(1), at(2)]);
        expect(points[0].value).toBeNull();
        expect(points[1].value).toBeCloseTo(90);
        expect(points[2].value).toBeCloseTo(50);
    });

    it('emits a null when a middle bucket is missing on one side', () => {
        const hit = series(BLKS_HIT_PER_SEC, [90, 80, 70]);
        const read: MetricSeries = {
            name: BLKS_READ_PER_SEC,
            metric: BLKS_READ_PER_SEC,
            data: [
                { time: at(0), value: 10 },
                { time: at(2), value: 30 },
            ],
        };

        const points = buildCacheHitRatioPoints([hit, read]);

        expect(points.map(p => p.time)).toEqual([at(0), at(1), at(2)]);
        expect(points[0].value).toBeCloseTo(90);
        expect(points[1].value).toBeNull();
        expect(points[2].value).toBeCloseTo(70);
    });

    it('covers the union of timestamps in chronological order', () => {
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [50], 3),
            series(BLKS_READ_PER_SEC, [10, 20], 0),
        ]);

        expect(points).toEqual([
            { time: at(0), value: null },
            { time: at(1), value: null },
            { time: at(3), value: null },
        ]);
    });
});

describe('latestCacheHitRatio', () => {
    it('returns the last bucket that has a ratio', () => {
        expect(latestCacheHitRatio([
            { time: 'a', value: 90 },
            { time: 'b', value: 80 },
            { time: 'c', value: null },
        ])).toBe(80);
    });

    it('does not skip a genuine zero', () => {
        expect(latestCacheHitRatio([
            { time: 'a', value: 90 },
            { time: 'b', value: 0 },
        ])).toBe(0);
    });

    it('is null when every bucket is idle or there are none', () => {
        expect(latestCacheHitRatio([])).toBeNull();
        expect(latestCacheHitRatio([
            { time: 'a', value: null },
            { time: 'b', value: null },
        ])).toBeNull();
    });
});
