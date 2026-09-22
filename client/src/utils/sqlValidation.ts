/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - client for the SQL validation endpoint.
 *
 * Generated SQL is planned with EXPLAIN in a rolled-back, read-only
 * transaction before the UI offers to run it, so that hallucinated
 * columns and syntax errors are caught before the user clicks Run.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { apiFetch } from './apiClient';
import {
    CONNECTION_ID_COMMENT_RE,
    stripConnectionIdComment,
} from '../components/shared/sqlDetection';

/** Outcome of planning a single statement. */
export type SqlStatementStatus = 'valid' | 'invalid' | 'unsupported';

/** Validation outcome for one statement in a block. */
export interface SqlStatementValidation {
    query: string;
    status: SqlStatementStatus;
    error: string;
}

/** Response body of POST /api/v1/connections/{id}/query/validate. */
export interface SqlValidationResponse {
    valid: boolean;
    total_statements: number;
    statements: SqlStatementValidation[];
}

/**
 * Narrows a decoded response body to a validation response.
 *
 * The body is decoded as unknown rather than asserted, so that a
 * malformed or unexpected payload is rejected here instead of
 * surfacing as an undefined statement list further down.
 */
const isSqlValidationResponse = (
    value: unknown,
): value is SqlValidationResponse => (
    typeof value === 'object'
    && value !== null
    && Array.isArray((value as { statements?: unknown }).statements)
);

/**
 * Validates a block of SQL, resolving to null when validation could not
 * be attempted at all (no connection, request failure).
 */
export type SqlBlockValidator =
    (_sql: string) => Promise<SqlValidationResponse | null>;

/**
 * Maximum number of validation requests in flight at once.
 *
 * A long analysis renders dozens of SQL blocks, each of which validates
 * when it mounts; without a bound they would all hit the server in the
 * same tick.
 */
export const MAX_CONCURRENT_VALIDATIONS = 3;

let inFlight = 0;
const waiting: (() => void)[] = [];

/** Take a slot in the concurrency window, queueing when it is full. */
const acquireSlot = (): Promise<void> => {
    if (inFlight < MAX_CONCURRENT_VALIDATIONS) {
        inFlight++;
        return Promise.resolve();
    }
    return new Promise<void>((resolve) => {
        waiting.push(() => {
            inFlight++;
            resolve();
        });
    });
};

/** Release a slot and start the next queued request, if any. */
const releaseSlot = (): void => {
    inFlight--;
    const next = waiting.shift();
    if (next) {
        next();
    }
};

/** Cached results, so a re-render never re-validates the same SQL. */
const validationCache = new Map<string, Promise<SqlValidationResponse | null>>();

/** Build the cache key for a validation request. */
const cacheKey = (
    connectionId: number,
    databaseName: string | undefined,
    sql: string,
): string => `${connectionId}\u0000${databaseName ?? ''}\u0000${sql}`;

/** Drop every cached validation result. Exported for tests. */
export const clearSqlValidationCache = (): void => {
    validationCache.clear();
};

/** Issue the validation request, respecting the concurrency bound. */
const requestValidation = async (
    connectionId: number,
    databaseName: string | undefined,
    sql: string,
): Promise<SqlValidationResponse | null> => {
    await acquireSlot();
    try {
        const body: Record<string, unknown> = { query: sql };
        if (databaseName) {
            body.database_name = databaseName;
        }

        const response = await apiFetch(
            `/api/v1/connections/${connectionId}/query/validate`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            },
        );

        if (!response.ok) {
            return null;
        }

        const data: unknown = await response.json();
        if (!isSqlValidationResponse(data)) {
            return null;
        }
        return data;
    } catch {
        // A validation failure must never block the user: the caller
        // treats null as "could not validate" and still offers Run.
        return null;
    } finally {
        releaseSlot();
    }
};

/**
 * Validate a block of SQL against a connection.
 *
 * Identical requests share a single in-flight promise and its cached
 * result, so repeated renders of the same analysis do not re-validate.
 * Resolves to null when the request could not be completed.
 */
export const validateSql = (
    connectionId: number,
    sql: string,
    databaseName?: string,
): Promise<SqlValidationResponse | null> => {
    const trimmed = sql.trim();
    if (!trimmed) {
        return Promise.resolve(null);
    }

    const key = cacheKey(connectionId, databaseName, trimmed);
    const cached = validationCache.get(key);
    if (cached) {
        return cached;
    }

    const pending = requestValidation(connectionId, databaseName, trimmed)
        .then((result) => {
            if (result === null) {
                // Do not cache failures: a transient error should not
                // permanently mark the block as unvalidated.
                validationCache.delete(key);
            }
            return result;
        });

    validationCache.set(key, pending);
    return pending;
};

/** Build a validator bound to a single connection and database. */
export const createSqlValidator = (
    connectionId: number,
    databaseName?: string,
): SqlBlockValidator =>
    (sql: string) => validateSql(connectionId, sql, databaseName);

/**
 * Build a validator for cluster analysis, where each block may carry a
 * `-- connection_id: N` routing comment naming its target server.
 *
 * Blocks without a routing comment fall back to `defaultConnectionId`,
 * and are skipped when there is none.
 */
export const createRoutedSqlValidator = (
    defaultConnectionId?: number,
    databaseName?: string,
): SqlBlockValidator =>
    (sql: string) => {
        const match = CONNECTION_ID_COMMENT_RE.exec(sql);
        const targetId = match
            ? parseInt(match[1], 10)
            : defaultConnectionId;
        if (targetId === undefined || Number.isNaN(targetId)) {
            return Promise.resolve(null);
        }
        return validateSql(
            targetId,
            stripConnectionIdComment(sql),
            databaseName,
        );
    };

/**
 * Extract the contents of every ```sql fenced block in a markdown
 * document, in document order.
 */
export const extractSqlCodeBlocks = (markdown: string): string[] => {
    const blocks: string[] = [];
    const fenceRe = /```sql\s*\n([\s\S]*?)```/gi;
    let match = fenceRe.exec(markdown);
    while (match !== null) {
        const body = match[1].trim();
        if (body) {
            blocks.push(body);
        }
        match = fenceRe.exec(markdown);
    }
    return blocks;
};
