/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useRef } from 'react';
import { apiFetch } from '../utils/apiClient';
import { djb2Hash } from '../utils/textHelpers';

export interface PlanNode {
    'Node Type': string;
    'Total Cost': number;
    'Startup Cost': number;
    'Plan Rows': number;
    'Plan Width': number;
    'Relation Name'?: string;
    'Schema'?: string;
    'Alias'?: string;
    'Index Name'?: string;
    'Filter'?: string;
    'Index Cond'?: string;
    'Hash Cond'?: string;
    'Join Type'?: string;
    'Sort Key'?: string[];
    'Sort Method'?: string;
    'Sort Space Used'?: number;
    'Sort Space Type'?: string;
    'Parallel Aware'?: boolean;
    'Async Capable'?: boolean;
    'Output'?: string[];
    'Scan Direction'?: string;
    'Strategy'?: string;
    'Merge Cond'?: string;
    'Recheck Cond'?: string;
    'Join Filter'?: string;
    'Inner Unique'?: boolean;
    'Rows Removed by Filter'?: number;
    'Rows Removed by Join Filter'?: number;
    'Group Key'?: string[];
    'Partial Mode'?: string;
    'Workers Planned'?: number;
    'Workers Launched'?: number;
    'Single Copy'?: boolean;
    'Subplan Name'?: string;
    'CTE Name'?: string;
    'Parent Relationship'?: string;
    Plans?: PlanNode[];
    [key: string]: unknown;
}

export interface UseQueryPlanReturn {
    textPlan: string | null;
    jsonPlan: PlanNode[] | null;
    loading: boolean;
    error: string | null;
    fetch: () => void;
}

/** Cache TTL: 5 minutes. */
const CACHE_TTL_MS = 5 * 60 * 1000;

interface CacheEntry {
    textPlan: string | null;
    jsonPlan: PlanNode[] | null;
    timestamp: number;
}

/** Module-level cache for query plans. */
const planCache = new Map<string, CacheEntry>();

/**
 * Build a cache key from query plan identifiers.
 */
function computeCacheKey(
    query: string,
    connectionId: number,
    databaseName: string,
): string {
    const raw =
        `plan:${query}:${connectionId}:${databaseName}`;
    return djb2Hash(raw);
}

/**
 * Response shape from the query execution endpoint.
 */
interface QueryResponse {
    results: Array<{
        columns: string[];
        rows: string[][];
        row_count: number;
        truncated: boolean;
        query: string;
        error?: string;
    }>;
    total_statements: number;
}

/** Check if a query contains $N parameter placeholders. */
function hasParameters(query: string): boolean {
    return /\$\d+/.test(query);
}

/**
 * Message shown when the captured statement is one that
 * PostgreSQL's EXPLAIN cannot accept.
 */
export const NON_EXPLAINABLE_PLAN_MESSAGE =
    'Query plans are not available for this type of statement. '
    + 'PostgreSQL can only explain SELECT, INSERT, UPDATE, '
    + 'DELETE, and similar data statements, not maintenance or '
    + 'utility commands such as VACUUM, ANALYZE, or REINDEX.';

/**
 * Message shown when PostgreSQL's EXPLAIN accepts the statement
 * but reports no plan structure for it.
 */
export const PLANLESS_STATEMENT_MESSAGE =
    'PostgreSQL accepts this statement for EXPLAIN but produces '
    + 'no query plan for it, because it runs as a utility '
    + 'command.';

/**
 * How PostgreSQL's EXPLAIN treats a captured statement.
 */
export type ExplainSupport =
    /** EXPLAIN accepts the statement and returns a plan. */
    | 'plan'
    /** EXPLAIN accepts the statement but returns no plan. */
    | 'planless'
    /** EXPLAIN rejects the statement with a syntax error. */
    | 'unsupported';

/**
 * Leading keywords for which PostgreSQL's EXPLAIN returns a
 * plan.  TABLE is included because a bare `TABLE foo` is just a
 * SELECT in disguise and plans exactly like one; the word
 * boundary keeps `TABLESAMPLE` and similar tokens out, and a
 * CREATE TABLE ... AS is handled separately below.  WITH is
 * included because a CTE almost always fronts a SELECT or DML
 * statement; reliably identifying the inner statement would
 * need balanced-paren parsing rather than a regex, so we allow
 * it and preserve the pre-existing behaviour.
 */
const EXPLAINABLE_LEADING_KEYWORDS =
    /^(?:select|insert|update|delete|merge|values|table|execute|declare|with)\b/i;

/**
 * Statements that EXPLAIN parses but executes as utility
 * commands, so the server answers with 'Utility statements have
 * no plan structure' rather than a plan.
 */
const PLANLESS_PATTERN =
    /^refresh\s+materialized\s+view\b/i;

/** CREATE MATERIALIZED VIEW ... AS <query>. */
const CREATE_MATVIEW_PATTERN =
    /^create\s+(?:or\s+replace\s+)?materialized\s+view\b/i;

/**
 * CREATE [TEMP|UNLOGGED] TABLE ... AS <query>.  The trailing
 * query keyword is required so that a plain CREATE TABLE with,
 * say, a GENERATED ALWAYS AS column is not misread as a CTAS.
 */
const CREATE_TABLE_AS_PATTERN =
    /^create\s+(?:global\s+|local\s+)?(?:temp(?:orary)?\s+|unlogged\s+)?table\b[\s\S]*?\bas\s+\(?\s*(?:select|values|table|execute|with)\b/i;

/**
 * Strip leading whitespace, SQL comments, and opening
 * parentheses so that the statement's first keyword can be
 * inspected.  pg_stat_statements text is often prefixed with a
 * framework tag comment or wrapped in parentheses.
 */
function stripLeadingNoise(query: string): string {
    let text = query;

    for (;;) {
        const trimmed = text.replace(/^[\s(]+/, '');
        if (trimmed !== text) {
            text = trimmed;
            continue;
        }
        if (text.startsWith('--')) {
            const newline = text.indexOf('\n');
            text = newline === -1 ? '' : text.slice(newline + 1);
            continue;
        }
        if (text.startsWith('/*')) {
            const close = text.indexOf('*/');
            text = close === -1 ? '' : text.slice(close + 2);
            continue;
        }
        return text;
    }
}

/**
 * Classify how PostgreSQL's EXPLAIN treats the given statement.
 * This is deliberately an allowlist of the leading keywords
 * EXPLAIN supports rather than a denylist of utility commands:
 * pg_stat_statements records utility statements such as VACUUM
 * or REINDEX alongside DML, and an allowlist fails safe by
 * treating anything unrecognised as unsupported instead of
 * surfacing a raw syntax error from the server.
 */
export function classifyExplainSupport(
    query: string,
): ExplainSupport {
    const text = stripLeadingNoise(query ?? '');
    if (text === '') {
        return 'unsupported';
    }
    if (PLANLESS_PATTERN.test(text)) {
        return 'planless';
    }
    if (
        EXPLAINABLE_LEADING_KEYWORDS.test(text)
        || CREATE_MATVIEW_PATTERN.test(text)
        || CREATE_TABLE_AS_PATTERN.test(text)
    ) {
        return 'plan';
    }
    return 'unsupported';
}

/**
 * Pull the plan nodes out of a parsed EXPLAIN FORMAT JSON
 * response.  PostgreSQL answers with [{ "Plan": { ... } }] for a
 * planned statement, but a utility statement that EXPLAIN
 * accepts yields [ "Utility Statement" ], so check the shape
 * before trusting it and return null when no plan is present.
 */
function extractJsonPlan(parsed: unknown): PlanNode[] | null {
    if (!Array.isArray(parsed) || parsed.length === 0) {
        return null;
    }
    const entries = parsed.filter(
        (entry): entry is Record<string, unknown> =>
            typeof entry === 'object' && entry !== null,
    );
    if (entries.length !== parsed.length) {
        return null;
    }
    if (entries[0].Plan) {
        return entries.map(
            entry => entry.Plan as PlanNode,
        );
    }
    return entries as unknown as PlanNode[];
}

/**
 * Execute an EXPLAIN query via the standard query endpoint.
 */
async function fetchExplain(
    query: string,
    format: 'text' | 'json',
    connectionId: number,
    databaseName: string,
): Promise<string> {
    let explainQuery: string;
    if (hasParameters(query)) {
        // GENERIC_PLAN (PG 16+) plans parameterized queries
        // without needing actual parameter values.
        explainQuery = format === 'json'
            ? `EXPLAIN (VERBOSE, GENERIC_PLAN, FORMAT JSON) ${query}`
            : `EXPLAIN (GENERIC_PLAN) ${query}`;
    } else {
        explainQuery = format === 'json'
            ? `EXPLAIN (VERBOSE, FORMAT JSON) ${query}`
            : `EXPLAIN ${query}`;
    }

    const response = await apiFetch(
        `/api/v1/connections/${connectionId}/query`,
        {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                query: explainQuery,
                database_name: databaseName,
            }),
        },
    );

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(
            `EXPLAIN ${format} request failed: ${errorText}`,
        );
    }

    const result: QueryResponse = await response.json();

    if (result.results[0]?.error) {
        throw new Error(result.results[0].error);
    }

    return result.results[0].rows.map(r => r[0]).join('\n');
}

/**
 * Hook that fetches EXPLAIN plans for a PostgreSQL query in
 * both text and JSON formats.  Does not auto-fetch; the caller
 * must invoke the returned `fetch` function.
 */
export function useQueryPlan(
    query: string,
    connectionId: number,
    databaseName: string,
): UseQueryPlanReturn {
    const [textPlan, setTextPlan] =
        useState<string | null>(null);
    const [jsonPlan, setJsonPlan] =
        useState<PlanNode[] | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const queryRef = useRef(query);
    const connRef = useRef(connectionId);
    const dbRef = useRef(databaseName);
    queryRef.current = query;
    connRef.current = connectionId;
    dbRef.current = databaseName;

    const fetchPlan = useCallback((): void => {
        const q = queryRef.current;
        const conn = connRef.current;
        const db = dbRef.current;

        // Utility statements such as VACUUM are tracked by
        // pg_stat_statements but cannot be wrapped in EXPLAIN,
        // and a few others parse but yield no plan, so
        // short-circuit both rather than issuing a request that
        // cannot usefully succeed.
        const support = classifyExplainSupport(q);
        if (support !== 'plan') {
            setTextPlan(null);
            setJsonPlan(null);
            setLoading(false);
            setError(
                support === 'planless'
                    ? PLANLESS_STATEMENT_MESSAGE
                    : NON_EXPLAINABLE_PLAN_MESSAGE,
            );
            return;
        }

        const cacheKey = computeCacheKey(q, conn, db);

        // Check the cache first.
        const cached = planCache.get(cacheKey);
        if (
            cached
            && (Date.now() - cached.timestamp) < CACHE_TTL_MS
        ) {
            setTextPlan(cached.textPlan);
            setJsonPlan(cached.jsonPlan);
            setLoading(false);
            setError(null);
            return;
        }

        setLoading(true);
        setError(null);

        const textPromise = fetchExplain(
            q, 'text', conn, db,
        );
        const jsonPromise = fetchExplain(
            q, 'json', conn, db,
        );

        Promise.allSettled([textPromise, jsonPromise])
            .then(([textResult, jsonResult]) => {
                let newTextPlan: string | null = null;
                let newJsonPlan: PlanNode[] | null = null;
                const errors: string[] = [];

                if (textResult.status === 'fulfilled') {
                    newTextPlan = textResult.value;
                } else {
                    errors.push(
                        `Text plan: ${textResult.reason}`,
                    );
                }

                if (jsonResult.status === 'fulfilled') {
                    try {
                        const parsed = JSON.parse(
                            jsonResult.value,
                        );
                        const extracted =
                            extractJsonPlan(parsed);
                        if (extracted) {
                            newJsonPlan = extracted;
                        } else {
                            errors.push(
                                'JSON plan: the response '
                                + 'contains no plan structure',
                            );
                        }
                    } catch {
                        errors.push(
                            'JSON plan: failed to parse '
                            + 'response',
                        );
                    }
                } else {
                    errors.push(
                        `JSON plan: ${jsonResult.reason}`,
                    );
                }

                setTextPlan(newTextPlan);
                setJsonPlan(newJsonPlan);

                if (
                    newTextPlan === null
                    && newJsonPlan === null
                ) {
                    setError(errors.join('; '));
                } else {
                    setError(null);
                    planCache.set(cacheKey, {
                        textPlan: newTextPlan,
                        jsonPlan: newJsonPlan,
                        timestamp: Date.now(),
                    });
                }
            })
            .catch((err: unknown) => {
                setError((err as Error).message);
            })
            .finally(() => {
                setLoading(false);
            });
    }, []);

    return {
        textPlan,
        jsonPlan,
        loading,
        error,
        fetch: fetchPlan,
    };
}

export default useQueryPlan;
