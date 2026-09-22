/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - hook that validates a generated SQL block
 * against its target connection before the UI offers to run it.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useEffect, useState } from 'react';
import { validateSql } from '../utils/sqlValidation';
import type { SqlValidationResponse } from '../utils/sqlValidation';

/**
 * Validation state of a SQL code block.
 *
 * - `skipped`: nothing to validate (not SQL, or a parameter template).
 * - `pending`: a validation request is in flight.
 * - `valid`: every statement planned successfully.
 * - `invalid`: at least one statement failed to plan.
 * - `unsupported`: no statement failed, but at least one could not be
 *   planned (DDL, `SET`, `VACUUM` and friends). Still runnable.
 * - `unknown`: validation could not be performed.
 */
export type SqlValidationStatus =
    | 'skipped'
    | 'pending'
    | 'valid'
    | 'invalid'
    | 'unsupported'
    | 'unknown';

export interface SqlValidationState {
    status: SqlValidationStatus;
    /** Combined error text when the status is `invalid`. */
    error: string;
}

export interface UseSqlValidationOptions {
    /** SQL to validate; an empty string skips validation. */
    sql: string;
    /** Connection to plan the SQL against. */
    connectionId: number;
    /** Optional database within the connection. */
    databaseName?: string;
    /** When false, validation is skipped entirely. */
    enabled?: boolean;
}

const SKIPPED: SqlValidationState = { status: 'skipped', error: '' };

/** Collapse a per-statement response into a single block-level state. */
export const summariseValidation = (
    response: SqlValidationResponse | null,
): SqlValidationState => {
    if (!response) {
        return { status: 'unknown', error: '' };
    }

    const failures = response.statements.filter(
        (s) => s.status === 'invalid',
    );
    if (failures.length > 0) {
        const messages = failures
            .map((s) => s.error)
            .filter((message) => Boolean(message));
        return {
            status: 'invalid',
            error: messages.length > 0
                ? messages.join('\n')
                : 'This statement could not be planned by PostgreSQL.',
        };
    }

    if (response.statements.some((s) => s.status === 'unsupported')) {
        return { status: 'unsupported', error: '' };
    }

    return { status: 'valid', error: '' };
};

/**
 * Validate `sql` against `connectionId` once per distinct request.
 *
 * The effect depends only on the request inputs, so a re-render for any
 * other reason does not re-fire it, and results are shared through the
 * module-level cache in `sqlValidation`.
 */
export const useSqlValidation = (
    options: UseSqlValidationOptions,
): SqlValidationState => {
    const { sql, connectionId, databaseName, enabled = true } = options;
    const shouldValidate = enabled && Boolean(sql.trim()) && connectionId > 0;

    const [state, setState] = useState<SqlValidationState>(
        shouldValidate ? { status: 'pending', error: '' } : SKIPPED,
    );

    useEffect(() => {
        if (!shouldValidate) {
            setState(SKIPPED);
            return;
        }

        let cancelled = false;
        setState({ status: 'pending', error: '' });

        validateSql(connectionId, sql, databaseName).then((response) => {
            if (!cancelled) {
                setState(summariseValidation(response));
            }
        }).catch(() => {
            if (!cancelled) {
                setState({ status: 'unknown', error: '' });
            }
        });

        return () => {
            cancelled = true;
        };
    }, [shouldValidate, sql, connectionId, databaseName]);

    return state;
};

export default useSqlValidation;
