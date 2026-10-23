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
import { apiGet, ApiError } from '../utils/apiClient';

/**
 * Which kind of model made an alert's Tier 3 anomaly decision. For
 * `decision`, the reasoning text was generated after the fact to
 * explain a probability-only verdict.
 */
export type AlertReasoningSource = 'decision' | 'reasoning';

/** Body of `GET /api/v1/alerts/reasoning`. */
export interface AlertReasoningResponse {
    alert_id: number;
    status: 'available' | 'unavailable' | 'not_applicable';
    source?: AlertReasoningSource;
    reasoning?: string;
}

/**
 * The state of a reasoning lookup as the UI sees it. `unavailable`
 * and a retryable `error` both invite a retry; `not_applicable` and a
 * non-retryable `error` (the alert is missing or not visible) do not.
 */
export type AlertReasoningState =
    | { status: 'idle' }
    | { status: 'loading' }
    | { status: 'available'; reasoning: string; source?: AlertReasoningSource }
    | { status: 'unavailable' }
    | { status: 'not_applicable' }
    | { status: 'error'; message: string; retryable: boolean };

export interface UseAlertReasoningResult {
    state: AlertReasoningState;
    /**
     * Fetch the reasoning unless it is already known or a request is
     * in flight. Pass `force` to refetch after an unavailable or error
     * result.
     */
    load: (force?: boolean) => void;
}

/**
 * Session cache of `available` results, keyed by alert id. The first
 * request for an alert classified by a decision model makes the
 * server call an LLM, so a result is never fetched twice.
 */
const reasoningCache = new Map<string, AlertReasoningState>();

/**
 * Requests in flight, keyed by alert id, so that two rows showing the
 * same alert, or a popover closed and reopened mid-request, share one
 * server call rather than starting a second LLM call.
 */
const inFlight = new Map<string, Promise<AlertReasoningState>>();

/**
 * Bumped by each clear, so that a request started before a logout
 * cannot write its result into the next user's cache.
 */
let cacheGeneration = 0;

/** Empty the session cache; called on logout and by tests. */
export const clearAlertReasoningCache = (): void => {
    cacheGeneration += 1;
    reasoningCache.clear();
    inFlight.clear();
};

const toState = (body: AlertReasoningResponse): AlertReasoningState => {
    switch (body?.status) {
        case 'available':
            // An available status without text is of no use to show;
            // treat it as a transient failure so the user can retry.
            if (typeof body.reasoning !== 'string' || body.reasoning.trim() === '') {
                return { status: 'unavailable' };
            }
            return {
                status: 'available',
                reasoning: body.reasoning,
                source: body.source,
            };
        case 'unavailable':
            return { status: 'unavailable' };
        case 'not_applicable':
            return { status: 'not_applicable' };
        default:
            return {
                status: 'error',
                message: 'The server returned an unexpected response.',
                retryable: true,
            };
    }
};

const toErrorState = (err: unknown): AlertReasoningState => {
    if (err instanceof ApiError) {
        if (err.statusCode === 404) {
            return {
                status: 'error',
                message: 'This alert no longer exists or is not visible to you.',
                retryable: false,
            };
        }
        if (err.statusCode === 400) {
            return {
                status: 'error',
                message: 'The alert identifier was rejected by the server.',
                retryable: false,
            };
        }
    }
    return {
        status: 'error',
        message: 'The detection reasoning could not be loaded.',
        retryable: true,
    };
};

const fetchReasoning = (key: string): Promise<AlertReasoningState> => {
    const pending = inFlight.get(key);
    if (pending) {
        return pending;
    }
    const generation = cacheGeneration;
    const promise = apiGet<AlertReasoningResponse>(
        `/api/v1/alerts/reasoning?alert_id=${encodeURIComponent(key)}`,
    )
        .then(toState, toErrorState)
        .then((state) => {
            if (state.status === 'available' && generation === cacheGeneration) {
                reasoningCache.set(key, state);
            }
            return state;
        })
        .finally(() => {
            if (inFlight.get(key) === promise) {
                inFlight.delete(key);
            }
        });
    inFlight.set(key, promise);
    return promise;
};

/**
 * Load an alert's Tier 3 detection reasoning on demand. Nothing is
 * fetched until `load` is called, because the first request for an
 * alert can make the server call a (paid) LLM.
 */
export const useAlertReasoning = (
    alertId: number | string,
): UseAlertReasoningResult => {
    const key = String(alertId);
    const [state, setState] = useState<AlertReasoningState>(
        () => reasoningCache.get(key) ?? { status: 'idle' },
    );
    // The key the latest request was made for, so that a response for
    // a previous alert, or one arriving after unmount, is dropped.
    const activeKey = useRef<string | null>(key);

    useEffect(() => {
        activeKey.current = key;
        setState(reasoningCache.get(key) ?? { status: 'idle' });
        return () => {
            activeKey.current = null;
        };
    }, [key]);

    const load = useCallback((force = false) => {
        const cached = reasoningCache.get(key);
        if (cached) {
            setState(cached);
            return;
        }
        if (!force && state.status !== 'idle') {
            return;
        }
        if (state.status === 'loading') {
            return;
        }
        setState({ status: 'loading' });
        void fetchReasoning(key).then((next) => {
            if (activeKey.current === key) {
                setState(next);
            }
        });
    }, [key, state.status]);

    return { state, load };
};

export default useAlertReasoning;
