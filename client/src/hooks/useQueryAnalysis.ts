/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback } from 'react';
import { useAICapabilities } from '../contexts/AICapabilitiesContext';
import { apiGet } from '../utils/apiClient';
import { formatTime } from '../utils/formatters';
import { formatConnectionContext } from '../utils/connectionContext';
import { getKnowledgebaseTool, AnalysisTool } from '../utils/mcpTools';
import { QUERY_ANALYSIS_TOOLS } from '../utils/analysisTools';
import {
    SQL_CODE_BLOCK_RULES,
    SQL_PLACEHOLDER_RULES,
} from '../utils/analysisPrompts';
import { runAgenticLoop } from '../utils/agenticLoop';
import { fetchTimelineEventsForRange } from '../utils/timelineEvents';
import { LLMContentBlock, ToolResult } from '../types/llm';
import { useAnalysisState } from './useAnalysisState';

export interface QueryAnalysisInput {
    queryText: string;
    queryId: string;
    calls: number;
    totalExecTime: number;
    meanExecTime: number;
    rows: number;
    sharedBlksHit: number;
    sharedBlksRead: number;
    connectionId: number;
    databaseName: string;
}

export interface UseQueryAnalysisReturn {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    analyze: (input: QueryAnalysisInput) => Promise<void>;
    reset: () => void;
}

interface Message {
    role: string;
    content: string | LLMContentBlock[] | ToolResult[];
}

// Module-level cache for query analysis results (persists across component mounts)
const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes
const analysisCache = new Map<string, { analysis: string; timestamp: number }>();

export function clearQueryAnalysisCache(): void {
    analysisCache.clear();
}

const QUERY_ANALYSIS_SYSTEM_PROMPT = `You are a PostgreSQL query optimization expert analyzing query performance data from the pgEdge AI DBA Workbench monitoring system.

Your task is to:
1. Assess the query's performance characteristics based on the provided statistics
2. Use tools to gather additional context (schema info, execution plans, metric baselines)
3. Identify potential performance issues based on execution stats and I/O patterns
4. Suggest specific optimizations with validated SQL
5. If timeline events are provided, correlate them with observed performance changes

Structure your response as:

## Summary
Brief assessment of query performance.

## Performance Analysis
Analysis of execution stats, I/O patterns, timing.

## Optimization Opportunities
Specific suggestions for improving the query.

## Recommendations
Numbered list of actionable steps.
${SQL_CODE_BLOCK_RULES}${SQL_PLACEHOLDER_RULES}

Keep responses concise and actionable. Do not offer to perform additional actions, run further queries, or investigate anything else. Do not ask follow-up questions or ask what the user would like to do next. Begin your response directly with the first section heading (## Summary). Do not include any introductory text, preamble, or conversational remarks before the report. Your analysis is displayed in a read-only report that the user cannot respond to.

If a search_knowledgebase tool is available, use it to look up unfamiliar PostgreSQL features, extensions, or pgEdge-specific concepts before making recommendations. If a test_query tool is available, validate all SQL queries before including them in the report.`;

/**
 * Compute a djb2 hash of the given string and return it as a string.
 */
function djb2Hash(str: string): string {
    let hash = 5381;
    for (let i = 0; i < str.length; i++) {
        hash = ((hash << 5) + hash + str.charCodeAt(i)) | 0;
    }
    return String(hash >>> 0);
}

/**
 * Compute a cache key from stable identifiers: query ID,
 * connection ID, and database name.
 */
function computeCacheKey(
    queryId: string,
    connectionId: number,
    databaseName: string,
): string {
    const keySource = [queryId, connectionId, databaseName].join(':');
    return djb2Hash(keySource);
}

/**
 * Check whether a cached analysis exists for the given parameters.
 */
export function hasCachedQueryAnalysis(
    queryId: string,
    connectionId: number,
    databaseName: string,
): boolean {
    const cacheKey = computeCacheKey(queryId, connectionId, databaseName);
    const cached = analysisCache.get(cacheKey);
    return !!cached && (Date.now() - cached.timestamp) < CACHE_TTL_MS;
}

/**
 * Hook for managing LLM-powered query performance analysis.
 * Uses an agentic loop so the LLM can call tools (schema info,
 * query validation, metrics) to produce higher-quality reports.
 */
export const useQueryAnalysis = (): UseQueryAnalysisReturn => {
    const { maxIterations } = useAICapabilities();
    const {
        state,
        setAnalysis,
        setLoading,
        setError,
        setProgressMessage,
        setActiveTools,
        reset,
    } = useAnalysisState('Preparing query data...');

    const analyze = useCallback(async (input: QueryAnalysisInput): Promise<void> => {
        // Check cache first to avoid flash of empty state
        const cacheKey = computeCacheKey(
            input.queryId,
            input.connectionId,
            input.databaseName,
        );
        const cached = analysisCache.get(cacheKey);
        if (cached && (Date.now() - cached.timestamp) < CACHE_TTL_MS) {
            setAnalysis(cached.analysis);
            setError(null);
            setLoading(false);
            return;
        }

        setLoading(true);
        setError(null);
        setAnalysis(null);
        setProgressMessage('Preparing query data...');
        setActiveTools([]);

        try {
            // Fetch knowledgebase tool definition if available
            const kbTool = await getKnowledgebaseTool();

            // Compute hit ratio
            const totalBlks = input.sharedBlksHit + input.sharedBlksRead;
            const hitRatio = totalBlks > 0
                ? ((input.sharedBlksHit / totalBlks) * 100).toFixed(2)
                : 'N/A';

            // Fetch connection context and timeline events in parallel
            let connectionContext = '';
            let timelineContext = '';
            setActiveTools(['Fetching server context']);
            const [ctxResult, timelineResult] = await Promise.allSettled([
                apiGet<Record<string, unknown>>(
                    `/api/v1/connections/${input.connectionId}/context`
                ).then(data => formatConnectionContext(data)).catch(() => ''),
                fetchTimelineEventsForRange(input.connectionId, undefined),
            ]);
            if (ctxResult.status === 'fulfilled') {
                connectionContext = ctxResult.value;
            }
            if (timelineResult.status === 'fulfilled') {
                timelineContext = timelineResult.value;
            }

            // Build user message with connection and database info so the
            // LLM knows which connection_id and database_name to pass to tools
            const userMessage = `Analyze the following PostgreSQL query and its performance statistics:

Query Text:
\`\`\`sql
${input.queryText}
\`\`\`

Query Statistics (from pg_stat_statements):
- Total Calls: ${input.calls.toLocaleString()}
- Total Execution Time: ${formatTime(input.totalExecTime)}
- Mean Execution Time: ${formatTime(input.meanExecTime)}
- Total Rows Returned: ${input.rows.toLocaleString()}
- Shared Blocks Hit: ${input.sharedBlksHit.toLocaleString()}
- Shared Blocks Read: ${input.sharedBlksRead.toLocaleString()}
- Buffer Hit Ratio: ${hitRatio}%

Connection ID: ${input.connectionId}
Database: ${input.databaseName}
${connectionContext}${timelineContext}

Analyze performance, check schema context, validate any SQL suggestions, and provide actionable optimization recommendations.`;

            const messages: Message[] = [
                { role: 'user', content: userMessage },
            ];

            // Build tools list - add knowledgebase tool if available
            const tools: AnalysisTool[] = kbTool
                ? [...QUERY_ANALYSIS_TOOLS, kbTool]
                : QUERY_ANALYSIS_TOOLS;

            setProgressMessage('Starting analysis...');

            const analysisText = await runAgenticLoop({
                messages,
                tools,
                systemPrompt: QUERY_ANALYSIS_SYSTEM_PROMPT,
                maxIterations,
                onActiveTools: setActiveTools,
                onProgress: setProgressMessage,
            });

            setAnalysis(analysisText);

            // Cache the result
            analysisCache.set(cacheKey, {
                analysis: analysisText,
                timestamp: Date.now(),
            });
        } catch (err) {
            console.error('Query analysis error:', err);
            setActiveTools([]);
            setError((err as Error).message);
        } finally {
            setLoading(false);
        }
    }, [maxIterations, setAnalysis, setLoading, setError, setProgressMessage, setActiveTools]);

    return {
        analysis: state.analysis,
        loading: state.loading,
        error: state.error,
        progressMessage: state.progressMessage,
        activeTools: state.activeTools,
        analyze,
        reset,
    };
};

export default useQueryAnalysis;
