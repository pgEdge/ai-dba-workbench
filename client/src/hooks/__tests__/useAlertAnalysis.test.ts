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
    useAlertAnalysis,
    clearAlertAnalysisCache,
    type AlertInput,
} from '../useAlertAnalysis';

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
            progressMessage: 'Gathering context...',
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

const mockMaxIterations = 10;

vi.mock('../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ maxIterations: mockMaxIterations }),
}));

const mockApiGet = vi.fn();
const mockApiPut = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
}));

vi.mock('../../utils/connectionContext', () => ({
    formatConnectionContext: vi.fn().mockReturnValue('Connection context'),
}));

vi.mock('../../utils/mcpTools', () => ({
    getKnowledgebaseTool: vi.fn().mockResolvedValue(null),
}));

vi.mock('../../utils/analysisTools', () => ({
    ALERT_ANALYSIS_TOOLS: [],
}));

const mockRunAgenticLoop = vi.fn();

vi.mock('../../utils/agenticLoop', () => ({
    runAgenticLoop: (...args: unknown[]) => mockRunAgenticLoop(...args),
}));

vi.mock('../../utils/timelineEvents', () => ({
    fetchTimelineEventsCentered: vi.fn().mockResolvedValue(''),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let testCounter = 0;

function makeAlertInput(overrides: Partial<AlertInput> = {}): AlertInput {
    return {
        id: testCounter,
        alertType: 'threshold',
        severity: 'warning',
        title: 'High CPU Usage',
        description: 'CPU usage exceeded threshold',
        metricName: 'cpu_usage',
        metricValue: 85.5,
        operator: '>',
        thresholdValue: 80,
        connectionId: 1,
        triggeredAt: '2024-01-01T12:00:00Z',
        ...overrides,
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useAlertAnalysis', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        testCounter++;
        mockAnalysis = null;
        mockLoading = false;
        mockError = null;
        clearAlertAnalysisCache();
        mockRunAgenticLoop.mockResolvedValue('Analysis complete.');
        mockApiGet.mockResolvedValue({});
        mockApiPut.mockResolvedValue({});
    });

    it('returns initial state with null analysis', () => {
        const { result } = renderHook(() => useAlertAnalysis());

        expect(result.current.analysis).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.analyze).toBe('function');
        expect(typeof result.current.reset).toBe('function');
    });

    it('analyze sets loading to true and clears previous state', async () => {
        const { result } = renderHook(() => useAlertAnalysis());

        await act(async () => {
            await result.current.analyze(makeAlertInput());
        });

        expect(mockSetLoading).toHaveBeenCalledWith(true);
        expect(mockSetError).toHaveBeenCalledWith(null);
        expect(mockSetAnalysis).toHaveBeenCalledWith(null);
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Gathering context...');
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('analyze calls runAgenticLoop with correct parameters', async () => {
        const { result } = renderHook(() => useAlertAnalysis());
        const alert = makeAlertInput();

        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
        expect(mockRunAgenticLoop).toHaveBeenCalledWith(
            expect.objectContaining({
                maxIterations: mockMaxIterations,
                onActiveTools: mockSetActiveTools,
                onProgress: mockSetProgressMessage,
            }),
        );

        const callArgs = mockRunAgenticLoop.mock.calls[0][0];
        expect(callArgs.messages).toHaveLength(1);
        expect(callArgs.messages[0].role).toBe('user');
        expect(callArgs.messages[0].content).toContain('High CPU Usage');
        expect(callArgs.systemPrompt).toContain('PostgreSQL database expert');
    });

    it('analyze sets analysis result on success', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('The alert analysis result.');

        const { result } = renderHook(() => useAlertAnalysis());

        await act(async () => {
            await result.current.analyze(makeAlertInput());
        });

        expect(mockSetAnalysis).toHaveBeenCalledWith('The alert analysis result.');
    });

    it('analyze saves result to server when alert has id', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('Analysis text');

        const { result } = renderHook(() => useAlertAnalysis());
        const alert = makeAlertInput({ id: 42, metricValue: 90 });

        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockApiPut).toHaveBeenCalledWith(
            '/api/v1/alerts/analysis',
            expect.objectContaining({
                alert_id: 42,
                analysis: 'Analysis text',
                metric_value: 90,
            }),
        );
    });

    it('analyze does not save to server when alert has no id', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('Analysis text');

        const { result } = renderHook(() => useAlertAnalysis());
        const alert = makeAlertInput({ id: undefined });

        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('analyze sets error on runAgenticLoop failure', async () => {
        mockRunAgenticLoop.mockRejectedValueOnce(new Error('LLM unavailable'));

        const { result } = renderHook(() => useAlertAnalysis());

        await act(async () => {
            await result.current.analyze(makeAlertInput());
        });

        expect(mockSetError).toHaveBeenCalledWith('LLM unavailable');
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('analyze sets loading to false in finally block', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('Result');

        const { result } = renderHook(() => useAlertAnalysis());

        await act(async () => {
            await result.current.analyze(makeAlertInput());
        });

        // setLoading(false) should be called at the end
        expect(mockSetLoading).toHaveBeenLastCalledWith(false);
    });

    it('uses server-side cache when aiAnalysis is present and metric is close', async () => {
        const { result } = renderHook(() => useAlertAnalysis());

        const alert = makeAlertInput({
            aiAnalysis: 'Cached server analysis',
            aiAnalysisMetricValue: 85,
            metricValue: 86, // Within 10% of 85
        });

        await act(async () => {
            await result.current.analyze(alert);
        });

        // Should use cached analysis without calling runAgenticLoop
        expect(mockRunAgenticLoop).not.toHaveBeenCalled();
        expect(mockSetAnalysis).toHaveBeenCalledWith('Cached server analysis');
        expect(mockSetLoading).toHaveBeenLastCalledWith(false);
    });

    it('does not use server cache when metric value differs by more than 10%', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('Fresh analysis');

        const { result } = renderHook(() => useAlertAnalysis());

        const alert = makeAlertInput({
            aiAnalysis: 'Stale analysis',
            aiAnalysisMetricValue: 50,
            metricValue: 90, // More than 10% different from 50
        });

        await act(async () => {
            await result.current.analyze(alert);
        });

        // Should call runAgenticLoop because cached value is stale
        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
        expect(mockSetAnalysis).toHaveBeenCalledWith('Fresh analysis');
    });

    it('uses client-side cache on second call with same alert id', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('First analysis');

        const { result } = renderHook(() => useAlertAnalysis());

        const alert = makeAlertInput({ id: 100, metricValue: 80 });

        // First call
        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        // Second call with same alert and similar metric value
        const alert2 = makeAlertInput({ id: 100, metricValue: 82 }); // Within 10%

        await act(async () => {
            await result.current.analyze(alert2);
        });

        // Should use cached analysis
        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
        expect(mockSetAnalysis).toHaveBeenLastCalledWith('First analysis');
    });

    it('clearAlertAnalysisCache clears the client cache', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useAlertAnalysis());

        const alert = makeAlertInput({ id: 200, metricValue: 50 });

        // First call - should use runAgenticLoop
        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        // Clear cache
        clearAlertAnalysisCache();

        // Second call - should use runAgenticLoop again
        await act(async () => {
            await result.current.analyze(alert);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(2);
    });

    it('reset calls the reset function from useAnalysisState', () => {
        const { result } = renderHook(() => useAlertAnalysis());

        act(() => {
            result.current.reset();
        });

        expect(mockReset).toHaveBeenCalledTimes(1);
    });

    it('includes alert details in user message', async () => {
        const { result } = renderHook(() => useAlertAnalysis());

        const alert = makeAlertInput({
            alertType: 'anomaly',
            severity: 'critical',
            title: 'Memory pressure',
            description: 'Unusual memory pattern',
            metricName: 'memory_usage',
            metricValue: 95,
            operator: '>=',
            thresholdValue: 90,
            connectionId: 5,
            triggeredAt: '2024-06-15T08:30:00Z',
        });

        await act(async () => {
            await result.current.analyze(alert);
        });

        const callArgs = mockRunAgenticLoop.mock.calls[0][0];
        const userMessage = callArgs.messages[0].content;

        expect(userMessage).toContain('Alert Type: anomaly');
        expect(userMessage).toContain('Severity: critical');
        expect(userMessage).toContain('Title: Memory pressure');
        expect(userMessage).toContain('Description: Unusual memory pattern');
        expect(userMessage).toContain('Metric: memory_usage');
        expect(userMessage).toContain('Current Value: 95');
        expect(userMessage).toContain('Threshold: >= 90');
        expect(userMessage).toContain('Connection ID: 5');
        expect(userMessage).toContain('Triggered At: 2024-06-15T08:30:00Z');
    });

    it('handles missing optional fields gracefully', async () => {
        const { result } = renderHook(() => useAlertAnalysis());

        const alert: AlertInput = {
            severity: 'warning',
            title: 'Minimal alert',
            connectionId: 1,
        };

        await act(async () => {
            await result.current.analyze(alert);
        });

        const callArgs = mockRunAgenticLoop.mock.calls[0][0];
        const userMessage = callArgs.messages[0].content;

        expect(userMessage).toContain('Description: N/A');
        expect(userMessage).toContain('Current Value: N/A');
    });

    it('fetches connection context', async () => {
        const { result } = renderHook(() => useAlertAnalysis());

        await act(async () => {
            await result.current.analyze(makeAlertInput({ connectionId: 7 }));
        });

        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/7/context');
    });

    it('handles apiPut failure silently', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('Analysis');
        mockApiPut.mockRejectedValueOnce(new Error('Save failed'));

        const { result } = renderHook(() => useAlertAnalysis());

        // Should not throw
        await act(async () => {
            await result.current.analyze(makeAlertInput({ id: 1 }));
        });

        expect(mockSetAnalysis).toHaveBeenCalledWith('Analysis');
        expect(mockSetError).not.toHaveBeenCalledWith('Save failed');
    });
});
