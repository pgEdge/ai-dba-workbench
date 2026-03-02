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
import { formatConnectionContext } from '../utils/connectionContext';
import { getKnowledgebaseTool, AnalysisTool } from '../utils/mcpTools';
import { SERVER_ANALYSIS_TOOLS } from '../utils/analysisTools';
import {
    SQL_CODE_BLOCK_RULES,
    SQL_PLACEHOLDER_RULES,
} from '../utils/analysisPrompts';
import { runAgenticLoop } from '../utils/agenticLoop';
import { fetchTimelineEventsCentered } from '../utils/timelineEvents';
import { Message } from '../types/llm';
import { ANALYSIS_CACHE_TTL_MS } from '../utils/textHelpers';
import { useAnalysisState } from './useAnalysisState';

// Module-level cache for analysis results (persists across dialog open/close)
const analysisCache = new Map<string, { analysis: string; timestamp: number }>();

/**
 * Clear the analysis cache. Called when a server restart is detected
 * to ensure stale analysis reports are not served.
 */
export function clearAnalysisCache(): void {
    analysisCache.clear();
}

export interface ServerAnalysisInput {
    type: 'server' | 'cluster';
    id: number | string;
    name: string;
    serverIds?: number[];
    servers?: Array<{ id: number; name: string }>;
}

export interface UseServerAnalysisReturn {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    analyze: (input: ServerAnalysisInput) => Promise<void>;
    reset: () => void;
}

const BASE_SYSTEM_PROMPT = `You are a PostgreSQL database expert performing a comprehensive analysis for the pgEdge AI DBA Workbench monitoring system.

Your task is to:
1. Gather information about the server using the provided tools
2. Analyze performance metrics, schema design, and security configuration
3. If timeline events are provided, check for correlations (config changes, restarts, alerts, extension changes)
4. Provide prioritized, actionable recommendations

Structure your response as:

## Performance Review
Analysis of current performance metrics, trends, and bottlenecks.

## Schema Review
Assessment of database schema design, indexing, and optimization opportunities.

## Security Review
Evaluation of security configuration, access controls, and best practices.

## Suggested Improvements
Prioritized recommendations for improving the server/cluster.
${SQL_CODE_BLOCK_RULES}${SQL_PLACEHOLDER_RULES}

Keep responses concise and actionable. Do not offer to perform additional actions, run further queries, or investigate anything else. Do not ask follow-up questions or ask what the user would like to do next. Begin your response directly with the first section heading (## Performance Review). Do not include any introductory text, preamble, or conversational remarks before the report. Your analysis is displayed in a read-only report that the user cannot respond to.

If a search_knowledgebase tool is available, use it to look up unfamiliar PostgreSQL features, extensions, or pgEdge-specific concepts before making recommendations. If a get_blackouts tool is available, check whether the server is in a maintenance window before raising concerns about anomalous metrics. If a test_query tool is available, validate all SQL queries before including them in the report.`;

const CLUSTER_ADDENDUM = `

IMPORTANT: This is a CLUSTER analysis covering multiple servers. When providing SQL queries, you MUST include a SQL comment as the FIRST LINE of every SQL code block indicating which server the query should run on, using this exact format:
-- connection_id: {id}
where {id} is the numeric connection ID of the target server. This comment is used by the UI to route query execution to the correct server.`;

/**
 * Build context for a single server: connection context + timeline events.
 */
async function buildServerContext(
    connectionId: number,
    serverName?: string,
): Promise<string> {
    const [ctxResult, timelineResult] = await Promise.allSettled([
        apiGet<Record<string, unknown>>(`/api/v1/connections/${connectionId}/context`)
            .then(data => formatConnectionContext(data))
            .catch(() => ''),
        fetchTimelineEventsCentered(connectionId),
    ]);

    let context = '';
    if (ctxResult.status === 'fulfilled') {
        context += ctxResult.value;
    }
    if (timelineResult.status === 'fulfilled') {
        context += timelineResult.value;
    }

    if (serverName) {
        return `\n--- Server: ${serverName} (connection_id: ${connectionId}) ---${context}`;
    }

    return context;
}

/**
 * Hook for managing LLM-powered server/cluster analysis.
 * Implements an agentic loop to gather context via tools before
 * providing recommendations.
 */
export const useServerAnalysis = (): UseServerAnalysisReturn => {
    const { maxIterations } = useAICapabilities();
    const {
        state,
        setAnalysis,
        setLoading,
        setError,
        setProgressMessage,
        setActiveTools,
        reset,
    } = useAnalysisState('Gathering context...');

    const analyze = useCallback(async (input: ServerAnalysisInput): Promise<void> => {
        setLoading(true);
        setError(null);
        setAnalysis(null);
        setProgressMessage('Gathering context...');
        setActiveTools([]);

        const cacheKey = `${input.type}-${input.id}`;

        // Check client-side cache (with TTL validation)
        const cached = analysisCache.get(cacheKey);
        if (cached && (Date.now() - cached.timestamp) < ANALYSIS_CACHE_TTL_MS) {
            setAnalysis(cached.analysis);
            setLoading(false);
            return;
        }

        // Fetch knowledgebase tool definition if available
        const kbTool = await getKnowledgebaseTool();

        // Build context based on analysis type
        let contextText = '';
        let systemPrompt = BASE_SYSTEM_PROMPT;

        if (input.type === 'server') {
            contextText = await buildServerContext(Number(input.id));
        } else {
            // Cluster analysis: fetch context for all servers in parallel
            systemPrompt = BASE_SYSTEM_PROMPT + CLUSTER_ADDENDUM;

            const serverList = input.servers || [];
            const serverIdList = input.serverIds || serverList.map(s => s.id);

            const contextPromises = serverIdList.map(serverId => {
                const serverInfo = serverList.find(s => s.id === serverId);
                const name = serverInfo?.name || `Server ${serverId}`;
                return buildServerContext(serverId, name);
            });

            const results = await Promise.allSettled(contextPromises);
            const sections: string[] = [];
            for (const result of results) {
                if (result.status === 'fulfilled' && result.value) {
                    sections.push(result.value);
                }
            }
            contextText = sections.join('\n');
        }

        // Build user message
        const userMessage = input.type === 'server'
            ? `Perform a comprehensive analysis of this PostgreSQL server:

Server Name: ${input.name}
Connection ID: ${input.id}
${contextText}

Analyze performance metrics, schema design, security configuration, and provide prioritized improvement recommendations.`
            : `Perform a comprehensive analysis of this PostgreSQL cluster:

Cluster Name: ${input.name}
Servers: ${(input.servers || []).map(s => `${s.name} (connection_id: ${s.id})`).join(', ')}
${contextText}

Analyze performance metrics, schema design, security configuration, and replication health across all servers. Provide prioritized improvement recommendations.`;

        const messages: Message[] = [
            { role: 'user', content: userMessage }
        ];

        // Build tools list - add knowledgebase tool if available
        const tools: AnalysisTool[] = kbTool
            ? [...SERVER_ANALYSIS_TOOLS, kbTool]
            : SERVER_ANALYSIS_TOOLS;

        setProgressMessage('Starting analysis...');

        try {
            const analysisText = await runAgenticLoop({
                messages,
                tools,
                systemPrompt,
                maxIterations,
                onActiveTools: setActiveTools,
                onProgress: setProgressMessage,
            });

            setAnalysis(analysisText);
            analysisCache.set(cacheKey, { analysis: analysisText, timestamp: Date.now() });
        } catch (err) {
            console.error('Server analysis error:', err);
            setActiveTools([]);
            setError(err instanceof Error ? err.message : String(err));
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

/**
 * Check whether a cached (non-expired) server analysis exists for the
 * given type and id.  Used by the UI to show a "cached" indicator on
 * the analyze button.
 */
export function hasCachedServerAnalysis(
    type: 'server' | 'cluster',
    id: number | string,
): boolean {
    const cacheKey = `${type}-${id}`;
    const cached = analysisCache.get(cacheKey);
    return !!cached && (Date.now() - cached.timestamp) < ANALYSIS_CACHE_TTL_MS;
}

export default useServerAnalysis;
