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
import type { MetricSeries } from '../../types';

/** Build a metric series in the shape useMetrics returns. */
const series = (metric: string, values: number[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
}) as MetricSeries;

describe('ObjectDashboard types helpers', () => {
    describe('extractSparklineData', () => {
        it('returns the points for the named metric', () => {
            const data = [series('a', [1, 2]), series('b', [3])];
            expect(extractSparklineData(data, 'b')).toEqual([
                { time: '2026-01-01T00:00:00Z', value: 3 },
            ]);
        });

        it('returns an empty array for null data or unknown metric', () => {
            expect(extractSparklineData(null, 'a')).toEqual([]);
            expect(extractSparklineData([series('a', [1])], 'z')).toEqual([]);
        });
    });

    describe('extractLatestValue', () => {
        it('returns the last point when it is non-zero', () => {
            expect(extractLatestValue([series('a', [1, 2, 3])], 'a')).toBe(3);
        });

        it('falls back to the last non-zero value', () => {
            expect(extractLatestValue([series('a', [1, 7, 0])], 'a')).toBe(7);
        });

        it('returns 0 when every point is zero', () => {
            expect(extractLatestValue([series('a', [0, 0])], 'a')).toBe(0);
        });

        it('returns null when the series has no points', () => {
            expect(extractLatestValue([series('a', [])], 'a')).toBeNull();
            expect(extractLatestValue(null, 'a')).toBeNull();
        });
    });

    describe('buildChartData', () => {
        it('maps matched series onto display names', () => {
            const result = buildChartData(
                [series('a', [1, 2])],
                ['a', 'b'],
                ['Alpha', 'Beta'],
            );
            expect(result).toEqual({
                categories: ['2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z'],
                series: [
                    { name: 'Alpha', data: [1, 2] },
                    { name: 'Beta', data: [] },
                ],
            });
        });

        it('falls back to the metric name without display names', () => {
            const result = buildChartData([series('a', [5])], ['a']);
            expect(result?.series[0].name).toBe('a');
        });

        it('returns null for null input or when no series matched', () => {
            expect(buildChartData(null, ['a'])).toBeNull();
            expect(buildChartData([series('a', [1])], ['z'])).toBeNull();
        });
    });

    describe('formatTimestamp', () => {
        it('returns Never for a missing timestamp', () => {
            expect(formatTimestamp(undefined)).toBe('Never');
            expect(formatTimestamp('')).toBe('Never');
        });

        it('formats a valid timestamp with date and time parts', () => {
            const formatted = formatTimestamp('2026-01-01T00:05:00Z');
            expect(formatted).toMatch(/2026/);
            expect(formatted).toMatch(/\d{2}:\d{2}:\d{2}/);
        });
    });
});
