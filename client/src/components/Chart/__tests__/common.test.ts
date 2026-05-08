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

/* eslint-disable @typescript-eslint/no-explicit-any */

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
