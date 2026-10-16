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
    alignPointsToWindow,
    buildDerivedChartData,
    buildMetricChartData,
    buildWindowCategories,
} from '../metricsChart';
import type {
    MetricDataPoint,
    MetricSeries,
    MetricsWindow,
} from '../types';

const HOUR_WINDOW: MetricsWindow = {
    start: '2026-09-16T09:00:00.000Z',
    end: '2026-09-16T10:00:00.000Z',
    bucketSeconds: 900,
};

const series = (
    metric: string,
    data: MetricDataPoint[],
): MetricSeries => ({ name: metric, metric, data });

describe('buildWindowCategories', () => {
    it('emits one category per bucket across the window', () => {
        expect(buildWindowCategories(HOUR_WINDOW)).toEqual([
            '2026-09-16T09:00:00.000Z',
            '2026-09-16T09:15:00.000Z',
            '2026-09-16T09:30:00.000Z',
            '2026-09-16T09:45:00.000Z',
        ]);
    });

    it('spans the whole range even when no data exists for it', () => {
        const day = buildWindowCategories({
            start: '2026-09-15T10:00:00.000Z',
            end: '2026-09-16T10:00:00.000Z',
            bucketSeconds: 3600,
        });
        expect(day).toHaveLength(24);
        expect(day[0]).toBe('2026-09-15T10:00:00.000Z');
        expect(day[23]).toBe('2026-09-16T09:00:00.000Z');
    });

    it('returns nothing for an absent or unusable window', () => {
        expect(buildWindowCategories(null)).toEqual([]);
        expect(buildWindowCategories(undefined)).toEqual([]);
        expect(buildWindowCategories({
            ...HOUR_WINDOW, start: 'not a date',
        })).toEqual([]);
        expect(buildWindowCategories({
            ...HOUR_WINDOW, end: 'not a date',
        })).toEqual([]);
        expect(buildWindowCategories({
            ...HOUR_WINDOW, bucketSeconds: 0,
        })).toEqual([]);
        expect(buildWindowCategories({
            ...HOUR_WINDOW,
            end: '2026-09-16T08:00:00.000Z',
        })).toEqual([]);
    });

    it('caps an absurd bucket count rather than allocating unbounded', () => {
        const categories = buildWindowCategories({
            start: '2026-08-16T10:00:00.000Z',
            end: '2026-09-16T10:00:00.000Z',
            bucketSeconds: 1,
        });
        expect(categories).toHaveLength(5000);
    });
});

describe('alignPointsToWindow', () => {
    const categories = buildWindowCategories(HOUR_WINDOW);

    it('places each point in its bucket and leaves the rest as gaps', () => {
        const aligned = alignPointsToWindow(
            [
                { time: '2026-09-16T09:00:00.000Z', value: 1 },
                { time: '2026-09-16T09:30:00.000Z', value: 3 },
            ],
            categories,
            HOUR_WINDOW,
        );
        expect(aligned.data).toEqual([1, null, 3, null]);
        expect(aligned.filled).toEqual([false, false, false, false]);
    });

    it('records which buckets were carried forward', () => {
        const aligned = alignPointsToWindow(
            [
                { time: '2026-09-16T09:00:00.000Z', value: 1 },
                {
                    time: '2026-09-16T09:15:00.000Z',
                    value: 1,
                    filled: true,
                },
                { time: '2026-09-16T09:30:00.000Z', value: null },
            ],
            categories,
            HOUR_WINDOW,
        );
        expect(aligned.data).toEqual([1, 1, null, null]);
        expect(aligned.filled).toEqual([false, true, false, false]);
    });

    it('rounds a point to the nearest bucket start', () => {
        const aligned = alignPointsToWindow(
            [{ time: '2026-09-16T09:16:40.000Z', value: 7 }],
            categories,
            HOUR_WINDOW,
        );
        expect(aligned.data).toEqual([null, 7, null, null]);
    });

    it('drops points outside the window and unparseable times', () => {
        const aligned = alignPointsToWindow(
            [
                { time: '2026-09-16T08:00:00.000Z', value: 5 },
                { time: '2026-09-16T11:00:00.000Z', value: 6 },
                { time: 'not a date', value: 7 },
            ],
            categories,
            HOUR_WINDOW,
        );
        expect(aligned.data).toEqual([null, null, null, null]);
    });

    it('matches on the exact bucket time when no window is given', () => {
        const aligned = alignPointsToWindow(
            [
                { time: '2026-09-16T09:30:00.000Z', value: 2 },
                { time: '2026-09-16T09:31:00.000Z', value: 9 },
            ],
            categories,
            null,
        );
        expect(aligned.data).toEqual([null, null, 2, null]);
    });

    it('returns all gaps for empty input', () => {
        expect(alignPointsToWindow([], categories, HOUR_WINDOW).data)
            .toEqual([null, null, null, null]);
        expect(alignPointsToWindow(undefined, categories, HOUR_WINDOW).data)
            .toEqual([null, null, null, null]);
        expect(alignPointsToWindow([
            { time: '2026-09-16T09:00:00.000Z', value: 1 },
        ], [], HOUR_WINDOW).data).toEqual([]);
    });
});

describe('buildMetricChartData', () => {
    it('anchors the axis to the window, not to the data', () => {
        const result = buildMetricChartData(
            [series('cpu', [
                { time: '2026-09-16T09:45:00.000Z', value: 12 },
            ])],
            ['cpu'],
            ['CPU'],
            HOUR_WINDOW,
        );
        expect(result?.categories).toHaveLength(4);
        expect(result?.series).toEqual([
            {
                name: 'CPU',
                data: [null, null, null, 12],
                filled: [false, false, false, false],
            },
        ]);
    });

    it('falls back to the bucket times when no window is reported', () => {
        const result = buildMetricChartData(
            [series('cpu', [
                { time: '2026-09-16T09:00:00.000Z', value: 1 },
                { time: '2026-09-16T09:15:00.000Z', value: 2 },
            ])],
            ['cpu'],
            undefined,
            null,
        );
        expect(result?.categories).toEqual([
            '2026-09-16T09:00:00.000Z',
            '2026-09-16T09:15:00.000Z',
        ]);
        expect(result?.series[0].name).toBe('cpu');
        expect(result?.series[0].data).toEqual([1, 2]);
    });

    it('carries the filled flags through to the chart series', () => {
        const result = buildMetricChartData(
            [series('cpu', [
                { time: '2026-09-16T09:00:00.000Z', value: 4 },
                {
                    time: '2026-09-16T09:15:00.000Z',
                    value: 4,
                    filled: true,
                },
            ])],
            ['cpu'],
            ['CPU'],
            HOUR_WINDOW,
        );
        expect(result?.series[0].filled).toEqual([false, true, false, false]);
    });

    it('returns null when nothing matched', () => {
        expect(buildMetricChartData(null, ['cpu'])).toBeNull();
        expect(buildMetricChartData([], ['cpu'], undefined, HOUR_WINDOW))
            .toBeNull();
        expect(buildMetricChartData(
            [series('cpu', [])], ['cpu'], undefined, HOUR_WINDOW,
        )).toBeNull();
    });
});

describe('buildDerivedChartData', () => {
    it('aligns client-derived points to the window', () => {
        const result = buildDerivedChartData(
            [
                { time: '2026-09-16T09:00:00.000Z', value: 99 },
                { time: '2026-09-16T09:15:00.000Z', value: null },
            ],
            'Cache Hit Ratio %',
            HOUR_WINDOW,
        );
        expect(result?.categories).toHaveLength(4);
        expect(result?.series).toEqual([
            {
                name: 'Cache Hit Ratio %',
                data: [99, null, null, null],
                filled: [false, false, false, false],
            },
        ]);
    });

    it('uses the point times when no window is reported', () => {
        const result = buildDerivedChartData(
            [{ time: '2026-09-16T09:00:00.000Z', value: 50 }],
            'Ratio',
        );
        expect(result?.categories).toEqual(['2026-09-16T09:00:00.000Z']);
        expect(result?.series[0].data).toEqual([50]);
    });

    it('returns null when there are no points', () => {
        expect(buildDerivedChartData([], 'Ratio', HOUR_WINDOW)).toBeNull();
    });
});
