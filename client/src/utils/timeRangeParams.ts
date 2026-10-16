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
 * The single source of truth for turning the dashboard's selected time
 * range into the query parameters the metrics endpoints expect.
 *
 * Every metrics endpoint takes `time_range`, and accepts `time_start`
 * and `time_end` only for the 'custom' range, so each panel that
 * follows the selector previously carried its own copy of the same two
 * rules. The copies had begun to drift, so they now delegate here.
 *
 * This is deliberately distinct from utils/timelineRange.ts, which
 * resolves the Event Timeline's own TimelineTimeRange type into
 * absolute Date bounds for client-side filtering.
 */

import type { TimeRangeState } from '../components/Dashboard/types';

/**
 * True when the selected window can be sent to the server.
 *
 * A custom range without both bounds is a transient state that the
 * server rejects with a 400, so callers use this to skip the request
 * altogether rather than fire one that is known to fail.
 */
export const isTimeRangeQueryable = (
    timeRange: TimeRangeState,
): boolean => {
    const { range, customStart, customEnd } = timeRange;
    return range !== 'custom' || (!!customStart && !!customEnd);
};

/**
 * Append `time_range`, and the custom bounds where they apply, to the
 * given parameters, returning the same object so that the call can be
 * chained onto a URLSearchParams constructor.
 *
 * The bounds are emitted only for the 'custom' range and only when both
 * are present, which mirrors what the server accepts.
 */
export const appendTimeRangeParams = (
    params: URLSearchParams,
    timeRange: TimeRangeState,
): URLSearchParams => {
    const { range, customStart, customEnd } = timeRange;

    params.set('time_range', range);

    if (range === 'custom' && customStart && customEnd) {
        params.set('time_start', customStart);
        params.set('time_end', customEnd);
    }

    return params;
};
