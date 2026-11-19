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
 * Types for proposed actions: database writes that are reviewed by a
 * supervisor before they run. They mirror the `Action`, `ActionResponse`
 * and related schemas in the server's OpenAPI specification.
 */

export type ActionReviewStatus = 'pending' | 'passed' | 'vetoed';

export type ActionApproval = 'none' | 'approved' | 'overridden';

export type ActionExecStatus =
    | 'not_started'
    | 'executing'
    | 'succeeded'
    | 'failed'
    | 'cancelled'
    | 'unknown';

export type ActionSource =
    | 'chat_tool'
    | 'chat_block'
    | 'alert_analysis'
    | 'query_analysis'
    | 'server_analysis'
    | 'chart_analysis';

/** Sources the client may send when it creates an action itself. */
export type ClientActionSource = Exclude<ActionSource, 'chat_tool'>;

export interface ActionReview {
    decision?: string;
    danger?: string;
    reasons?: string[];
    /** Which reviewer produced the verdict; "none" means no reviewer ran. */
    reviewer?: string;
}

export interface ActionStatementResult {
    statement: string;
    command_tag?: string;
    rows_affected?: number;
    duration_ms?: number;
    error?: string;
    applied?: boolean;
    columns?: string[];
    rows?: string[][];
    truncated?: boolean;
    /** Server caveat, e.g. a sequence change a rollback did not undo. */
    note?: string;
}

export interface Action {
    id: number;
    connection_id: number;
    database_name?: string | null;
    statements: string[];
    source: ActionSource;
    proposed_by?: string;
    proposed_via?: string;
    /** Null for anyone but the proposer or a superuser. */
    conversation_id?: string | null;
    alert_id?: number | null;
    parent_action_id?: number | null;
    attempt?: number;
    rationale?: string | null;
    /** Null for anyone but the proposer or a superuser. */
    evidence?: Record<string, unknown> | null;
    review_status: ActionReviewStatus;
    review?: ActionReview | null;
    approval: ActionApproval;
    approved_by?: string | null;
    approved_via?: string | null;
    approved_at?: string | null;
    exec_status: ActionExecStatus;
    transactional?: boolean | null;
    results?: ActionStatementResult[] | null;
    created_at?: string;
    updated_at?: string;
    executed_at?: string | null;
}

/** The body returned when an action is created. */
export interface ActionCreateResponse extends Action {
    attempts_left: number;
    chain_exhausted: boolean;
    best_attempt?: Action | null;
    /** Present when execute was requested but the action did not start. */
    execute_error?: string;
}

export interface ActionCreateRequest {
    sql: string;
    database_name?: string;
    rationale?: string;
    source: ClientActionSource;
    context?: string;
    alert_id?: number;
    conversation_id?: string;
    execute?: boolean;
}

export interface ActionOverride {
    /** Must equal the connection's name exactly. */
    confirm: string;
}

/**
 * The JSON text block a `propose_action` tool call returns.
 */
export interface ProposeActionResult {
    action_id: number;
    review_status: ActionReviewStatus;
    danger?: string;
    reasons?: string[];
    attempts_left?: number;
    chain_exhausted?: boolean;
    best_action_id?: number;
    statements?: string[];
}

/**
 * Where a write confirmed from a rendered code block is recorded as
 * coming from, so it can be audited back to the analysis that suggested it.
 */
export interface ActionOrigin {
    actionSource?: ClientActionSource;
    actionContext?: string;
    alertId?: number;
    conversationId?: string;
}

/** Execution states after which nothing further will change. */
export const TERMINAL_EXEC_STATUSES: ReadonlySet<ActionExecStatus> = new Set([
    'succeeded',
    'failed',
    'cancelled',
    'unknown',
]);

export const isTerminalExecStatus = (status: ActionExecStatus): boolean =>
    TERMINAL_EXEC_STATUSES.has(status);

/**
 * Parse the text of a `propose_action` tool result. Returns null when it
 * is not the expected JSON object, for example an error message.
 */
export const parseProposeActionResult = (
    text: string,
): ProposeActionResult | null => {
    try {
        const parsed: unknown = JSON.parse(text);
        if (
            parsed !== null
            && typeof parsed === 'object'
            && typeof (parsed as ProposeActionResult).action_id === 'number'
            && typeof (parsed as ProposeActionResult).review_status
                === 'string'
        ) {
            return parsed as ProposeActionResult;
        }
    } catch {
        // Not JSON: an error message or other text.
    }
    return null;
};

/** Map a danger rating to a theme colour for its chip. */
export const dangerColour = (
    danger: string | undefined,
): 'success' | 'warning' | 'error' | 'default' => {
    switch (danger) {
        case 'low':
            return 'success';
        case 'moderate':
            return 'warning';
        case 'high':
        case 'critical':
            return 'error';
        default:
            return 'default';
    }
};
