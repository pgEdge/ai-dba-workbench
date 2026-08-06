/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * Default backoff parameters tuned for a monitoring dashboard.
 *
 * The first retry fires after roughly three seconds so a brief
 * backend hiccup heals quickly, while the cap keeps a prolonged
 * outage from hammering the server. With these values the delay
 * schedule is 3s, 6s, 12s, 24s, then 45s for every subsequent
 * attempt.
 */
export const DEFAULT_RETRY_BASE_DELAY_MS = 3000;
export const DEFAULT_RETRY_MAX_DELAY_MS = 45000;

/**
 * A fetch function suitable for use with {@link useRetryingFetch}.
 *
 * The function performs its own request, handles its own success
 * and error state, and returns a boolean describing the outcome:
 * `true` when the fetch succeeded and `false` when it failed. A
 * rejected promise is also treated as a failure.
 */
export type RetryableFetch = () => Promise<boolean>;

export interface UseRetryingFetchOptions {
    /**
     * A value that, when it changes, cancels any pending retry and
     * resets the backoff schedule. Pass the dashboard `lastRefresh`
     * so a manual refresh always takes priority over the backoff.
     */
    resetKey?: unknown;
    /**
     * When false, any pending retry is cancelled and no new retries
     * are scheduled. Defaults to true.
     */
    enabled?: boolean;
    /** Delay before the first retry, in milliseconds. */
    baseDelayMs?: number;
    /** Maximum delay between retries, in milliseconds. */
    maxDelayMs?: number;
}

export interface UseRetryingFetchResult {
    /**
     * Run the supplied fetch and manage retry scheduling from its
     * outcome. Calling `run` supersedes any pending retry and resets
     * the backoff, so it doubles as the "fetch now" entry point.
     * Resolves with the boolean the fetch returned.
     */
    run: (fetchFn: RetryableFetch) => Promise<boolean>;
    /** True while a retry is scheduled after a failed attempt. */
    retrying: boolean;
    /** Cancel any pending retry and reset the backoff schedule. */
    cancel: () => void;
}

/**
 * Provide capped exponential backoff retries for a data-fetching
 * hook.
 *
 * The codebase has no React Query or SWR, so each dashboard panel
 * fetches independently and previously had no way to recover from a
 * transient backend failure on its own. This hook centralises the
 * retry-with-backoff behaviour those panels share: on failure it
 * reschedules the fetch with an exponentially growing, capped delay;
 * on success it stops retrying; and it resets the schedule whenever
 * `resetKey` changes so a manual refresh wins over any pending
 * backoff.
 */
export function useRetryingFetch(
    options: UseRetryingFetchOptions = {},
): UseRetryingFetchResult {
    const {
        resetKey,
        enabled = true,
        baseDelayMs = DEFAULT_RETRY_BASE_DELAY_MS,
        maxDelayMs = DEFAULT_RETRY_MAX_DELAY_MS,
    } = options;

    const [retrying, setRetrying] = useState<boolean>(false);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const failureCountRef = useRef<number>(0);
    // Monotonic attempt generation. Each execute() captures the value
    // current at its start; run(), cancel(), and a resetKey change bump
    // it. When an in-flight fetch resolves, its captured generation is
    // compared against the current one so a stale attempt (superseded by
    // a fresh run, a cancel, or a reset while its promise was pending)
    // is ignored instead of mutating failure/retry state.
    const generationRef = useRef<number>(0);
    const fetchFnRef = useRef<RetryableFetch | null>(null);
    const executeRef = useRef<() => Promise<boolean>>(() => Promise.resolve(false));
    const mountedRef = useRef<boolean>(true);
    const enabledRef = useRef<boolean>(enabled);
    enabledRef.current = enabled;

    const clearTimer = useCallback((): void => {
        if (timerRef.current !== null) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
        }
    }, []);

    const cancel = useCallback((): void => {
        clearTimer();
        // Invalidate any in-flight attempt so a late failure cannot
        // resurrect the retry loop after an explicit cancel.
        generationRef.current += 1;
        failureCountRef.current = 0;
        if (mountedRef.current) {
            setRetrying(false);
        }
    }, [clearTimer]);

    const computeDelay = useCallback(
        (failureCount: number): number => {
            // failureCount is a 1-based count of consecutive failures.
            const exponential = baseDelayMs * 2 ** (failureCount - 1);
            return Math.min(exponential, maxDelayMs);
        },
        [baseDelayMs, maxDelayMs],
    );

    const execute = useCallback(async (): Promise<boolean> => {
        const fetchFn = fetchFnRef.current;
        if (!fetchFn || !enabledRef.current) {
            return false;
        }

        // Capture the generation this attempt belongs to before awaiting.
        const generation = generationRef.current;

        let success = false;
        try {
            success = await fetchFn();
        } catch {
            success = false;
        }

        // Bail out if the hook unmounted or was disabled mid-flight so
        // we neither schedule an orphaned retry nor call setState on an
        // unmounted component.
        if (!mountedRef.current || !enabledRef.current) {
            return success;
        }

        // Ignore a stale attempt whose generation was superseded while
        // its fetch promise was still pending (a fresh run, a cancel, or
        // a resetKey change). Acting on it would wrongly increment the
        // failure count and schedule a retry for an already-resolved
        // situation.
        if (generation !== generationRef.current) {
            return success;
        }

        if (success) {
            failureCountRef.current = 0;
            clearTimer();
            setRetrying(false);
        } else {
            failureCountRef.current += 1;
            const delay = computeDelay(failureCountRef.current);
            clearTimer();
            setRetrying(true);
            timerRef.current = setTimeout(() => {
                timerRef.current = null;
                void executeRef.current();
            }, delay);
        }

        return success;
    }, [clearTimer, computeDelay]);

    executeRef.current = execute;

    const run = useCallback(
        (fetchFn: RetryableFetch): Promise<boolean> => {
            fetchFnRef.current = fetchFn;
            // A fresh invocation supersedes any pending retry so a
            // manual refresh always starts from a clean schedule. Bumping
            // the generation also invalidates any older in-flight attempt.
            generationRef.current += 1;
            failureCountRef.current = 0;
            clearTimer();
            setRetrying(false);
            return execute();
        },
        [clearTimer, execute],
    );

    // Reset the backoff whenever the reset key changes (for example a
    // manual refresh). The consuming effect re-invokes run separately,
    // so this only needs to clear pending retries.
    useEffect(() => {
        // Invalidate any in-flight attempt so a late failure from before
        // the reset cannot restart the backoff after the manual refresh.
        generationRef.current += 1;
        failureCountRef.current = 0;
        clearTimer();
        setRetrying(false);
    }, [resetKey, clearTimer]);

    // Cancel pending retries when disabled.
    useEffect(() => {
        if (!enabled) {
            cancel();
        }
    }, [enabled, cancel]);

    // Track mount state and clear any timer on unmount.
    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            clearTimer();
        };
    }, [clearTimer]);

    return { run, retrying, cancel };
}

export default useRetryingFetch;
