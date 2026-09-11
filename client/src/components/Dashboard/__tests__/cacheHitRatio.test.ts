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

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2024-01-01T00:0${idx}:00Z`,
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

    it('pairs buckets by index and makes idle buckets null', () => {
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [90, 0, 50]),
            series(BLKS_READ_PER_SEC, [10, 0, 50]),
        ]);

        expect(points.map(p => p.time)).toEqual([
            '2024-01-01T00:00:00Z',
            '2024-01-01T00:01:00Z',
            '2024-01-01T00:02:00Z',
        ]);
        expect(points[0].value).toBeCloseTo(90);
        expect(points[1].value).toBeNull();
        expect(points[2].value).toBeCloseTo(50);
    });

    it('truncates to the shorter series', () => {
        const points = buildCacheHitRatioPoints([
            series(BLKS_HIT_PER_SEC, [90, 95, 99]),
            series(BLKS_READ_PER_SEC, [10]),
        ]);

        expect(points).toHaveLength(1);
        expect(points[0].value).toBeCloseTo(90);
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
