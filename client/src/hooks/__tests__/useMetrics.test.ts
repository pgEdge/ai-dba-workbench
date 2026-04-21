/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useMetrics, useBaselines } from '../useMetrics';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

const mockUser = { id: 1, username: 'testuser' };
let mockRefreshTrigger = 0;

vi.mock('../../contexts/AuthContext', () => ({
    useAuth: () => ({ user: mockUser }),
}));

vi.mock('../../contexts/DashboardContext', () => ({
    useDashboard: () => ({ refreshTrigger: mockRefreshTrigger }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeMetricSeries() {
    return [
        {
            name: 'cpu_usage',
            data: [10, 20, 30, 40, 50],
            timestamps: ['2024-01-01T00:00:00Z', '2024-01-01T01:00:00Z'],
        },
    ];
}

function makeBaselines() {
    return [
        {
            metric_name: 'cpu_usage',
            mean: 30.5,
            stddev: 10.2,
            p50: 30,
            p95: 48,
            p99: 50,
        },
    ];
}

// ---------------------------------------------------------------------------
// Tests - useMetrics
// ---------------------------------------------------------------------------

describe('useMetrics', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockRefreshTrigger = 0;
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('returns initial state when params is null', () => {
        const { result } = renderHook(() => useMetrics(null));

        expect(result.current.data).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.refetch).toBe('function');
    });

    it('fetches data when params are provided', async () => {
        mockApiGet.mockResolvedValueOnce(makeMetricSeries());

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
            connectionId: 1,
        };

        const { result } = renderHook(() => useMetrics(params));

        await waitFor(() => {
            expect(result.current.data).not.toBeNull();
        });

        expect(mockApiGet).toHaveBeenCalledTimes(1);
        expect(mockApiGet).toHaveBeenCalledWith(
            expect.stringContaining('/api/v1/metrics/query?'),
        );
        expect(result.current.data).toEqual(makeMetricSeries());
    });

    it('builds URL with all parameters', async () => {
        mockApiGet.mockResolvedValueOnce(makeMetricSeries());

        const params = {
            probeName: 'pg_stat_user_tables',
            timeRange: '7d',
            connectionId: 5,
            databaseName: 'mydb',
            schemaName: 'public',
            tableName: 'users',
            buckets: 60,
            aggregation: 'avg',
            metrics: ['seq_scan', 'idx_scan'],
        };

        renderHook(() => useMetrics(params));

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('probe_name=pg_stat_user_tables');
        expect(url).toContain('time_range=7d');
        expect(url).toContain('connection_id=5');
        expect(url).toContain('database_name=mydb');
        expect(url).toContain('schema_name=public');
        expect(url).toContain('table_name=users');
        expect(url).toContain('buckets=60');
        expect(url).toContain('aggregation=avg');
        expect(url).toContain('metrics=seq_scan%2Cidx_scan');
    });

    it('builds URL with connection_ids array', async () => {
        mockApiGet.mockResolvedValueOnce(makeMetricSeries());

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
            connectionIds: [1, 2, 3],
        };

        renderHook(() => useMetrics(params));

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('connection_ids=1%2C2%2C3');
    });

    it('sets loading to true during initial fetch', async () => {
        let resolvePromise: (value: unknown) => void;
        mockApiGet.mockImplementationOnce(() =>
            new Promise(resolve => {
                resolvePromise = resolve;
            }),
        );

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
        };

        const { result } = renderHook(() => useMetrics(params));

        expect(result.current.loading).toBe(true);

        await act(async () => {
            resolvePromise!(makeMetricSeries());
        });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
    });

    it('sets error on API failure', async () => {
        mockApiGet.mockRejectedValueOnce(new Error('Network error'));

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
        };

        const { result } = renderHook(() => useMetrics(params));

        await waitFor(() => {
            expect(result.current.error).toBe('Network error');
        });

        expect(result.current.data).toBeNull();
        expect(result.current.loading).toBe(false);
    });

    it('refetch triggers a new API call', async () => {
        mockApiGet.mockResolvedValue(makeMetricSeries());

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
        };

        const { result } = renderHook(() => useMetrics(params));

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        await act(async () => {
            result.current.refetch();
        });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('refetches when params change', async () => {
        mockApiGet.mockResolvedValue(makeMetricSeries());

        const { rerender } = renderHook(
            ({ params }) => useMetrics(params),
            {
                initialProps: {
                    params: {
                        probeName: 'probe1',
                        timeRange: '24h',
                    },
                },
            },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({
            params: {
                probeName: 'probe2',
                timeRange: '24h',
            },
        });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('does not flash loading on auto-refresh after initial load', async () => {
        mockApiGet.mockResolvedValue(makeMetricSeries());

        const params = {
            probeName: 'pg_stat_activity',
            timeRange: '24h',
        };

        const { result, rerender } = renderHook(() => useMetrics(params));

        // Wait for initial load
        await waitFor(() => {
            expect(result.current.data).not.toBeNull();
        });

        expect(result.current.loading).toBe(false);

        // Simulate refetch (after initial load)
        await act(async () => {
            result.current.refetch();
        });

        // Loading should not flash to true after initial load
        // (due to initialLoadDoneRef pattern)
        rerender();
        expect(result.current.loading).toBe(false);
    });
});

// ---------------------------------------------------------------------------
// Tests - useBaselines
// ---------------------------------------------------------------------------

describe('useBaselines', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('returns initial state when probeName is null', () => {
        const { result } = renderHook(() =>
            useBaselines(null, 1),
        );

        expect(result.current.baselines).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
    });

    it('returns initial state when connectionId is null', () => {
        const { result } = renderHook(() =>
            useBaselines('pg_stat_activity', null),
        );

        expect(result.current.baselines).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
    });

    it('fetches baselines when probeName and connectionId are provided', async () => {
        mockApiGet.mockResolvedValueOnce(makeBaselines());

        const { result } = renderHook(() =>
            useBaselines('pg_stat_activity', 1),
        );

        await waitFor(() => {
            expect(result.current.baselines).not.toBeNull();
        });

        expect(mockApiGet).toHaveBeenCalledTimes(1);
        expect(mockApiGet).toHaveBeenCalledWith(
            expect.stringContaining('/api/v1/metrics/baselines?'),
        );
        expect(result.current.baselines).toEqual(makeBaselines());
    });

    it('builds URL with metrics parameter', async () => {
        mockApiGet.mockResolvedValueOnce(makeBaselines());

        renderHook(() =>
            useBaselines('pg_stat_activity', 1, ['cpu_usage', 'memory']),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('probe_name=pg_stat_activity');
        expect(url).toContain('connection_id=1');
        expect(url).toContain('metrics=cpu_usage%2Cmemory');
    });

    it('sets loading to true during fetch', async () => {
        let resolvePromise: (value: unknown) => void;
        mockApiGet.mockImplementationOnce(() =>
            new Promise(resolve => {
                resolvePromise = resolve;
            }),
        );

        const { result } = renderHook(() =>
            useBaselines('pg_stat_activity', 1),
        );

        expect(result.current.loading).toBe(true);

        await act(async () => {
            resolvePromise!(makeBaselines());
        });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
    });

    it('sets error on API failure', async () => {
        mockApiGet.mockRejectedValueOnce(new Error('Baseline fetch failed'));

        const { result } = renderHook(() =>
            useBaselines('pg_stat_activity', 1),
        );

        await waitFor(() => {
            expect(result.current.error).toBe('Baseline fetch failed');
        });

        expect(result.current.baselines).toBeNull();
        expect(result.current.loading).toBe(false);
    });

    it('refetches when connectionId changes', async () => {
        mockApiGet.mockResolvedValue(makeBaselines());

        const { rerender } = renderHook(
            ({ connectionId }) => useBaselines('pg_stat_activity', connectionId),
            { initialProps: { connectionId: 1 } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ connectionId: 2 });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        const url = mockApiGet.mock.calls[1][0];
        expect(url).toContain('connection_id=2');
    });

    it('refetches when probeName changes', async () => {
        mockApiGet.mockResolvedValue(makeBaselines());

        const { rerender } = renderHook(
            ({ probeName }) => useBaselines(probeName, 1),
            { initialProps: { probeName: 'probe1' } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ probeName: 'probe2' });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        const url = mockApiGet.mock.calls[1][0];
        expect(url).toContain('probe_name=probe2');
    });
});
