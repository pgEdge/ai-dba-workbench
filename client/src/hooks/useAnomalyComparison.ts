/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '../contexts/useAuth';
import { useDashboard } from '../contexts/useDashboard';
import type { TimeRangeState } from '../components/Dashboard/types';
import type {
    AnomalyAlert,
    AnomalyComparisonResponse,
    ComparisonMetric,
    ComparisonModel,
    ComparisonWindow,
} from '../components/Dashboard/ServerDashboard/baselineComparison';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';
import { isTimeRangeQueryable } from '../utils/timeRangeParams';
import { resolveTimeRangeBounds } from '../utils/timelineRange';
import { useRequestSequence } from './useRequestSequence';

/**
 * Buckets requested from the comparison endpoint. It is the server's
 * own default; the client sends it explicitly because it also splits
 * the window into the same buckets to match alerts to points.
 */
export const COMPARISON_BUCKETS = 300;

/** The most anomaly alerts one request returns; the endpoint's cap. */
const ALERT_LIMIT = 1000;

const METRICS_URL = '/api/v1/metrics/anomaly-comparison/metrics';

/** What to compare: everything but the dashboard's time range. */
export interface AnomalyComparisonQuery {
    connectionId: number;
    metric: string;
    /** Required for a per-database metric, and null otherwise. */
    databaseName: string | null;
    model: ComparisonModel;
    /** Null asks the server for its default sensitivity. */
    sensitivity: number | null;
}

/**
 * Build the comparison request URL for a query and a resolved window.
 * The window is sent as absolute bounds, so that the server buckets
 * exactly the window the client matches alerts against rather than
 * resolving a preset on its own clock.
 */
export const buildComparisonUrl = (
    query: AnomalyComparisonQuery,
    window: ComparisonWindow
): string => {
    const params = new URLSearchParams({
        connection_id: query.connectionId.toString(),
        metric: query.metric,
        model: query.model,
        buckets: COMPARISON_BUCKETS.toString(),
    });
    if (query.databaseName !== null) {
        params.set('database_name', query.databaseName);
    }
    if (query.sensitivity !== null) {
        params.set('sensitivity', query.sensitivity.toString());
    }
    params.set('time_range', 'custom');
    params.set('time_start', new Date(window.startMs).toISOString());
    params.set('time_end', new Date(window.endMs).toISOString());
    return `/api/v1/metrics/anomaly-comparison?${params.toString()}`;
};

/**
 * Resolve the dashboard's time range into absolute bounds, split into
 * the buckets the comparison is requested with. The comparison request
 * sends these bounds, so the server and the client bucket the same
 * window.
 */
export const resolveComparisonWindow = (timeRange: TimeRangeState): ComparisonWindow => {
    const { range, customStart, customEnd } = timeRange;
    const bounds =
        range === 'custom'
            ? resolveTimeRangeBounds({
                  start: new Date(customStart ?? ''),
                  end: new Date(customEnd ?? ''),
              })
            : resolveTimeRangeBounds(range);
    return {
        startMs: bounds.startTime.getTime(),
        endMs: bounds.endTime.getTime(),
        buckets: COMPARISON_BUCKETS,
    };
};

/** Build the request for a connection's anomaly alerts in a window. */
export const buildAnomalyAlertsUrl = (connectionId: number, window: ComparisonWindow): string => {
    const params = new URLSearchParams({
        alert_type: 'anomaly',
        connection_id: connectionId.toString(),
        start_time: new Date(window.startMs).toISOString(),
        end_time: new Date(window.endMs).toISOString(),
        limit: ALERT_LIMIT.toString(),
    });
    return `/api/v1/alerts?${params.toString()}`;
};

/**
 * Build the request for a connection's anomaly alerts that were raised
 * before the window and have not cleared. The alerts endpoint filters
 * on `triggered_at` only, so an alert still open across the window's
 * start needs this second request. Its span covers the outside points
 * it held the engine back from alerting on.
 */
export const buildOpenAnomalyAlertsUrl = (
    connectionId: number,
    window: ComparisonWindow
): string => {
    const params = new URLSearchParams({
        alert_type: 'anomaly',
        connection_id: connectionId.toString(),
        exclude_cleared: 'true',
        end_time: new Date(window.startMs).toISOString(),
        limit: ALERT_LIMIT.toString(),
    });
    return `/api/v1/alerts?${params.toString()}`;
};

/** The alerts of both lists, keeping the first of any repeated id. */
const mergeAlerts = (first: AnomalyAlert[], second: AnomalyAlert[]): AnomalyAlert[] => {
    const seen = new Set(first.map((a) => a.id));
    return [...first, ...second.filter((a) => !seen.has(a.id))];
};

/** A thrown value's message, or the fallback when it carries none. */
const messageOf = (err: unknown, fallback: string): string =>
    (err as Error | undefined)?.message || fallback;

// ---------------------------------------------------------------------------
// useComparisonMetrics
// ---------------------------------------------------------------------------

export interface UseComparisonMetricsReturn {
    metrics: ComparisonMetric[];
    loading: boolean;
    error: string | null;
}

const NO_METRICS: ComparisonMetric[] = [];

/**
 * Fetch the baselined metrics the comparison endpoint accepts. The
 * list is fixed by the alerter's registry, so it is fetched once per
 * login rather than on every dashboard refresh.
 */
export const useComparisonMetrics = (): UseComparisonMetricsReturn => {
    const { user } = useAuth();
    const isLoggedIn = !!user;
    const [result, setResult] = useState<{
        metrics: ComparisonMetric[];
        error: string | null;
    } | null>(null);

    useEffect(() => {
        if (!isLoggedIn) {
            return undefined;
        }
        let active = true;
        apiGet<ComparisonMetric[] | null>(METRICS_URL)
            .then((metrics) => {
                if (active) {
                    setResult({ metrics: metrics ?? NO_METRICS, error: null });
                }
            })
            .catch((err: unknown) => {
                logger.error('Error fetching baselined metrics:', err);
                if (active) {
                    setResult({
                        metrics: NO_METRICS,
                        error: messageOf(err, 'Failed to load baselined metrics'),
                    });
                }
            });
        return () => {
            active = false;
        };
    }, [isLoggedIn]);

    return {
        metrics: result?.metrics ?? NO_METRICS,
        loading: isLoggedIn && result === null,
        error: result?.error ?? null,
    };
};

/**
 * Where a capped alert list stops being complete. The alerts arrive
 * newest first, so when fewer were returned than `total`, every alert
 * at or after the oldest one returned was loaded and older ones may
 * not have been. Null means the list is complete; with no usable
 * time, nothing in the window is known to be complete.
 */
export const alertsCompleteFrom = (
    alerts: AnomalyAlert[],
    total: number | undefined,
    window: ComparisonWindow
): number | null => {
    if (total === undefined || alerts.length >= total) {
        return null;
    }
    const times = alerts.map((a) => Date.parse(a.triggered_at)).filter((t) => !Number.isNaN(t));
    return times.length > 0 ? Math.min(...times) : window.endMs;
};

// ---------------------------------------------------------------------------
// useAnomalyComparison
// ---------------------------------------------------------------------------

export interface UseAnomalyComparisonReturn {
    data: AnomalyComparisonResponse | null;
    /**
     * Every anomaly alert for the connection in the window, and those
     * raised before it that are still open, unfiltered.
     */
    alerts: AnomalyAlert[];
    window: ComparisonWindow | null;
    loading: boolean;
    error: string | null;
    /** Set when the alerts failed to load; the chart is still shown. */
    alertsError: string | null;
    /**
     * Set when the alerts were capped: the time from which every
     * alert in the window was loaded. Null when all were loaded.
     */
    alertsCompleteFromMs: number | null;
}

/** The settled outcome of a request, and the selection it was for. */
interface ComparisonResult {
    key: string;
    data: AnomalyComparisonResponse | null;
    alerts: AnomalyAlert[];
    window: ComparisonWindow;
    error: string | null;
    alertsError: string | null;
    alertsCompleteFromMs: number | null;
}

const NO_ALERTS: AnomalyAlert[] = [];

/** The body of an anomaly alerts response. */
interface AlertsBody {
    alerts?: AnomalyAlert[] | null;
    total?: number;
}

/** The alerts a settled alerts request loaded; none when it failed. */
const loadedAlertsOf = (settled: PromiseSettledResult<AlertsBody>): AnomalyAlert[] =>
    settled.status === 'fulfilled' ? (settled.value.alerts ?? NO_ALERTS) : NO_ALERTS;

/** The error message of a settled request, or null when it succeeded. */
const failureOf = (settled: PromiseSettledResult<unknown>, fallback: string): string | null =>
    settled.status === 'rejected' ? messageOf(settled.reason, fallback) : null;

/** Log each of the comparison's requests that failed. */
const logFailures = (requests: [PromiseSettledResult<unknown>, string][]): void => {
    for (const [settled, description] of requests) {
        if (settled.status === 'rejected') {
            logger.error(`Error fetching ${description}:`, settled.reason);
        }
    }
};

/**
 * Build the result of a comparison request from its three settled
 * requests: the comparison, the alerts in the window and the alerts
 * still open from before it. A failed alerts request leaves the
 * comparison shown with an alerts error; the alerts are complete from
 * the in-window request's cap only when that request succeeded.
 */
export const settleComparison = (
    key: string,
    window: ComparisonWindow,
    comparison: PromiseSettledResult<AnomalyComparisonResponse>,
    alerts: PromiseSettledResult<AlertsBody>,
    openAlerts: PromiseSettledResult<AlertsBody>
): ComparisonResult => {
    const loadedAlerts = loadedAlertsOf(alerts);
    const alertsFailure = alerts.status === 'rejected' ? alerts : openAlerts;
    return {
        key,
        window,
        data: comparison.status === 'fulfilled' ? comparison.value : null,
        error: failureOf(comparison, 'Failed to load the baseline comparison'),
        alerts: mergeAlerts(loadedAlerts, loadedAlertsOf(openAlerts)),
        alertsError: failureOf(alertsFailure, 'Failed to load anomaly alerts'),
        alertsCompleteFromMs:
            alerts.status === 'fulfilled'
                ? alertsCompleteFrom(loadedAlerts, alerts.value.total, window)
                : null,
    };
};

/** What the hook returns for the result of the current selection. */
const toHookReturn = (
    current: ComparisonResult | null,
    loading: boolean
): UseAnomalyComparisonReturn => {
    if (current === null) {
        return {
            data: null,
            alerts: NO_ALERTS,
            window: null,
            loading,
            error: null,
            alertsError: null,
            alertsCompleteFromMs: null,
        };
    }
    return {
        data: current.data,
        alerts: current.alerts,
        window: current.window,
        loading,
        error: current.error,
        alertsError: current.alertsError,
        alertsCompleteFromMs: current.alertsCompleteFromMs,
    };
};

/**
 * Fetch the baseline comparison for a metric, together with the
 * connection's anomaly alerts over the same window, following the
 * dashboard's time range and refresh cycle.
 *
 * Each result records the selection it was fetched for and is returned
 * only whilst that is still the selection, so a changed metric, model,
 * sensitivity or window reads as loading from its first render, whilst
 * a periodic refresh of the same selection keeps the previous result
 * on screen until the new one lands.
 */
export const useAnomalyComparison = (
    query: AnomalyComparisonQuery | null
): UseAnomalyComparisonReturn => {
    const { user } = useAuth();
    const { refreshTrigger, timeRange } = useDashboard();
    const { beginRequest, supersedeRequest } = useRequestSequence();
    const [result, setResult] = useState<ComparisonResult | null>(null);

    const isLoggedIn = !!user;
    const { range, customStart, customEnd } = timeRange;
    const queryable = isTimeRangeQueryable({ range, customStart, customEnd });
    const key = query ? JSON.stringify([query, range, customStart, customEnd]) : null;
    // Read through a ref so that the fetch depends on the query's
    // values, which `key` encodes, rather than on the identity of the
    // object the caller passes on each render.
    const queryRef = useRef(query);
    queryRef.current = query;

    const fetchData = useCallback(async (): Promise<void> => {
        const query = queryRef.current;
        if (!query || key === null) {
            return;
        }
        const selectedRange: TimeRangeState = { range, customStart, customEnd };
        if (!isTimeRangeQueryable(selectedRange)) {
            supersedeRequest();
            return;
        }

        const window = resolveComparisonWindow(selectedRange);
        const isCurrent = beginRequest();

        const [comparison, alerts, openAlerts] = await Promise.allSettled([
            apiGet<AnomalyComparisonResponse>(buildComparisonUrl(query, window)),
            apiGet<AlertsBody>(buildAnomalyAlertsUrl(query.connectionId, window)),
            apiGet<AlertsBody>(buildOpenAnomalyAlertsUrl(query.connectionId, window)),
        ]);
        if (!isCurrent()) {
            return;
        }

        logFailures([
            [comparison, 'baseline comparison'],
            [alerts, 'anomaly alerts'],
            [openAlerts, 'open anomaly alerts'],
        ]);
        setResult(settleComparison(key, window, comparison, alerts, openAlerts));
    }, [key, range, customStart, customEnd, beginRequest, supersedeRequest]);

    useEffect(() => {
        if (isLoggedIn) {
            void fetchData();
        }
    }, [isLoggedIn, fetchData, refreshTrigger]);

    const current = result?.key === key ? result : null;
    return toHookReturn(current, isLoggedIn && key !== null && queryable && current === null);
};

export default useAnomalyComparison;
