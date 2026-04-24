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
    useQueryAnalysis,
    QueryAnalysisInput,
    clearQueryAnalysisCache,
    hasCachedQueryAnalysis,
} from '../useQueryAnalysis';

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
            progressMessage: 'Preparing query data...',
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

const mockRunAgenticLoop = vi.fn();
vi.mock('../../utils/agenticLoop', () => ({
    runAgenticLoop: (...args: unknown[]) => mockRunAgenticLoop(...args),
}));

const mockApiGet = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

const mockGetKnowledgebaseTool = vi.fn();
vi.mock('../../utils/mcpTools', () => ({
    getKnowledgebaseTool: (...args: unknown[]) =>
        mockGetKnowledgebaseTool(...args),
    // Re-export the type so the import in the source module resolves
    AnalysisTool: undefined,
}));

vi.mock('../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({
        aiEnabled: true,
        maxIterations: 10,
        loading: false,
    }),
}));

const mockFetchTimelineEventsForRange = vi.fn();
vi.mock('../../utils/timelineEvents', () => ({
    fetchTimelineEventsForRange: (...args: unknown[]) =>
        mockFetchTimelineEventsForRange(...args),
}));

const mockFormatConnectionContext = vi.fn();
vi.mock('../../utils/connectionContext', () => ({
    formatConnectionContext: (...args: unknown[]) =>
        mockFormatConnectionContext(...args),
}));

vi.mock('../../utils/analysisTools', () => ({
    QUERY_ANALYSIS_TOOLS: [
        { name: 'get_schema', description: 'Get schema info' },
    ],
}));

vi.mock('../../utils/analysisPrompts', () => ({
    SQL_CODE_BLOCK_RULES: '',
    SQL_PLACEHOLDER_RULES: '',
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeInput(
    overrides: Partial<QueryAnalysisInput> = {},
): QueryAnalysisInput {
    return {
        queryText: 'SELECT * FROM orders WHERE customer_id = $1',
        queryId: 'def456',
        calls: 5000,
        totalExecTime: 25000,
        meanExecTime: 5.0,
        rows: 5000,
        sharedBlksHit: 4500,
        sharedBlksRead: 500,
        connectionId: 1,
        databaseName: 'testdb',
        ...overrides,
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useQueryAnalysis', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        clearQueryAnalysisCache();
        mockAnalysis = null;
        mockLoading = false;
        mockError = null;
        mockRunAgenticLoop.mockResolvedValue('## Summary\nAnalysis result.');
        mockGetKnowledgebaseTool.mockResolvedValue(null);
        mockApiGet.mockResolvedValue({});
        mockFormatConnectionContext.mockReturnValue('');
        mockFetchTimelineEventsForRange.mockResolvedValue('');
    });

    it('returns initial state with null analysis and not loading', () => {
        const { result } = renderHook(() => useQueryAnalysis());

        expect(result.current.analysis).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(result.current.progressMessage).toBe(
            'Preparing query data...',
        );
        expect(result.current.activeTools).toEqual([]);
    });

    it('analyze() triggers the agentic loop', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);
        expect(mockRunAgenticLoop).toHaveBeenCalledWith(
            expect.objectContaining({
                messages: expect.arrayContaining([
                    expect.objectContaining({ role: 'user' }),
                ]),
                systemPrompt: expect.stringContaining(
                    'PostgreSQL query optimization expert',
                ),
                maxIterations: 10,
            }),
        );
    });

    it('sets analysis text after successful agentic loop', async () => {
        const analysisText = '## Summary\nQuery is well-optimized.';
        mockRunAgenticLoop.mockResolvedValue(analysisText);

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockSetAnalysis).toHaveBeenCalledWith(analysisText);
    });

    it('sets loading to true then false during analysis', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockSetLoading).toHaveBeenCalledWith(true);
        expect(mockSetLoading).toHaveBeenCalledWith(false);

        // true should be called before false
        const loadingCalls = mockSetLoading.mock.calls.map(
            (c: unknown[]) => c[0],
        );
        expect(loadingCalls.indexOf(true)).toBeLessThan(
            loadingCalls.lastIndexOf(false),
        );
    });

    it('calls setError when the agentic loop throws', async () => {
        mockRunAgenticLoop.mockRejectedValue(
            new Error('LLM service unavailable'),
        );

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockSetError).toHaveBeenCalledWith('LLM service unavailable');
        expect(mockSetActiveTools).toHaveBeenCalledWith([]);
    });

    it('uses cache for repeated calls with the same input', async () => {
        const input = makeInput();
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        // Second call with the same input should use cache
        mockRunAgenticLoop.mockClear();
        mockSetAnalysis.mockClear();

        await act(async () => {
            await result.current.analyze(input);
        });

        expect(mockRunAgenticLoop).not.toHaveBeenCalled();
        expect(mockSetAnalysis).toHaveBeenCalledWith(
            '## Summary\nAnalysis result.',
        );
    });

    it('does not use cache for different queryId', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput({ queryId: 'q1' }));
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(1);

        await act(async () => {
            await result.current.analyze(makeInput({ queryId: 'q2' }));
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(2);
    });

    it('does not use cache for different connectionId', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput({ connectionId: 1 }));
        });

        await act(async () => {
            await result.current.analyze(makeInput({ connectionId: 2 }));
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(2);
    });

    it('does not use cache for different databaseName', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput({ databaseName: 'db1' }));
        });

        await act(async () => {
            await result.current.analyze(makeInput({ databaseName: 'db2' }));
        });

        expect(mockRunAgenticLoop).toHaveBeenCalledTimes(2);
    });

    it('reset() delegates to the analysis state reset', () => {
        const { result } = renderHook(() => useQueryAnalysis());

        result.current.reset();

        expect(mockReset).toHaveBeenCalledTimes(1);
    });

    it('fetches connection context and timeline events in parallel', async () => {
        mockApiGet.mockResolvedValue({ server: 'pg-1' });
        mockFormatConnectionContext.mockReturnValue('\nServer: pg-1');
        mockFetchTimelineEventsForRange.mockResolvedValue(
            '\nTimeline: event1',
        );

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockApiGet).toHaveBeenCalledWith(
            '/api/v1/connections/1/context',
        );
        expect(mockFetchTimelineEventsForRange).toHaveBeenCalledWith(
            1,
            undefined,
        );
    });

    it('includes knowledgebase tool when available', async () => {
        const kbTool = {
            name: 'search_knowledgebase',
            description: 'Search KB',
        };
        mockGetKnowledgebaseTool.mockResolvedValue(kbTool);

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        const loopArgs = mockRunAgenticLoop.mock.calls[0][0];
        expect(loopArgs.tools).toContainEqual(kbTool);
    });

    it('sets progress messages during analysis', async () => {
        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(makeInput());
        });

        expect(mockSetProgressMessage).toHaveBeenCalledWith(
            'Preparing query data...',
        );
        expect(mockSetProgressMessage).toHaveBeenCalledWith(
            'Starting analysis...',
        );
    });
});

describe('hasCachedQueryAnalysis', () => {
    beforeEach(() => {
        clearQueryAnalysisCache();
    });

    it('returns false when no cache entry exists', () => {
        expect(hasCachedQueryAnalysis('q1', 1, 'testdb')).toBe(false);
    });

    it('returns true after an analysis has been cached', async () => {
        mockRunAgenticLoop.mockResolvedValue('Cached analysis.');
        mockGetKnowledgebaseTool.mockResolvedValue(null);
        mockApiGet.mockResolvedValue({});
        mockFormatConnectionContext.mockReturnValue('');
        mockFetchTimelineEventsForRange.mockResolvedValue('');

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(
                makeInput({
                    queryId: 'cached-q',
                    connectionId: 42,
                    databaseName: 'mydb',
                }),
            );
        });

        expect(hasCachedQueryAnalysis('cached-q', 42, 'mydb')).toBe(true);
    });
});

describe('clearQueryAnalysisCache', () => {
    beforeEach(() => {
        clearQueryAnalysisCache();
    });

    it('removes all cached entries', async () => {
        mockRunAgenticLoop.mockResolvedValue('Analysis.');
        mockGetKnowledgebaseTool.mockResolvedValue(null);
        mockApiGet.mockResolvedValue({});
        mockFormatConnectionContext.mockReturnValue('');
        mockFetchTimelineEventsForRange.mockResolvedValue('');

        const { result } = renderHook(() => useQueryAnalysis());

        await act(async () => {
            await result.current.analyze(
                makeInput({
                    queryId: 'clear-q',
                    connectionId: 10,
                    databaseName: 'cleardb',
                }),
            );
        });

        expect(hasCachedQueryAnalysis('clear-q', 10, 'cleardb')).toBe(true);

        clearQueryAnalysisCache();

        expect(hasCachedQueryAnalysis('clear-q', 10, 'cleardb')).toBe(false);
    });
});
