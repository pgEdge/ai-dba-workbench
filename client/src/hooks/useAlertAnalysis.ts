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
import { apiGet, apiPut } from '../utils/apiClient';
import { formatConnectionContext } from '../utils/connectionContext';
import {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
} from '../types/llm';
import { TimelineEvent } from '../components/EventTimeline/types';
import { getKnowledgebaseTool, AnalysisTool } from '../utils/mcpTools';

/**
 * Strip any conversational preamble before the first markdown heading.
 * LLMs sometimes add introductory text despite instructions not to.
 */
function stripPreamble(text: string): string {
    const headingIndex = text.search(/^##\s/m);
    if (headingIndex > 0) {
        return text.substring(headingIndex);
    }
    return text;
}

// Human-readable display names for MCP tools used during alert analysis
const TOOL_DISPLAY_NAMES: Record<string, string> = {
    query_metrics: 'Querying metrics',
    get_metric_baselines: 'Fetching metric baselines',
    get_alert_history: 'Reviewing alert history',
    get_alert_rules: 'Checking alert rules',
    get_blackouts: 'Checking blackouts',
    search_knowledgebase: 'Searching knowledgebase',
};

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

interface Message {
    role: string;
    content: string | LLMContentBlock[] | ToolResult[];
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

CRITICAL rules for code blocks - the user executes SQL directly from the UI so accuracy is essential:

1. SQL code blocks (\`\`\`sql) must ONLY contain executable SQL statements and SQL comments (lines starting with --). NEVER include any of the following in SQL code blocks:
   - Configuration file snippets (e.g. shared_buffers = 8GB, work_mem = 16MB)
   - File paths or filenames
   - Shell commands
   - Explanatory prose or notes
   Use \`\`\`conf for postgresql.conf snippets, \`\`\`bash for shell commands, and \`\`\`text for other content.

2. Place each SQL query in its own separate \`\`\`sql code block. NEVER combine multiple queries in one block.

3. Every SQL query MUST be correct and executable. The user will run these directly. Incorrect SQL wastes their time and erodes trust. You MUST verify all column names against the actual PostgreSQL system catalog. The correct column names are:
   - pg_stat_user_tables: schemaname, relname, seq_scan, seq_tup_read, idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count, autovacuum_count, analyze_count, autoanalyze_count
   - pg_statio_user_tables: schemaname, relname, heap_blks_read, heap_blks_hit, idx_blks_read, idx_blks_hit, toast_blks_read, toast_blks_hit, tidx_blks_read, tidx_blks_hit
   - pg_stat_activity: datid, datname, pid, leader_pid, usesysid, usename, application_name, client_addr, client_hostname, client_port, backend_start, xact_start, query_start, state_change, wait_event_type, wait_event, state, backend_xid, backend_xmin, query, backend_type
   - pg_stat_statements: userid, dbid, queryid, query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, shared_blks_written, temp_blks_read, temp_blks_written
   - pg_stat_bgwriter: checkpoints_timed, checkpoints_req, buffers_checkpoint, buffers_clean, maxwritten_clean, buffers_backend, buffers_alloc
   - pg_class: oid, relname, relnamespace, reltype, relowner, relam, relfilenode, reltablespace, relpages, reltuples, relallvisible, reltoastrelid, relhasindex, relisshared, relpersistence, relkind, relnatts, relchecks, relhasrules, relhastriggers, relhassubclass
   - pg_stat_database: datid, datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated, tup_deleted, conflicts, temp_files, temp_bytes, deadlocks
   NEVER use "tablename" - the column is always "relname" in PostgreSQL catalogs. When in doubt, keep queries simple and use only columns you are certain exist.

4. Ensure all SQL syntax, function names, and catalog column names are valid for the specific PostgreSQL version in use (provided in the server context below). Do not use features, functions, or columns introduced in newer versions. For example, pg_stat_statements column names changed between PostgreSQL 12 and 13.

5. When suggesting ALTER SYSTEM or other DDL statements, place them in separate code blocks from diagnostic SELECT queries.

6. NEVER use placeholder names like \`schema_name\`, \`table_name\`, \`your_table\`, \`my_table\`, \`your_database\`, or similar invented identifiers in SQL code blocks. Users execute SQL directly from the UI, and placeholders cause runtime errors. Instead:
   - If the alert context or tool results provide specific object names, use those exact names in the SQL.
   - If remediation requires acting on specific database objects that are not yet known, first provide a diagnostic query that identifies the affected objects (e.g., tables with high dead tuple ratios), then provide the remediation SQL using the actual names returned by that diagnostic query.
   - If the specific objects cannot be determined, provide ONLY the diagnostic query and explain that the user should run the remediation command on the objects it identifies. Do NOT generate non-executable SQL containing placeholders.

Keep responses concise and actionable. Do not offer to perform additional actions, run further queries, or investigate anything else. Do not ask follow-up questions or ask what the user would like to do next. Your analysis is displayed in a read-only report that the user cannot respond to.

If a search_knowledgebase tool is available, use it to look up unfamiliar PostgreSQL features, extensions, or pgEdge-specific concepts before making recommendations. If a get_blackouts tool is available, check whether the alert occurred during a maintenance window.`;

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
    },
    {
        name: "get_blackouts",
        description: "Get active and recent blackout (maintenance window) periods for a connection",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID to query" },
                active_only: { type: "boolean", description: "Only return currently active blackouts" },
                include_schedules: { type: "boolean", description: "Also return recurring blackout schedules" }
            },
            required: []
        }
    }
];

/**
 * Fetch timeline events surrounding an alert's trigger time.
 * Looks at a 24-hour window centered on the alert.
 */
async function fetchTimelineEvents(
    connectionId: number,
    triggeredAt: string | undefined,
): Promise<string> {
    const alertTime = triggeredAt ? new Date(triggeredAt) : new Date();
    const startTime = new Date(alertTime.getTime() - 12 * 60 * 60 * 1000);
    const endTime = new Date(alertTime.getTime() + 12 * 60 * 60 * 1000);

    const params = new URLSearchParams({
        start_time: startTime.toISOString(),
        end_time: endTime.toISOString(),
        connection_id: String(connectionId),
        limit: '100',
    });

    const data = await apiGet<{ events?: TimelineEvent[] }>(
        `/api/v1/timeline/events?${params}`
    ).catch(() => null);
    if (!data) { return ''; }

    const events = data.events;
    if (!events || events.length === 0) { return ''; }

    const lines = events.map(e => {
        const time = new Date(e.occurred_at).toLocaleString();
        const summary = e.summary ? `: ${e.summary}` : '';
        return `  [${time}] ${e.event_type} - ${e.title}${summary}`;
    });

    return '\nTimeline Events (24h window around alert):\n' + lines.join('\n');
}

/**
 * Check if two metric values are close enough to consider the
 * cached analysis still valid. Returns true if the values are
 * within 10% of each other (relative to the larger value).
 */
const isMetricValueClose = (a: number, b: number): boolean => {
    if (a === b) return true;
    if (a === 0 && b === 0) return true;
    const larger = Math.max(Math.abs(a), Math.abs(b));
    if (larger === 0) return true;
    return Math.abs(a - b) / larger <= 0.1;
};

/**
 * Hook for managing LLM-powered alert analysis
 * Implements an agentic loop to gather context via tools before providing recommendations
 */
export const useAlertAnalysis = (): UseAlertAnalysisReturn => {
    const { user: _user } = useAuth();
    const [analysis, setAnalysis] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [progressMessage, setProgressMessage] = useState<string>('Gathering context...');
    const [activeTools, setActiveTools] = useState<string[]>([]);

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
            fetchTimelineEvents(
                alert.connectionId,
                alert.triggeredAt || alert.time
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
            ? [...ANALYSIS_TOOLS, kbTool]
            : ANALYSIS_TOOLS;

        setProgressMessage('Starting analysis...');

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
                        tools,
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
                    const cleanedText = stripPreamble(textContent);
                    setAnalysis(cleanedText);
                    analysisText = cleanedText;
                    gotResponse = true;
                    setActiveTools([]);
                    break;
                }

                // Add assistant message with tool uses
                messages.push({ role: 'assistant', content: data.content as LLMContentBlock[] });

                // Update progress with tool names
                const toolNames = toolUses.map(t => TOOL_DISPLAY_NAMES[t.name || ''] || t.name || 'unknown tool');
                const uniqueNames = [...new Set(toolNames)];
                setActiveTools(uniqueNames);
                setProgressMessage(uniqueNames.length === 1
                    ? uniqueNames[0] + '...'
                    : `Running ${uniqueNames.length} tools...`);

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
                            tool_use_id: toolUse.id ?? '',
                            content: resultText,
                        });
                    } catch (toolErr) {
                        toolResults.push({
                            type: 'tool_result',
                            tool_use_id: toolUse.id ?? '',
                            content: `Tool execution error: ${(toolErr as Error).message}`,
                            is_error: true,
                        });
                    }
                }

                messages.push({ role: 'user', content: toolResults });
                setProgressMessage('Analyzing results...');
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
                apiPut('/api/v1/alerts/analysis', {
                    alert_id: alert.id,
                    analysis: analysisText,
                    metric_value: metricVal,
                }).catch(() => {});
            }

            if (iterations >= maxIterations && !gotResponse) {
                throw new Error('Analysis exceeded maximum iterations');
            }

        } catch (err) {
            console.error('Alert analysis error:', err);
            setActiveTools([]);
            setError((err as Error).message);
        } finally {
            setLoading(false);
        }
    }, []);

    const reset = useCallback((): void => {
        setAnalysis(null);
        setError(null);
        setLoading(false);
        setProgressMessage('Gathering context...');
        setActiveTools([]);
    }, []);

    return { analysis, loading, error, progressMessage, activeTools, analyze, reset };
};

export default useAlertAnalysis;
