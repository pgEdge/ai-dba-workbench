/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { renderHook } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { useRequestSequence } from '../useRequestSequence';

describe('useRequestSequence', () => {
    it('treats the only in-flight request as current', () => {
        const { result } = renderHook(() => useRequestSequence());

        const isCurrent = result.current.beginRequest();

        expect(isCurrent()).toBe(true);
    });

    it('supersedes an earlier request when a newer one starts', () => {
        const { result } = renderHook(() => useRequestSequence());

        const first = result.current.beginRequest();
        const second = result.current.beginRequest();

        expect(first()).toBe(false);
        expect(second()).toBe(true);
    });

    it('abandons the in-flight request without starting one', () => {
        const { result } = renderHook(() => useRequestSequence());

        const isCurrent = result.current.beginRequest();
        result.current.supersedeRequest();

        expect(isCurrent()).toBe(false);
    });

    it('reports every request as superseded after unmounting', () => {
        const { result, unmount } = renderHook(() => useRequestSequence());

        const isCurrent = result.current.beginRequest();
        unmount();

        expect(isCurrent()).toBe(false);
    });

    it('keeps a stable identity across re-renders', () => {
        const { result, rerender } = renderHook(() => useRequestSequence());

        const { beginRequest, supersedeRequest } = result.current;
        rerender();

        expect(result.current.beginRequest).toBe(beginRequest);
        expect(result.current.supersedeRequest).toBe(supersedeRequest);
    });
});
