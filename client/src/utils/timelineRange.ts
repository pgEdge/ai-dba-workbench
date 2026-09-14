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
 * The single source of truth for resolving a timeline time range, whether
 * a rolling preset or an arbitrary window, into concrete bounds.
 *
 * Both the fetch layer in hooks/useTimelineEvents.ts and the rendering
 * helpers in components/EventTimeline/utils.ts previously carried their
 * own copy of this calculation, and the copies had diverged; they now
 * both delegate here.
 */

/** An arbitrary window chosen by the user. */
export interface CustomTimeRange {
    start: Date;
    end: Date;
}

/** The rolling windows offered by the timeline selector. */
export type TimeRangePreset = '1h' | '6h' | '24h' | '7d' | '30d';

/** Either a rolling preset or an arbitrary window. */
export type TimelineTimeRange = TimeRangePreset | CustomTimeRange;

/** Resolved absolute bounds for a time range. */
export interface TimeRangeBounds {
    startTime: Date;
    endTime: Date;
}

/** Milliseconds in an hour, the unit every preset is expressed in. */
const HOUR_MS = 60 * 60 * 1000;

/** Hours spanned by each preset, with 24h as the fallback. */
const PRESET_HOURS: Record<TimeRangePreset, number> = {
    '1h': 1,
    '6h': 6,
    '24h': 24,
    '7d': 7 * 24,
    '30d': 30 * 24,
};

const DEFAULT_PRESET_HOURS = PRESET_HOURS['24h'];

/**
 * Narrow an unknown range to an arbitrary window. Anything carrying both
 * a start and an end is treated as custom; a preset is a plain string.
 */
export const isCustomTimeRange = (
    timeRange: unknown,
): timeRange is CustomTimeRange =>
    typeof timeRange === 'object' &&
    timeRange !== null &&
    'start' in timeRange &&
    'end' in timeRange;

/**
 * Resolve a time range into absolute start and end times. A custom
 * window is returned as given, coercing each bound through Date so that
 * a caller holding ISO strings still gets Date objects back. Anything
 * that is not a recognised preset falls back to the last 24 hours.
 */
export const resolveTimeRangeBounds = (
    timeRange: TimelineTimeRange,
): TimeRangeBounds => {
    if (isCustomTimeRange(timeRange)) {
        return {
            startTime: new Date(timeRange.start),
            endTime: new Date(timeRange.end),
        };
    }

    const now = new Date();
    const hours = PRESET_HOURS[timeRange] ?? DEFAULT_PRESET_HOURS;

    return {
        startTime: new Date(now.getTime() - hours * HOUR_MS),
        endTime: now,
    };
};

/** The longest custom window the server accepts, in days. */
export const MAX_CUSTOM_WINDOW_DAYS = 366;

/** MAX_CUSTOM_WINDOW_DAYS expressed in milliseconds. */
export const MAX_CUSTOM_WINDOW_MS = MAX_CUSTOM_WINDOW_DAYS * 24 * HOUR_MS;

/**
 * The reasons a custom window can be rejected, mirroring the checks the
 * server applies in ResolveCustomWindow so that the client never offers
 * a window the server will answer with a 400.
 */
export type CustomWindowError =
    | 'end-not-after-start'
    | 'start-in-future'
    | 'span-too-long';

/**
 * Validate a custom window against the server's rules: the end must be
 * strictly after the start, the start must not be in the future, and the
 * span must not exceed MAX_CUSTOM_WINDOW_MS. As on the server, an end in
 * the future is clamped to now before the span is measured, because a
 * picker set to the current day routinely overshoots by a few minutes.
 * Returns the first failing rule, or null when the window is acceptable.
 */
export const validateCustomWindow = (
    start: Date,
    end: Date,
    now: Date = new Date(),
): CustomWindowError | null => {
    const startMs = start.getTime();
    const endMs = end.getTime();
    const nowMs = now.getTime();

    if (!(endMs > startMs)) {
        return 'end-not-after-start';
    }
    if (!(startMs < nowMs)) {
        return 'start-in-future';
    }
    if (Math.min(endMs, nowMs) - startMs > MAX_CUSTOM_WINDOW_MS) {
        return 'span-too-long';
    }
    return null;
};

export default resolveTimeRangeBounds;
