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
import { renderHook, act } from '@testing-library/react';
import {
    useChartAnalysis,
    clearChartAnalysisCache,
    hasCachedAnalysis,
    type ChartAnalysisInput,
} from '../useChartAnalysis';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockSetAnalysis = vi.fn();
const mockSetLoading = vi.fn();
const mockSetError = vi.fn();
const mockSetProgressMessage = vi.fn();
const mockSetActiveTools = vi.fn();
const mockReset = vi.fn();

let mockAnalysis: string | null = null;
let mockLoading = false;
let mockError: string | null = null;

vi.mock('../useAnalysisState', () => ({
    useAnalysisState: () => ({
        state: {
            analysis: mockAnalysis,
            loading: mockLoading,
            error: mockError,
            progressMessage: 'Preparing chart data...',
            activeTools: [],
        },
        setAnalysis: mockSetAnalysis,
        setLoading: mockSetLoading,
        setError: mockSetError,
        setProgressMessage: mockSetProgressMessage,
        setActiveTools: mockSetActiveTools,
        reset: mockReset,
    }),
}));

const mockApiFetch = vi.fn();
const mockApiGet = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

vi.mock('../../utils/connectionContext', () => ({
    formatConnectionContext: vi.fn().mockReturnValue('Connection context'),
}));

vi.mock('../../utils/timelineEvents', () => ({
    fetchTimelineEventsForRange: vi.fn().mockResolvedValue(''),
}));

vi.mock('../../utils/textHelpers', () => ({
    stripPreamble: vi.fn((text: string) => text),
    djb2Hash: vi.fn((str: string) => `hash_${str.substring(0, 10)}`),
    ANALYSIS_CACHE_TTL_MS: 30 * 60 * 1000,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let testCounter = 0;

function makeChartInput(overrides: Partial<ChartAnalysisInput> = {}): ChartAnalysisInput {
    return {
        metricDescription: `CPU Usage ${testCounter}`,
        connectionId: 1,
        connectionName: 'Production DB',
        databaseName: 'testdb',
        timeRange: '24h',
        data: {
            series: [
                {
                    name: 'cpu_usage',
                    data: [10, 20, 30, 40, 50],
                },
            ],
            categories: [
                '2024-01-01T00:00:00Z',
                '2024-01-01T06:00:00Z',
                '2024-01-01T12:00:00Z',
                '2024-01-01T18:00:00Z',
                '2024-01-02T00:00:00Z',
            ],
        },
        ...overrides,
    };
}

function makeSuccessResponse(text = 'Chart analysis complete.') {
    return {
        ok: true,
        text: vi.fn().mockResolvedValue(''),
        json: vi.fn().mockResolvedValue({
            content: [{ type: 'text', text }],
        }),
    };
}

function makeErrorResponse(errorText = 'Internal Server Error') {
    return {
        ok: false,
        text: vi.fn().mockResolvedValue(errorText),
        json: vi.fn(),
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useChartAnalysis', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        testCounter++;
        mockAnalysis = null;
        mockLoading = false;
        mockError = null;
        clearChartAnalysisCache();
        mockApiFetch.mockResolvedValue(makeSuccessResponse());
        mockApiGet.mockResolvedValue({});
    });

    it('returns initial state with null analysis', () => {
        const { result } = renderHook(() => useChartAnalysis());

        expect(result.current.analysis).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.analyze).toBe('function');
        expect(typeof result.current.reset).toBe('function');
    });

    it('analyze sets loading to true and clears previous state', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockSetLoading).toHaveBeenCalledWith(true);
        expect(mockSetError).toHaveBeenCalledWith(null);
        expect(mockSetAnalysis).toHaveBeenCalledWith(null);
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Preparing chart data...');
    });

    it('analyze calls LLM API with correct parameters', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/llm/chat',
            expect.objectContaining({
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
            }),
        );

        const callArgs = mockApiFetch.mock.calls[0];
        const body = JSON.parse(callArgs[1].body);

        expect(body.system).toContain('PostgreSQL database expert');
        expect(body.messages).toHaveLength(1);
        expect(body.messages[0].role).toBe('user');
    });

    it('analyze includes chart data in user message', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        const userMessage = body.messages[0].content;

        expect(userMessage).toContain('CPU Usage');
        expect(userMessage).toContain('Connection: Production DB');
        expect(userMessage).toContain('Database: testdb');
        expect(userMessage).toContain('Time Range: 24h');
        expect(userMessage).toContain('Series "cpu_usage"');
    });

    it('analyze includes series statistics in user message', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        const input = makeChartInput({
            data: {
                series: [
                    {
                        name: 'test_metric',
                        data: [10, 20, 30, 40, 50],
                    },
                ],
            },
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        const userMessage = body.messages[0].content;

        expect(userMessage).toContain('Min: 10');
        expect(userMessage).toContain('Max: 50');
        expect(userMessage).toContain('Avg: 30.00');
        expect(userMessage).toContain('Latest: 50');
    });

    it('analyze sets analysis result on success', async () => {
        const analysisText = 'The chart shows normal patterns.';
        mockApiFetch.mockResolvedValueOnce(makeSuccessResponse(analysisText));

        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockSetAnalysis).toHaveBeenCalledWith(analysisText);
    });

    it('analyze sets error on API failure', async () => {
        mockApiFetch.mockResolvedValueOnce(makeErrorResponse('Service Unavailable'));

        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockSetError).toHaveBeenCalledWith(
            expect.stringContaining('Service Unavailable'),
        );
    });

    it('analyze sets error when apiFetch throws', async () => {
        mockApiFetch.mockRejectedValueOnce(new Error('Network error'));

        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockSetError).toHaveBeenCalledWith('Network error');
    });

    it('analyze sets loading to false in finally block', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput());
        });

        expect(mockSetLoading).toHaveBeenLastCalledWith(false);
    });

    it('uses cache on second call with same parameters', async () => {
        const analysisText = 'Cached result';
        mockApiFetch.mockResolvedValue(makeSuccessResponse(analysisText));

        const { result } = renderHook(() => useChartAnalysis());

        const input = makeChartInput({ metricDescription: 'cached_metric' });

        // First call
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(1);

        // Second call with same input
        await act(async () => {
            await result.current.analyze(input);
        });

        // Should use cache, not call API again
        expect(mockApiFetch).toHaveBeenCalledTimes(1);
        expect(mockSetAnalysis).toHaveBeenLastCalledWith(analysisText);
    });

    it('clearChartAnalysisCache clears the cache', async () => {
        mockApiFetch.mockResolvedValue(makeSuccessResponse('Analysis'));

        const { result } = renderHook(() => useChartAnalysis());

        const input = makeChartInput({ metricDescription: 'clear_test' });

        // First call
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(1);

        // Clear cache
        clearChartAnalysisCache();

        // Second call should hit API again
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(2);
    });

    it('hasCachedAnalysis returns true when cache exists', async () => {
        mockApiFetch.mockResolvedValue(makeSuccessResponse());

        const { result } = renderHook(() => useChartAnalysis());

        const metricDescription = 'cache_check_metric';
        const input = makeChartInput({
            metricDescription,
            connectionId: 99,
            databaseName: 'checkdb',
            timeRange: '1h',
        });

        // Before analysis - no cache
        expect(hasCachedAnalysis(metricDescription, 99, 'checkdb', '1h')).toBe(false);

        // Analyze
        await act(async () => {
            await result.current.analyze(input);
        });

        // After analysis - should have cache
        expect(hasCachedAnalysis(metricDescription, 99, 'checkdb', '1h')).toBe(true);
    });

    it('reset calls the reset function from useAnalysisState', () => {
        const { result } = renderHook(() => useChartAnalysis());

        act(() => {
            result.current.reset();
        });

        expect(mockReset).toHaveBeenCalledTimes(1);
    });

    it('handles empty series gracefully', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        const input = makeChartInput({
            data: {
                series: [
                    { name: 'empty_series', data: [] },
                ],
            },
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        expect(body.messages[0].content).toContain('No data points');
    });

    it('fetches connection context when connectionId is provided', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput({ connectionId: 42 }));
        });

        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/42/context');
    });

    it('does not fetch connection context when connectionId is undefined', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput({ connectionId: undefined }));
        });

        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('updates progress messages during analysis', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput({ connectionId: 1 }));
        });

        // Should have progress messages
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Preparing chart data...');
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Analyzing data...');
    });

    it('sets activeTools during fetch phases', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        await act(async () => {
            await result.current.analyze(makeChartInput({ connectionId: 1 }));
        });

        expect(mockSetActiveTools).toHaveBeenCalledWith(['Preparing chart data']);
        expect(mockSetActiveTools).toHaveBeenCalledWith([
            'Fetching server context',
            'Fetching timeline events',
        ]);
        expect(mockSetActiveTools).toHaveBeenCalledWith(['Analyzing data']);
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('handles multiple series in chart data', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        const input = makeChartInput({
            data: {
                series: [
                    { name: 'series1', data: [1, 2, 3] },
                    { name: 'series2', data: [4, 5, 6] },
                ],
                categories: ['a', 'b', 'c'],
            },
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        const body = JSON.parse(mockApiFetch.mock.calls[0][1].body);
        const userMessage = body.messages[0].content;

        expect(userMessage).toContain('Series "series1"');
        expect(userMessage).toContain('Series "series2"');
    });

    it('handles missing optional fields in input', async () => {
        const { result } = renderHook(() => useChartAnalysis());

        const input: ChartAnalysisInput = {
            metricDescription: 'Minimal metric',
            data: {
                series: [{ name: 'test', data: [1, 2, 3] }],
            },
        };

        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiFetch).toHaveBeenCalledTimes(1);
        // Should not throw and should call API
    });
});
