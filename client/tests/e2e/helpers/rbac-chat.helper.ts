/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ConnectionResponse } from './api.helper';

/**
 * Escape a string so it can be embedded literally inside a RegExp.
 */
function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Wording-independent leak detector for the Ask Ellie RBAC tests.
 *
 * The LLM rewords its refusals on every run, so matching denial WORDS is
 * both structurally wrong (a partial leak that also says "error" would
 * pass) and flaky. Instead, key the check on the forbidden DATA itself:
 * given the set of connections a user must NOT see, return true when the
 * supplied text discloses any of them.
 *
 * Two disclosure shapes are recognised for each forbidden connection:
 * - a host appearing near its port (within 40 characters), which is how
 *   the connection tables and prose enumerate a server, and
 * - a `postgres://…host` / `postgresql://…host` connection URI.
 *
 * @param text - Rendered text to inspect (chat reply or tool output).
 * @param forbidden - Connections the caller must not be able to see.
 * @returns true when any forbidden connection is disclosed in `text`.
 */
export function connectionLeaks(
    text: string,
    forbidden: ConnectionResponse[],
): boolean {
    return forbidden.some((c) => {
        const host = escapeRegExp(c.host);
        const tuple = new RegExp(`${host}[\\s\\S]{0,40}\\b${c.port}\\b`, 'i'); // host near its port
        const uri = new RegExp(`postgres(?:ql)?://[^\\s]*${host}`, 'i');
        return tuple.test(text) || uri.test(text);
    });
}
