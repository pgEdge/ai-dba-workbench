/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback } from 'react';
import { useAuth } from '../contexts/AuthContext';

// Module-level cache for analysis results (persists across dialog open/close)
const analysisCache = new Map<number, { analysis: string; metricValue: number }>();

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

interface ToolInputSchema {
    type: string;
    properties: Record<string, { type: string; description: string }>;
    required: string[];
}

interface AnalysisTool {
    name: string;
    description: string;
    inputSchema: ToolInputSchema;
}

interface ContentBlock {
    type: string;
    text?: string;
    id?: string;
    name?: string;
    input?: Record<string, unknown>;
}

interface LLMResponse {
    content?: ContentBlock[];
}

interface ToolCallResponse {
    content?: Array<{ text?: string }>;
    isError?: boolean;
}

interface ToolResult {
    type: 'tool_result';
    tool_use_id: string;
    content: string;
    is_error?: boolean;
}

interface Message {
    role: string;
    content: string | ContentBlock[] | ToolResult[];
}

export interface UseAlertAnalysisReturn {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    analyze: (alert: AlertInput) => Promise<void>;
    reset: () => void;
}

const SYSTEM_PROMPT = `You are a PostgreSQL database expert analyzing an alert from the pgEdge AI DBA Workbench monitoring system.

Your task is to:
1. Understand the alert context using the provided tools
2. Analyze historical patterns and current state
3. Provide actionable remediation recommendations
4. Suggest threshold tuning if appropriate

Structure your response as:

## Summary
Brief 1-2 sentence summary of the alert and its significance.

## Analysis
What the data tells us about this alert, including frequency of similar alerts, comparison to baseline patterns, and correlations.

## Remediation Steps
Numbered list of specific actions to address the issue.

## Threshold Tuning
If the current threshold seems misconfigured, recommend changes with rationale.

Keep responses concise and actionable.`;

// Tool definitions for the LLM (must use camelCase inputSchema to match Go struct)
const ANALYSIS_TOOLS: AnalysisTool[] = [
    {
        name: "get_alert_history",
        description: "Get historical alerts for the same rule or metric on a connection",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID to query" },
                rule_id: { type: "integer", description: "Filter by alert rule ID" },
                metric_name: { type: "string", description: "Filter by metric name" },
                time_start: { type: "string", description: "Start of time range (e.g., '7d', '24h')" },
                limit: { type: "integer", description: "Max results (default 50)" }
            },
            required: ["connection_id"]
        }
    },
    {
        name: "get_alert_rules",
        description: "Get current alerting rules and thresholds configuration",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID for specific thresholds" },
                category: { type: "string", description: "Filter by category" },
                enabled_only: { type: "boolean", description: "Only enabled rules" }
            },
            required: []
        }
    },
    {
        name: "get_metric_baselines",
        description: "Get statistical baselines for metrics (mean, stddev, min, max)",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID to query" },
                metric_name: { type: "string", description: "Filter to specific metric" }
            },
            required: ["connection_id"]
        }
    },
    {
        name: "query_metrics",
        description: "Query historical metric values with time-based aggregation",
        inputSchema: {
            type: "object",
            properties: {
                probe_name: { type: "string", description: "Name of the probe to query" },
                connection_id: { type: "integer", description: "Connection ID" },
                time_start: { type: "string", description: "Start of time range" },
                metrics: { type: "string", description: "Comma-separated metric columns" },
                buckets: { type: "integer", description: "Number of time buckets" }
            },
            required: ["probe_name"]
        }
    }
];

/**
 * Hook for managing LLM-powered alert analysis
 * Implements an agentic loop to gather context via tools before providing recommendations
 */
export const useAlertAnalysis = (): UseAlertAnalysisReturn => {
    const { user } = useAuth();
    const [analysis, setAnalysis] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);

    const analyze = useCallback(async (alert: AlertInput): Promise<void> => {
        setLoading(true);
        setError(null);
        setAnalysis(null);

        // Check server-side cache (from alert object)
        if (alert.aiAnalysis && alert.aiAnalysisMetricValue != null &&
            alert.metricValue != null && alert.aiAnalysisMetricValue === Number(alert.metricValue)) {
            setAnalysis(alert.aiAnalysis);
            setLoading(false);
            return;
        }

        // Check client-side cache (from previous analysis in this session)
        if (alert.id != null && alert.metricValue != null) {
            const cached = analysisCache.get(alert.id);
            if (cached && cached.metricValue === Number(alert.metricValue)) {
                setAnalysis(cached.analysis);
                setLoading(false);
                return;
            }
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

Provide remediation recommendations and any threshold tuning suggestions.`;

        const messages: Message[] = [
            { role: 'user', content: userMessage }
        ];

        try {
            // Agentic loop - keep calling until no more tool use
            const maxIterations = 10;
            let iterations = 0;
            let gotResponse = false; // Track completion with local variable (not state) to avoid stale closure
            let analysisText = '';

            while (iterations < maxIterations) {
                iterations++;

                const response = await fetch('/api/v1/llm/chat', {
                    method: 'POST',
                    credentials: 'include',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({
                        messages,
                        tools: ANALYSIS_TOOLS,
                        system: SYSTEM_PROMPT,
                    }),
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error(`Analysis request failed: ${errorText}`);
                }

                const data: LLMResponse = await response.json();

                // Check if response contains tool use
                const toolUses = data.content?.filter(c => c.type === 'tool_use') || [];

                if (toolUses.length === 0) {
                    // No tool use - extract final text response
                    const textContent = data.content?.filter(c => c.type === 'text')
                        .map(c => c.text)
                        .join('\n') || '';
                    setAnalysis(textContent);
                    analysisText = textContent;
                    gotResponse = true;
                    break;
                }

                // Add assistant message with tool uses
                messages.push({ role: 'assistant', content: data.content as ContentBlock[] });

                // Execute tool calls and add results
                const toolResults: ToolResult[] = [];
                for (const toolUse of toolUses) {
                    try {
                        const toolResponse = await fetch('/api/v1/mcp/tools/call', {
                            method: 'POST',
                            credentials: 'include',
                            headers: {
                                'Content-Type': 'application/json',
                            },
                            body: JSON.stringify({
                                name: toolUse.name,
                                arguments: toolUse.input,
                            }),
                        });

                        const toolData: ToolCallResponse = await toolResponse.json();
                        const resultText = toolData.content?.[0]?.text ||
                            (toolData.isError ? `Error: ${toolData.content?.[0]?.text}` : 'No data returned');

                        toolResults.push({
                            type: 'tool_result',
                            tool_use_id: toolUse.id!,
                            content: resultText,
                        });
                    } catch (toolErr) {
                        toolResults.push({
                            type: 'tool_result',
                            tool_use_id: toolUse.id!,
                            content: `Tool execution error: ${(toolErr as Error).message}`,
                            is_error: true,
                        });
                    }
                }

                messages.push({ role: 'user', content: toolResults });
            }

            // Save successful analysis to cache
            if (gotResponse && alert.id) {
                const metricVal = Number(alert.metricValue ?? 0);

                // Update client-side cache
                analysisCache.set(alert.id, {
                    analysis: analysisText,
                    metricValue: metricVal,
                });

                // Save to server (fire-and-forget)
                fetch('/api/v1/alerts/analysis', {
                    method: 'PUT',
                    credentials: 'include',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        alert_id: alert.id,
                        analysis: analysisText,
                        metric_value: metricVal,
                    }),
                }).catch(() => {});
            }

            if (iterations >= maxIterations && !gotResponse) {
                throw new Error('Analysis exceeded maximum iterations');
            }

        } catch (err) {
            console.error('Alert analysis error:', err);
            setError((err as Error).message);
        } finally {
            setLoading(false);
        }
    }, [user]);

    const reset = useCallback((): void => {
        setAnalysis(null);
        setError(null);
        setLoading(false);
    }, []);

    return { analysis, loading, error, analyze, reset };
};

export default useAlertAnalysis;
