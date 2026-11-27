/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useActionBlockStart } from '../useActionBlockStart';
import type { ActionBlockStartOptions } from '../useActionBlockStart';

const options = (
    over: Partial<ActionBlockStartOptions> = {},
): ActionBlockStartOptions => ({
    connectionId: 1,
    create: vi.fn().mockResolvedValue(null),
    load: vi.fn().mockResolvedValue(null),
    ...over,
});

describe('useActionBlockStart', () => {
    it('creates once however often it re-renders', () => {
        const opts = options({ sql: 'DELETE FROM t', source: 'chat_block' });
        const { rerender } = renderHook(
            (props: ActionBlockStartOptions) => { useActionBlockStart(props); },
            { initialProps: opts },
        );
        rerender({ ...opts, context: 'changed' });
        expect(opts.create).toHaveBeenCalledTimes(1);
        expect(opts.create).toHaveBeenCalledWith(1, {
            sql: 'DELETE FROM t', source: 'chat_block', execute: true,
        });
    });

    it('creates nothing for SQL of unknown origin', () => {
        const opts = options({ sql: 'DELETE FROM t' });
        renderHook(() => { useActionBlockStart(opts); });
        expect(opts.create).not.toHaveBeenCalled();
        expect(opts.load).not.toHaveBeenCalled();
    });

    it('loads afresh when the action id changes', () => {
        const opts = options({ actionId: 3 });
        const { rerender } = renderHook(
            (props: ActionBlockStartOptions) => { useActionBlockStart(props); },
            { initialProps: opts },
        );
        rerender({ ...opts, actionId: 3 });
        rerender({ ...opts, actionId: 4 });
        expect(opts.load).toHaveBeenNthCalledWith(1, 3);
        expect(opts.load).toHaveBeenNthCalledWith(2, 4);
        expect(opts.load).toHaveBeenCalledTimes(2);
    });
});
