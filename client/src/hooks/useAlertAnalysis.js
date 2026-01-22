/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback } from 'react';
import { useAuth } from '../contexts/AuthContext';

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
const ANALYSIS_TOOLS = [
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
export const useAlertAnalysis = () => {
    const { sessionToken: token } = useAuth();
    const [analysis, setAnalysis] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    const analyze = useCallback(async (alert) => {
        setLoading(true);
        setError(null);
        setAnalysis(null);

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

        const messages = [
            { role: 'user', content: userMessage }
        ];

        try {
            // Agentic loop - keep calling until no more tool use
            let maxIterations = 10;
            let iterations = 0;

            while (iterations < maxIterations) {
                iterations++;

                const response = await fetch('/api/llm/chat', {
                    method: 'POST',
                    headers: {
                        'Authorization': `Bearer ${token}`,
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

                const data = await response.json();

                // Check if response contains tool use
                const toolUses = data.content?.filter(c => c.type === 'tool_use') || [];

                if (toolUses.length === 0) {
                    // No tool use - extract final text response
                    const textContent = data.content?.filter(c => c.type === 'text')
                        .map(c => c.text)
                        .join('\n') || '';
                    setAnalysis(textContent);
                    break;
                }

                // Add assistant message with tool uses
                messages.push({ role: 'assistant', content: data.content });

                // Execute tool calls and add results
                const toolResults = [];
                for (const toolUse of toolUses) {
                    try {
                        const toolResponse = await fetch('/api/mcp/tools/call', {
                            method: 'POST',
                            headers: {
                                'Authorization': `Bearer ${token}`,
                                'Content-Type': 'application/json',
                            },
                            body: JSON.stringify({
                                name: toolUse.name,
                                arguments: toolUse.input,
                            }),
                        });

                        const toolData = await toolResponse.json();
                        const resultText = toolData.content?.[0]?.text ||
                            (toolData.isError ? `Error: ${toolData.content?.[0]?.text}` : 'No data returned');

                        toolResults.push({
                            type: 'tool_result',
                            tool_use_id: toolUse.id,
                            content: resultText,
                        });
                    } catch (toolErr) {
                        toolResults.push({
                            type: 'tool_result',
                            tool_use_id: toolUse.id,
                            content: `Tool execution error: ${toolErr.message}`,
                            is_error: true,
                        });
                    }
                }

                messages.push({ role: 'user', content: toolResults });
            }

            if (iterations >= maxIterations && !analysis) {
                throw new Error('Analysis exceeded maximum iterations');
            }

        } catch (err) {
            console.error('Alert analysis error:', err);
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, [token]);

    const reset = useCallback(() => {
        setAnalysis(null);
        setError(null);
        setLoading(false);
    }, []);

    return { analysis, loading, error, analyze, reset };
};

export default useAlertAnalysis;
