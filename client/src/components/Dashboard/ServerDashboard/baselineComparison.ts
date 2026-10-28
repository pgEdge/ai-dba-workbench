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
 * Pure helpers for the Baseline comparison section, which charts a
 * baselined metric against the band the anomaly engine scores it
 * against (GitHub issue #348).
 *
 * The server's `/api/v1/metrics/anomaly-comparison` endpoint returns
 * one point per bucket, each carrying the sample with the largest
 * |z| in that bucket together with the baseline mean and effective
 * standard deviation that apply to it. These helpers turn that into
 * the band, the value line and two kinds of deviation marker: solid
 * markers for the anomaly alerts the engine actually raised, and
 * hollow markers for points outside the band that raised no alert.
 */

/** A baselined metric the comparison endpoint can chart. */
export interface ComparisonMetric {
    name: string;
    probe_name: string;
    /** True when the metric is reported per database. */
    per_database: boolean;
}

/** The baseline model the band is drawn from. */
export type ComparisonModel = 'as_scored' | 'daily' | 'hourly' | 'all';

/** One bucket of the comparison response. */
export interface ComparisonPoint {
    time: string;
    value: number | null;
    /** Null, like the three fields after it, when no baseline applies. */
    period_type: string | null;
    mean: number | null;
    /** The effective (floored) standard deviation. */
    stddev: number | null;
    /** True when the band shown is not warm enough to score with. */
    cold: boolean;
    z_score: number | null;
    /** True when the point is scored and outside the band. */
    outside: boolean;
}

/** The body of `GET /api/v1/metrics/anomaly-comparison`. */
export interface AnomalyComparisonResponse {
    metric: string;
    connection_id: number;
    database_name: string | null;
    model: ComparisonModel;
    sensitivity: number;
    default_sensitivity: number;
    /** 'alerter' when the alerter published its settings, else 'defaults'. */
    scoring_source: string;
    baselines_as_of: string | null;
    points: ComparisonPoint[] | null;
}

/** The fields of an `/api/v1/alerts` row this section reads. */
export interface AnomalyAlert {
    id: number;
    metric_name?: string | null;
    database_name?: string | null;
    metric_value?: number | null;
    anomaly_score?: number | null;
    /** A JSON string; the period type is at baseline_context.period_type. */
    anomaly_details?: string | null;
    triggered_at: string;
    /** 'active', 'acknowledged' or 'cleared'. */
    status?: string;
    /** When the alert cleared; absent (or null) whilst it is open. */
    cleared_at?: string | null;
}

/** The window the comparison was requested for, split into buckets. */
export interface ComparisonWindow {
    startMs: number;
    endMs: number;
    buckets: number;
}

/** An engine-raised anomaly alert placed on a chart point. */
export interface EngineMarker {
    value: number | null;
    zScore: number | null;
    periodType: string | null;
    triggeredAt: string;
}

/** Everything the chart and its tooltip need, aligned per point. */
export interface ComparisonChartModel {
    categories: string[];
    points: ComparisonPoint[];
    lower: (number | null)[];
    upper: (number | null)[];
    warmLower: (number | null)[];
    warmWidth: (number | null)[];
    coldLower: (number | null)[];
    coldWidth: (number | null)[];
    warmValues: (number | null)[];
    coldValues: (number | null)[];
    engineMarkers: (EngineMarker | null)[];
    outsideMarkers: (number | null)[];
    hasBand: boolean;
    hasCold: boolean;
    hasEngine: boolean;
    hasOutside: boolean;
}

// ---------------------------------------------------------------------------
// Labels and options
// ---------------------------------------------------------------------------

/**
 * Friendly names for the baselined metrics, which are those in the
 * anomaly registry (`BaselineMetrics()` in pkg/anomaly/metrics.go). A
 * metric missing from the map is shown by its raw name, so a metric
 * added to the registry still appears before it is given a label here.
 */
const METRIC_LABELS: Record<string, string> = {
    connection_utilization_percent: 'Connection utilisation (%)',
    'pg_stat_activity.count': 'Backends',
    'pg_stat_activity.blocked_count': 'Blocked backends',
    'pg_stat_activity.idle_in_transaction_seconds': 'Idle in transaction (s)',
    'pg_stat_activity.max_lock_wait_seconds': 'Longest lock wait (s)',
    'pg_stat_activity.max_query_duration_seconds': 'Longest running query (s)',
    'pg_stat_activity.max_xact_duration_seconds': 'Longest transaction (s)',
    'pg_stat_replication.lag_bytes': 'Replication lag (bytes)',
    'pg_replication_slots.max_retained_bytes': 'Largest slot retained WAL (bytes)',
    'spock_exception_log.recent_count': 'Spock exceptions',
    'spock_resolutions.recent_count': 'Spock conflict resolutions',
    'pg_stat_checkpointer.checkpoints_req_delta': 'Requested checkpoints',
    'pg_stat_database.cache_hit_ratio': 'Cache hit ratio (%)',
    'pg_stat_database.deadlocks_delta': 'Deadlocks',
    'pg_stat_database.temp_files_delta': 'Temporary files',
    'pg_stat_statements.slow_query_count': 'Slow queries',
    'pg_sys_cpu_usage_info.processor_time_percent': 'CPU usage (%)',
    'pg_sys_disk_info.used_percent': 'Disk usage (%)',
    'pg_sys_load_avg_info.load_avg_fifteen_minutes': 'Load average (15 min)',
    'pg_sys_memory_info.used_percent': 'Memory usage (%)',
};

/** The friendly label for a metric, or its raw name when it has none. */
export const friendlyMetricLabel = (name: string): string => METRIC_LABELS[name] ?? name;

/** The model selector's options, in display order. */
export const MODEL_OPTIONS: { value: ComparisonModel; label: string }[] = [
    { value: 'as_scored', label: 'As scored' },
    { value: 'daily', label: 'Day of week' },
    { value: 'hourly', label: 'Hour of day' },
    { value: 'all', label: 'Overall' },
];

/** The sensitivities offered, in standard deviations. */
export const SENSITIVITY_OPTIONS = [2, 3, 4];

/** Used when the server reports no usable default sensitivity. */
const FALLBACK_SENSITIVITY = 3;

/**
 * Round a sensitivity to the nearest offered value, preferring the
 * lower on a tie so that the band errs on the side of flagging more.
 */
export const nearestSensitivity = (value: number | undefined): number => {
    if (value === undefined || !Number.isFinite(value)) {
        return FALLBACK_SENSITIVITY;
    }
    return SENSITIVITY_OPTIONS.reduce((best, option) =>
        Math.abs(option - value) < Math.abs(best - value) ? option : best
    );
};

/** Display names for the baseline period types. */
const PERIOD_TYPE_LABELS: Record<string, string> = {
    hourly: 'Hour of day',
    daily: 'Day of week',
    all: 'Overall',
};

/** A period type's display name, or the raw value when unknown. */
export const periodTypeLabel = (periodType: string | null): string =>
    periodType === null ? 'unknown' : (PERIOD_TYPE_LABELS[periodType] ?? periodType);

/**
 * Read the baseline period type an anomaly alert was scored against
 * from its `anomaly_details` JSON. Older or malformed details yield
 * null rather than an error, since the marker is still worth showing.
 */
export const parsePeriodType = (details: string | null | undefined): string | null => {
    if (!details) {
        return null;
    }
    let parsed: unknown;
    try {
        parsed = JSON.parse(details);
    } catch {
        return null;
    }
    if (typeof parsed !== 'object' || parsed === null) {
        return null;
    }
    const context = (parsed as { baseline_context?: unknown }).baseline_context;
    if (typeof context !== 'object' || context === null) {
        return null;
    }
    const periodType = (context as { period_type?: unknown }).period_type;
    return typeof periodType === 'string' ? periodType : null;
};

/** Escape text for the HTML string an ECharts tooltip renders. */
export const escapeHtml = (text: string): string =>
    text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');

const NUMBER_FORMAT = new Intl.NumberFormat('en-GB', {
    maximumFractionDigits: 2,
});

/** Format a value for the tooltip, or a dash when there is none. */
export const formatComparisonValue = (value: number | null | undefined): string =>
    typeof value === 'number' && Number.isFinite(value) ? NUMBER_FORMAT.format(value) : '--';

/**
 * Keep the anomaly alerts for the charted metric and database. A
 * server-level metric's alerts carry no database name, which the
 * alerts endpoint omits from the JSON altogether.
 */
export const filterEngineAlerts = (
    alerts: AnomalyAlert[],
    metric: string,
    databaseName: string | null
): AnomalyAlert[] =>
    alerts.filter((a) => a.metric_name === metric && (a.database_name ?? null) === databaseName);

// ---------------------------------------------------------------------------
// Chart model
// ---------------------------------------------------------------------------

/** Index of the point nearest the given time; -1 when there are none. */
const nearestIndex = (times: number[], time: number): number => {
    let best = -1;
    let bestDistance = Infinity;
    times.forEach((t, i) => {
        const distance = Math.abs(t - time);
        if (distance < bestDistance) {
            best = i;
            bestDistance = distance;
        }
    });
    return best;
};

/**
 * Build a function that maps a time to its bucket in the window, or
 * null when the window cannot be divided into buckets, in which case
 * callers match on the nearest point instead.
 */
const bucketer = (window: ComparisonWindow): ((time: number) => number) | null => {
    const bucketMs = (window.endMs - window.startMs) / window.buckets;
    if (!Number.isFinite(bucketMs) || bucketMs <= 0) {
        return null;
    }
    const last = window.buckets - 1;
    return (time: number): number =>
        Math.min(Math.max(Math.floor((time - window.startMs) / bucketMs), 0), last);
};

/** The absolute z-score of an alert, treating a missing one as zero. */
const alertStrength = (marker: EngineMarker): number => Math.abs(marker.zScore ?? 0);

/**
 * Place each engine alert on a chart point: the point in the alert's
 * bucket, or the nearest point when that bucket has none. Returns the
 * markers per point, the buckets that hold an engine alert and the
 * indices of the points the markers were drawn on.
 *
 * An alert triggered outside a usable window gets no marker, since it
 * was not raised on any point shown. One still open from before the
 * window covers points through its span instead (see `alertSpans`).
 */
const placeEngineAlerts = (
    alerts: AnomalyAlert[],
    times: number[],
    pointBuckets: number[],
    bucketOf: ((time: number) => number) | null,
    points: ComparisonPoint[],
    window: ComparisonWindow
): { markers: (EngineMarker | null)[]; buckets: Set<number>; indices: Set<number> } => {
    const markers: (EngineMarker | null)[] = points.map(() => null);
    const buckets = new Set<number>();
    const indices = new Set<number>();
    const pointByBucket = new Map<number, number>();
    pointBuckets.forEach((bucket, i) => pointByBucket.set(bucket, i));

    for (const a of alerts) {
        const time = Date.parse(a.triggered_at);
        if (Number.isNaN(time) || points.length === 0) {
            continue;
        }
        if (bucketOf && (time < window.startMs || time > window.endMs)) {
            continue;
        }

        const bucket = bucketOf ? bucketOf(time) : nearestIndex(times, time);
        const index = pointByBucket.get(bucket) ?? nearestIndex(times, time);
        buckets.add(bucket);
        indices.add(index);

        const marker: EngineMarker = {
            value: a.metric_value ?? points[index].value,
            zScore: a.anomaly_score ?? null,
            periodType: parsePeriodType(a.anomaly_details),
            triggeredAt: a.triggered_at,
        };
        const existing = markers[index];
        if (!existing || alertStrength(marker) > alertStrength(existing)) {
            markers[index] = marker;
        }
    }

    return { markers, buckets, indices };
};

/**
 * The time each alert stops covering points: when it cleared, the end
 * of the window whilst it is still open, or its trigger time when it
 * is marked cleared with no usable clear time. Null for an alert whose
 * trigger time cannot be read.
 */
const alertSpan = (
    a: AnomalyAlert,
    window: ComparisonWindow
): { from: number; to: number } | null => {
    const from = Date.parse(a.triggered_at);
    if (Number.isNaN(from)) {
        return null;
    }
    const cleared = a.cleared_at ? Date.parse(a.cleared_at) : NaN;
    if (!Number.isNaN(cleared)) {
        return { from, to: cleared };
    }
    return { from, to: a.status === 'cleared' ? from : window.endMs };
};

/**
 * Build a predicate telling whether a point lies within an alert's
 * span, from the bucket before its trigger (as for its marker) to the
 * bucket it cleared in. Whilst an anomaly alert is open the engine
 * raises nothing new for the metric, so the outside points under it
 * are accounted for rather than unalerted.
 */
const alertSpans = (
    alerts: AnomalyAlert[],
    times: number[],
    pointBuckets: number[],
    bucketOf: ((time: number) => number) | null,
    window: ComparisonWindow
): ((i: number) => boolean) => {
    const spans = alerts
        .map((a) => alertSpan(a, window))
        .filter((span): span is { from: number; to: number } => span !== null)
        .filter((span) => span.to >= window.startMs || !bucketOf);
    if (bucketOf) {
        const ranges = spans.map((span) => ({
            from: bucketOf(span.from) - 1,
            to: bucketOf(span.to),
        }));
        return (i) => ranges.some((r) => pointBuckets[i] >= r.from && pointBuckets[i] <= r.to);
    }
    return (i) => spans.some((span) => times[i] >= span.from && times[i] <= span.to);
};

/**
 * Split the value line into a warm and a cold series. The cold series
 * also takes the warm neighbours of each cold point, so the segments
 * that join the two are drawn faded and the line stays continuous.
 */
const splitValues = (
    points: ComparisonPoint[]
): { warm: (number | null)[]; cold: (number | null)[] } => {
    const isCold = (i: number): boolean => points[i]?.cold === true;
    return {
        warm: points.map((p) => (p.cold ? null : p.value)),
        cold: points.map((p, i) => (isCold(i) || isCold(i - 1) || isCold(i + 1) ? p.value : null)),
    };
};

/**
 * Build the chart model for a comparison response and the engine's
 * alerts, which the caller has already filtered to the metric and
 * database. `sensitivity` is the k of the band mean ± k·σ, and should
 * be the one the server used, so that the band matches `outside`.
 *
 * An alert covers its own bucket and the one before it, because the
 * engine stamps `triggered_at` when it evaluates, which lags the
 * collection time of the value that tripped it. It also covers the
 * point its marker is drawn on and the point before that, and every
 * point up to the bucket it cleared in (or the window's end whilst it
 * is open), since the engine raises nothing new whilst it is open.
 * Alerts triggered before the window that are still open belong in
 * `alerts` too: they cover points but draw no marker.
 *
 * `alertsCompleteFromMs` is set when the alerts were capped: no alert
 * older than it was loaded, so no point at or before it is marked as
 * outside without an engine alert.
 */
export const buildComparisonChart = (
    points: ComparisonPoint[],
    alerts: AnomalyAlert[],
    sensitivity: number,
    window: ComparisonWindow,
    alertsCompleteFromMs: number | null = null
): ComparisonChartModel => {
    const times = points.map((p) => Date.parse(p.time));
    const bucketOf = bucketer(window);
    const pointBuckets = times.map((t, i) => (bucketOf ? bucketOf(t) : i));

    const lower: (number | null)[] = [];
    const upper: (number | null)[] = [];
    for (const p of points) {
        const hasBand = p.mean !== null && p.stddev !== null;
        lower.push(hasBand ? (p.mean as number) - sensitivity * (p.stddev as number) : null);
        upper.push(hasBand ? (p.mean as number) + sensitivity * (p.stddev as number) : null);
    }
    const width = (i: number): number | null => {
        const lo = lower[i];
        const hi = upper[i];
        return lo === null || hi === null ? null : hi - lo;
    };

    const { markers, buckets, indices } = placeEngineAlerts(
        alerts,
        times,
        pointBuckets,
        bucketOf,
        points,
        window
    );
    const inSpan = alertSpans(alerts, times, pointBuckets, bucketOf, window);
    const covered = (i: number): boolean =>
        buckets.has(pointBuckets[i]) ||
        buckets.has(pointBuckets[i] + 1) ||
        indices.has(i) ||
        indices.has(i + 1) ||
        inSpan(i);
    const unloaded = (i: number): boolean => {
        if (alertsCompleteFromMs === null) {
            return false;
        }
        return bucketOf
            ? pointBuckets[i] <= bucketOf(alertsCompleteFromMs)
            : times[i] <= alertsCompleteFromMs;
    };
    const outsideMarkers = points.map((p, i) =>
        p.outside && !covered(i) && !unloaded(i) ? p.value : null
    );
    const values = splitValues(points);

    return {
        categories: points.map((p) => p.time),
        points,
        lower,
        upper,
        warmLower: points.map((p, i) => (p.cold ? null : lower[i])),
        warmWidth: points.map((p, i) => (p.cold ? null : width(i))),
        coldLower: points.map((p, i) => (p.cold ? lower[i] : null)),
        coldWidth: points.map((p, i) => (p.cold ? width(i) : null)),
        warmValues: values.warm,
        coldValues: values.cold,
        engineMarkers: markers,
        outsideMarkers,
        hasBand: lower.some((v) => v !== null),
        hasCold: points.some((p) => p.cold),
        hasEngine: markers.some((m) => m !== null),
        hasOutside: outsideMarkers.some((v) => v !== null),
    };
};

/** The lines of the tooltip for one chart point, as plain text. */
export const describeComparisonIndex = (
    chart: ComparisonChartModel,
    index: number,
    sensitivity: number
): string[] => {
    const p = chart.points[index] as ComparisonPoint | undefined;
    if (!p) {
        return [];
    }

    const lines = [`Value: ${formatComparisonValue(p.value)}`];
    const lo = chart.lower[index];
    const hi = chart.upper[index];
    if (lo !== null && hi !== null) {
        lines.push(
            `Band (±${sensitivity}σ): ${formatComparisonValue(lo)} to ` + formatComparisonValue(hi)
        );
        lines.push(`Baseline: ${periodTypeLabel(p.period_type)}` + (p.cold ? ' (cold)' : ''));
    } else {
        lines.push('No baseline applies');
    }
    if (p.z_score !== null) {
        lines.push(`z-score: ${formatComparisonValue(p.z_score)}`);
    }

    const marker = chart.engineMarkers[index];
    if (marker) {
        lines.push(
            `Engine anomaly alert: value ${formatComparisonValue(marker.value)}` +
                `, z-score ${formatComparisonValue(marker.zScore)}` +
                `, baseline ${periodTypeLabel(marker.periodType)}`
        );
    }
    if (chart.outsideMarkers[index] !== null) {
        lines.push('Outside the band with no engine alert');
    }
    return lines;
};

// ---------------------------------------------------------------------------
// ECharts options
// ---------------------------------------------------------------------------

export const BAND_LABEL_PREFIX = 'Baseline band';
export const COLD_BAND_LABEL = 'Cold baseline band';
export const COLD_VALUE_LABEL = 'Value against a cold baseline';
export const ENGINE_ANOMALY_LABEL = 'Engine anomaly alert';
export const OUTSIDE_LABEL = 'Outside band, no engine alert';

/** Theme colours for the chart, resolved by the caller from the palette. */
export interface ComparisonColours {
    value: string;
    band: string;
    engine: string;
    outside: string;
    /** The chart's background, which fills the hollow markers. */
    background: string;
}

/** One ECharts series of the comparison chart. */
export interface ComparisonSeries {
    type: 'line';
    name: string;
    data: (number | null)[];
    symbol?: string;
    itemStyle?: Record<string, unknown>;
    [key: string]: unknown;
}

interface TooltipParam {
    dataIndex: number;
    axisValue: string;
}

/** The ECharts options the section merges over the line chart's own. */
export interface ComparisonOptions {
    legend: { data: string[] };
    tooltip: {
        trigger: 'axis';
        formatter: (params: TooltipParam | TooltipParam[]) => string;
    };
    series: ComparisonSeries[];
}

/** Opacity of the warm band, the cold band and the cold value line. */
const WARM_BAND_OPACITY = 0.25;
const COLD_BAND_OPACITY = 0.1;
const COLD_LINE_OPACITY = 0.4;

/** Tooltip header for a category: its local time, or the raw value. */
const formatTooltipTime = (axisValue: string): string => {
    const time = Date.parse(axisValue);
    return Number.isNaN(time) ? axisValue : new Date(time).toLocaleString('en-GB');
};

/**
 * One band, drawn as an invisible lower edge with the band's width
 * stacked on top of it and shaded. `stackStrategy: 'all'` stacks the
 * width onto a negative lower edge too, which the default same-sign
 * strategy would instead stack onto zero.
 */
const bandSeries = (
    stack: string,
    name: string,
    lowerData: (number | null)[],
    widthData: (number | null)[],
    colour: string,
    opacity: number
): ComparisonSeries[] => {
    const shared = {
        type: 'line' as const,
        stack,
        stackStrategy: 'all',
        symbol: 'none',
        lineStyle: { opacity: 0 },
        silent: true,
    };
    return [
        { ...shared, name: `${name} lower edge`, data: lowerData },
        {
            ...shared,
            name,
            data: widthData,
            areaStyle: { color: colour, opacity },
        },
    ];
};

/**
 * A series that draws only markers: no line, and every non-null point
 * given a symbol. ECharts draws nothing for a line series whose values
 * are separated by gaps unless the symbols are shown.
 */
const markerSeries = (
    name: string,
    data: (number | null)[],
    symbol: string,
    itemStyle: Record<string, unknown>
): ComparisonSeries => ({
    type: 'line',
    name,
    data,
    symbol,
    symbolSize: 11,
    showSymbol: true,
    showAllSymbol: true,
    lineStyle: { opacity: 0 },
    itemStyle,
    z: 5,
});

/**
 * Build the series, legend and tooltip for the comparison chart. The
 * series replace the line chart's own outright, because the band and
 * the markers need styling the shared builder does not offer.
 */
export const buildComparisonOptions = (
    chart: ComparisonChartModel,
    valueLabel: string,
    sensitivity: number,
    colours: ComparisonColours
): ComparisonOptions => {
    const bandLabel = `${BAND_LABEL_PREFIX} (±${sensitivity}σ)`;
    const lineBase = {
        type: 'line' as const,
        symbol: 'none',
        itemStyle: { color: colours.value },
        z: 3,
    };

    const series: ComparisonSeries[] = [
        ...bandSeries(
            'warm',
            bandLabel,
            chart.warmLower,
            chart.warmWidth,
            colours.band,
            WARM_BAND_OPACITY
        ),
        ...bandSeries(
            'cold',
            COLD_BAND_LABEL,
            chart.coldLower,
            chart.coldWidth,
            colours.band,
            COLD_BAND_OPACITY
        ),
        { ...lineBase, name: valueLabel, data: chart.warmValues },
        {
            ...lineBase,
            name: COLD_VALUE_LABEL,
            data: chart.coldValues,
            lineStyle: { opacity: COLD_LINE_OPACITY, type: 'dashed' },
        },
        markerSeries(
            ENGINE_ANOMALY_LABEL,
            chart.engineMarkers.map((m) => m?.value ?? null),
            'triangle',
            { color: colours.engine }
        ),
        markerSeries(
            // A circle filled with the background rather than ECharts'
            // 'emptyCircle', which a line series fills with a fixed white
            // and so shows as a white disc in the dark theme.
            OUTSIDE_LABEL,
            chart.outsideMarkers,
            'circle',
            {
                color: colours.background,
                borderColor: colours.outside,
                borderWidth: 2,
            }
        ),
    ];

    const legend = [
        valueLabel,
        ...(chart.hasCold ? [COLD_VALUE_LABEL] : []),
        ...(chart.warmWidth.some((v) => v !== null) ? [bandLabel] : []),
        ...(chart.coldWidth.some((v) => v !== null) ? [COLD_BAND_LABEL] : []),
        ...(chart.hasEngine ? [ENGINE_ANOMALY_LABEL] : []),
        ...(chart.hasOutside ? [OUTSIDE_LABEL] : []),
    ];

    const formatter = (params: TooltipParam | TooltipParam[]): string => {
        const list = Array.isArray(params) ? params : [params];
        if (list.length === 0) {
            return '';
        }
        const { dataIndex, axisValue } = list[0];
        const lines = describeComparisonIndex(chart, dataIndex, sensitivity);
        return (
            `<strong>${escapeHtml(formatTooltipTime(axisValue))}</strong>` +
            lines.map((line) => `<br/>${escapeHtml(line)}`).join('')
        );
    };

    return {
        legend: { data: legend },
        tooltip: { trigger: 'axis', formatter },
        series,
    };
};
