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

export function buildYAxis(): object {
    return {
        type: 'value',
        axisLabel: {
            formatter: formatNumericValue,
        },
    };
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
