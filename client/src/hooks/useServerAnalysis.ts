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
import { apiGet } from '../utils/apiClient';
import { formatConnectionContext } from '../utils/connectionContext';
import {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
    ToolInputSchema,
} from '../types/llm';
import { TimelineEvent } from '../components/EventTimeline/types';

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

// Human-readable display names for MCP tools used during server analysis
const TOOL_DISPLAY_NAMES: Record<string, string> = {
    query_metrics: 'Querying metrics',
    get_metric_baselines: 'Fetching metric baselines',
    get_alert_history: 'Reviewing alert history',
    get_alert_rules: 'Checking alert rules',
    query_database: 'Querying database',
    get_schema_info: 'Inspecting schema',
    list_probes: 'Listing probes',
    describe_probe: 'Examining probe details',
};

// Module-level cache for analysis results (persists across dialog open/close)
const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes
const analysisCache = new Map<string, { analysis: string; timestamp: number }>();

export interface ServerAnalysisInput {
    type: 'server' | 'cluster';
    id: number | string;
    name: string;
    serverIds?: number[];
    servers?: Array<{ id: number; name: string }>;
}

interface AnalysisTool {
    name: string;
    description: string;
    inputSchema: ToolInputSchema;
}

interface Message {
    role: string;
    content: string | LLMContentBlock[] | ToolResult[];
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
   - If the server context or tool results provide specific object names, use those exact names in the SQL.
   - If remediation requires acting on specific database objects that are not yet known, first provide a diagnostic query that identifies the affected objects (e.g., tables with high dead tuple ratios), then provide the remediation SQL using the actual names returned by that diagnostic query.
   - If the specific objects cannot be determined, provide ONLY the diagnostic query and explain that the user should run the remediation command on the objects it identifies. Do NOT generate non-executable SQL containing placeholders.

7. NEVER suggest dropping indexes that implement PRIMARY KEY or UNIQUE constraints, even if they show zero scans in pg_stat_user_indexes. These indexes enforce data integrity constraints and cannot be removed without dropping the constraint itself. Low scan counts on constraint indexes are normal and expected; they serve a correctness purpose, not a performance purpose.

Keep responses concise and actionable. Do not offer to perform additional actions, run further queries, or investigate anything else. Do not ask follow-up questions or ask what the user would like to do next. Begin your response directly with the first section heading (## Performance Review). Do not include any introductory text, preamble, or conversational remarks before the report. Your analysis is displayed in a read-only report that the user cannot respond to.`;

const CLUSTER_ADDENDUM = `

IMPORTANT: This is a CLUSTER analysis covering multiple servers. When providing SQL queries, you MUST include a SQL comment as the FIRST LINE of every SQL code block indicating which server the query should run on, using this exact format:
-- connection_id: {id}
where {id} is the numeric connection ID of the target server. This comment is used by the UI to route query execution to the correct server.`;

// Tool definitions for the LLM (must use camelCase inputSchema to match Go struct)
const ANALYSIS_TOOLS: AnalysisTool[] = [
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
        name: "query_database",
        description: "Execute a read-only SQL query against a PostgreSQL connection",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID to query" },
                query: { type: "string", description: "SQL query to execute (read-only)" },
                database_name: { type: "string", description: "Optional database name to connect to" }
            },
            required: ["connection_id", "query"]
        }
    },
    {
        name: "get_schema_info",
        description: "Get schema information for a database including tables, indexes, and constraints",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID to query" },
                database_name: { type: "string", description: "Database name" },
                schema_name: { type: "string", description: "Schema name (default: public)" }
            },
            required: ["connection_id"]
        }
    },
    {
        name: "list_probes",
        description: "List available monitoring probes and their descriptions",
        inputSchema: {
            type: "object",
            properties: {
                connection_id: { type: "integer", description: "Connection ID (optional, for connection-specific probes)" }
            },
            required: []
        }
    },
    {
        name: "describe_probe",
        description: "Get detailed information about a specific monitoring probe including its metrics",
        inputSchema: {
            type: "object",
            properties: {
                probe_name: { type: "string", description: "Name of the probe to describe" }
            },
            required: ["probe_name"]
        }
    }
];

/**
 * Fetch timeline events surrounding the current time.
 * Uses a 24-hour window centered on now.
 */
async function fetchTimelineEvents(
    connectionId: number,
): Promise<string> {
    const now = new Date();
    const startTime = new Date(now.getTime() - 12 * 60 * 60 * 1000);
    const endTime = new Date(now.getTime() + 12 * 60 * 60 * 1000);

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

    return '\nTimeline Events (24h window):\n' + lines.join('\n');
}

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
        fetchTimelineEvents(connectionId),
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
    const { user: _user } = useAuth();
    const [analysis, setAnalysis] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [progressMessage, setProgressMessage] = useState<string>('Gathering context...');
    const [activeTools, setActiveTools] = useState<string[]>([]);

    const analyze = useCallback(async (input: ServerAnalysisInput): Promise<void> => {
        setLoading(true);
        setError(null);
        setAnalysis(null);
        setProgressMessage('Gathering context...');
        setActiveTools([]);

        const cacheKey = `${input.type}-${input.id}`;

        // Check client-side cache (with TTL validation)
        const cached = analysisCache.get(cacheKey);
        if (cached && (Date.now() - cached.timestamp) < CACHE_TTL_MS) {
            setAnalysis(cached.analysis);
            setLoading(false);
            return;
        }

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

        setProgressMessage('Starting analysis...');

        try {
            // Agentic loop - keep calling until no more tool use
            const maxIterations = 15;
            let iterations = 0;
            let gotResponse = false;
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
                        system: systemPrompt,
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
            if (gotResponse) {
                analysisCache.set(cacheKey, { analysis: analysisText, timestamp: Date.now() });
            }

            if (iterations >= maxIterations && !gotResponse) {
                throw new Error('Analysis exceeded maximum iterations');
            }

        } catch (err) {
            console.error('Server analysis error:', err);
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
    return !!cached && (Date.now() - cached.timestamp) < CACHE_TTL_MS;
}

export default useServerAnalysis;
