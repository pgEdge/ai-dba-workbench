/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Chooses where a SQL block in an Ellie reply
 * may be run: on the server and database the reply's own tool calls
 * used, on the server named by a `-- connection_id: N` comment, or on
 * one the user picks when neither identifies a single target.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ToolActivity } from './ToolStatus';
import type { RunTargetOption } from '../shared/ConnectionSelectorCodeBlock';
import {
    hasSqlParameters,
    parseConnectionIdComment,
} from '../shared/sqlDetection';

/**
 * Tools that run SQL on a monitored database through the server's
 * connection resolver: they use `connection_id`/`database_name` when
 * given, and otherwise the user's currently selected connection.
 */
export const MONITORED_DB_TOOLS: ReadonlySet<string> = new Set([
    'query_database',
    'execute_explain',
    'test_query',
    'get_schema_info',
    'count_rows',
    'similarity_search',
]);

/**
 * Tools that run SQL on the Workbench datastore rather than on a
 * monitored server; SQL written for the datastore cannot be run through
 * the per-connection query endpoint.
 */
export const DATASTORE_SQL_TOOLS: ReadonlySet<string> = new Set([
    'query_datastore',
]);

/**
 * A monitored server and database a tool call ran on.
 */
export interface ToolCallTarget {
    connectionId: number;
    /** Omitted when the call used the connection's default database. */
    databaseName?: string;
}

/**
 * Where the SQL-running tool calls behind one reply went.
 */
export interface ReplyTargets {
    /** Distinct explicit targets, in first-use order. */
    targets: ToolCallTarget[];
    /**
     * True when a monitored-database tool ran without a valid
     * `connection_id`, so it used whichever connection the user had
     * selected at the time; the client cannot tell which that was.
     */
    unattributed: boolean;
    /** True when the reply queried the Workbench datastore. */
    usedDatastore: boolean;
}

/**
 * Name and default database of a server the user can see.
 */
export interface KnownServer {
    name: string;
    databaseName?: string;
}

/**
 * How a SQL block should be rendered.
 *
 * - `copy`: no Run button.
 * - `template`: SQL with placeholders; no Run button, with the shared
 *   template notice.
 * - `run`: a Run button bound to one target.
 * - `select`: a Run button behind a server selector with no default.
 */
export type SqlBlockPlan =
    | { kind: 'copy' }
    | { kind: 'template'; code: string }
    | { kind: 'run'; code: string; target: RunTargetOption }
    | { kind: 'select'; code: string; options: RunTargetOption[] };

const isValidConnectionId = (value: unknown): value is number =>
    typeof value === 'number' && Number.isSafeInteger(value) && value > 0;

const isValidDatabaseName = (value: unknown): value is string =>
    typeof value === 'string' && value !== '';

/**
 * Extract the connection target from a tool call's arguments. This
 * mirrors the server's parseConnectionArgs: only a numeric
 * `connection_id` selects a connection, and only a non-empty string
 * `database_name` overrides the database.
 */
export const extractToolTarget = (
    input: Record<string, unknown> | undefined,
): Pick<ToolActivity, 'connectionId' | 'databaseName'> => {
    const result: Pick<ToolActivity, 'connectionId' | 'databaseName'> = {};
    if (!input) {
        return result;
    }
    if (isValidConnectionId(input.connection_id)) {
        result.connectionId = input.connection_id;
        if (isValidDatabaseName(input.database_name)) {
            result.databaseName = input.database_name;
        }
    }
    return result;
};

/**
 * Summarise where a reply's SQL-running tool calls went. Failed calls
 * count too: a reply may well discuss the server a call failed on.
 * Values are validated again here because activity is reloaded from
 * stored conversations.
 */
export const collectReplyTargets = (
    activity: ToolActivity[] | undefined,
): ReplyTargets => {
    const targets: ToolCallTarget[] = [];
    const seen = new Set<string>();
    let unattributed = false;
    let usedDatastore = false;

    for (const tool of activity ?? []) {
        if (DATASTORE_SQL_TOOLS.has(tool.name)) {
            usedDatastore = true;
            continue;
        }
        if (!MONITORED_DB_TOOLS.has(tool.name)) {
            continue;
        }
        if (!isValidConnectionId(tool.connectionId)) {
            unattributed = true;
            continue;
        }
        const databaseName = isValidDatabaseName(tool.databaseName)
            ? tool.databaseName
            : undefined;
        const key = `${tool.connectionId}/${databaseName ?? ''}`;
        if (!seen.has(key)) {
            seen.add(key);
            targets.push({ connectionId: tool.connectionId, databaseName });
        }
    }

    return { targets, unattributed, usedDatastore };
};

/**
 * Turn a tool-call target into a selectable option, naming the server
 * and resolving the default database from the user's server list so
 * the Run button can say exactly where it will run.
 */
const toOption = (
    target: ToolCallTarget,
    servers: Map<number, KnownServer>,
): RunTargetOption => {
    const server = servers.get(target.connectionId);
    return {
        connectionId: target.connectionId,
        serverName: server?.name ?? `Server ${target.connectionId}`,
        databaseName: target.databaseName ?? server?.databaseName,
    };
};

/**
 * Convert targets to options, dropping any that resolve to the same
 * server and database (for example an explicit database that is also
 * the connection's default).
 */
const toDistinctOptions = (
    targets: ToolCallTarget[],
    servers: Map<number, KnownServer>,
): RunTargetOption[] => {
    const options: RunTargetOption[] = [];
    const seen = new Set<string>();
    for (const target of targets) {
        const option = toOption(target, servers);
        const key = `${option.connectionId}/${option.databaseName ?? ''}`;
        if (!seen.has(key)) {
            seen.add(key);
            options.push(option);
        }
    }
    return options;
};

/**
 * Plan a block whose `-- connection_id: N` comment names its server. It
 * runs there, on the database the reply used on that server or the
 * server's default, unless the reply used several databases on it, in
 * which case the user chooses between them.
 */
const planAnnotatedBlock = (
    id: number,
    runCode: string,
    reply: ReplyTargets,
    servers: Map<number, KnownServer>,
): SqlBlockPlan => {
    const options = toDistinctOptions(
        reply.targets.filter((t) => t.connectionId === id),
        servers,
    );
    if (options.length > 1) {
        return { kind: 'select', code: runCode, options };
    }
    return {
        kind: 'run',
        code: runCode,
        target: options[0] ?? toOption({ connectionId: id }, servers),
    };
};

/**
 * Decide how to render a SQL code block from an Ellie reply.
 *
 * A Run button is bound to a single target only when that target is
 * unambiguous: either the block's `-- connection_id: N` comment names a
 * server on which the reply used at most one database, or, without a
 * comment, every SQL-running tool call in the reply went to the same
 * server and database. Otherwise the user must choose from a selector,
 * which lists the reply's own targets first and then the other servers
 * they can see. Blocks with placeholders are templates, shown with the
 * shared template notice, and datastore-only replies without a comment
 * get no Run button.
 *
 * @param code - The code block text, possibly with a leading comment.
 * @param reply - Where the reply's tool calls went.
 * @param servers - Servers the user can see, keyed by connection ID.
 */
export const planSqlBlock = (
    code: string,
    reply: ReplyTargets,
    servers: Map<number, KnownServer>,
): SqlBlockPlan => {
    const annotation = parseConnectionIdComment(code);
    const runCode = annotation.code;

    if (hasSqlParameters(runCode)) {
        return { kind: 'template', code: runCode };
    }

    if (annotation.connectionId !== null) {
        return planAnnotatedBlock(
            annotation.connectionId,
            runCode,
            reply,
            servers,
        );
    }

    const replyOptions = toDistinctOptions(reply.targets, servers);

    if (
        replyOptions.length === 1
        && !reply.unattributed
        && !reply.usedDatastore
    ) {
        return { kind: 'run', code: runCode, target: replyOptions[0] };
    }

    if (
        replyOptions.length === 0
        && reply.usedDatastore
        && !reply.unattributed
    ) {
        return { kind: 'copy' };
    }

    const otherServers = toDistinctOptions(
        Array.from(servers.keys())
            .filter((id) => !replyOptions.some((o) => o.connectionId === id))
            .map((connectionId) => ({ connectionId })),
        servers,
    );
    const options = [...replyOptions, ...otherServers];
    if (options.length === 0) {
        return { kind: 'copy' };
    }
    return { kind: 'select', code: runCode, options };
};
