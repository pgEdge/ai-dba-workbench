/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useRef } from 'react';

export interface UseRequestSequenceResult {
    /**
     * Register the start of a request and return a predicate that is
     * true only whilst that request is still the newest one and the
     * owning component remains mounted. Call it immediately before
     * starting the request, and consult the predicate before parsing a
     * response and again before every state update derived from it.
     */
    beginRequest: () => () => boolean;
    /**
     * Abandon whatever request is in flight without starting a new
     * one. Use it on an early return that deliberately skips a fetch,
     * such as a half-entered custom time range, so that a response for
     * the previous selection cannot land afterwards and be displayed
     * as though it belonged to the new one.
     */
    supersedeRequest: () => void;
}

/**
 * Order overlapping fetches so that only the newest one may write
 * state.
 *
 * A mounted flag alone cannot do this. The usual shape is an effect
 * that starts a fetch and whose cleanup sets the flag false, but when
 * a dependency such as the selected time range changes, the next run
 * of that effect sets the flag back to true before the earlier request
 * has resolved, so the superseded response passes the guard and
 * overwrites data belonging to the current selection. Comparing a
 * monotonically increasing request number closes that window: the
 * older request's number no longer matches, so its response is
 * discarded however late it arrives.
 *
 * The mounted flag is tracked here too, on a mount-only effect, so
 * consumers no longer need one of their own and no longer need to
 * reset it on every dependency change.
 */
export const useRequestSequence = (): UseRequestSequenceResult => {
    const isMountedRef = useRef<boolean>(true);
    const requestIdRef = useRef<number>(0);

    useEffect(() => {
        isMountedRef.current = true;

        return () => {
            isMountedRef.current = false;
        };
    }, []);

    const beginRequest = useCallback((): (() => boolean) => {
        const requestId = ++requestIdRef.current;

        return (): boolean =>
            isMountedRef.current && requestIdRef.current === requestId;
    }, []);

    const supersedeRequest = useCallback((): void => {
        requestIdRef.current += 1;
    }, []);

    return { beginRequest, supersedeRequest };
};

export default useRequestSequence;
