/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import {
    COMPARISON_BUCKETS,
    buildAnomalyAlertsUrl,
    buildComparisonUrl,
    resolveComparisonWindow,
    useAnomalyComparison,
    useComparisonMetrics,
    type AnomalyComparisonQuery,
} from '../useAnomalyComparison';
import type { TimeRangeState } from '../../components/Dashboard/types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args) as unknown,
}));

let mockUser: { id: number } | null = { id: 1 };
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

let mockTimeRange: TimeRangeState = { range: '1h' };
let mockRefreshTrigger = 0;
vi.mock('../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        refreshTrigger: mockRefreshTrigger,
        timeRange: mockTimeRange,
    }),
}));

vi.mock('../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const QUERY: AnomalyComparisonQuery = {
    connectionId: 7,
    metric: 'pg_stat_database.cache_hit_ratio',
    databaseName: 'app',
    model: 'hourly',
    sensitivity: 3,
};

const RESPONSE = {
    metric: QUERY.metric,
    connection_id: 7,
    database_name: 'app',
    model: 'hourly',
    sensitivity: 3,
    default_sensitivity: 3,
    scoring_source: 'alerter',
    baselines_as_of: '2026-10-28T12:00:00Z',
    points: [],
};

const ALERTS = {
    alerts: [{ id: 1, triggered_at: '2026-10-28T11:00:00Z' }],
    total: 1,
};

/** Answer each URL by its path, failing any the test did not expect. */
const routeApi = (
    routes: Record<string, () => Promise<unknown>>,
): void => {
    mockApiGet.mockImplementation((url: string) => {
        const path = url.split('?')[0];
        const route = routes[path] as (() => Promise<unknown>) | undefined;
        return route ? route() : Promise.reject(new Error(`unexpected ${url}`));
    });
};

beforeEach(() => {
    mockApiGet.mockReset();
    mockUser = { id: 1 };
    mockTimeRange = { range: '1h' };
    mockRefreshTrigger = 0;
});

// ---------------------------------------------------------------------------
// URL and window builders
// ---------------------------------------------------------------------------

describe('buildComparisonUrl', () => {
    it('includes every parameter for a per-database query', () => {
        const url = buildComparisonUrl(QUERY, { range: '24h' });
        const params = new URL(url, 'http://x').searchParams;
        expect(url.startsWith('/api/v1/metrics/anomaly-comparison?')).toBe(true);
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('metric')).toBe(QUERY.metric);
        expect(params.get('database_name')).toBe('app');
        expect(params.get('model')).toBe('hourly');
        expect(params.get('sensitivity')).toBe('3');
        expect(params.get('buckets')).toBe(String(COMPARISON_BUCKETS));
        expect(params.get('time_range')).toBe('24h');
    });

    it('omits the database and sensitivity when they are not set', () => {
        const url = buildComparisonUrl(
            { ...QUERY, databaseName: null, sensitivity: null },
            { range: 'custom', customStart: 'a', customEnd: 'b' },
        );
        const params = new URL(url, 'http://x').searchParams;
        expect(params.has('database_name')).toBe(false);
        expect(params.has('sensitivity')).toBe(false);
        expect(params.get('time_start')).toBe('a');
        expect(params.get('time_end')).toBe('b');
    });
});

describe('resolveComparisonWindow', () => {
    it('resolves a preset relative to now', () => {
        const now = Date.parse('2026-10-28T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(now);
        try {
            expect(resolveComparisonWindow({ range: '6h' })).toEqual({
                startMs: now - 6 * 3_600_000,
                endMs: now,
                buckets: COMPARISON_BUCKETS,
            });
        } finally {
            vi.useRealTimers();
        }
    });

    it('uses the bounds of a custom range', () => {
        expect(resolveComparisonWindow({
            range: 'custom',
            customStart: '2026-10-01T00:00:00Z',
            customEnd: '2026-10-02T00:00:00Z',
        })).toEqual({
            startMs: Date.parse('2026-10-01T00:00:00Z'),
            endMs: Date.parse('2026-10-02T00:00:00Z'),
            buckets: COMPARISON_BUCKETS,
        });
    });
});

describe('buildAnomalyAlertsUrl', () => {
    it('asks for the connection\'s anomaly alerts within the window', () => {
        const url = buildAnomalyAlertsUrl(7, {
            startMs: Date.parse('2026-10-28T00:00:00Z'),
            endMs: Date.parse('2026-10-28T01:00:00Z'),
            buckets: 10,
        });
        const params = new URL(url, 'http://x').searchParams;
        expect(url.startsWith('/api/v1/alerts?')).toBe(true);
        expect(params.get('alert_type')).toBe('anomaly');
        expect(params.get('connection_id')).toBe('7');
        expect(params.get('start_time')).toBe('2026-10-28T00:00:00.000Z');
        expect(params.get('end_time')).toBe('2026-10-28T01:00:00.000Z');
        expect(params.get('limit')).toBe('1000');
    });
});

// ---------------------------------------------------------------------------
// useComparisonMetrics
// ---------------------------------------------------------------------------

describe('useComparisonMetrics', () => {
    it('loads the metric list', async () => {
        const metrics = [{ name: 'm', probe_name: 'p', per_database: false }];
        routeApi({
            '/api/v1/metrics/anomaly-comparison/metrics':
                () => Promise.resolve(metrics),
        });
        const { result } = renderHook(() => useComparisonMetrics());
        expect(result.current.loading).toBe(true);
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        expect(result.current.metrics).toEqual(metrics);
        expect(result.current.error).toBeNull();
    });

    it('treats a null body as no metrics', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison/metrics':
                () => Promise.resolve(null),
        });
        const { result } = renderHook(() => useComparisonMetrics());
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        expect(result.current.metrics).toEqual([]);
    });

    it('reports a failure', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison/metrics':
                () => Promise.reject(new Error('boom')),
        });
        const { result } = renderHook(() => useComparisonMetrics());
        await waitFor(() => { expect(result.current.error).toBe('boom'); });
        expect(result.current.loading).toBe(false);
    });

    it('falls back to a generic message for an empty error', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison/metrics':
                () => Promise.reject(new Error('')),
        });
        const { result } = renderHook(() => useComparisonMetrics());
        await waitFor(() => {
            expect(result.current.error).toBe('Failed to load baselined metrics');
        });
    });

    it('does nothing whilst logged out', () => {
        mockUser = null;
        const { result } = renderHook(() => useComparisonMetrics());
        expect(result.current.loading).toBe(false);
        expect(mockApiGet).not.toHaveBeenCalled();
    });
});

// ---------------------------------------------------------------------------
// useAnomalyComparison
// ---------------------------------------------------------------------------

describe('useAnomalyComparison', () => {
    it('loads the comparison and the anomaly alerts together', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison': () => Promise.resolve(RESPONSE),
            '/api/v1/alerts': () => Promise.resolve(ALERTS),
        });
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        expect(result.current.loading).toBe(true);
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        expect(result.current.data).toEqual(RESPONSE);
        expect(result.current.alerts).toEqual(ALERTS.alerts);
        expect(result.current.window?.buckets).toBe(COMPARISON_BUCKETS);
        expect(result.current.error).toBeNull();
        expect(result.current.alertsError).toBeNull();
    });

    it('still charts the comparison when the alerts fail', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison': () => Promise.resolve(RESPONSE),
            '/api/v1/alerts': () => Promise.reject(new Error('')),
        });
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        expect(result.current.data).toEqual(RESPONSE);
        expect(result.current.alerts).toEqual([]);
        expect(result.current.alertsError).toBe('Failed to load anomaly alerts');
    });

    it('treats an alerts body without a list as no alerts', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison': () => Promise.resolve(RESPONSE),
            '/api/v1/alerts': () => Promise.resolve({}),
        });
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        expect(result.current.alerts).toEqual([]);
    });

    it('reports a failed comparison', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison':
                () => Promise.reject(new Error('unknown metric')),
            '/api/v1/alerts': () => Promise.resolve(ALERTS),
        });
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        await waitFor(() => {
            expect(result.current.error).toBe('unknown metric');
        });
        expect(result.current.data).toBeNull();
        expect(result.current.loading).toBe(false);
    });

    it('falls back to a generic message for an empty comparison error', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison':
                () => Promise.reject(new Error('')),
            '/api/v1/alerts': () => Promise.resolve(ALERTS),
        });
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        await waitFor(() => {
            expect(result.current.error).toBe(
                'Failed to load the baseline comparison',
            );
        });
    });

    it('does not fetch without a query or whilst logged out', () => {
        const { result } = renderHook(() => useAnomalyComparison(null));
        expect(result.current.loading).toBe(false);
        mockUser = null;
        renderHook(() => useAnomalyComparison(QUERY));
        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('skips a half-entered custom range', () => {
        mockTimeRange = { range: 'custom', customStart: 'a' };
        const { result } = renderHook(() => useAnomalyComparison(QUERY));
        expect(result.current.loading).toBe(false);
        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('ignores a response for a superseded query', async () => {
        let resolveFirst: (value: unknown) => void = () => {};
        let call = 0;
        routeApi({
            '/api/v1/metrics/anomaly-comparison': () => {
                call += 1;
                if (call === 1) {
                    return new Promise((resolve) => { resolveFirst = resolve; });
                }
                return Promise.resolve({ ...RESPONSE, model: 'all' });
            },
            '/api/v1/alerts': () => Promise.resolve(ALERTS),
        });
        const { result, rerender } = renderHook(
            ({ q }) => useAnomalyComparison(q),
            { initialProps: { q: QUERY } },
        );
        rerender({ q: { ...QUERY, model: 'all' } });
        await waitFor(() => { expect(result.current.data?.model).toBe('all'); });
        resolveFirst({ ...RESPONSE, model: 'hourly' });
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(result.current.data?.model).toBe('all');
    });

    it('keeps the previous result whilst a refresh is in flight', async () => {
        routeApi({
            '/api/v1/metrics/anomaly-comparison': () => Promise.resolve(RESPONSE),
            '/api/v1/alerts': () => Promise.resolve(ALERTS),
        });
        const { result, rerender } = renderHook(() => useAnomalyComparison(QUERY));
        await waitFor(() => { expect(result.current.loading).toBe(false); });
        mockRefreshTrigger = 1;
        rerender();
        expect(result.current.loading).toBe(false);
        expect(result.current.data).toEqual(RESPONSE);
        await waitFor(() => { expect(mockApiGet).toHaveBeenCalledTimes(4); });
    });
});
