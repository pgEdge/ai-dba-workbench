/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/* eslint-disable @typescript-eslint/no-explicit-any */

import { describe, it, expect } from 'vitest';
import { buildLineOptions } from '../options/line';
import { buildBarOptions } from '../options/bar';
import { buildPieOptions } from '../options/pie';
import { ChartData } from '../types';

const sampleData: ChartData = {
    categories: ['Jan', 'Feb', 'Mar'],
    series: [
        { name: 'Sales', data: [100, 200, 300] },
        { name: 'Profit', data: [50, 80, 120] },
    ],
};

describe('buildLineOptions', () => {
    it('returns object with tooltip, legend, grid, xAxis, yAxis, dataZoom, and series', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        expect(result).toHaveProperty('tooltip');
        expect(result).toHaveProperty('legend');
        expect(result).toHaveProperty('grid');
        expect(result).toHaveProperty('xAxis');
        expect(result).toHaveProperty('yAxis');
        expect(result).toHaveProperty('dataZoom');
        expect(result).toHaveProperty('series');
    });

    it('produces the correct number of series', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        expect(result.series).toHaveLength(2);
    });

    it('sets each series type to line', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.type).toBe('line');
        });
    });

    it('applies smooth when smooth option is true', () => {
        const result = buildLineOptions(sampleData, { smooth: true }) as any;
        result.series.forEach((s: any) => {
            expect(s.smooth).toBe(true);
        });
    });

    it('does not apply smooth by default', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.smooth).toBe(false);
        });
    });

    it('sets stack to total when stacked is true', () => {
        const result = buildLineOptions(sampleData, { stacked: true }) as any;
        result.series.forEach((s: any) => {
            expect(s.stack).toBe('total');
        });
    });

    it('does not set stack when stacked is not provided', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.stack).toBeUndefined();
        });
    });

    it('sets areaStyle when areaFill is true', () => {
        const result = buildLineOptions(sampleData, { areaFill: true }) as any;
        result.series.forEach((s: any) => {
            expect(s.areaStyle).toBeDefined();
        });
    });

    it('does not set areaStyle when areaFill is not provided', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.areaStyle).toBeUndefined();
        });
    });

    it('sets symbol to a visible marker when showMarkers is true', () => {
        const result = buildLineOptions(sampleData, {
            showMarkers: true,
        }) as any;
        result.series.forEach((s: any) => {
            expect(s.symbol).not.toBe('none');
        });
    });

    it('sets symbol to none when showMarkers is not provided', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.symbol).toBe('none');
        });
    });

    it('uses custom markerSymbol when provided with showMarkers', () => {
        const result = buildLineOptions(sampleData, {
            showMarkers: true,
            markerSymbol: 'diamond',
        }) as any;
        result.series.forEach((s: any) => {
            expect(s.symbol).toBe('diamond');
        });
    });

    it('passes category data to xAxis', () => {
        const result = buildLineOptions(sampleData, {}) as any;
        expect(result.xAxis.data).toEqual(['Jan', 'Feb', 'Mar']);
    });
});

describe('buildBarOptions', () => {
    it('returns object with the expected structure', () => {
        const result = buildBarOptions(sampleData, {}) as any;
        expect(result).toHaveProperty('tooltip');
        expect(result).toHaveProperty('legend');
        expect(result).toHaveProperty('grid');
        expect(result).toHaveProperty('xAxis');
        expect(result).toHaveProperty('yAxis');
        expect(result).toHaveProperty('dataZoom');
        expect(result).toHaveProperty('series');
    });

    it('sets each series type to bar', () => {
        const result = buildBarOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.type).toBe('bar');
        });
    });

    it('swaps axes when horizontal is true', () => {
        const result = buildBarOptions(sampleData, {
            horizontal: true,
        }) as any;
        expect(result.xAxis.type).toBe('value');
        expect(result.yAxis.type).toBe('category');
    });

    it('uses category xAxis and value yAxis by default', () => {
        const result = buildBarOptions(sampleData, {}) as any;
        expect(result.xAxis.type).toBe('category');
        expect(result.yAxis.type).toBe('value');
    });

    it('sets stack to total when stacked is true', () => {
        const result = buildBarOptions(sampleData, { stacked: true }) as any;
        result.series.forEach((s: any) => {
            expect(s.stack).toBe('total');
        });
    });

    it('does not set stack when stacked is not provided', () => {
        const result = buildBarOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.stack).toBeUndefined();
        });
    });

    it('sets barMaxWidth to 50 on each series', () => {
        const result = buildBarOptions(sampleData, {}) as any;
        result.series.forEach((s: any) => {
            expect(s.barMaxWidth).toBe(50);
        });
    });
});

describe('buildPieOptions', () => {
    it('returns object with tooltip, legend, and series', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result).toHaveProperty('tooltip');
        expect(result).toHaveProperty('legend');
        expect(result).toHaveProperty('series');
    });

    it('does not include xAxis or yAxis', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result).not.toHaveProperty('xAxis');
        expect(result).not.toHaveProperty('yAxis');
    });

    it('sets tooltip trigger to item', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result.tooltip.trigger).toBe('item');
    });

    it('sets series[0] type to pie', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result.series[0].type).toBe('pie');
    });

    it('transforms categories and first series data into pie data', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        const pieData = result.series[0].data;
        expect(pieData).toEqual([
            { name: 'Jan', value: 100 },
            { name: 'Feb', value: 200 },
            { name: 'Mar', value: 300 },
        ]);
    });

    it('uses donut radius when isDonut is true', () => {
        const result = buildPieOptions(sampleData, {
            isDonut: true,
        }) as any;
        expect(result.series[0].radius).toEqual(['40%', '70%']);
    });

    it('uses full pie radius when isDonut is not set', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result.series[0].radius).toBe('70%');
    });

    it('includes percentage in label formatter when showPercentage is true', () => {
        const result = buildPieOptions(sampleData, {
            showPercentage: true,
        }) as any;
        expect(result.series[0].label.formatter).toContain('{d}%');
    });

    it('does not include percentage in label formatter by default', () => {
        const result = buildPieOptions(sampleData, {}) as any;
        expect(result.series[0].label.formatter).not.toContain('{d}%');
    });
});
