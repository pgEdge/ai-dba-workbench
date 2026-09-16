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
    extractSparklineData,
    extractLatestValue,
    buildChartData,
    formatTimestamp,
} from '../types';
import type { MetricDataPoint, MetricSeries } from '../../types';

/** Build a metric series in the shape useMetrics returns. */
const series = (
    metric: string,
    values: (number | null)[],
): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx): MetricDataPoint => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
});

describe('ObjectDashboard types helpers', () => {
    describe('extractSparklineData', () => {
        it('returns the points for the named metric', () => {
            const data = [series('a', [1, 2]), series('b', [3])];
            expect(extractSparklineData(data, 'b'))
                .toEqual([{ time: '2026-01-01T00:00:00Z', value: 3 }]);
        });

        it('returns an empty array for null data or an unknown metric', () => {
            expect(extractSparklineData(null, 'a')).toEqual([]);
            expect(extractSparklineData([series('a', [1])], 'z')).toEqual([]);
        });
    });

    describe('extractLatestValue', () => {
        it('returns the last point when it is non-zero', () => {
            expect(extractLatestValue([series('a', [1, 2, 3])], 'a')).toBe(3);
        });

        it('returns a trailing zero rather than an earlier value', () => {
            expect(extractLatestValue([series('a', [1, 7, 0])], 'a')).toBe(0);
            expect(extractLatestValue([series('a', [10, 0])], 'a')).toBe(0);
        });

        it('returns 0 when every point is zero', () => {
            expect(extractLatestValue([series('a', [0, 0])], 'a')).toBe(0);
        });

        it('returns null when the series has no points', () => {
            expect(extractLatestValue([series('a', [])], 'a')).toBeNull();
            expect(extractLatestValue(null, 'a')).toBeNull();
        });

        it('skips a trailing null bucket', () => {
            expect(extractLatestValue([series('a', [4, 6, null])], 'a'))
                .toBe(6);
        });

        it('skips a null in the middle and a leading null', () => {
            expect(extractLatestValue([series('a', [null, 4, null, 0])], 'a'))
                .toBe(0);
            expect(extractLatestValue([series('a', [null, 4, null])], 'a'))
                .toBe(4);
        });

        it('returns null when every point is null', () => {
            expect(extractLatestValue([series('a', [null, null])], 'a'))
                .toBeNull();
        });

        it('returns 0 when the only readings are zero amongst nulls', () => {
            expect(extractLatestValue([series('a', [null, 0, null])], 'a'))
                .toBe(0);
        });
    });

    describe('formatTimestamp', () => {
        it('formats a timestamp for display', () => {
            const formatted = formatTimestamp('2026-01-02T03:04:05Z');
            expect(formatted).not.toBe('Never');
            expect(formatted).toContain('2026');
        });

        it('reports a missing timestamp as Never', () => {
            expect(formatTimestamp(undefined)).toBe('Never');
            expect(formatTimestamp('')).toBe('Never');
        });

        it('reports an unparseable timestamp as an invalid date', () => {
            // toLocaleString returns 'Invalid Date' rather than
            // throwing, so the catch is a belt-and-braces fallback.
            expect(formatTimestamp('not a date')).toBe('Invalid Date');
        });
    });

    describe('buildChartData', () => {
        it('returns null for null input or when nothing matches', () => {
            expect(buildChartData(null, ['a'])).toBeNull();
            expect(buildChartData([series('a', [1])], ['z'])).toBeNull();
            expect(buildChartData([series('a', [])], ['a'])).toBeNull();
        });

        it('maps series to display names with shared categories', () => {
            const result = buildChartData(
                [series('a', [1, 2]), series('b', [3, 4])],
                ['a', 'b'],
                ['Alpha', 'Beta'],
            );
            expect(result).toEqual({
                categories: ['2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z'],
                series: [
                    {
                        name: 'Alpha',
                        data: [1, 2],
                        filled: [false, false],
                    },
                    {
                        name: 'Beta',
                        data: [3, 4],
                        filled: [false, false],
                    },
                ],
            });
        });

        it('falls back to the metric name when no display name is given', () => {
            const result = buildChartData([series('a', [1])], ['a']);
            expect(result?.series[0].name).toBe('a');
        });

        it('passes null values through untouched', () => {
            const result = buildChartData(
                [series('a', [null, 2, null]), series('b', [1, null, 3])],
                ['a', 'b'],
            );
            expect(result?.series[0].data).toEqual([null, 2, null]);
            expect(result?.series[1].data).toEqual([1, null, 3]);
            expect(result?.categories).toHaveLength(3);
        });

        it('keeps an all-null series and its bucket times', () => {
            const result = buildChartData(
                [series('a', [null, null])],
                ['a'],
            );
            expect(result?.series[0].data).toEqual([null, null]);
            expect(result?.categories).toEqual([
                '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z',
            ]);
        });

        it('takes categories from the first metric that was returned', () => {
            const result = buildChartData(
                [series('b', [5, 6])],
                ['a', 'b'],
            );
            // A metric the response did not carry is aligned onto the
            // same axis as the others, every bucket a gap, rather than
            // being left ragged and shorter than the categories.
            expect(result?.series[0].data).toEqual([null, null]);
            expect(result?.series[1].data).toEqual([5, 6]);
            expect(result?.categories).toEqual([
                '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z',
            ]);
        });
    });
});
