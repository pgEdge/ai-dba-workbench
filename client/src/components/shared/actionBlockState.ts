/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - pure helpers for ActionBlock: which action
 * the block shows, what the user may do with it, and the request that
 * creates one from SQL.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { User } from '../../contexts/AuthContext';
import { isTerminalExecStatus } from '../../types/actions';
import type {
    Action,
    ActionCreateRequest,
    ClientActionSource,
} from '../../types/actions';

export const REVIEWING = 'Supervisor reviewing…';

export const pluralise = (count: number, word: string): string =>
    `${count} ${word}${count === 1 ? '' : 's'}`;

/**
 * Pick the action to show. The hook keeps the last action it saw, which
 * is stale for a different id until the new one has loaded.
 */
export const selectCurrentAction = (
    polled: Action | null,
    initialAction: Action | undefined,
    actionId: number | undefined,
    sql: string | undefined,
): Action | null => {
    const wantedId = initialAction?.id ?? actionId;
    const matches = sql !== undefined
        || wantedId === undefined
        || polled?.id === wantedId;
    const polledCurrent = polled && matches ? polled : null;
    return polledCurrent ?? initialAction ?? null;
};

export interface CreateOptions {
    databaseName?: string;
    context?: string;
    alertId?: number;
    conversationId?: string;
}

/** Build the request that creates an action from SQL and runs it. */
export const buildCreateRequest = (
    sql: string,
    source: ClientActionSource,
    { databaseName, context, alertId, conversationId }: CreateOptions,
): ActionCreateRequest => {
    const body: ActionCreateRequest = { sql, source, execute: true };
    if (databaseName) {
        body.database_name = databaseName;
    }
    if (context) {
        body.context = context;
    }
    if (alertId !== undefined) {
        body.alert_id = alertId;
    }
    if (conversationId) {
        body.conversation_id = conversationId;
    }
    return body;
};

/**
 * The id of an existing action to load on mount, if any. Only a run in
 * progress needs following; one awaiting its approval or already finished
 * will not change by itself.
 */
export const actionIdToLoad = (
    initialAction: Action | undefined,
    actionId: number | undefined,
): number | undefined => {
    if (!initialAction) {
        return actionId;
    }
    return initialAction.exec_status === 'executing'
        ? initialAction.id
        : undefined;
};

export interface ActionFlags {
    reviewing: boolean;
    vetoed: boolean;
    finished: boolean;
    /**
     * Whether the block may be closed: once the run has finished, or
     * while a vetoed action has not started. An override that is running
     * keeps the block, so its progress and Cancel stay in view.
     */
    dismissible: boolean;
    /** Whether a reviewed action awaiting approval may be run. */
    canExecute: boolean;
    canOverride: boolean;
    canRetry: boolean;
    canRetryOverride: boolean;
    showOverrideDialog: boolean;
}

/**
 * The server retries an action that was decided but did not start only
 * for whoever decided it, or a superuser.
 */
export const mayRetryAction = (
    current: Action,
    user: User | null | undefined,
): boolean => {
    const isApprover = Boolean(user?.username)
        && current.approved_by === user?.username;
    return user?.isSuperuser === true || isApprover;
};

/** Work out what the block shows and which controls it offers. */
export const deriveActionFlags = (
    current: Action,
    user: User | null | undefined,
    serverName: string | undefined,
): ActionFlags => {
    const vetoed = current.review_status === 'vetoed';
    const notStarted = current.exec_status === 'not_started';
    const hasServer = Boolean(serverName);

    // Anyone who can reach the action may try an override; the server
    // refuses (403) a user without write access and the error is shown.
    const canOverride = vetoed && notStarted
        && current.approval === 'none' && hasServer;

    // An override is retried by confirming the connection name again.
    const mayRetry = mayRetryAction(current, user);
    const canRetry = current.approval === 'approved' && mayRetry;
    const canRetryOverride =
        current.approval === 'overridden' && mayRetry && hasServer;

    const finished = isTerminalExecStatus(current.exec_status);

    return {
        reviewing: current.review_status === 'pending',
        vetoed,
        finished,
        dismissible: finished
            || (vetoed && notStarted),
        canExecute: current.review_status === 'passed'
            && current.approval === 'none',
        canOverride,
        canRetry,
        canRetryOverride,
        showOverrideDialog: canOverride || canRetryOverride,
    };
};

/** The heading describing the supervisor's verdict. */
export const reviewLabelOf = (current: Action): string => {
    if (current.review_status === 'pending') {
        return REVIEWING;
    }
    if (current.review_status === 'vetoed') {
        return 'Vetoed by Ellie\'s supervisor';
    }
    return current.review?.reviewer === 'none'
        ? 'Not yet reviewed'
        : 'Approved by Ellie\'s supervisor';
};

/** The chip text giving the supervisor's danger rating. */
export const dangerLabelOf = (danger: string): string => `Danger: ${danger}`;

/** Rows changed by the statements that did not fail. */
export const rowsAffectedOf = (current: Action): number =>
    (current.results ?? []).reduce(
        (sum, r) => sum + (r.error ? 0 : (r.rows_affected ?? 0)),
        0,
    );
