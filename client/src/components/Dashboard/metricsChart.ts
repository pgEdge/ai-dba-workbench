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
 * Turning a metrics query response into chart data.
 *
 * The x-axis of a time-series chart is anchored to the window the
 * server reports having queried (`time_start`, `time_end` and
 * `bucket_seconds`, exposed by `useMetrics` as `MetricsWindow`) rather
 * than to the points that came back. An instance whose history is
 * shorter than the selected range then shows that history against the
 * whole range, so switching between 1h, 6h, 24h, 7d and 30d visibly
 * changes the axis instead of redrawing the same few hours each time.
 *
 * Each series is aligned onto those categories by bucket time, so a
 * bucket the server had no reading for is a `null` that ECharts draws
 * as a gap, distinct from a genuine zero. Buckets whose value is the
 * last observation carried forward rather than an observed sample are
 * flagged, and the chart option builders render those stretches
 * distinctly (see `client/src/components/Chart/options/common.ts`).
 */

import type { ChartData } from '../Chart/types';
import type { MetricDataPoint, MetricSeries, MetricsWindow } from './types';

/**
 * Upper bound on generated categories. A malformed or absurd window
 * (a bucket width of a second across thirty days, say) must not be
 * allowed to allocate an unbounded array; beyond this many buckets the
 * chart would be unreadable anyway.
 */
const MAX_CATEGORIES = 5000;

const MS_PER_SECOND = 1000;

/**
 * Build the chart categories for a window: one ISO timestamp per
 * bucket, from `start` inclusive to `end` exclusive. Returns an empty
 * array for an absent or unusable window, which leaves callers free to
 * fall back to the bucket times carried by the data.
 */
export const buildWindowCategories = (
    window: MetricsWindow | null | undefined,
): string[] => {
    if (!window) { return []; }

    const start = Date.parse(window.start);
    const end = Date.parse(window.end);
    const bucketMs = window.bucketSeconds * MS_PER_SECOND;

    if (Number.isNaN(start) || Number.isNaN(end)) { return []; }
    if (!Number.isFinite(bucketMs) || bucketMs <= 0) { return []; }
    if (end <= start) { return []; }

    const count = Math.min(
        Math.max(Math.round((end - start) / bucketMs), 1),
        MAX_CATEGORIES,
    );

    const categories: string[] = [];
    for (let i = 0; i < count; i++) {
        categories.push(new Date(start + i * bucketMs).toISOString());
    }
    return categories;
};

/** A series' values and carried-forward flags, aligned to an axis. */
export interface AlignedSeries {
    data: (number | null)[];
    filled: boolean[];
}

/**
 * Align a series' points onto window-derived categories.
 *
 * Each point is placed in the bucket whose start is nearest its
 * timestamp, which tolerates the server labelling a bucket by its start
 * whilst the generated axis steps from the window start; a point more
 * than half a bucket outside the window is dropped. Categories with no
 * point become `null`, drawn as a gap rather than as zero.
 */
export const alignPointsToWindow = (
    points: MetricDataPoint[] | undefined,
    categories: string[],
    window: MetricsWindow | null | undefined,
): AlignedSeries => {
    const data: (number | null)[] = new Array<number | null>(
        categories.length,
    ).fill(null);
    const filled: boolean[] = new Array<boolean>(categories.length).fill(false);

    if (!points || points.length === 0 || categories.length === 0) {
        return { data, filled };
    }

    const start = Date.parse(categories[0]);
    const bucketMs = (window?.bucketSeconds ?? 0) * MS_PER_SECOND;

    for (const point of points) {
        const time = Date.parse(point.time);
        if (Number.isNaN(time) || Number.isNaN(start)) { continue; }

        const index = bucketMs > 0
            ? Math.round((time - start) / bucketMs)
            : categories.indexOf(point.time);

        if (index < 0 || index >= categories.length) { continue; }

        data[index] = point.value;
        filled[index] = point.filled === true;
    }

    return { data, filled };
};

/**
 * Build chart data for the named metrics of a metrics query response.
 *
 * `window` is the window the server reported; when it is absent (an
 * error, or a response that predates the envelope) the categories fall
 * back to the bucket times of the first metric that returned data, the
 * behaviour this helper replaced. Returns null when none of the named
 * metrics produced any point, which callers render as an empty state.
 */
export const buildMetricChartData = (
    series: MetricSeries[] | null,
    metricNames: string[],
    displayNames?: string[],
    window?: MetricsWindow | null,
): ChartData | null => {
    if (!series) { return null; }

    const matched = metricNames.map(metric =>
        series.find(s => s.metric === metric),
    );

    if (matched.every(s => !s || s.data.length === 0)) { return null; }

    const windowCategories = buildWindowCategories(window);
    const categories = windowCategories.length > 0
        ? windowCategories
        : matched.find(s => s && s.data.length > 0)?.data.map(d => d.time) ?? [];

    const effectiveWindow = windowCategories.length > 0 ? window : null;

    return {
        categories,
        series: matched.map((found, idx) => {
            const aligned = alignPointsToWindow(
                found?.data, categories, effectiveWindow,
            );
            return {
                name: displayNames?.[idx] ?? metricNames[idx],
                data: aligned.data,
                filled: aligned.filled,
            };
        }),
    };
};

/**
 * Build chart data from points that were derived client-side (a cache
 * hit ratio computed per bucket, for instance) rather than read
 * straight from a named metric of the response. The points are aligned
 * to the same window-anchored axis as any other series.
 */
export const buildDerivedChartData = (
    points: MetricDataPoint[],
    name: string,
    window?: MetricsWindow | null,
): ChartData | null => {
    if (points.length === 0) { return null; }

    const windowCategories = buildWindowCategories(window);
    const categories = windowCategories.length > 0
        ? windowCategories
        : points.map(p => p.time);
    const aligned = alignPointsToWindow(
        points,
        categories,
        windowCategories.length > 0 ? window : null,
    );

    return {
        categories,
        series: [{ name, data: aligned.data, filled: aligned.filled }],
    };
};
