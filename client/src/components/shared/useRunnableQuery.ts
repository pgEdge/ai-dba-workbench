/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - useRunnableQuery hook. Runs the SQL in a
 * runnable code block, and tracks the confirmation and action state of
 * a block whose SQL writes.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useMemo } from 'react';
import { apiFetch } from '../../utils/apiClient';
import { hasText } from '../../utils/textHelpers';

export interface StatementResult {
    columns?: string[];
    rows?: string[][];
    row_count?: number;
    truncated?: boolean;
    query: string;
    error?: string;
}

export interface QueryResponse {
    results?: StatementResult[];
    total_statements?: number;
    /** Set on a 409: the SQL writes, so it must be proposed as an action. */
    requires_action?: boolean;
    write_statements?: string[];
    error?: string;
}

/** The connection, and optionally the database, a query runs against. */
export interface QueryTarget {
    connectionId: number;
    databaseName?: string;
}

function sameTarget(
    a: QueryTarget | undefined,
    b: QueryTarget,
): a is QueryTarget {
    return a !== undefined
        && a.connectionId === b.connectionId
        && a.databaseName === b.databaseName;
}

export interface QueryState {
    loading: boolean;
    response: QueryResponse | null;
    error: string | null;
    pendingConfirmation: boolean;
    writeStatements: string[];
    /**
     * The SQL the server refused as a write, captured when Run was
     * pressed. Confirming proposes exactly this SQL, even if the block's
     * content has changed since.
     */
    confirmSQL?: string;
    /** The target the server refused `confirmSQL` on. */
    confirmTarget?: QueryTarget;
}

export const IDLE_QUERY_STATE: QueryState = {
    loading: false,
    response: null,
    error: null,
    pendingConfirmation: false,
    writeStatements: [],
};

const errorState = (error: string): QueryState => ({
    ...IDLE_QUERY_STATE,
    error,
});

/** The `error` field of a JSON error body, or null when it has none. */
function jsonErrorOf(body: unknown): string | null {
    if (typeof body !== 'object' || body === null) {
        return null;
    }
    const { error } = body as { error?: unknown };
    return typeof error === 'string' && hasText(error) ? error : null;
}

/**
 * The error message in a failed query response body: a JSON body gives
 * its `error` field, and any other body is the message itself.
 */
function errorMessageOf(text: string, fallback: string): string {
    try {
        return jsonErrorOf(JSON.parse(text)) ?? fallback;
    } catch {
        return hasText(text) ? text : fallback;
    }
}

/**
 * Read the error message from a failed query response. The body can be
 * read only once, so it is read as text and parsed afterwards.
 */
async function readErrorMessage(response: Response): Promise<string> {
    const text = await response.text().catch(() => '');
    return errorMessageOf(text, `Query failed with status ${response.status}`);
}

/** Turn a 409 into a write confirmation, or an error if it is not one. */
async function refusalState(
    response: Response,
    sql: string,
    target: QueryTarget,
): Promise<QueryState> {
    const refusal: QueryResponse = await response.json().catch(() => ({}));
    if (!refusal.requires_action) {
        return errorState(
            jsonErrorOf(refusal) ?? 'Query failed with status 409',
        );
    }
    return {
        ...IDLE_QUERY_STATE,
        pendingConfirmation: true,
        writeStatements: refusal.write_statements ?? [],
        confirmSQL: sql,
        confirmTarget: target,
    };
}

/** Send `sql` to the query endpoint and return the resulting state. */
export async function executeQuery(
    sql: string,
    connectionId: number,
    databaseName?: string,
): Promise<QueryState> {
    try {
        const body: Record<string, unknown> = { query: sql };
        if (databaseName) {
            body.database_name = databaseName;
        }
        const response = await apiFetch(
            `/api/v1/connections/${connectionId}/query`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            },
        );
        if (response.status === 409) {
            return await refusalState(
                response,
                sql,
                { connectionId, databaseName },
            );
        }
        if (!response.ok) {
            return errorState(await readErrorMessage(response));
        }
        const data: QueryResponse = await response.json();
        return { ...IDLE_QUERY_STATE, response: data };
    } catch (err) {
        return errorState((err as Error).message);
    }
}

/** A confirmed write, with the target it was refused on. */
export interface ConfirmedWrite {
    sql: string;
    target: QueryTarget;
}

export interface UseRunnableQueryResult {
    queryState: QueryState;
    /** The write handed to an ActionBlock, or null when there is none. */
    confirmedWrite: ConfirmedWrite | null;
    run: () => Promise<void>;
    confirm: () => void;
    dismiss: () => void;
}

/**
 * Run a code block's SQL. A write the server refuses waits for
 * confirmation; confirming hands the SQL and target captured at Run time
 * to an ActionBlock, which proposes and runs it. A refusal belongs to the
 * connection and database it was refused on, so changing either before
 * confirming discards it and the write needs a fresh Run. While the
 * action is showing, Run does nothing: only dismissing the action clears
 * it, and the action keeps the target it was confirmed on.
 */
export function useRunnableQuery(
    executableSQL: string,
    connectionId: number,
    databaseName?: string,
): UseRunnableQueryResult {
    const [queryState, setQueryState] = useState<QueryState>(IDLE_QUERY_STATE);
    const [confirmedWrite, setConfirmedWrite] =
        useState<ConfirmedWrite | null>(null);
    const target = useMemo<QueryTarget>(
        () => ({ connectionId, databaseName }),
        [connectionId, databaseName],
    );

    // Discard a refusal once the target moves on, including one whose
    // run finished after the move.
    const staleRefusal = confirmedWrite === null
        && queryState.pendingConfirmation
        && !sameTarget(queryState.confirmTarget, target);
    if (staleRefusal) {
        setQueryState(IDLE_QUERY_STATE);
    }

    const run = useCallback(async () => {
        if (confirmedWrite !== null) {
            return;
        }
        const sql = executableSQL;
        if (!sql.trim()) {
            setQueryState(
                errorState('No executable SQL found in this code block'),
            );
            return;
        }
        setQueryState({ ...IDLE_QUERY_STATE, loading: true });
        setQueryState(
            await executeQuery(sql, target.connectionId, target.databaseName),
        );
    }, [confirmedWrite, executableSQL, target]);

    const confirm = useCallback(() => {
        const { confirmSQL, confirmTarget } = queryState;
        if (confirmSQL === undefined || !sameTarget(confirmTarget, target)) {
            return;
        }
        setConfirmedWrite({ sql: confirmSQL, target: confirmTarget });
    }, [queryState, target]);

    const dismiss = useCallback(() => {
        setConfirmedWrite(null);
        setQueryState(IDLE_QUERY_STATE);
    }, []);

    return {
        queryState: staleRefusal ? IDLE_QUERY_STATE : queryState,
        confirmedWrite,
        run,
        confirm,
        dismiss,
    };
}
