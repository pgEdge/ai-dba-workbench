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
import { useAnalysisState } from '../useAnalysisState';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useAnalysisState', () => {
    it('returns initial state with default progress message', () => {
        const { result } = renderHook(() => useAnalysisState());

        expect(result.current.state.analysis).toBeNull();
        expect(result.current.state.loading).toBe(false);
        expect(result.current.state.error).toBeNull();
        expect(result.current.state.progressMessage).toBe('Gathering context...');
        expect(result.current.state.activeTools).toEqual([]);
    });

    it('returns initial state with custom progress message', () => {
        const { result } = renderHook(() =>
            useAnalysisState('Custom progress...')
        );

        expect(result.current.state.progressMessage).toBe('Custom progress...');
    });

    it('setAnalysis updates the analysis state', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setAnalysis('Analysis complete');
        });

        expect(result.current.state.analysis).toBe('Analysis complete');
    });

    it('setAnalysis accepts null to clear analysis', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setAnalysis('Some analysis');
        });

        expect(result.current.state.analysis).toBe('Some analysis');

        act(() => {
            result.current.setAnalysis(null);
        });

        expect(result.current.state.analysis).toBeNull();
    });

    it('setLoading updates the loading state', () => {
        const { result } = renderHook(() => useAnalysisState());

        expect(result.current.state.loading).toBe(false);

        act(() => {
            result.current.setLoading(true);
        });

        expect(result.current.state.loading).toBe(true);

        act(() => {
            result.current.setLoading(false);
        });

        expect(result.current.state.loading).toBe(false);
    });

    it('setError updates the error state', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setError('An error occurred');
        });

        expect(result.current.state.error).toBe('An error occurred');
    });

    it('setError accepts null to clear error', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setError('Error message');
        });

        expect(result.current.state.error).toBe('Error message');

        act(() => {
            result.current.setError(null);
        });

        expect(result.current.state.error).toBeNull();
    });

    it('setProgressMessage updates the progress message', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setProgressMessage('Processing...');
        });

        expect(result.current.state.progressMessage).toBe('Processing...');
    });

    it('setActiveTools updates the active tools array', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setActiveTools(['tool1', 'tool2']);
        });

        expect(result.current.state.activeTools).toEqual(['tool1', 'tool2']);
    });

    it('setActiveTools can set an empty array', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setActiveTools(['tool1']);
        });

        expect(result.current.state.activeTools).toEqual(['tool1']);

        act(() => {
            result.current.setActiveTools([]);
        });

        expect(result.current.state.activeTools).toEqual([]);
    });

    it('reset restores all state to initial values', () => {
        const { result } = renderHook(() =>
            useAnalysisState('Initial progress')
        );

        // Modify all state
        act(() => {
            result.current.setAnalysis('Some analysis');
            result.current.setLoading(true);
            result.current.setError('Some error');
            result.current.setProgressMessage('Different progress');
            result.current.setActiveTools(['tool1', 'tool2']);
        });

        // Verify all state is modified
        expect(result.current.state.analysis).toBe('Some analysis');
        expect(result.current.state.loading).toBe(true);
        expect(result.current.state.error).toBe('Some error');
        expect(result.current.state.progressMessage).toBe('Different progress');
        expect(result.current.state.activeTools).toEqual(['tool1', 'tool2']);

        // Reset
        act(() => {
            result.current.reset();
        });

        // Verify all state is reset
        expect(result.current.state.analysis).toBeNull();
        expect(result.current.state.loading).toBe(false);
        expect(result.current.state.error).toBeNull();
        expect(result.current.state.progressMessage).toBe('Initial progress');
        expect(result.current.state.activeTools).toEqual([]);
    });

    it('reset uses the custom initial progress message', () => {
        const { result } = renderHook(() =>
            useAnalysisState('Custom initial message')
        );

        act(() => {
            result.current.setProgressMessage('Changed message');
        });

        expect(result.current.state.progressMessage).toBe('Changed message');

        act(() => {
            result.current.reset();
        });

        expect(result.current.state.progressMessage).toBe('Custom initial message');
    });

    it('multiple state updates can be batched', () => {
        const { result } = renderHook(() => useAnalysisState());

        act(() => {
            result.current.setLoading(true);
            result.current.setProgressMessage('Step 1');
            result.current.setActiveTools(['tool1']);
        });

        expect(result.current.state.loading).toBe(true);
        expect(result.current.state.progressMessage).toBe('Step 1');
        expect(result.current.state.activeTools).toEqual(['tool1']);
    });

    it('returns stable function references', () => {
        const { result, rerender } = renderHook(() => useAnalysisState());

        const {
            setAnalysis,
            setLoading,
            setError,
            setProgressMessage,
            setActiveTools,
            reset,
        } = result.current;

        rerender();

        expect(result.current.setAnalysis).toBe(setAnalysis);
        expect(result.current.setLoading).toBe(setLoading);
        expect(result.current.setError).toBe(setError);
        expect(result.current.setProgressMessage).toBe(setProgressMessage);
        expect(result.current.setActiveTools).toBe(setActiveTools);
        expect(result.current.reset).toBe(reset);
    });
});
