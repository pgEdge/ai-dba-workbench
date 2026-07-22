/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

const MONTHS_SHORT = [
    'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
];

const MS_PER_DAY = 86_400_000;

function pad2(n: number): string {
    return n < 10 ? `0${n}` : String(n);
}

/**
 * Formats a Date as a short time string "HH:mm".
 */
function formatTime(d: Date): string {
    return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

/**
 * Formats a Date as "MMM d HH:mm" (e.g. "Jan 5 14:30").
 */
function formatDateTimeShort(d: Date): string {
    return MONTHS_SHORT[d.getMonth()] + ' ' + d.getDate()
        + ' ' + formatTime(d);
}

/**
 * Formats a Date as "MMM d" (e.g. "Jan 5").
 */
function formatDateOnly(d: Date): string {
    return `${MONTHS_SHORT[d.getMonth()]} ${d.getDate()}`;
}

/**
 * Formats a Date as a full readable string for tooltips,
 * e.g. "Jan 5, 2025 14:30:05".
 */
function formatDateTimeFull(d: Date): string {
    return MONTHS_SHORT[d.getMonth()] + ' ' + d.getDate()
        + ', ' + d.getFullYear()
        + ' ' + pad2(d.getHours())
        + ':' + pad2(d.getMinutes())
        + ':' + pad2(d.getSeconds());
}

/**
 * Detects the time span of the categories array and returns an
 * appropriate formatter for axis labels.
 */
function buildTimeLabelFormatter(
    categories: string[],
): (value: string) => string {
    let spanMs = 0;
    if (categories.length >= 2) {
        const first = new Date(categories[0]).getTime();
        const last = new Date(categories[categories.length - 1]).getTime();
        if (!Number.isNaN(first) && !Number.isNaN(last)) {
            spanMs = Math.abs(last - first);
        }
    }

    return (value: string) => {
        const d = new Date(value);
        if (Number.isNaN(d.getTime())) {return value;}

        if (spanMs < MS_PER_DAY) {
            return formatTime(d);
        }
        if (spanMs <= 7 * MS_PER_DAY) {
            return formatDateTimeShort(d);
        }
        return formatDateOnly(d);
    };
}

/**
 * Formats a numeric value with SI-style abbreviations for display on
 * Y-axis labels and tooltips.
 */
function formatNumericValue(value: number): string {
    const abs = Math.abs(value);
    if (abs >= 1e9) {return `${(value / 1e9).toFixed(1)}B`;}
    if (abs >= 1e6) {return `${(value / 1e6).toFixed(1)}M`;}
    if (abs >= 1e3) {return `${(value / 1e3).toFixed(1)}K`;}
    if (Number.isInteger(value)) {return value.toString();}
    return value.toFixed(1);
}

interface TooltipParam {
    axisValue: string;
    marker: string;
    seriesName: string;
    value: number;
}

export function buildTooltip(show: boolean): object {
    return {
        show,
        trigger: 'axis',
        confine: false,
        appendToBody: true,
        formatter: (params: TooltipParam | TooltipParam[]) => {
            const list = Array.isArray(params) ? params : [params];
            if (list.length === 0) {return '';}

            const d = new Date(list[0].axisValue);
            const header = Number.isNaN(d.getTime())
                ? list[0].axisValue
                : formatDateTimeFull(d);

            const lines = list.map((p) => {
                const val = typeof p.value === 'number'
                    ? formatNumericValue(p.value)
                    : String(p.value);
                return `${p.marker} ${p.seriesName}: ${val}`;
            });

            return '<strong>' + header + '</strong><br/>'
                + lines.join('<br/>');
        },
    };
}

export function buildLegend(show: boolean): object {
    return {
        show,
        bottom: 0,
    };
}

export function buildGrid(): object {
    return {
        left: '3%',
        right: '4%',
        bottom: '15%',
        top: '10%',
        containLabel: true,
    };
}

export function buildXAxis(categories?: string[]): object {
    const cats = categories ?? [];
    const formatter = buildTimeLabelFormatter(cats);

    return {
        type: 'category',
        data: cats,
        boundaryGap: true,
        axisLabel: {
            formatter,
            hideOverlap: true,
        },
    };
}

/**
 * Builds the numeric value axis. When `seriesData` is supplied the
 * range of the data is inspected; if every finite point shares the
 * same value the axis would otherwise collapse to a zero-height range
 * (`min === max`), leaving ECharts nothing to interpolate against so
 * the line/area renders as blank. In that degenerate case a synthetic
 * range is padded around the flat value so the series stays visible.
 * For any non-degenerate range no `min`/`max` is emitted, so ECharts
 * auto-scaling is preserved exactly as before.
 *
 * When `stacked` is true the series are drawn on top of one another,
 * so the rendered height at each x-index is the cumulative total across
 * all series at that index rather than any single raw value. ECharts
 * stacks positive values upward from the zero baseline and negative
 * values downward from that same baseline, so the two signs do not net
 * against one another. The per-index positive and negative totals are
 * therefore accumulated separately: the positive total is the top of
 * the rendered stack and the negative total is the bottom. A sign that
 * never appears anywhere in the data is not folded into the observed
 * range, so a single-sign stacked chart (e.g. one series flat at 100)
 * still pads tightly around its real total rather than being anchored
 * to a spurious zero baseline. A mixed-sign stacked chart (e.g. one
 * series flat at +100 and another at -100) is bounded by both the
 * positive and negative totals, preserving its true ~200-unit span
 * instead of collapsing to a tiny window around a netted zero.
 */
export function buildYAxis(
    seriesData?: number[][],
    stacked?: boolean,
): object {
    const axis: {
        type: string;
        axisLabel: { formatter: (value: number) => string };
        min?: number;
        max?: number;
    } = {
        type: 'value',
        axisLabel: {
            formatter: formatNumericValue,
        },
    };

    let min = Infinity;
    let max = -Infinity;
    let hasValue = false;
    const observe = (value: number) => {
        hasValue = true;
        if (value < min) {min = value;}
        if (value > max) {max = value;}
    };

    const series = seriesData ?? [];
    if (stacked) {
        // Stacked series render as a cumulative total at each x-index.
        // ECharts stacks positive values upward and negative values
        // downward from a shared zero baseline, so the two signs are
        // accumulated separately: the positive total is the top of the
        // stack and the negative total is the bottom. A first pass
        // records the longest series and whether either sign occurs
        // anywhere; a sign that never appears is not folded into the
        // observed range, keeping single-sign stacks padded tightly
        // around their real total rather than anchored to a spurious
        // zero baseline.
        let maxLen = 0;
        let hasPositive = false;
        let hasNegative = false;
        for (const s of series) {
            if (s.length > maxLen) {maxLen = s.length;}
            for (const value of s) {
                if (!Number.isFinite(value)) {continue;}
                if (value > 0) {hasPositive = true;}
                else if (value < 0) {hasNegative = true;}
            }
        }
        for (let i = 0; i < maxLen; i++) {
            let posSum = 0;
            let negSum = 0;
            let finiteAtIndex = false;
            for (const s of series) {
                const value = s[i];
                if (!Number.isFinite(value)) {continue;}
                finiteAtIndex = true;
                if (value > 0) {posSum += value;}
                else if (value < 0) {negSum += value;}
            }
            if (!finiteAtIndex) {continue;}
            // Observe each sign's total only when that sign is present
            // somewhere in the data. When neither sign occurs the index
            // holds only finite zeros, so observe the flat zero baseline
            // so an all-zero stacked series still pads to a visible range.
            if (hasPositive) {observe(posSum);}
            if (hasNegative) {observe(negSum);}
            if (!hasPositive && !hasNegative) {observe(0);}
        }
    } else {
        for (const s of series) {
            for (const value of s) {
                if (!Number.isFinite(value)) {continue;}
                observe(value);
            }
        }
    }

    if (hasValue && min === max) {
        const flat = min;
        const padding = flat === 0 ? 1 : Math.abs(flat) * 0.1;
        axis.min = flat - padding;
        axis.max = flat + padding;
    }

    return axis;
}

export function buildDataZoom(enabled: boolean): object[] {
    return [
        {
            type: 'slider',
            show: enabled,
            start: 0,
            end: 100,
        },
    ];
}
