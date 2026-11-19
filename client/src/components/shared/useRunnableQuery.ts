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

import { useState, useCallback } from 'react';
import { apiFetch } from '../../utils/apiClient';

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

/** Read the error message from a failed query response. */
async function readErrorMessage(response: Response): Promise<string> {
    let errorMessage = `Query failed with status ${response.status}`;
    try {
        const errorData = await response.json();
        if (errorData.error) {
            errorMessage = errorData.error;
        }
    } catch {
        // If JSON parsing fails, try plain text
        const errorText = await response.text();
        if (errorText) {
            errorMessage = errorText;
        }
    }
    return errorMessage;
}

/** Turn a 409 into a write confirmation, or an error if it is not one. */
async function refusalState(
    response: Response,
    sql: string,
): Promise<QueryState> {
    const refusal: QueryResponse = await response.json().catch(() => ({}));
    if (!refusal.requires_action) {
        return errorState(refusal.error || 'Query failed with status 409');
    }
    return {
        ...IDLE_QUERY_STATE,
        pendingConfirmation: true,
        writeStatements: refusal.write_statements || [],
        confirmSQL: sql,
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
            return await refusalState(response, sql);
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

export interface UseRunnableQueryResult {
    queryState: QueryState;
    /** The SQL handed to an ActionBlock, or null when there is none. */
    actionSQL: string | null;
    run: () => Promise<void>;
    confirm: () => void;
    dismiss: () => void;
}

/**
 * Run a code block's SQL. A write the server refuses waits for
 * confirmation; confirming hands the SQL captured at Run time to an
 * ActionBlock, which proposes and runs it. While that action is showing,
 * Run does nothing: only dismissing the action clears it.
 */
export function useRunnableQuery(
    executableSQL: string,
    connectionId: number,
    databaseName?: string,
): UseRunnableQueryResult {
    const [queryState, setQueryState] = useState<QueryState>(IDLE_QUERY_STATE);
    const [actionSQL, setActionSQL] = useState<string | null>(null);

    const run = useCallback(async () => {
        if (actionSQL !== null) {
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
        setQueryState(await executeQuery(sql, connectionId, databaseName));
    }, [actionSQL, executableSQL, connectionId, databaseName]);

    const confirm = useCallback(() => {
        setActionSQL(queryState.confirmSQL ?? null);
    }, [queryState.confirmSQL]);

    const dismiss = useCallback(() => {
        setActionSQL(null);
        setQueryState(IDLE_QUERY_STATE);
    }, []);

    return { queryState, actionSQL, run, confirm, dismiss };
}
