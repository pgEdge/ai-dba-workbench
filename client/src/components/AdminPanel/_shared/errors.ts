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
 * Error helpers shared by the AdminPanel CRUD components.
 *
 * Every AdminPanel mutation handler used to inline an `err instanceof Error`
 * check followed by a fallback string; centralising that here removes a
 * substantial amount of duplication and keeps fallback wording consistent.
 *
 * AdminPanel components MUST use {@link extractErrorMessage} for the generic
 * "something went wrong" path in their catch blocks. Do not reintroduce
 * ad-hoc patterns such as `String(err)` (which produces unfriendly
 * `[object Object]`-style output) or inline `err instanceof Error ? ... : ...`
 * ternaries that hard-code a divergent fallback wording. The only acceptable
 * variation is passing a context-specific `fallback` argument when the
 * surrounding code already conveys meaningful context (for example,
 * `extractErrorMessage(err, 'Failed to load recipients')`).
 */

/**
 * Default fallback message used when a thrown value is not an `Error`
 * instance. AdminPanel components surface this exact wording to keep
 * non-`Error` throws presenting a consistent, user-friendly string.
 */
export const DEFAULT_ERROR_MESSAGE = 'An unexpected error occurred';

/**
 * Extracts a user-displayable message from a thrown value.
 *
 * Contract:
 *
 * - When `err` is an `Error` instance, returns its `message` property
 *   verbatim. Callers therefore never need to special-case `Error` themselves.
 * - When `err` is any other value (a string, `null`, `undefined`, a plain
 *   object, etc.), returns `fallback`, which defaults to
 *   {@link DEFAULT_ERROR_MESSAGE} (`'An unexpected error occurred'`). This
 *   guarantees AdminPanel components never display `[object Object]` or
 *   other implementation-leaking strings produced by `String(err)`.
 *
 * Pass an explicit `fallback` only when the call site can provide a more
 * descriptive, context-specific message (for example, "Failed to load
 * recipients"). Otherwise rely on the default to keep wording uniform
 * across the AdminPanel.
 *
 * @param err - The value thrown from a `try`/`catch` block. Typed as
 *   `unknown` because TypeScript widens the catch parameter to `unknown`
 *   under `useUnknownInCatchVariables`.
 * @param fallback - Optional override for the non-`Error` fallback string.
 *   Defaults to {@link DEFAULT_ERROR_MESSAGE}.
 * @returns A user-safe error message string suitable for display in an
 *   `Alert`, toast, or form-level error banner.
 */
export function extractErrorMessage(
    err: unknown,
    fallback: string = DEFAULT_ERROR_MESSAGE,
): string {
    if (err instanceof Error) {
        return err.message;
    }
    return fallback;
}
