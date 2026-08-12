/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Helpers for reasoning about captured SQL statement text.
 *
 * `pg_stat_statements` tracks utility and DDL commands alongside
 * ordinary DML, so statement text taken from it cannot simply be
 * wrapped in EXPLAIN. PostgreSQL's EXPLAIN accepts only SELECT,
 * INSERT, UPDATE, DELETE, MERGE, VALUES, EXECUTE, DECLARE,
 * CREATE TABLE AS and CREATE MATERIALIZED VIEW statements; the
 * grammar additionally accepts REFRESH MATERIALIZED VIEW.
 */

/**
 * Leading keywords that introduce a statement EXPLAIN accepts.
 * SELECT covers WITH-prefixed, TABLE and parenthesised forms,
 * which the grammar folds into a select statement.
 */
const EXPLAINABLE_KEYWORDS = new Set([
    'SELECT',
    'INSERT',
    'UPDATE',
    'DELETE',
    'MERGE',
    'VALUES',
    'EXECUTE',
    'DECLARE',
    'WITH',
    'TABLE',
]);

/** CREATE [GLOBAL|LOCAL] [TEMP|TEMPORARY|UNLOGGED] TABLE ... AS. */
const CREATE_TABLE_AS_RE =
    /^CREATE\s+(?:(?:GLOBAL|LOCAL)\s+)?(?:(?:TEMP|TEMPORARY|UNLOGGED)\s+)?TABLE\b[\s\S]*\bAS\b/i;

/** CREATE [MATERIALIZED] VIEW, optionally recursive-free. */
const CREATE_MATVIEW_RE = /^CREATE\s+MATERIALIZED\s+VIEW\b/i;

/** REFRESH MATERIALIZED VIEW, accepted by the EXPLAIN grammar. */
const REFRESH_MATVIEW_RE = /^REFRESH\s+MATERIALIZED\s+VIEW\b/i;

/**
 * Remove leading whitespace, SQL comments and opening parentheses
 * so that the first significant keyword can be inspected.
 */
function stripLeadingNoise(query: string): string {
    let text = query;
    for (;;) {
        const before = text;
        text = text
            .replace(/^\s+/, '')
            .replace(/^--[^\n]*(?:\n|$)/, '')
            .replace(/^\/\*[\s\S]*?\*\//, '')
            .replace(/^\(+/, '');
        if (text === before) {
            return text;
        }
    }
}

/**
 * Return the leading command keyword of a statement in upper
 * case, or an empty string when no keyword can be identified.
 */
export function getStatementKeyword(query: string): string {
    const match = /^[A-Za-z][A-Za-z_]*/.exec(
        stripLeadingNoise(query ?? ''),
    );
    return match ? match[0].toUpperCase() : '';
}

/**
 * Report whether PostgreSQL's EXPLAIN accepts the given
 * statement. Empty or unrecognised text is treated as not
 * explainable, so the caller avoids issuing a doomed request.
 */
export function isExplainable(query: string): boolean {
    const text = stripLeadingNoise(query ?? '');
    const keyword = getStatementKeyword(text);

    if (keyword === '') {
        return false;
    }
    if (EXPLAINABLE_KEYWORDS.has(keyword)) {
        return true;
    }
    if (keyword === 'CREATE') {
        return CREATE_TABLE_AS_RE.test(text)
            || CREATE_MATVIEW_RE.test(text);
    }
    if (keyword === 'REFRESH') {
        return REFRESH_MATVIEW_RE.test(text);
    }
    return false;
}

/**
 * Build a friendly explanation of why a statement has no query
 * plan. The keyword is included when one could be identified.
 */
export function buildNotExplainableMessage(keyword: string): string {
    const subject = keyword
        ? `${keyword} statements`
        : 'this statement';
    return (
        `PostgreSQL cannot produce a query plan for ${subject}. `
        + 'EXPLAIN supports only SELECT, INSERT, UPDATE, DELETE, '
        + 'MERGE, VALUES, EXECUTE, DECLARE, CREATE TABLE AS and '
        + 'CREATE MATERIALIZED VIEW statements; maintenance and '
        + 'other utility commands are recorded without a plan.'
    );
}
