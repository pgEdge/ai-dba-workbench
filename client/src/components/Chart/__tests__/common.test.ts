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
 * Unit coverage for the shared echarts option builders. These helpers
 * are pure functions, so the tests directly assert the structure of the
 * returned options. Particular attention is paid to the formatter
 * closures inside `buildXAxis`, `buildYAxis`, and `buildTooltip`,
 * because those branches encode all the date/time and numeric
 * abbreviation rules used across every chart in the dashboard.
 */

import { describe, it, expect } from 'vitest';
import {
    buildTooltip,
    buildLegend,
    buildGrid,
    buildXAxis,
    buildYAxis,
    buildDataZoom,
} from '../options/common';

const isoMinusHours = (h: number) =>
    new Date(Date.now() - h * 3600 * 1000).toISOString();
const isoMinusDays = (d: number) =>
    new Date(Date.now() - d * 86_400_000).toISOString();

describe('buildLegend', () => {
    it('passes show through and pins the legend to the bottom', () => {
        expect(buildLegend(true)).toEqual({ show: true, bottom: 0 });
        expect(buildLegend(false)).toEqual({ show: false, bottom: 0 });
    });
});

describe('buildGrid', () => {
    it('returns the canonical grid layout with containLabel set', () => {
        expect(buildGrid()).toEqual({
            left: '3%',
            right: '4%',
            bottom: '15%',
            top: '10%',
            containLabel: true,
        });
    });
});

describe('buildDataZoom', () => {
    it('returns a single slider entry with show toggled by enabled', () => {
        const enabled = buildDataZoom(true) as Record<string, unknown>[];
        expect(enabled).toHaveLength(1);
        expect(enabled[0]).toMatchObject({
            type: 'slider',
            show: true,
            start: 0,
            end: 100,
        });
        const disabled = buildDataZoom(false) as Record<string, unknown>[];
        expect(disabled[0].show).toBe(false);
    });
});

describe('buildXAxis formatter', () => {
    interface AxisOpts {
        axisLabel: { formatter: (value: string) => string };
        data: string[];
    }

    it('uses HH:mm formatter for spans under one day', () => {
        const cats = [isoMinusHours(2), isoMinusHours(1)];
        const xAxis = buildXAxis(cats) as unknown as AxisOpts;
        const formatted = xAxis.axisLabel.formatter('2026-04-20T14:05:00Z');
        // Result is HH:mm with two digits each.
        expect(formatted).toMatch(/^\d{2}:\d{2}$/);
    });

    it('uses "MMM d HH:mm" formatter for spans within a week', () => {
        const cats = [isoMinusDays(3), isoMinusDays(0)];
        const xAxis = buildXAxis(cats) as unknown as AxisOpts;
        const formatted = xAxis.axisLabel.formatter(
            '2026-01-05T14:05:00Z',
        );
        expect(formatted).toMatch(
            /^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d+ \d{2}:\d{2}$/,
        );
    });

    it('uses "MMM d" formatter for spans longer than a week', () => {
        const cats = [isoMinusDays(30), isoMinusDays(0)];
        const xAxis = buildXAxis(cats) as unknown as AxisOpts;
        const formatted = xAxis.axisLabel.formatter('2026-01-05T00:00:00Z');
        expect(formatted).toMatch(/^[A-Z][a-z]{2} \d+$/);
    });

    it('returns the raw value when the timestamp is unparseable', () => {
        const xAxis = buildXAxis(['not-a-date', 'also-not-a-date']) as unknown as AxisOpts;
        expect(xAxis.axisLabel.formatter('still-bad')).toBe('still-bad');
    });

    it('handles a single-category list without crashing', () => {
        const xAxis = buildXAxis(['2026-01-01T00:00:00Z']) as unknown as AxisOpts;
        // Span is 0 -> falls into the "under a day" branch (HH:mm).
        const formatted = xAxis.axisLabel.formatter('2026-01-01T00:00:00Z');
        expect(formatted).toMatch(/^\d{2}:\d{2}$/);
    });

    it('falls back to an empty category list when categories is omitted', () => {
        const xAxis = buildXAxis() as unknown as AxisOpts;
        expect(xAxis.data).toEqual([]);
    });

    it('treats a NaN-bookended span as zero (HH:mm)', () => {
        const xAxis = buildXAxis(['bad', 'also-bad']) as unknown as AxisOpts;
        // spanMs stays at 0; valid date renders in HH:mm.
        expect(
            xAxis.axisLabel.formatter('2026-04-20T03:07:00Z'),
        ).toMatch(/^\d{2}:\d{2}$/);
    });
});

describe('buildYAxis formatter', () => {
    interface YAxisOpts {
        axisLabel: { formatter: (value: number) => string };
        min?: number;
        max?: number;
    }

    it('formats billions with B suffix', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis.axisLabel.formatter(2_500_000_000)).toBe('2.5B');
        expect(yAxis.axisLabel.formatter(-3_000_000_000)).toBe('-3.0B');
    });

    it('formats millions with M suffix', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis.axisLabel.formatter(1_500_000)).toBe('1.5M');
    });

    it('formats thousands with K suffix', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis.axisLabel.formatter(2_500)).toBe('2.5K');
    });

    it('returns plain integers for small whole numbers', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis.axisLabel.formatter(42)).toBe('42');
        expect(yAxis.axisLabel.formatter(0)).toBe('0');
    });

    it('rounds non-integer small numbers to 1 decimal', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis.axisLabel.formatter(3.14159)).toBe('3.1');
    });
});

describe('buildYAxis range padding', () => {
    interface YAxisOpts {
        type: string;
        axisLabel: { formatter: (value: number) => string };
        min?: number;
        max?: number;
    }

    it('omits min/max when no series data is supplied', () => {
        const yAxis = buildYAxis() as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('omits min/max when every point is non-finite', () => {
        const yAxis = buildYAxis([
            [NaN, Infinity, -Infinity],
        ]) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('leaves auto-scaling intact for a varied series', () => {
        const yAxis = buildYAxis([
            [10, 20, 30],
            [15, 25],
        ]) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('pads a proportional range around a flat non-zero series', () => {
        // Regression: a brand-new read-only database reports a
        // cache-hit-ratio that is a flat 100.0 for its whole history.
        const yAxis = buildYAxis([
            [100, 100, 100, 100],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(90);
        expect(yAxis.max).toBeCloseTo(110);
        expect(Number(yAxis.max) - Number(yAxis.min)).toBeGreaterThan(0);
    });

    it('pads a fixed range around a flat zero series', () => {
        const yAxis = buildYAxis([
            [0, 0, 0],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBe(-1);
        expect(yAxis.max).toBe(1);
    });

    it('pads proportionally for a flat large-scale series', () => {
        const yAxis = buildYAxis([
            [1e9, 1e9],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(9e8);
        expect(yAxis.max).toBeCloseTo(1.1e9);
    });

    it('pads around a flat negative series without collapsing', () => {
        const yAxis = buildYAxis([
            [-50, -50],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(-55);
        expect(yAxis.max).toBeCloseTo(-45);
    });

    it('ignores non-finite points when computing the range', () => {
        const yAxis = buildYAxis([
            [NaN, 100, Infinity, 100],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(90);
        expect(yAxis.max).toBeCloseTo(110);
    });
});

describe('buildYAxis stacked range padding', () => {
    interface YAxisOpts {
        type: string;
        axisLabel: { formatter: (value: number) => string };
        min?: number;
        max?: number;
    }

    it('pads around the cumulative total for flat stacked series', () => {
        // Regression: a stacked checkpoint-write breakdown holds two
        // series each flat at 100. The rendered stacked total is a
        // flat 200, so the padded range must bound ~200 rather than
        // the raw per-point value of 100 (which clipped the top off).
        const yAxis = buildYAxis(
            [
                [100, 100, 100],
                [100, 100, 100],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(180);
        expect(yAxis.max).toBeCloseTo(220);
    });

    it('uses raw per-point values when stacking is disabled', () => {
        // The same data unstacked stays degenerate at 100, proving the
        // stacking flag is what shifts the range to the cumulative sum.
        const yAxis = buildYAxis([
            [100, 100, 100],
            [100, 100, 100],
        ]) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(90);
        expect(yAxis.max).toBeCloseTo(110);
    });

    it('behaves identically to non-stacked for a single series', () => {
        const stacked = buildYAxis(
            [[100, 100, 100]],
            true,
        ) as unknown as YAxisOpts;
        const flat = buildYAxis([
            [100, 100, 100],
        ]) as unknown as YAxisOpts;
        expect(stacked.min).toBe(flat.min);
        expect(stacked.max).toBe(flat.max);
        expect(stacked.min).toBeCloseTo(90);
        expect(stacked.max).toBeCloseTo(110);
    });

    it('leaves auto-scaling intact when stacked totals vary', () => {
        // Per-point sums are 100 then 200, a non-degenerate range, so
        // no synthetic min/max is emitted even though each raw series
        // is individually flat.
        const yAxis = buildYAxis(
            [
                [100, 100],
                [0, 100],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('sums only available indices for ragged stacked series', () => {
        // The second series is shorter; the trailing index sums just
        // the first series. Totals are 30, 30, 30 -> flat at 30.
        const yAxis = buildYAxis(
            [
                [10, 10, 30],
                [20, 20],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(27);
        expect(yAxis.max).toBeCloseTo(33);
    });

    it('skips stacked points where every series is non-finite', () => {
        // Index 1 is non-finite across both series and is ignored; the
        // remaining totals are a flat 50, so the range pads around 50.
        const yAxis = buildYAxis(
            [
                [30, NaN, 30],
                [20, Infinity, 20],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(45);
        expect(yAxis.max).toBeCloseTo(55);
    });

    it('omits min/max when all stacked points are non-finite', () => {
        const yAxis = buildYAxis(
            [
                [NaN, Infinity],
                [-Infinity, NaN],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('preserves the real span for a flat mixed-sign stacked chart', () => {
        // Regression: ECharts stacks a positive series upward from zero
        // and a negative series downward from the same baseline, so a
        // series flat at +100 and another flat at -100 render a real
        // ~200-unit span from -100 to +100. Summing them into one
        // scalar netted the totals to a flat 0 and collapsed the axis to
        // a tiny [-1, 1] window; the positive and negative totals must
        // be tracked separately so the true span survives. The range is
        // non-degenerate (-100 != 100), so no synthetic min/max is
        // emitted and ECharts auto-scales across the full span.
        const yAxis = buildYAxis(
            [
                [100, 100],
                [-100, -100],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('leaves auto-scaling intact for varying mixed-sign stacks', () => {
        // Positive totals climb 50 -> 80 while the negative total holds
        // at -30. The observed range spans -30 to 80, which is already
        // non-degenerate, so no synthetic padding is applied.
        const yAxis = buildYAxis(
            [
                [50, 80],
                [-30, -30],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis).not.toHaveProperty('min');
        expect(yAxis).not.toHaveProperty('max');
    });

    it('pads around the negative total for an all-negative stack', () => {
        // No positive value appears, so the zero baseline is never
        // observed; the flat negative total of -50 pads like the
        // non-stacked flat-negative case rather than snapping to zero.
        const yAxis = buildYAxis(
            [
                [-20, -20],
                [-30, -30],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis.min).toBeCloseTo(-55);
        expect(yAxis.max).toBeCloseTo(-45);
    });

    it('pads a fixed range around an all-zero stacked series', () => {
        // Neither sign is present, so the flat zero baseline is observed
        // and padded to the same fixed [-1, 1] window as the
        // non-stacked flat-zero case.
        const yAxis = buildYAxis(
            [
                [0, 0, 0],
            ],
            true,
        ) as unknown as YAxisOpts;
        expect(yAxis.min).toBe(-1);
        expect(yAxis.max).toBe(1);
    });
});

describe('buildTooltip formatter', () => {
    interface TooltipOpts {
        show: boolean;
        formatter: (params: unknown) => string;
    }

    it('honours the show flag', () => {
        expect((buildTooltip(true) as TooltipOpts).show).toBe(true);
        expect((buildTooltip(false) as TooltipOpts).show).toBe(false);
    });

    it('renders a header with the parsed date and one line per series', () => {
        const tooltip = buildTooltip(true) as TooltipOpts;
        const html = tooltip.formatter([
            {
                axisValue: '2026-04-20T14:05:06Z',
                marker: '<span></span>',
                seriesName: 'CPU',
                value: 42,
            },
            {
                axisValue: '2026-04-20T14:05:06Z',
                marker: '<span></span>',
                seriesName: 'IO',
                value: 1500,
            },
        ]);
        expect(html).toContain('<strong>');
        expect(html).toContain('CPU: 42');
        expect(html).toContain('IO: 1.5K');
    });

    it('falls back to the raw axisValue when the date is unparseable', () => {
        const tooltip = buildTooltip(true) as TooltipOpts;
        const html = tooltip.formatter([
            {
                axisValue: 'badge',
                marker: '*',
                seriesName: 'X',
                value: 1,
            },
        ]);
        expect(html).toContain('<strong>badge</strong>');
    });

    it('returns empty string for an empty params array', () => {
        const tooltip = buildTooltip(true) as TooltipOpts;
        expect(tooltip.formatter([])).toBe('');
    });

    it('accepts a single param object (not an array)', () => {
        const tooltip = buildTooltip(true) as TooltipOpts;
        const html = tooltip.formatter({
            axisValue: '2026-04-20T14:05:06Z',
            marker: '*',
            seriesName: 'CPU',
            value: 5,
        });
        expect(html).toContain('CPU: 5');
    });

    it('coerces non-numeric values to strings', () => {
        const tooltip = buildTooltip(true) as TooltipOpts;
        const html = tooltip.formatter([
            {
                axisValue: '2026-04-20T14:05:06Z',
                marker: '*',
                seriesName: 'Status',
                value: 'online' as unknown as number,
            },
        ]);
        expect(html).toContain('Status: online');
    });
});
