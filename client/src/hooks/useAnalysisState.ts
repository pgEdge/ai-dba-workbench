/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Shared state management for analysis hooks.
 *
 * Provides the common state (analysis, loading, error, progressMessage,
 * activeTools) and reset logic used by useAlertAnalysis,
 * useChartAnalysis, and useServerAnalysis.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback } from 'react';

export interface AnalysisState {
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
}

export interface AnalysisStateControls {
    /** Current state values. */
    state: AnalysisState;
    /** Set the analysis text. */
    setAnalysis: (value: string | null) => void;
    /** Set the loading flag. */
    setLoading: (value: boolean) => void;
    /** Set the error message. */
    setError: (value: string | null) => void;
    /** Set the progress message. */
    setProgressMessage: (value: string) => void;
    /** Set the active tool names. */
    setActiveTools: (value: string[]) => void;
    /** Reset all state to initial values. */
    reset: () => void;
}

/**
 * Hook providing the shared state management used by all analysis hooks.
 *
 * @param initialProgress - The initial progress message (varies by hook).
 */
export function useAnalysisState(
    initialProgress: string = 'Gathering context...',
): AnalysisStateControls {
    const [analysis, setAnalysis] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [progressMessage, setProgressMessage] = useState<string>(initialProgress);
    const [activeTools, setActiveTools] = useState<string[]>([]);

    const reset = useCallback((): void => {
        setAnalysis(null);
        setError(null);
        setLoading(false);
        setProgressMessage(initialProgress);
        setActiveTools([]);
    }, [initialProgress]);

    return {
        state: { analysis, loading, error, progressMessage, activeTools },
        setAnalysis,
        setLoading,
        setError,
        setProgressMessage,
        setActiveTools,
        reset,
    };
}
