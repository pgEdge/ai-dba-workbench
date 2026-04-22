/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useEffect, useRef, useCallback } from 'react';
import { apiFetch } from '../utils/apiClient';
import { formatTime } from '../utils/formatters';
import { LLMResponse } from '../types/llm';
import { djb2Hash, ANALYSIS_CACHE_TTL_MS } from '../utils/textHelpers';
import { useAnalysisState } from './useAnalysisState';

export interface QueryOverviewInput {
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

export interface UseQueryOverviewReturn {
    summary: string | null;
    loading: boolean;
    error: string | null;
    generatedAt: Date | null;
    refresh: () => void;
}

/** Module-level cache for overview summaries. */
const overviewCache = new Map<
    string,
    { summary: string; timestamp: number }
>();

const SYSTEM_PROMPT = `You are a PostgreSQL expert providing a brief status overview of a query from pg_stat_statements.

Produce a concise 2-3 sentence plain-text summary covering:
- Whether the query appears healthy or has potential issues
- The most notable performance characteristic (e.g. high call frequency, slow mean time, poor cache hit ratio)
- One key recommendation if applicable

Do NOT use markdown formatting, headings, bullets, or code blocks. Write plain prose only. Keep it under 60 words.

CRITICAL: Your output is rendered in a static, read-only panel. Do NOT ask questions or offer to investigate further.`;

/**
 * Build a cache key from the query overview identifiers.
 */
function computeCacheKey(
    queryId: string,
    connectionId: number,
    databaseName: string,
): string {
    const raw = `overview:${queryId}:${connectionId}:${databaseName}`;
    return djb2Hash(raw);
}

/**
 * Hook that generates a brief plain-text summary of query
 * performance via the LLM chat endpoint.  Automatically triggers
 * when a non-null input is provided and caches results for 30
 * minutes.
 */
export function useQueryOverview(
    input: QueryOverviewInput | null,
): UseQueryOverviewReturn {
    const {
        state,
        setAnalysis,
        setLoading,
        setError,
    } = useAnalysisState();

    const [generatedAt, setGeneratedAt] = useState<Date | null>(null);
    const triggeredRef = useRef<boolean>(false);
    const lastKeyRef = useRef<string>('');

    const generateSummary = useCallback(
        async (data: QueryOverviewInput): Promise<void> => {
            const cacheKey = computeCacheKey(
                data.queryId,
                data.connectionId,
                data.databaseName,
            );

            // Check the cache first
            const cached = overviewCache.get(cacheKey);
            if (
                cached
                && (Date.now() - cached.timestamp) < ANALYSIS_CACHE_TTL_MS
            ) {
                setAnalysis(cached.summary);
                setGeneratedAt(new Date(cached.timestamp));
                setLoading(false);
                setError(null);
                return;
            }

            setLoading(true);
            setError(null);

            try {
                const totalBlks =
                    data.sharedBlksHit + data.sharedBlksRead;
                const hitRatio = totalBlks > 0
                    ? (
                        (data.sharedBlksHit / totalBlks) * 100
                    ).toFixed(1)
                    : 'N/A';

                const queryPreview =
                    data.queryText.length > 200
                        ? data.queryText.substring(0, 200) + '...'
                        : data.queryText;

                const userMessage =
                    `Query: ${queryPreview}\n`
                    + `Calls: ${data.calls.toLocaleString()}`
                    + ` | Mean Time: ${formatTime(data.meanExecTime)}`
                    + ` | Total Time: ${formatTime(data.totalExecTime)}`
                    + ` | Rows: ${data.rows.toLocaleString()}`
                    + ` | Buffer Hit Ratio: ${hitRatio}%`;

                const response = await apiFetch('/api/v1/llm/chat', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({
                        messages: [
                            { role: 'user', content: userMessage },
                        ],
                        system: SYSTEM_PROMPT,
                    }),
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error(
                        `Overview request failed: ${errorText}`
                    );
                }

                const result: LLMResponse = await response.json();
                const text = result.content
                    ?.filter(c => c.type === 'text')
                    .map(c => c.text)
                    .join('\n') ?? '';

                setAnalysis(text);
                setGeneratedAt(new Date());

                overviewCache.set(cacheKey, {
                    summary: text,
                    timestamp: Date.now(),
                });
            } catch (err) {
                console.error('Query overview error:', err);
                setError((err as Error).message);
            } finally {
                setLoading(false);
            }
        },
        [setAnalysis, setLoading, setError],
    );

    // Auto-trigger when input is provided
    useEffect(() => {
        if (!input) {
            triggeredRef.current = false;
            lastKeyRef.current = '';
            return;
        }

        const key = `${input.queryId}:${input.connectionId}:${input.databaseName}`;
        if (key !== lastKeyRef.current) {
            triggeredRef.current = false;
            lastKeyRef.current = key;
        }

        if (!triggeredRef.current) {
            triggeredRef.current = true;
            generateSummary(input);
        }
    }, [input, generateSummary]);

    // Refresh: clear cache and re-generate
    const refresh = useCallback((): void => {
        if (!input) { return; }
        const cacheKey = computeCacheKey(
            input.queryId,
            input.connectionId,
            input.databaseName,
        );
        overviewCache.delete(cacheKey);
        generateSummary(input);
    }, [input, generateSummary]);

    if (!input) {
        return {
            summary: null,
            loading: false,
            error: null,
            generatedAt: null,
            refresh: () => {},
        };
    }

    return {
        summary: state.analysis,
        loading: state.loading,
        error: state.error,
        generatedAt,
        refresh,
    };
}

export default useQueryOverview;
