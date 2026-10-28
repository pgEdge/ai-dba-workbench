/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useDebouncedValue } from '../useDebouncedValue';

describe('useDebouncedValue', () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('returns the initial value immediately', () => {
        const { result } = renderHook(() => useDebouncedValue('a', 300));
        expect(result.current).toBe('a');
    });

    it('updates only after the value has settled for the delay', () => {
        const { result, rerender } = renderHook(
            ({ value }) => useDebouncedValue(value, 300),
            { initialProps: { value: 'a' } },
        );

        rerender({ value: 'ab' });
        act(() => { vi.advanceTimersByTime(299); });
        expect(result.current).toBe('a');

        act(() => { vi.advanceTimersByTime(1); });
        expect(result.current).toBe('ab');
    });

    it('restarts the timer on each change', () => {
        const { result, rerender } = renderHook(
            ({ value }) => useDebouncedValue(value, 300),
            { initialProps: { value: 'a' } },
        );

        rerender({ value: 'ab' });
        act(() => { vi.advanceTimersByTime(200); });
        rerender({ value: 'abc' });
        act(() => { vi.advanceTimersByTime(200); });
        expect(result.current).toBe('a');

        act(() => { vi.advanceTimersByTime(100); });
        expect(result.current).toBe('abc');
    });
});
