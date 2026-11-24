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
import { apiGet, apiPost, ApiError } from '../utils/apiClient';
import { isTerminalExecStatus } from '../types/actions';
import type {
    Action,
    ActionCreateRequest,
    ActionCreateResponse,
    ActionOverride,
} from '../types/actions';

/** First and longest delay between polls, in milliseconds. */
export const POLL_INITIAL_MS = 500;
export const POLL_MAX_MS = 5000;

export interface UseActionResult {
    action: Action | null;
    /** True while a create or execute request is in flight. */
    busy: boolean;
    /** Error from the last request, or the create response's execute_error. */
    error: string | null;
    /**
     * True once the server has refused to execute the action with a 403.
     * The client cannot learn a user's per-connection write access before
     * asking, so this is the first point at which it knows the action
     * cannot be run from this session.
     */
    writeDenied: boolean;
    /** Create an action, optionally running it at once. */
    create: (
        connectionId: number,
        body: ActionCreateRequest,
    ) => Promise<Action | null>;
    /** Approve and run an action, with an optional override. */
    execute: (id: number, override?: ActionOverride) => Promise<boolean>;
    cancel: (id: number) => Promise<boolean>;
    /** Fetch an existing action and follow it while it runs. */
    load: (id: number, keepError?: boolean) => Promise<Action | null>;
}

const messageOf = (err: unknown): string =>
    err instanceof Error ? err.message : 'Request failed';

/**
 * Drives one proposed action: creating it, executing and cancelling it,
 * and polling `GET /api/v1/actions/{id}` with an exponential back-off
 * (500ms up to 5s) until its execution status is terminal. Polling stops
 * when the component unmounts.
 */
export const useAction = (): UseActionResult => {
    const [action, setAction] = useState<Action | null>(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [writeDenied, setWriteDenied] = useState(false);

    const mountedRef = useRef(true);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    // Bumped whenever polling restarts or stops, so a stale poll that
    // resolves late cannot revive itself.
    const pollGenRef = useRef(0);

    const stopPolling = useCallback(() => {
        pollGenRef.current += 1;
        if (timerRef.current !== null) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
        }
    }, []);

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            stopPolling();
        };
    }, [stopPolling]);

    const startPolling = useCallback((id: number) => {
        stopPolling();
        const gen = pollGenRef.current;
        let delay = POLL_INITIAL_MS;

        // Fetch the action once; true means it may still change, so the
        // next poll should be scheduled.
        const poll = async (): Promise<boolean> => {
            try {
                const next = await apiGet<Action>(`/api/v1/actions/${id}`);
                if (!mountedRef.current || gen !== pollGenRef.current) {
                    return false;
                }
                setAction(next);
                return !isTerminalExecStatus(next.exec_status);
            } catch (err) {
                if (!mountedRef.current || gen !== pollGenRef.current) {
                    return false;
                }
                // The action is gone or off limits: nothing to follow.
                if (
                    err instanceof ApiError
                    && (err.statusCode === 403 || err.statusCode === 404)
                ) {
                    setError(messageOf(err));
                    return false;
                }
                // Anything else is treated as transient.
                return true;
            }
        };

        const schedule = () => {
            timerRef.current = setTimeout(() => {
                timerRef.current = null;
                void poll().then((again) => {
                    if (again) {
                        delay = Math.min(delay * 2, POLL_MAX_MS);
                        schedule();
                    }
                });
            }, delay);
        };
        schedule();
    }, [stopPolling]);

    // Poll only an action that is running, or that has been approved and
    // is about to start; one awaiting its approval will not change.
    const followIfActive = useCallback((next: Action) => {
        if (
            next.exec_status === 'executing'
            || (next.exec_status === 'not_started' && next.approval !== 'none')
        ) {
            startPolling(next.id);
        } else {
            stopPolling();
        }
    }, [startPolling, stopPolling]);

    const create = useCallback(async (
        connectionId: number,
        body: ActionCreateRequest,
    ): Promise<Action | null> => {
        setBusy(true);
        setError(null);
        try {
            const created = await apiPost<ActionCreateResponse>(
                `/api/v1/connections/${connectionId}/actions`,
                body,
            );
            if (!mountedRef.current) {
                return created;
            }
            setAction(created);
            if (created.execute_error) {
                setError(created.execute_error);
            }
            followIfActive(created);
            return created;
        } catch (err) {
            if (mountedRef.current) {
                setError(messageOf(err));
            }
            return null;
        } finally {
            if (mountedRef.current) {
                setBusy(false);
            }
        }
    }, [followIfActive]);

    const load = useCallback(async (
        id: number,
        keepError = false,
    ): Promise<Action | null> => {
        if (!keepError) {
            setError(null);
        }
        try {
            const loaded = await apiGet<Action>(`/api/v1/actions/${id}`);
            if (!mountedRef.current) {
                return loaded;
            }
            setAction(loaded);
            followIfActive(loaded);
            return loaded;
        } catch (err) {
            if (mountedRef.current && !keepError) {
                setError(messageOf(err));
            }
            return null;
        }
    }, [followIfActive]);

    const execute = useCallback(async (
        id: number,
        override?: ActionOverride,
    ): Promise<boolean> => {
        setBusy(true);
        setError(null);
        try {
            const body = override
                ? { override: true, confirm: override.confirm }
                : undefined;
            const started = await apiPost<Action>(
                `/api/v1/actions/${id}/execute`,
                body,
            );
            if (!mountedRef.current) {
                return true;
            }
            // The server answers 202 before the run has finished, so an
            // action that still reads not_started is about to start.
            setAction(started);
            if (isTerminalExecStatus(started.exec_status)) {
                stopPolling();
            } else {
                startPolling(id);
            }
            return true;
        } catch (err) {
            if (mountedRef.current) {
                setError(messageOf(err));
                if (err instanceof ApiError && err.statusCode === 403) {
                    setWriteDenied(true);
                }
            }
            // A refusal means the action is not in the state the UI
            // showed, so fetch it again rather than keep a stale button.
            if (
                err instanceof ApiError
                && (err.statusCode === 409 || err.statusCode === 503)
                && mountedRef.current
            ) {
                await load(id, true);
            }
            return false;
        } finally {
            if (mountedRef.current) {
                setBusy(false);
            }
        }
    }, [startPolling, stopPolling, load]);

    const cancel = useCallback(async (id: number): Promise<boolean> => {
        setError(null);
        try {
            await apiPost<unknown>(`/api/v1/actions/${id}/cancel`);
            return true;
        } catch (err) {
            if (mountedRef.current) {
                setError(messageOf(err));
            }
            return false;
        }
    }, []);

    return {
        action, busy, error, writeDenied, create, execute, cancel, load,
    };
};

export default useAction;
