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
    useServerAnalysis,
    clearAnalysisCache,
    hasCachedServerAnalysis,
} from '../useServerAnalysis';
import type { ServerSelection, ClusterSelection } from '../../types/selection';

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

vi.mock('../../contexts/AICapabilitiesContext', () => ({
    useAICapabilities: () => ({ maxIterations: mockMaxIterations }),
}));

const mockApiGet = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

vi.mock('../../utils/connectionContext', () => ({
    formatConnectionContext: vi.fn().mockReturnValue('Connection context'),
}));

vi.mock('../../utils/mcpTools', () => ({
    getKnowledgebaseTool: vi.fn().mockResolvedValue(null),
}));

vi.mock('../../utils/analysisTools', () => ({
    SERVER_ANALYSIS_TOOLS: [],
}));

const mockRunAgenticLoop = vi.fn();

vi.mock('../../utils/agenticLoop', () => ({
    runAgenticLoop: (...args: unknown[]) => mockRunAgenticLoop(...args),
}));

vi.mock('../../utils/timelineEvents', () => ({
    fetchTimelineEventsCentered: vi.fn().mockResolvedValue(''),
}));

vi.mock('../../utils/textHelpers', () => ({
    ANALYSIS_CACHE_TTL_MS: 30 * 60 * 1000,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let testCounter = 0;

function makeServerInput(overrides: Partial<ServerSelection> = {}): ServerSelection {
    return {
        type: 'server',
        id: testCounter,
        name: `Test Server ${testCounter}`,
        status: 'online',
        description: '',
        host: 'localhost',
        port: 5432,
        role: 'primary',
        version: '16',
        database: 'postgres',
        username: 'postgres',
        os: 'linux',
        platform: 'x86_64',
        ...overrides,
    };
}

function makeClusterInput(overrides: Partial<ClusterSelection> = {}): ClusterSelection {
    return {
        type: 'cluster',
        id: `cluster-${testCounter}`,
        name: `Test Cluster ${testCounter}`,
        status: 'online',
        description: '',
        servers: [
            { id: 1, name: 'Primary' },
            { id: 2, name: 'Replica' },
        ] as ClusterSelection['servers'],
        serverIds: [1, 2],
        ...overrides,
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useServerAnalysis', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        testCounter++;
        mockAnalysis = null;
        mockLoading = false;
        mockError = null;
        clearAnalysisCache();
        mockRunAgenticLoop.mockResolvedValue('Server analysis complete.');
        mockApiGet.mockResolvedValue({});
    });

    it('returns initial state with null analysis', () => {
        const { result } = renderHook(() => useServerAnalysis());

        expect(result.current.analysis).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(typeof result.current.analyze).toBe('function');
        expect(typeof result.current.reset).toBe('function');
    });

    it('analyze sets loading to true and clears previous state', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        expect(mockSetLoading).toHaveBeenCalledWith(true);
        expect(mockSetError).toHaveBeenCalledWith(null);
        expect(mockSetAnalysis).toHaveBeenCalledWith(null);
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Gathering context...');
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('analyze calls runAgenticLoop with correct parameters for server', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        const input = makeServerInput({ id: 42, name: 'Production Server' });

        await act(async () => {
            await result.current.analyze(input);
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
        expect(callArgs.messages[0].content).toContain('Production Server');
        expect(callArgs.messages[0].content).toContain('Connection ID: 42');
        expect(callArgs.systemPrompt).toContain('PostgreSQL database expert');
    });

    it('analyze includes cluster addendum in system prompt for clusters', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeClusterInput());
        });

        const callArgs = mockRunAgenticLoop.mock.calls[0][0];
        expect(callArgs.systemPrompt).toContain('CLUSTER analysis');
        expect(callArgs.systemPrompt).toContain('connection_id');
    });

    it('analyze sets analysis result on success', async () => {
        mockRunAgenticLoop.mockResolvedValueOnce('The server is healthy.');

        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        expect(mockSetAnalysis).toHaveBeenCalledWith('The server is healthy.');
    });

    it('analyze sets error on runAgenticLoop failure', async () => {
        mockRunAgenticLoop.mockRejectedValueOnce(new Error('Analysis failed'));

        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        expect(mockSetError).toHaveBeenCalledWith('Analysis failed');
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('analyze sets loading to false in finally block', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        expect(mockSetLoading).toHaveBeenLastCalledWith(false);
    });

    it('uses cache on second call with same server id', async () => {
        mockRunAgenticLoop.mockResolvedValue('Cached analysis');

        const { result } = renderHook(() => useServerAnalysis());

        const input = makeServerInput({ type: 'server', id: 100, name: 'Cache Test' });

        // First call
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        // Second call with same input
        await act(async () => {
            await result.current.analyze(input);
        });

        // Should use cache
        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
        expect(mockSetAnalysis).toHaveBeenLastCalledWith('Cached analysis');
    });

    it('clearAnalysisCache clears the cache', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useServerAnalysis());

        const input = makeServerInput({ id: 200 });

        // First call
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        // Clear cache
        clearAnalysisCache();

        // Second call should hit runAgenticLoop again
        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(2);
    });

    it('hasCachedServerAnalysis returns true when cache exists', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useServerAnalysis());

        // Before analysis - no cache
        expect(hasCachedServerAnalysis('server', 300)).toBe(false);

        // Analyze
        await act(async () => {
            await result.current.analyze(makeServerInput({ type: 'server', id: 300 }));
        });

        // After analysis - should have cache
        expect(hasCachedServerAnalysis('server', 300)).toBe(true);
    });

    it('hasCachedServerAnalysis returns false for different id', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput({ type: 'server', id: 400 }));
        });

        expect(hasCachedServerAnalysis('server', 400)).toBe(true);
        expect(hasCachedServerAnalysis('server', 401)).toBe(false);
    });

    it('hasCachedServerAnalysis returns false for different type', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput({ type: 'server', id: 500 }));
        });

        expect(hasCachedServerAnalysis('server', 500)).toBe(true);
        expect(hasCachedServerAnalysis('cluster', 500)).toBe(false);
    });

    it('reset calls the reset function from useAnalysisState', () => {
        const { result } = renderHook(() => useServerAnalysis());

        act(() => {
            result.current.reset();
        });

        expect(mockReset).toHaveBeenCalledTimes(1);
    });

    it('fetches connection context for server analysis', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput({ id: 55 }));
        });

        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/55/context');
    });

    it('fetches connection context for all servers in cluster', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        const input = makeClusterInput({
            servers: [
                { id: 10, name: 'Server A' },
                { id: 20, name: 'Server B' },
                { id: 30, name: 'Server C' },
            ],
            serverIds: [10, 20, 30],
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/10/context');
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/20/context');
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/30/context');
    });

    it('includes server list in cluster analysis user message', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        const input = makeClusterInput({
            name: 'Production Cluster',
            servers: [
                { id: 1, name: 'Primary' },
                { id: 2, name: 'Replica' },
            ],
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        const callArgs = mockRunAgenticLoop.mock.calls[0][0];
        const userMessage = callArgs.messages[0].content;

        expect(userMessage).toContain('Cluster Name: Production Cluster');
        expect(userMessage).toContain('Primary (connection_id: 1)');
        expect(userMessage).toContain('Replica (connection_id: 2)');
    });

    it('handles empty servers array for cluster', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        const input = makeClusterInput({
            servers: [],
            serverIds: [],
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        // Should not throw and should call runAgenticLoop
        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
    });

    it('handles serverIds without servers array', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        const input = makeClusterInput({
            id: 'cluster-test',
            name: 'Test Cluster',
            servers: [],
            serverIds: [1, 2],
        });

        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/1/context');
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/2/context');
    });

    it('updates progress message before starting analysis', async () => {
        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        expect(mockSetProgressMessage).toHaveBeenCalledWith('Gathering context...');
        expect(mockSetProgressMessage).toHaveBeenCalledWith('Starting analysis...');
    });

    it('handles apiGet failure gracefully', async () => {
        mockApiGet.mockRejectedValue(new Error('Context fetch failed'));
        mockRunAgenticLoop.mockResolvedValue('Analysis without context');

        const { result } = renderHook(() => useServerAnalysis());

        await act(async () => {
            await result.current.analyze(makeServerInput());
        });

        // Should still complete analysis even if context fetch fails
        expect(mockSetAnalysis).toHaveBeenCalledWith('Analysis without context');
        // setError(null) is called during initialization, but no error message should be set
        const errorCalls = mockSetError.mock.calls.filter(call => call[0] !== null);
        expect(errorCalls).toHaveLength(0);
    });

    it('caches server and cluster analyses separately', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis');

        const { result } = renderHook(() => useServerAnalysis());

        // Analyze as server
        await act(async () => {
            await result.current.analyze(makeServerInput({ type: 'server', id: 1 }));
        });

        expect(hasCachedServerAnalysis('server', 1)).toBe(true);
        expect(hasCachedServerAnalysis('cluster', 1)).toBe(false);

        // Analyze as cluster with same id
        await act(async () => {
            await result.current.analyze(makeClusterInput({ id: '1' }));
        });

        expect(hasCachedServerAnalysis('server', 1)).toBe(true);
        expect(hasCachedServerAnalysis('cluster', '1')).toBe(true);
    });
});
