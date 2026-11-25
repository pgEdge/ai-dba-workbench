/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/useClusterData';
import type { PerformanceSummaryData } from './types';
import { extractEstateServerIds } from '../../../utils/clusterHelpers';
import { logger } from '../../../utils/logger';
import { useRetryingFetch } from '../../../hooks/useRetryingFetch';
import { chunkConnectionIds } from '../../../utils/connectionIdBatches';
import type { Selection } from '../../../types/selection';

/**
 * Merge the responses to a batched request into the shape a single
 * response has. A single response is returned verbatim, so the common
 * path is unchanged; otherwise the `connections` arrays are
 * concatenated in batch order, which reproduces the order the server
 * would have returned for the unbatched list.
 *
 * The optional top-level `aggregate` is dropped from a merged result:
 * its weighted cache hit average cannot be recomputed from the response
 * fields alone. Nothing in the client reads it, so leaving it out is
 * honest rather than lossy; a consumer that needs it must either read
 * it from an unbatched response or have the server provide the weights.
 */
const mergeSummaries = async (
    responses: Response[],
): Promise<PerformanceSummaryData> => {
    const results: PerformanceSummaryData[] = await Promise.all(
        responses.map(r => r.json()),
    );

    if (results.length === 1) {return results[0];}

    return {
        time_range: results[0]?.time_range ?? '',
        connections: results.flatMap(r => r.connections ?? []),
    };
};

/**
 * Margin by which a shared window ends before the client's clock, in
 * milliseconds. See sharedWindowQuery.
 */
const SHARED_WINDOW_END_MARGIN_MS = 120_000;

/**
 * Length of a shared window: exactly one day. The server's bucket width
 * is the window's length in whole seconds divided by 60, rounded down,
 * and date_bin starts the buckets at the window's start, so a day gives
 * the 1440-second width and 60 full buckets of a plain 24-hour window.
 * A longer window would add a partial bucket at the end that only some
 * servers have a sample in, so the summed tiles would end on a drop.
 */
const SHARED_WINDOW_SPAN_MS = 24 * 60 * 60 * 1000;

/**
 * Build the query string for an explicit 24-hour window that every
 * batch of one fetch shares.
 *
 * The server buckets each response with date_bin, using the window's
 * start as the origin and a width derived from the window's length. With
 * time_range=24h each batched request resolves the window against its
 * own clock, so the batches' bucket times differ by the milliseconds
 * between the requests and the tiles cannot line up one server's buckets
 * with another's. Sending the same time_start and time_end to every
 * batch gives them all the same origin and width.
 *
 * The server clamps an end in its own future to its own now, which
 * would shorten the window and so narrow the bucket width. The end
 * therefore sits two minutes behind the client's clock, so a client
 * clock up to two minutes ahead of the server's causes no clamp; one
 * further ahead can still give batches different widths. The bounds are
 * whole seconds so the width calculation sees exactly the span sent.
 */
const sharedWindowQuery = (nowMs: number): string => {
    const endMs = Math.floor((nowMs - SHARED_WINDOW_END_MARGIN_MS) / 1000) * 1000;
    const startMs = endMs - SHARED_WINDOW_SPAN_MS;
    const iso = (ms: number): string => new Date(ms).toISOString().replace('.000Z', 'Z');
    return `time_range=custom&time_start=${encodeURIComponent(iso(startMs))}`
        + `&time_end=${encodeURIComponent(iso(endMs))}`;
};

interface UsePerformanceSummaryReturn {
    data: PerformanceSummaryData | null;
    loading: boolean;
    error: string | null;
    /** True while an automatic retry is pending after a failed fetch. */
    retrying: boolean;
}

/**
 * Custom hook for fetching performance summary data.
 * Follows the useTimelineEvents pattern with initialLoadDoneRef
 * to prevent flash on auto-refresh.
 */
export const usePerformanceSummary = (
    selection: Selection | null
): UsePerformanceSummaryReturn => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [data, setData] = useState<PerformanceSummaryData | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const { run, retrying } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: !!user && !!selection,
    });

    /*
     * The window here is deliberately fixed at 24 hours and does not
     * track the dashboard time selector. These tiles render above the
     * Monitoring section that holds the selector, so a user looking at
     * them cannot see the control that would be changing the numbers;
     * a fixed at-a-glance summary is the more honest reading. The
     * panels inside the Monitoring section do follow the selector.
     *
     * Build the URLs for the selection, or null when no request should
     * be made. A cluster or estate can hold more servers than one
     * request may name, so its ID list is batched to the server's cap
     * and the responses merged; a list within the cap still yields the
     * single URL it always did. Batches carry one explicit window
     * between them (sharedWindowQuery) so that their buckets line up.
     */
    const buildUrls = useCallback((): string[] | null => {
        if (!selection) {return null;}

        const base = '/api/v1/metrics/performance-summary';
        const batchUrls = (ids: number[]): string[] | null => {
            if (!ids.length) {return null;}
            const batches = chunkConnectionIds(ids);
            if (batches.length === 1) {
                return [`${base}?connection_ids=${batches[0].join(',')}&time_range=24h`];
            }
            const window = sharedWindowQuery(Date.now());
            return batches.map(
                batch => `${base}?connection_ids=${batch.join(',')}&${window}`,
            );
        };

        if (selection.type === 'server') {
            if (selection.id === undefined || selection.id === null) {return null;}
            return [`${base}?connection_id=${selection.id}&time_range=24h`];
        }

        if (selection.type === 'cluster') {
            return batchUrls(selection.serverIds ?? []);
        }

        if (selection.type === 'estate') {
            return batchUrls(extractEstateServerIds(selection));
        }

        return null;
    }, [selection]);

    const fetchData = useCallback(async (): Promise<boolean> => {
        if (!user) {return true;}

        const urls = buildUrls();
        if (!urls) {
            setData(null);
            return true;
        }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const responses = await Promise.all(urls.map(u => apiFetch(u)));

            for (const response of responses) {
                if (!response.ok) {
                    const errorData = await response.json().catch(() => ({})) as { error?: string };
                    throw new Error(errorData.error || `Failed to fetch performance data: ${response.status}`);
                }
            }

            const result = await mergeSummaries(responses);
            // Re-check mount state after the final await so a late
            // resolution cannot call setState on an unmounted component.
            if (isMountedRef.current) {
                setData(result);
                initialLoadDoneRef.current = true;
            }
            return true;
        } catch (err) {
            logger.error('Error fetching performance summary:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch performance data');
                setData(null);
            }
            return false;
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, buildUrls]);

    // Reset initial load state when selection changes
    const selectionId = selection && 'id' in selection ? selection.id : undefined;
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [selection?.type, selectionId]);

    // Fetch when dependencies change
    useEffect(() => {
        isMountedRef.current = true;

        if (user && selection) {
            void run(fetchData);
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, selection, run, fetchData, lastRefresh]);

    return { data, loading, error, retrying };
};

export default usePerformanceSummary;
