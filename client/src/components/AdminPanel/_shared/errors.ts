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
 */

/**
 * Default fallback message used when a thrown value is not an `Error`
 * instance. AdminPanel components historically use this exact wording.
 */
export const DEFAULT_ERROR_MESSAGE = 'An unexpected error occurred';

/**
 * Extracts a user-displayable message from a thrown value. When the value
 * is an `Error`, returns its `message`. Otherwise returns `fallback`,
 * which defaults to {@link DEFAULT_ERROR_MESSAGE}.
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
