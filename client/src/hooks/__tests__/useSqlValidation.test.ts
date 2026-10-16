/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Coverage for the useSqlValidation hook added by issue #532.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';

const mockValidateSql = vi.fn();
vi.mock('../../utils/sqlValidation', () => ({
    validateSql: (...args: unknown[]) => mockValidateSql(...args),
}));

import { useSqlValidation, summariseValidation } from '../useSqlValidation';
import type { SqlValidationResponse } from '../../utils/sqlValidation';

const response = (
    statuses: Array<['valid' | 'invalid' | 'unsupported', string]>,
): SqlValidationResponse => ({
    valid: !statuses.some(([status]) => status === 'invalid'),
    total_statements: statuses.length,
    statements: statuses.map(([status, error], i) => ({
        query: `SELECT ${i}`,
        status,
        error,
    })),
});

beforeEach(() => {
    mockValidateSql.mockReset();
});

describe('summariseValidation', () => {
    it('reports unknown for a null response', () => {
        expect(summariseValidation(null)).toEqual({
            status: 'unknown',
            error: '',
        });
    });

    it('reports valid when every statement planned', () => {
        expect(summariseValidation(response([['valid', '']]))).toEqual({
            status: 'valid',
            error: '',
        });
    });

    it('reports unsupported when nothing failed but something could not plan', () => {
        const state = summariseValidation(
            response([['valid', ''], ['unsupported', 'DDL']]),
        );
        expect(state.status).toBe('unsupported');
    });

    it('joins every failing statement error', () => {
        const state = summariseValidation(response([
            ['invalid', 'column "total_ram" does not exist'],
            ['invalid', 'syntax error at or near ")"'],
        ]));
        expect(state.status).toBe('invalid');
        expect(state.error).toBe(
            'column "total_ram" does not exist\nsyntax error at or near ")"',
        );
    });

    it('falls back to a generic message when no error text is given', () => {
        const state = summariseValidation(response([['invalid', '']]));
        expect(state.error).toContain('could not be planned');
    });
});

describe('useSqlValidation', () => {
    it('skips validation when disabled', () => {
        const { result } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1',
            connectionId: 1,
            enabled: false,
        }));

        expect(result.current.status).toBe('skipped');
        expect(mockValidateSql).not.toHaveBeenCalled();
    });

    it('skips validation for empty SQL or no connection', () => {
        const { result: empty } = renderHook(() => useSqlValidation({
            sql: '   ',
            connectionId: 1,
        }));
        expect(empty.current.status).toBe('skipped');

        const { result: noConn } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1',
            connectionId: 0,
        }));
        expect(noConn.current.status).toBe('skipped');
        expect(mockValidateSql).not.toHaveBeenCalled();
    });

    it('starts pending and settles on the validation result', async () => {
        mockValidateSql.mockResolvedValue(response([['valid', '']]));

        const { result } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1',
            connectionId: 2,
            databaseName: 'appdb',
        }));

        expect(result.current.status).toBe('pending');
        await waitFor(() => expect(result.current.status).toBe('valid'));
        expect(mockValidateSql).toHaveBeenCalledWith(2, 'SELECT 1', 'appdb');
    });

    it('surfaces an invalid result with its error text', async () => {
        mockValidateSql.mockResolvedValue(
            response([['invalid', 'relation "nope" does not exist']]),
        );

        const { result } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1 FROM nope',
            connectionId: 2,
        }));

        await waitFor(() => expect(result.current.status).toBe('invalid'));
        expect(result.current.error).toBe('relation "nope" does not exist');
    });

    it('reports unknown when the validator rejects', async () => {
        mockValidateSql.mockRejectedValue(new Error('offline'));

        const { result } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1',
            connectionId: 2,
        }));

        await waitFor(() => expect(result.current.status).toBe('unknown'));
    });

    it('does not re-validate when an unrelated re-render occurs', async () => {
        mockValidateSql.mockResolvedValue(response([['valid', '']]));

        const { result, rerender } = renderHook(
            (props: { sql: string }) => useSqlValidation({
                sql: props.sql,
                connectionId: 2,
            }),
            { initialProps: { sql: 'SELECT 1' } },
        );

        await waitFor(() => expect(result.current.status).toBe('valid'));
        rerender({ sql: 'SELECT 1' });
        rerender({ sql: 'SELECT 1' });

        expect(mockValidateSql).toHaveBeenCalledTimes(1);
    });

    it('re-validates when the SQL changes', async () => {
        mockValidateSql.mockResolvedValue(response([['valid', '']]));

        const { result, rerender } = renderHook(
            (props: { sql: string }) => useSqlValidation({
                sql: props.sql,
                connectionId: 2,
            }),
            { initialProps: { sql: 'SELECT 1' } },
        );

        await waitFor(() => expect(result.current.status).toBe('valid'));
        rerender({ sql: 'SELECT 2' });
        await waitFor(() =>
            expect(mockValidateSql).toHaveBeenCalledTimes(2));
    });

    it('drops a result that arrives after unmount', async () => {
        let settle: (value: SqlValidationResponse) => void = () => {};
        mockValidateSql.mockReturnValue(
            new Promise<SqlValidationResponse>((resolve) => {
                settle = resolve;
            }),
        );

        const { unmount } = renderHook(() => useSqlValidation({
            sql: 'SELECT 1',
            connectionId: 2,
        }));

        unmount();
        settle(response([['valid', '']]));
        await Promise.resolve();
    });
});
