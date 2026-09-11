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
    hasNonZeroData,
    extractLatestValue,
    extractLatestRate,
} from '../types';
import type { MetricDataPoint } from '../../types';

/** Build a metric series in the shape useMetrics returns. */
const series = (metric: string, values: number[]) => ({
    name: metric,
    metric,
    data: values.map((value, idx): MetricDataPoint => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
});

describe('ServerDashboard types helpers', () => {
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

    describe('hasNonZeroData', () => {
        it('is true when any point is non-zero', () => {
            expect(hasNonZeroData([series('a', [0, 0, 4])], 'a')).toBe(true);
        });

        it('is false when every point is zero or absent', () => {
            expect(hasNonZeroData([series('a', [0, 0])], 'a')).toBe(false);
            expect(hasNonZeroData(null, 'a')).toBe(false);
        });
    });

    describe('extractLatestValue', () => {
        it('returns the last point when it is non-zero', () => {
            expect(extractLatestValue([series('a', [1, 2, 3])], 'a')).toBe(3);
        });

        it('falls back to the last non-zero value for a gauge', () => {
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

    describe('extractLatestRate', () => {
        it('returns the final point even when it is zero', () => {
            expect(extractLatestRate([series('a', [5, 9, 0])], 'a')).toBe(0);
        });

        it('returns the final non-zero point unchanged', () => {
            expect(extractLatestRate([series('a', [5, 9])], 'a')).toBe(9);
        });

        it('returns null when the series has no points', () => {
            expect(extractLatestRate([series('a', [])], 'a')).toBeNull();
            expect(extractLatestRate(null, 'a')).toBeNull();
        });
    });
});
