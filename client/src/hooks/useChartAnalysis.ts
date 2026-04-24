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
import { ChartData } from '../components/Chart/types';
import { apiGet, apiFetch } from '../utils/apiClient';
import { formatConnectionContext } from '../utils/connectionContext';
import { stripPreamble, djb2Hash, ANALYSIS_CACHE_TTL_MS } from '../utils/textHelpers';
import { SQL_CODE_BLOCK_RULES } from '../utils/analysisPrompts';
import { fetchTimelineEventsForRange } from '../utils/timelineEvents';
import { LLMResponse } from '../types/llm';
import { useAnalysisState } from './useAnalysisState';
import { logger } from '../utils/logger';

export interface ChartAnalysisInput {
    metricDescription: string;
    connectionId?: number;
    connectionName?: string;
    databaseName?: string;
    timeRange?: string;
    data: ChartData;
}

export interface UseChartAnalysisReturn {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    analyze: (input: ChartAnalysisInput) => Promise<void>;
    reset: () => void;
}

// Module-level cache for chart analysis results (persists across component mounts)
const analysisCache = new Map<string, { analysis: string; timestamp: number }>();

export function clearChartAnalysisCache(): void {
    analysisCache.clear();
}

const CHART_ANALYSIS_SYSTEM_PROMPT = `You are a PostgreSQL database expert analyzing metrics from the pgEdge AI DBA Workbench monitoring system.

You are given chart data showing time-series metrics. Your task is to:
1. Identify trends, anomalies, and patterns in the data
2. Explain what the metrics mean and their significance
3. Provide actionable recommendations based on the data
4. If timeline events are provided, correlate them with observed metric changes (e.g. a config change or restart that coincides with a spike or drop)

Structure your response as:

## Summary
Brief 1-2 sentence summary of what the data shows.

## Trends & Patterns
Analysis of trends, anomalies, and notable patterns in the data.

## Recommendations
Numbered list of specific actions or considerations based on the data.

If suggesting SQL queries, use \`\`\`sql code blocks. SQL should be correct and executable PostgreSQL. Since queries cannot be validated in this context, add a SQL comment '-- NOTE: Verify column names before running' as the first line of each SQL code block.

Keep responses concise and actionable.

CRITICAL: Your output is rendered in a static, read-only report. The user CANNOT reply, ask questions, or interact with you in any way. You MUST NOT:
- Ask the user anything (e.g. "Would you like me to...", "Do you want me to...", "Shall I...")
- Offer to perform follow-up actions or further investigation
- Suggest that you can do additional work
- End with a question of any kind
Write your analysis as a final, self-contained report with no conversational elements.`;

/**
 * Compute a cache key from stable identifiers: metric description,
 * connection ID, database name, and time range.
 */
function computeCacheKey(
    metricDescription: string,
    connectionId: number | undefined,
    databaseName: string | undefined,
    timeRange: string | undefined,
): string {
    const keySource = [
        metricDescription,
        connectionId ?? '',
        databaseName ?? '',
        timeRange ?? '',
    ].join(':');
    return djb2Hash(keySource);
}

/**
 * Check whether a cached analysis exists for the given parameters.
 */
export function hasCachedAnalysis(
    metricDescription: string,
    connectionId: number | undefined,
    databaseName: string | undefined,
    timeRange: string | undefined,
): boolean {
    const cacheKey = computeCacheKey(
        metricDescription, connectionId, databaseName, timeRange
    );
    const cached = analysisCache.get(cacheKey);
    return !!cached && (Date.now() - cached.timestamp) < ANALYSIS_CACHE_TTL_MS;
}

/**
 * Serialize ChartData into a readable text format for the LLM.
 * Includes per-series summary statistics and a sampled data table.
 */
function serializeChartData(data: ChartData): string {
    const lines: string[] = [];

    for (const series of data.series) {
        const values = series.data;
        if (values.length === 0) {
            lines.push(`Series "${series.name}": No data points`);
            continue;
        }

        const min = Math.min(...values);
        const max = Math.max(...values);
        const avg = values.reduce((a, b) => a + b, 0) / values.length;
        const latest = values[values.length - 1];

        lines.push(`Series "${series.name}" (${values.length} points):`);
        lines.push(`  Min: ${min}, Max: ${max}, Avg: ${avg.toFixed(2)}, Latest: ${latest}`);
    }

    // Build a sampled data table
    const maxPoints = 50;
    const sampleSize = data.series.length > 0
        ? Math.min(maxPoints, data.series[0].data.length)
        : 0;

    if (sampleSize > 0) {
        const totalPoints = data.series[0].data.length;
        const step = totalPoints <= maxPoints
            ? 1
            : (totalPoints - 1) / (maxPoints - 1);

        lines.push('');
        const header = ['Index'];
        if (data.categories) {
            header.push('Timestamp');
        }
        for (const series of data.series) {
            header.push(series.name);
        }
        lines.push(header.join('\t'));

        for (let i = 0; i < sampleSize; i++) {
            const idx = totalPoints <= maxPoints
                ? i
                : Math.round(i * step);
            const row: string[] = [String(idx)];
            if (data.categories && data.categories[idx] !== undefined) {
                row.push(data.categories[idx]);
            }
            for (const series of data.series) {
                row.push(
                    series.data[idx] !== undefined
                        ? String(series.data[idx])
                        : 'N/A'
                );
            }
            lines.push(row.join('\t'));
        }
    }

    return lines.join('\n');
}

/**
 * Hook for managing LLM-powered chart data analysis.
 * Performs a single-shot LLM call with serialized chart data as context.
 */
export const useChartAnalysis = (): UseChartAnalysisReturn => {
    const {
        state,
        setAnalysis,
        setLoading,
        setError,
        setProgressMessage,
        setActiveTools,
        reset,
    } = useAnalysisState('Preparing chart data...');

    const analyze = useCallback(async (input: ChartAnalysisInput): Promise<void> => {
        // Check cache first to avoid flash of empty state
        const cacheKey = computeCacheKey(
            input.metricDescription,
            input.connectionId,
            input.databaseName,
            input.timeRange,
        );
        const cached = analysisCache.get(cacheKey);
        if (cached && (Date.now() - cached.timestamp) < ANALYSIS_CACHE_TTL_MS) {
            setAnalysis(cached.analysis);
            setError(null);
            setLoading(false);
            return;
        }

        setLoading(true);
        setError(null);
        setAnalysis(null);
        setProgressMessage('Preparing chart data...');
        setActiveTools(['Preparing chart data']);

        try {
            // Serialize chart data for the LLM
            const serializedData = serializeChartData(input.data);

            // Fetch connection context and timeline events in parallel
            let connectionContext = '';
            let timelineContext = '';
            if (input.connectionId != null) {
                setActiveTools(['Fetching server context', 'Fetching timeline events']);
                const [ctxResult, timelineResult] = await Promise.allSettled([
                    apiGet<Record<string, unknown>>(
                        `/api/v1/connections/${input.connectionId}/context`
                    ).then(data => formatConnectionContext(data)).catch(() => ''),
                    fetchTimelineEventsForRange(input.connectionId, input.timeRange),
                ]);
                if (ctxResult.status === 'fulfilled') {
                    connectionContext = ctxResult.value;
                }
                if (timelineResult.status === 'fulfilled') {
                    timelineContext = timelineResult.value;
                }
            }

            // Build the system prompt, appending SQL rules only when
            // a connection is provided (indicating a database context)
            const systemPrompt = input.connectionId != null
                ? CHART_ANALYSIS_SYSTEM_PROMPT + SQL_CODE_BLOCK_RULES
                : CHART_ANALYSIS_SYSTEM_PROMPT;

            // Build user message
            const connectionInfo = [
                input.connectionName ? `Connection: ${input.connectionName}` : '',
                input.databaseName ? `Database: ${input.databaseName}` : '',
                input.timeRange ? `Time Range: ${input.timeRange}` : '',
            ].filter(Boolean).join('\n');

            const userMessage = `Analyze the following chart data:

Metric: ${input.metricDescription}
${connectionInfo}
${connectionContext}${timelineContext}

Chart Data:
${serializedData}

Provide analysis of trends, anomalies, and actionable recommendations.`;

            setProgressMessage('Analyzing data...');
            setActiveTools(['Analyzing data']);

            const response = await apiFetch('/api/v1/llm/chat', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    messages: [{ role: 'user', content: userMessage }],
                    system: systemPrompt,
                }),
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Analysis request failed: ${errorText}`);
            }

            const data: LLMResponse = await response.json();

            const textContent = data.content
                ?.filter(c => c.type === 'text')
                .map(c => c.text)
                .join('\n') ?? '';

            const cleanedText = stripPreamble(textContent);
            setAnalysis(cleanedText);
            setActiveTools([]);

            // Cache the result
            analysisCache.set(cacheKey, {
                analysis: cleanedText,
                timestamp: Date.now(),
            });
        } catch (err) {
            logger.error('Chart analysis error:', err);
            setError(err instanceof Error ? err.message : String(err));
            setActiveTools([]);
        } finally {
            setLoading(false);
        }
    }, [setAnalysis, setLoading, setError, setProgressMessage, setActiveTools]);

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

export default useChartAnalysis;
