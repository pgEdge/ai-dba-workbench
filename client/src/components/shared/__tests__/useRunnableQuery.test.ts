/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useRunnableQuery } from '../useRunnableQuery';

const mockApiFetch = vi.fn();
vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const refusal = () => ({
    ok: false,
    status: 409,
    json: () => Promise.resolve({
        requires_action: true,
        write_statements: ['DELETE FROM t'],
    }),
    text: () => Promise.resolve(''),
});

interface Props {
    connectionId: number;
    databaseName?: string;
}

const renderQuery = (initial: Props) =>
    renderHook(
        ({ connectionId, databaseName }: Props) =>
            useRunnableQuery('DELETE FROM t', connectionId, databaseName),
        { initialProps: initial },
    );

describe('useRunnableQuery', () => {
    beforeEach(() => {
        mockApiFetch.mockReset();
    });

    it('proposes the write on the target it was refused on', async () => {
        mockApiFetch.mockResolvedValueOnce(refusal());
        const { result } = renderQuery({ connectionId: 1, databaseName: 'app' });

        await act(() => result.current.run());
        expect(result.current.queryState.pendingConfirmation).toBe(true);
        act(() => result.current.confirm());

        expect(result.current.confirmedWrite).toEqual({
            sql: 'DELETE FROM t',
            target: { connectionId: 1, databaseName: 'app' },
        });
    });

    it.each([
        ['connection', { connectionId: 2, databaseName: 'app' }],
        ['database', { connectionId: 1, databaseName: 'other' }],
    ])('discards a refusal when the %s changes', async (_name, next) => {
        mockApiFetch.mockResolvedValueOnce(refusal());
        const { result, rerender } = renderQuery({
            connectionId: 1, databaseName: 'app',
        });
        await act(() => result.current.run());

        rerender(next);
        expect(result.current.queryState.pendingConfirmation).toBe(false);
        act(() => result.current.confirm());
        expect(result.current.confirmedWrite).toBeNull();

        // Moving back does not revive the discarded refusal.
        rerender({ connectionId: 1, databaseName: 'app' });
        expect(result.current.queryState.pendingConfirmation).toBe(false);
    });

    it('drops a refusal that arrives after the target changed', async () => {
        let respond: (value: unknown) => void = () => undefined;
        mockApiFetch.mockReturnValueOnce(
            new Promise(resolve => { respond = resolve; }),
        );
        const { result, rerender } = renderQuery({ connectionId: 1 });

        let running: Promise<void> = Promise.resolve();
        act(() => { running = result.current.run(); });
        rerender({ connectionId: 2 });
        await act(async () => {
            respond(refusal());
            await running;
        });

        expect(result.current.queryState.pendingConfirmation).toBe(false);
    });

    it('keeps a confirmed write on its target when the block moves on', async () => {
        mockApiFetch.mockResolvedValueOnce(refusal());
        const { result, rerender } = renderQuery({ connectionId: 1 });
        await act(() => result.current.run());
        act(() => result.current.confirm());

        rerender({ connectionId: 2 });

        expect(result.current.queryState.pendingConfirmation).toBe(true);
        expect(result.current.confirmedWrite?.target).toEqual({
            connectionId: 1, databaseName: undefined,
        });
    });
});
