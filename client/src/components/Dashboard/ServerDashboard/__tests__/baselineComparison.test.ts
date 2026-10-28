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
    BAND_LABEL_PREFIX,
    COLD_BAND_LABEL,
    COLD_VALUE_LABEL,
    ENGINE_ANOMALY_LABEL,
    OUTSIDE_LABEL,
    buildComparisonChart,
    buildComparisonOptions,
    describeComparisonIndex,
    escapeHtml,
    filterEngineAlerts,
    formatComparisonValue,
    friendlyMetricLabel,
    nearestSensitivity,
    parsePeriodType,
    periodTypeLabel,
    type AnomalyAlert,
    type ComparisonPoint,
    type ComparisonWindow,
} from '../baselineComparison';

const START = Date.parse('2026-10-28T00:00:00Z');
const MINUTE = 60_000;

/** A ten-bucket window of ten minutes, so each bucket is one minute. */
const WINDOW: ComparisonWindow = {
    startMs: START,
    endMs: START + 10 * MINUTE,
    buckets: 10,
};

const at = (minutes: number): string => new Date(START + minutes * MINUTE).toISOString();

const point = (minutes: number, overrides: Partial<ComparisonPoint> = {}): ComparisonPoint => ({
    time: at(minutes),
    value: 10,
    period_type: 'hourly',
    mean: 10,
    stddev: 2,
    cold: false,
    z_score: 0,
    outside: false,
    ...overrides,
});

const alert = (minutes: number, overrides: Partial<AnomalyAlert> = {}): AnomalyAlert => ({
    id: minutes,
    metric_name: 'pg_sys_cpu_usage_info.processor_time_percent',
    database_name: null,
    metric_value: 50,
    anomaly_score: 4.2,
    anomaly_details: JSON.stringify({
        z_score: 4.2,
        baseline_context: { period_type: 'daily' },
    }),
    triggered_at: at(minutes),
    ...overrides,
});

const COLOURS = {
    value: '#000001',
    band: '#000002',
    engine: '#000003',
    outside: '#000004',
    background: '#000005',
};

describe('friendlyMetricLabel', () => {
    it('returns the friendly label for a known metric', () => {
        expect(friendlyMetricLabel('pg_sys_cpu_usage_info.processor_time_percent')).toBe(
            'CPU usage (%)'
        );
    });

    it('falls back to the raw name for an unknown metric', () => {
        expect(friendlyMetricLabel('made_up.metric')).toBe('made_up.metric');
    });
});

describe('nearestSensitivity', () => {
    it.each([
        [3, 3],
        [2.4, 2],
        [3.6, 4],
        [10, 4],
        [0.5, 2],
        [2.5, 2],
    ])('rounds %s to %s', (input, expected) => {
        expect(nearestSensitivity(input)).toBe(expected);
    });

    it('falls back to 3 for a non-finite value', () => {
        expect(nearestSensitivity(Number.NaN)).toBe(3);
        expect(nearestSensitivity(undefined)).toBe(3);
    });
});

describe('parsePeriodType', () => {
    it('reads the period type from the baseline context', () => {
        expect(parsePeriodType(alert(0).anomaly_details)).toBe('daily');
    });

    it.each([
        ['malformed JSON', '{not json'],
        ['a missing context', '{"z_score": 3}'],
        ['a non-object context', '{"baseline_context": "hourly"}'],
        ['a non-string period type', '{"baseline_context": {"period_type": 3}}'],
        ['a JSON null', 'null'],
        ['an empty string', ''],
        ['null', null],
        ['undefined', undefined],
    ])('returns null for %s', (_label, details) => {
        expect(parsePeriodType(details)).toBeNull();
    });
});

describe('periodTypeLabel', () => {
    it.each([
        ['hourly', 'Hour of day'],
        ['daily', 'Day of week'],
        ['all', 'Overall'],
        ['weekly', 'weekly'],
    ])('labels %s as %s', (input, expected) => {
        expect(periodTypeLabel(input)).toBe(expected);
    });

    it('labels a missing period type as unknown', () => {
        expect(periodTypeLabel(null)).toBe('unknown');
    });
});

describe('escapeHtml', () => {
    it('escapes markup characters', () => {
        expect(escapeHtml('<b a="1">&\'</b>')).toBe(
            '&lt;b a=&quot;1&quot;&gt;&amp;&#39;&lt;/b&gt;'
        );
    });
});

describe('formatComparisonValue', () => {
    it('formats numbers to at most two decimal places', () => {
        expect(formatComparisonValue(3.14159)).toBe('3.14');
        expect(formatComparisonValue(1200)).toBe('1,200');
    });

    it('formats a missing value as a dash', () => {
        expect(formatComparisonValue(null)).toBe('--');
        expect(formatComparisonValue(Number.POSITIVE_INFINITY)).toBe('--');
    });
});

describe('filterEngineAlerts', () => {
    const metric = 'pg_stat_database.cache_hit_ratio';

    it('keeps alerts for the metric and database only', () => {
        const alerts = [
            alert(1, { metric_name: metric, database_name: 'app' }),
            alert(2, { metric_name: metric, database_name: 'other' }),
            alert(3, { metric_name: 'other.metric', database_name: 'app' }),
        ];
        expect(filterEngineAlerts(alerts, metric, 'app').map((a) => a.id)).toEqual([1]);
    });

    it('treats an absent database as a server-level alert', () => {
        const alerts = [
            alert(1, { metric_name: metric, database_name: undefined }),
            alert(2, { metric_name: metric, database_name: 'app' }),
        ];
        expect(filterEngineAlerts(alerts, metric, null).map((a) => a.id)).toEqual([1]);
    });
});

describe('buildComparisonChart', () => {
    it('builds warm and cold bands with gaps where there is no band', () => {
        const chart = buildComparisonChart(
            [
                point(0),
                point(1, { cold: true }),
                point(2, { mean: null, stddev: null, period_type: null }),
            ],
            [],
            2,
            WINDOW
        );

        expect(chart.categories).toEqual([at(0), at(1), at(2)]);
        expect(chart.lower).toEqual([6, 6, null]);
        expect(chart.upper).toEqual([14, 14, null]);
        expect(chart.warmLower).toEqual([6, null, null]);
        expect(chart.warmWidth).toEqual([8, null, null]);
        expect(chart.coldLower).toEqual([null, 6, null]);
        expect(chart.coldWidth).toEqual([null, 8, null]);
        expect(chart.hasCold).toBe(true);
        expect(chart.hasBand).toBe(true);
    });

    it('splits the value line so junctions with cold points are faded', () => {
        const chart = buildComparisonChart(
            [
                point(0, { value: 1 }),
                point(1, { value: 2 }),
                point(2, { value: 3, cold: true }),
                point(3, { value: 4 }),
            ],
            [],
            3,
            WINDOW
        );

        expect(chart.warmValues).toEqual([1, 2, null, 4]);
        expect(chart.coldValues).toEqual([null, 2, 3, 4]);
    });

    it('marks engine alerts and suppresses outside markers in the same bucket', () => {
        const chart = buildComparisonChart(
            [
                point(0, { value: 30, outside: true }),
                point(1.5, { value: 40, outside: true }),
                point(2, { value: 10 }),
            ],
            [alert(1.9, { metric_value: 41, anomaly_score: -4 })],
            3,
            WINDOW
        );

        expect(chart.engineMarkers[0]).toBeNull();
        expect(chart.engineMarkers[1]).toEqual({
            value: 41,
            zScore: -4,
            periodType: 'daily',
            triggeredAt: at(1.9),
        });
        // The alert's bucket (1) and the one before it (0) are covered.
        expect(chart.outsideMarkers).toEqual([null, null, null]);
        expect(chart.hasEngine).toBe(true);
        expect(chart.hasOutside).toBe(false);
    });

    it('covers the bucket before an alert, because triggered_at lags collection', () => {
        const chart = buildComparisonChart(
            [
                point(0, { value: 30, outside: true }),
                point(1, { value: 31, outside: true }),
                point(2, { value: 32, outside: true }),
                point(3, { value: 33, outside: true }),
            ],
            [alert(2.5)],
            3,
            WINDOW
        );

        expect(chart.outsideMarkers).toEqual([30, null, null, 33]);
        expect(chart.hasOutside).toBe(true);
    });

    it('marks no outside points where alerts may not have been loaded', () => {
        const points = [
            point(0, { value: 30, outside: true }),
            point(3, { value: 33, outside: true }),
            point(4, { value: 34, outside: true }),
            point(8, { value: 38, outside: true }),
        ];
        // Alerts are complete only from 4.5 minutes, so buckets up to
        // and including 4 may hold an alert that was not fetched.
        const chart = buildComparisonChart(points, [], 3, WINDOW, START + 4.5 * MINUTE);
        expect(chart.outsideMarkers).toEqual([null, null, null, 38]);

        const complete = buildComparisonChart(points, [], 3, WINDOW, null);
        expect(complete.outsideMarkers).toEqual([30, 33, 34, 38]);
    });

    it('compares times directly for incomplete alerts in an unusable window', () => {
        const chart = buildComparisonChart(
            [point(0, { outside: true }), point(4, { outside: true })],
            [],
            3,
            { startMs: START, endMs: START, buckets: 10 },
            START + MINUTE
        );
        expect(chart.outsideMarkers).toEqual([null, 10]);
    });

    it('places an alert in an empty bucket on the nearest point', () => {
        const chart = buildComparisonChart(
            [point(0), point(5, { value: 20 })],
            [alert(6.2, { metric_value: null })],
            3,
            WINDOW
        );

        expect(chart.engineMarkers[1]?.value).toBe(20);
        expect(chart.engineMarkers[0]).toBeNull();
    });

    it('keeps the strongest alert when two land on one point', () => {
        const chart = buildComparisonChart(
            [point(0)],
            [
                alert(0.1, { anomaly_score: 3.1 }),
                alert(0.2, { anomaly_score: -5 }),
                alert(0.3, { anomaly_score: null }),
            ],
            3,
            WINDOW
        );

        expect(chart.engineMarkers[0]?.zScore).toBe(-5);
    });

    it('ignores alerts with an unparseable time and charts nothing without points', () => {
        const chart = buildComparisonChart(
            [point(0)],
            [alert(0, { triggered_at: 'not a time' })],
            3,
            WINDOW
        );
        expect(chart.engineMarkers).toEqual([null]);

        const empty = buildComparisonChart([], [alert(0)], 3, WINDOW);
        expect(empty.categories).toEqual([]);
        expect(empty.hasEngine).toBe(false);
    });

    it('matches buckets by nearest time when the window is unusable', () => {
        const chart = buildComparisonChart(
            [point(0, { outside: true }), point(4, { outside: true }), point(8, { outside: true })],
            [alert(7.9)],
            3,
            { startMs: START, endMs: START, buckets: 10 }
        );

        // Matched to point 2, which also covers the point before it.
        expect(chart.engineMarkers[2]).not.toBeNull();
        expect(chart.outsideMarkers).toEqual([10, null, null]);
    });

    it('clamps a point outside the window into the edge buckets', () => {
        const chart = buildComparisonChart(
            [point(-5, { outside: true }), point(15, { outside: true })],
            [alert(-1), alert(12)],
            3,
            WINDOW
        );

        expect(chart.engineMarkers.every((m) => m !== null)).toBe(true);
        expect(chart.outsideMarkers).toEqual([null, null]);
    });
});

describe('describeComparisonIndex', () => {
    it('describes a point with a band, an alert and the cold state', () => {
        const chart = buildComparisonChart(
            [point(0, { cold: true, z_score: 1.234, value: 12 })],
            [alert(0)],
            3,
            WINDOW
        );

        const lines = describeComparisonIndex(chart, 0, 3);
        expect(lines).toEqual([
            'Value: 12',
            'Band (±3σ): 4 to 16',
            'Baseline: Hour of day (cold)',
            'z-score: 1.23',
            'Engine anomaly alert: value 50, z-score 4.2, baseline Day of week',
        ]);
    });

    it('describes a point with no band that is outside with no alert', () => {
        const chart = buildComparisonChart(
            [
                point(0, {
                    mean: null,
                    stddev: null,
                    period_type: null,
                    z_score: null,
                    outside: true,
                }),
            ],
            [],
            3,
            WINDOW
        );

        expect(describeComparisonIndex(chart, 0, 3)).toEqual([
            'Value: 10',
            'No baseline applies',
            'Outside the band with no engine alert',
        ]);
    });

    it('returns nothing for an index beyond the data', () => {
        const chart = buildComparisonChart([], [], 3, WINDOW);
        expect(describeComparisonIndex(chart, 4, 3)).toEqual([]);
    });
});

describe('buildComparisonOptions', () => {
    const fullChart = buildComparisonChart(
        [point(0, { outside: true }), point(1, { cold: true }), point(2)],
        [alert(2)],
        3,
        WINDOW
    );

    it('names every visible series in the legend', () => {
        const options = buildComparisonOptions(fullChart, 'CPU usage (%)', 3, COLOURS);
        expect(options.legend.data).toEqual([
            'CPU usage (%)',
            COLD_VALUE_LABEL,
            `${BAND_LABEL_PREFIX} (±3σ)`,
            COLD_BAND_LABEL,
            ENGINE_ANOMALY_LABEL,
            OUTSIDE_LABEL,
        ]);
        expect(options.series).toHaveLength(8);
    });

    it('omits legend entries for absent series', () => {
        const chart = buildComparisonChart([point(0)], [], 3, WINDOW);
        const options = buildComparisonOptions(chart, 'CPU', 2, COLOURS);
        expect(options.legend.data).toEqual(['CPU', `${BAND_LABEL_PREFIX} (±2σ)`]);
    });

    it('draws engine alerts solid and outside points hollow', () => {
        const options = buildComparisonOptions(fullChart, 'CPU', 3, COLOURS);
        const engine = options.series.find((s) => s.name === ENGINE_ANOMALY_LABEL);
        const outside = options.series.find((s) => s.name === OUTSIDE_LABEL);
        expect(engine?.symbol).toBe('triangle');
        expect(engine?.itemStyle).toEqual({ color: COLOURS.engine });
        // A plain circle filled with the background, because ECharts
        // fills an 'emptyCircle' with a fixed white whatever the theme.
        expect(outside?.symbol).toBe('circle');
        expect(outside?.itemStyle).toEqual({
            color: COLOURS.background,
            borderColor: COLOURS.outside,
            borderWidth: 2,
        });
    });

    it('formats an escaped tooltip for the hovered index', () => {
        const chart = buildComparisonChart(
            [point(0)],
            [
                alert(0, {
                    anomaly_details: JSON.stringify({
                        baseline_context: { period_type: '<img src=x>' },
                    }),
                }),
            ],
            3,
            WINDOW
        );
        const options = buildComparisonOptions(chart, 'CPU', 3, COLOURS);
        const html = options.tooltip.formatter([{ dataIndex: 0, axisValue: at(0) }]);
        expect(html).toContain('&lt;img src=x&gt;');
        expect(html).not.toContain('<img');
        expect(html).toContain('Value: 10');
        expect(options.tooltip.formatter([])).toBe('');
        expect(options.tooltip.formatter({ dataIndex: 0, axisValue: 'x' })).toContain(
            '<strong>x</strong>'
        );
    });
});
