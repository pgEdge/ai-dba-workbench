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
import { normalizeChartData } from '../dataTransform';
import type { ChartData } from '../../types';

describe('normalizeChartData', () => {
    it('names an unnamed series and keeps the categories', () => {
        const result = normalizeChartData({
            categories: ['Jan'],
            series: [{ name: '', data: [1] }],
        });
        expect(result.categories).toEqual(['Jan']);
        expect(result.series[0].name).toBe('Unnamed');
    });

    it('substitutes empty arrays for missing series and data', () => {
        const result = normalizeChartData(
            { series: undefined } as unknown as ChartData,
        );
        expect(result.series).toEqual([]);

        const ragged = normalizeChartData({
            series: [{ name: 'a', data: undefined }],
        } as unknown as ChartData);
        expect(ragged.series[0].data).toEqual([]);
    });

    it('keeps the carried-forward flags, which the builders draw', () => {
        const result = normalizeChartData({
            categories: ['Jan', 'Feb'],
            series: [{ name: 'a', data: [1, 1], filled: [false, true] }],
        });
        expect(result.series[0].filled).toEqual([false, true]);
    });

    it('adds no flags to a series that has none', () => {
        const result = normalizeChartData({
            series: [{ name: 'a', data: [1] }],
        });
        expect(result.series[0]).not.toHaveProperty('filled');
    });
});
