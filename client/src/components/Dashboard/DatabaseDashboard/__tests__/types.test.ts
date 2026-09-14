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
    formatTimestamp,
    getVacuumStatus,
    extractSparklineData,
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

describe('DatabaseDashboard types helpers', () => {
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

    describe('formatTimestamp', () => {
        it('formats a timestamp for display', () => {
            const formatted = formatTimestamp('2026-01-02T03:04:05Z');
            expect(formatted).not.toBe('Never');
            expect(formatted).toContain('2026');
        });

        it('reports a missing timestamp as Never', () => {
            expect(formatTimestamp(undefined)).toBe('Never');
        });

        it('reports an unparseable timestamp as an invalid date', () => {
            // toLocaleString returns 'Invalid Date' rather than
            // throwing, so the catch is a belt-and-braces fallback.
            expect(formatTimestamp('not a date')).toBe('Invalid Date');
        });
    });

    describe('getVacuumStatus', () => {
        it('is good within a day, warning within a week', () => {
            const hoursAgo = new Date(Date.now() - 3600_000).toISOString();
            const daysAgo = new Date(
                Date.now() - 3 * 86_400_000
            ).toISOString();
            expect(getVacuumStatus(hoursAgo)).toBe('good');
            expect(getVacuumStatus(daysAgo)).toBe('warning');
        });

        it('is critical when old, missing or unparseable', () => {
            const longAgo = new Date(
                Date.now() - 30 * 86_400_000
            ).toISOString();
            expect(getVacuumStatus(longAgo)).toBe('critical');
            expect(getVacuumStatus(undefined)).toBe('critical');
            expect(getVacuumStatus('not a date')).toBe('critical');
        });
    });
});
