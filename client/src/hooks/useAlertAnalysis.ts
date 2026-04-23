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
import { apiGet, apiPut } from '../utils/apiClient';
import { formatConnectionContext } from '../utils/connectionContext';
import { getKnowledgebaseTool, AnalysisTool } from '../utils/mcpTools';
import { ALERT_ANALYSIS_TOOLS } from '../utils/analysisTools';
import { SQL_CODE_BLOCK_RULES } from '../utils/analysisPrompts';
import { runAgenticLoop } from '../utils/agenticLoop';
import { fetchTimelineEventsCentered } from '../utils/timelineEvents';
import { Message } from '../types/llm';
import { useAnalysisState } from './useAnalysisState';
import { logger } from '../utils/logger';

// Module-level cache for analysis results (persists across dialog open/close)
const analysisCache = new Map<number, { analysis: string; metricValue: number }>();

export function clearAlertAnalysisCache(): void {
    analysisCache.clear();
}

export interface AlertInput {
    id?: number;
    aiAnalysis?: string | null;
    aiAnalysisMetricValue?: number | null;
    alertType?: string;
    severity: string;
    title: string;
    description?: string;
    metricName?: string;
    metricValue?: number | string | null;
    operator?: string;
    thresholdValue?: number | string | null;
    connectionId: number;
    triggeredAt?: string;
    time?: string;
}

export interface UseAlertAnalysisReturn {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    analyze: (alert: AlertInput) => Promise<void>;
    reset: () => void;
}

const SYSTEM_PROMPT = `You are a PostgreSQL database expert analyzing an alert from the pgEdge AI DBA Workbench monitoring system.

Your task is to:
1. Understand the alert context using the provided tools
2. Analyze historical patterns and current state
3. If timeline events are provided, check for correlations (config changes, restarts, other alerts, extension changes) that may explain the alert
4. Provide actionable remediation recommendations
5. Suggest threshold tuning if appropriate

Structure your response as:

## Summary
Brief 1-2 sentence summary of the alert and its significance.

## Analysis
What the data tells us about this alert, including frequency of similar alerts, comparison to baseline patterns, and correlations.

## Remediation Steps
Numbered list of specific actions to address the issue.

## Threshold Tuning
If the current threshold seems misconfigured, recommend changes with rationale.
${SQL_CODE_BLOCK_RULES}

6. NEVER use placeholder names like \`schema_name\`, \`table_name\`, \`your_table\`, \`my_table\`, \`your_database\`, or similar invented identifiers in SQL code blocks. Users execute SQL directly from the UI, and placeholders cause runtime errors. Instead:
   - If the alert context or tool results provide specific object names, use those exact names in the SQL.
   - If remediation requires acting on specific database objects that are not yet known, first provide a diagnostic query that identifies the affected objects (e.g., tables with high dead tuple ratios), then provide the remediation SQL using the actual names returned by that diagnostic query.
   - If the specific objects cannot be determined, provide ONLY the diagnostic query and explain that the user should run the remediation command on the objects it identifies. Do NOT generate non-executable SQL containing placeholders.

Keep responses concise and actionable. Do not offer to perform additional actions, run further queries, or investigate anything else. Do not ask follow-up questions or ask what the user would like to do next. Your analysis is displayed in a read-only report that the user cannot respond to.

If a search_knowledgebase tool is available, use it to look up unfamiliar PostgreSQL features, extensions, or pgEdge-specific concepts before making recommendations. If a get_blackouts tool is available, check whether the alert occurred during a maintenance window. If a test_query tool is available, validate all SQL queries before including them in the report.`;

/**
 * Check if two metric values are close enough to consider the
 * cached analysis still valid. Returns true if the values are
 * within 10% of each other (relative to the larger value).
 */
const isMetricValueClose = (a: number, b: number): boolean => {
    if (a === b) { return true; }
    if (a === 0 && b === 0) { return true; }
    const larger = Math.max(Math.abs(a), Math.abs(b));
    if (larger === 0) { return true; }
    return Math.abs(a - b) / larger <= 0.1;
};

/**
 * Hook for managing LLM-powered alert analysis
 * Implements an agentic loop to gather context via tools before providing recommendations
 */
export const useAlertAnalysis = (): UseAlertAnalysisReturn => {
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

    const analyze = useCallback(async (alert: AlertInput): Promise<void> => {
        setLoading(true);
        setError(null);
        setAnalysis(null);
        setProgressMessage('Gathering context...');
        setActiveTools([]);

        // Check server-side cache (from alert object)
        if (alert.aiAnalysis && alert.aiAnalysisMetricValue != null &&
            alert.metricValue != null && isMetricValueClose(alert.aiAnalysisMetricValue, Number(alert.metricValue))) {
            setAnalysis(alert.aiAnalysis);
            setLoading(false);
            return;
        }

        // Check client-side cache (from previous analysis in this session)
        if (alert.id != null && alert.metricValue != null) {
            const cached = analysisCache.get(alert.id);
            if (cached && isMetricValueClose(cached.metricValue, Number(alert.metricValue))) {
                setAnalysis(cached.analysis);
                setLoading(false);
                return;
            }
        }

        // Fetch knowledgebase tool definition if available
        const kbTool = await getKnowledgebaseTool();

        // Fetch connection context and timeline events in parallel
        let connectionContext = '';
        let timelineContext = '';
        const [ctxResult, timelineResult] = await Promise.allSettled([
            apiGet<Record<string, unknown>>(`/api/v1/connections/${alert.connectionId}/context`)
                .then(data => formatConnectionContext(data))
                .catch(() => ''),
            fetchTimelineEventsCentered(
                alert.connectionId,
                alert.triggeredAt || alert.time,
            ),
        ]);
        if (ctxResult.status === 'fulfilled') {
            connectionContext = ctxResult.value;
        }
        if (timelineResult.status === 'fulfilled') {
            timelineContext = timelineResult.value;
        }

        // Build user message with alert context
        const userMessage = `Analyze this alert:

Alert Type: ${alert.alertType || 'threshold'}
Severity: ${alert.severity}
Title: ${alert.title}
Description: ${alert.description || 'N/A'}
Metric: ${alert.metricName || alert.title}
Current Value: ${alert.metricValue ?? 'N/A'}
Threshold: ${alert.operator || '>'} ${alert.thresholdValue ?? 'N/A'}
Connection ID: ${alert.connectionId}
Triggered At: ${alert.triggeredAt || alert.time}
${connectionContext}${timelineContext}
Provide remediation recommendations and any threshold tuning suggestions.`;

        const messages: Message[] = [
            { role: 'user', content: userMessage }
        ];

        // Build tools list - add knowledgebase tool if available
        const tools: AnalysisTool[] = kbTool
            ? [...ALERT_ANALYSIS_TOOLS, kbTool]
            : ALERT_ANALYSIS_TOOLS;

        setProgressMessage('Starting analysis...');

        try {
            const analysisText = await runAgenticLoop({
                messages,
                tools,
                systemPrompt: SYSTEM_PROMPT,
                maxIterations,
                onActiveTools: setActiveTools,
                onProgress: setProgressMessage,
            });

            setAnalysis(analysisText);

            // Save successful analysis to cache
            if (alert.id) {
                const metricVal = Number(alert.metricValue ?? 0);

                // Update client-side cache
                analysisCache.set(alert.id, {
                    analysis: analysisText,
                    metricValue: metricVal,
                });

                // Save to server (fire-and-forget)
                apiPut('/api/v1/alerts/analysis', {
                    alert_id: alert.id,
                    analysis: analysisText,
                    metric_value: metricVal,
                }).catch(() => {});
            }
        } catch (err) {
            logger.error('Alert analysis error:', err);
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

export default useAlertAnalysis;
