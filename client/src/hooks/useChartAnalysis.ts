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
import { ChartData } from '../components/Chart/types';
import { apiGet } from '../utils/apiClient';
import { formatConnectionContext } from '../utils/connectionContext';
import { LLMResponse } from '../types/llm';
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
const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes
const analysisCache = new Map<string, { analysis: string; timestamp: number }>();

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

If suggesting SQL queries, use \`\`\`sql code blocks. SQL should be correct and executable PostgreSQL.

Keep responses concise and actionable.

CRITICAL: Your output is rendered in a static, read-only report. The user CANNOT reply, ask questions, or interact with you in any way. You MUST NOT:
- Ask the user anything (e.g. "Would you like me to...", "Do you want me to...", "Shall I...")
- Offer to perform follow-up actions or further investigation
- Suggest that you can do additional work
- End with a question of any kind
Write your analysis as a final, self-contained report with no conversational elements.`;

const SQL_RULES = `

CRITICAL rules for SQL code blocks - the user executes SQL directly from the UI so accuracy is essential:

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

5. When suggesting ALTER SYSTEM or other DDL statements, place them in separate code blocks from diagnostic SELECT queries.`;

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
    return !!cached && (Date.now() - cached.timestamp) < CACHE_TTL_MS;
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
 * Fetch timeline events for a connection within a time range.
 * Returns a formatted string for inclusion in the LLM prompt.
 */
async function fetchTimelineEvents(
    connectionId: number,
    timeRange: string | undefined,
): Promise<string> {
    // Calculate absolute time range from relative string
    const now = new Date();
    let startTime: Date;
    switch (timeRange) {
        case '1h': startTime = new Date(now.getTime() - 60 * 60 * 1000); break;
        case '6h': startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000); break;
        case '24h': startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000); break;
        case '7d': startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000); break;
        case '30d': startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000); break;
        default: startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000); break;
    }

    const params = new URLSearchParams({
        start_time: startTime.toISOString(),
        end_time: now.toISOString(),
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

    return '\nTimeline Events:\n' + lines.join('\n');
}

/**
 * Hook for managing LLM-powered chart data analysis.
 * Performs a single-shot LLM call with serialized chart data as context.
 */
export const useChartAnalysis = (): UseChartAnalysisReturn => {
    const [analysis, setAnalysis] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [progressMessage, setProgressMessage] = useState<string>('Preparing chart data...');
    const [activeTools, setActiveTools] = useState<string[]>([]);

    const analyze = useCallback(async (input: ChartAnalysisInput): Promise<void> => {
        // Check cache first to avoid flash of empty state
        const cacheKey = computeCacheKey(
            input.metricDescription,
            input.connectionId,
            input.databaseName,
            input.timeRange,
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
                    fetchTimelineEvents(input.connectionId, input.timeRange),
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
                ? CHART_ANALYSIS_SYSTEM_PROMPT + SQL_RULES
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

            const response = await fetch('/api/v1/llm/chat', {
                method: 'POST',
                credentials: 'include',
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
                .join('\n') || '';

            const cleanedText = stripPreamble(textContent);
            setAnalysis(cleanedText);
            setActiveTools([]);

            // Cache the result
            analysisCache.set(cacheKey, {
                analysis: cleanedText,
                timestamp: Date.now(),
            });
        } catch (err) {
            console.error('Chart analysis error:', err);
            setError((err as Error).message);
            setActiveTools([]);
        } finally {
            setLoading(false);
        }
    }, []);

    const reset = useCallback((): void => {
        setAnalysis(null);
        setError(null);
        setLoading(false);
        setProgressMessage('Preparing chart data...');
        setActiveTools([]);
    }, []);

    return { analysis, loading, error, progressMessage, activeTools, analyze, reset };
};

export default useChartAnalysis;
