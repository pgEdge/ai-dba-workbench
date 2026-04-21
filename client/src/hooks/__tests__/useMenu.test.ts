/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useMenu } from '../useMenu';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useMenu', () => {
    it('returns initial state with anchorEl as null and open as false', () => {
        const { result } = renderHook(() => useMenu());

        expect(result.current.anchorEl).toBeNull();
        expect(result.current.open).toBe(false);
        expect(typeof result.current.handleOpen).toBe('function');
        expect(typeof result.current.handleClose).toBe('function');
    });

    it('sets anchorEl and opens menu when handleOpen is called', () => {
        const { result } = renderHook(() => useMenu());

        const mockElement = document.createElement('button');
        const mockEvent = {
            currentTarget: mockElement,
        } as React.MouseEvent<HTMLElement>;

        act(() => {
            result.current.handleOpen(mockEvent);
        });

        expect(result.current.anchorEl).toBe(mockElement);
        expect(result.current.open).toBe(true);
    });

    it('clears anchorEl and closes menu when handleClose is called', () => {
        const { result } = renderHook(() => useMenu());

        const mockElement = document.createElement('button');
        const mockEvent = {
            currentTarget: mockElement,
        } as React.MouseEvent<HTMLElement>;

        act(() => {
            result.current.handleOpen(mockEvent);
        });

        expect(result.current.open).toBe(true);

        act(() => {
            result.current.handleClose();
        });

        expect(result.current.anchorEl).toBeNull();
        expect(result.current.open).toBe(false);
    });

    it('handles multiple open/close cycles correctly', () => {
        const { result } = renderHook(() => useMenu());

        const mockElement1 = document.createElement('button');
        const mockElement2 = document.createElement('div');

        // First open
        act(() => {
            result.current.handleOpen({
                currentTarget: mockElement1,
            } as React.MouseEvent<HTMLElement>);
        });
        expect(result.current.anchorEl).toBe(mockElement1);
        expect(result.current.open).toBe(true);

        // Close
        act(() => {
            result.current.handleClose();
        });
        expect(result.current.open).toBe(false);

        // Second open with different element
        act(() => {
            result.current.handleOpen({
                currentTarget: mockElement2,
            } as React.MouseEvent<HTMLElement>);
        });
        expect(result.current.anchorEl).toBe(mockElement2);
        expect(result.current.open).toBe(true);
    });

    it('can change anchorEl by calling handleOpen again', () => {
        const { result } = renderHook(() => useMenu());

        const mockElement1 = document.createElement('button');
        const mockElement2 = document.createElement('span');

        act(() => {
            result.current.handleOpen({
                currentTarget: mockElement1,
            } as React.MouseEvent<HTMLElement>);
        });

        expect(result.current.anchorEl).toBe(mockElement1);

        act(() => {
            result.current.handleOpen({
                currentTarget: mockElement2,
            } as React.MouseEvent<HTMLElement>);
        });

        expect(result.current.anchorEl).toBe(mockElement2);
        expect(result.current.open).toBe(true);
    });
});
