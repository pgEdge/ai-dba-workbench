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
import {
    appendTimeRangeParams,
    isTimeRangeQueryable,
} from '../utils/timeRangeParams';
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

/** Build the comparison request URL for a query and time range. */
export const buildComparisonUrl = (
    query: AnomalyComparisonQuery,
    timeRange: TimeRangeState,
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
    appendTimeRangeParams(params, timeRange);
    return `/api/v1/metrics/anomaly-comparison?${params.toString()}`;
};

/**
 * Resolve the dashboard's time range into absolute bounds, split into
 * the buckets the comparison was requested with. The server resolves
 * a preset against its own clock, so the two can differ by the request
 * latency; that is far below a bucket for every range offered.
 */
export const resolveComparisonWindow = (
    timeRange: TimeRangeState,
): ComparisonWindow => {
    const { range, customStart, customEnd } = timeRange;
    const bounds = range === 'custom'
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
export const buildAnomalyAlertsUrl = (
    connectionId: number,
    window: ComparisonWindow,
): string => {
    const params = new URLSearchParams({
        alert_type: 'anomaly',
        connection_id: connectionId.toString(),
        start_time: new Date(window.startMs).toISOString(),
        end_time: new Date(window.endMs).toISOString(),
        limit: ALERT_LIMIT.toString(),
    });
    return `/api/v1/alerts?${params.toString()}`;
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
        if (!isLoggedIn) { return undefined; }
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
        return () => { active = false; };
    }, [isLoggedIn]);

    return {
        metrics: result?.metrics ?? NO_METRICS,
        loading: isLoggedIn && result === null,
        error: result?.error ?? null,
    };
};

// ---------------------------------------------------------------------------
// useAnomalyComparison
// ---------------------------------------------------------------------------

export interface UseAnomalyComparisonReturn {
    data: AnomalyComparisonResponse | null;
    /** Every anomaly alert for the connection in the window, unfiltered. */
    alerts: AnomalyAlert[];
    window: ComparisonWindow | null;
    loading: boolean;
    error: string | null;
    /** Set when the alerts failed to load; the chart is still shown. */
    alertsError: string | null;
}

/** The settled outcome of a request, and the selection it was for. */
interface ComparisonResult {
    key: string;
    data: AnomalyComparisonResponse | null;
    alerts: AnomalyAlert[];
    window: ComparisonWindow;
    error: string | null;
    alertsError: string | null;
}

const NO_ALERTS: AnomalyAlert[] = [];

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
    query: AnomalyComparisonQuery | null,
): UseAnomalyComparisonReturn => {
    const { user } = useAuth();
    const { refreshTrigger, timeRange } = useDashboard();
    const { beginRequest, supersedeRequest } = useRequestSequence();
    const [result, setResult] = useState<ComparisonResult | null>(null);

    const isLoggedIn = !!user;
    const { range, customStart, customEnd } = timeRange;
    const queryable = isTimeRangeQueryable({ range, customStart, customEnd });
    const key = query
        ? JSON.stringify([query, range, customStart, customEnd])
        : null;
    // Read through a ref so that the fetch depends on the query's
    // values, which `key` encodes, rather than on the identity of the
    // object the caller passes on each render.
    const queryRef = useRef(query);
    queryRef.current = query;

    const fetchData = useCallback(async (): Promise<void> => {
        const query = queryRef.current;
        if (!query || key === null) { return; }
        const selectedRange: TimeRangeState = { range, customStart, customEnd };
        if (!isTimeRangeQueryable(selectedRange)) {
            supersedeRequest();
            return;
        }

        const window = resolveComparisonWindow(selectedRange);
        const isCurrent = beginRequest();

        const [comparison, alerts] = await Promise.allSettled([
            apiGet<AnomalyComparisonResponse>(
                buildComparisonUrl(query, selectedRange),
            ),
            apiGet<{ alerts?: AnomalyAlert[] | null }>(
                buildAnomalyAlertsUrl(query.connectionId, window),
            ),
        ]);
        if (!isCurrent()) { return; }

        if (comparison.status === 'rejected') {
            logger.error('Error fetching baseline comparison:', comparison.reason);
        }
        if (alerts.status === 'rejected') {
            logger.error('Error fetching anomaly alerts:', alerts.reason);
        }

        setResult({
            key,
            window,
            data: comparison.status === 'fulfilled' ? comparison.value : null,
            error: comparison.status === 'rejected'
                ? messageOf(comparison.reason, 'Failed to load the baseline comparison')
                : null,
            alerts: alerts.status === 'fulfilled'
                ? alerts.value.alerts ?? NO_ALERTS
                : NO_ALERTS,
            alertsError: alerts.status === 'rejected'
                ? messageOf(alerts.reason, 'Failed to load anomaly alerts')
                : null,
        });
    }, [key, range, customStart, customEnd, beginRequest, supersedeRequest]);

    useEffect(() => {
        if (isLoggedIn) {
            void fetchData();
        }
    }, [isLoggedIn, fetchData, refreshTrigger]);

    const current = result !== null && result.key === key ? result : null;

    return {
        data: current?.data ?? null,
        alerts: current?.alerts ?? NO_ALERTS,
        window: current?.window ?? null,
        loading: isLoggedIn && key !== null && queryable && current === null,
        error: current?.error ?? null,
        alertsError: current?.alertsError ?? null,
    };
};

export default useAnomalyComparison;
