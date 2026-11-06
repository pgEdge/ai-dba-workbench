/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useEffect, useRef } from 'react';

/**
 * Shape of the API response from GET /api/v1/overview.
 * Exported so consumers can share a single definition.
 */
export interface OverviewResponse {
    status?: string;
    summary: string | null;
    generated_at: string;
    stale_at: string;
    snapshot?: Record<string, unknown>;
    restart_detected?: boolean;
    /** Generic failure message, present when status is 'error'. */
    error?: string;
}

export interface UseOverviewSSEReturn {
    overview: OverviewResponse | null;
    connected: boolean;
}

/**
 * Convert a polling overview URL to its SSE stream equivalent.
 *
 * Replaces `/overview` with `/overview/stream` in the path portion
 * while preserving any query parameters.
 *
 * @example
 *   toStreamUrl('/api/v1/overview?scope_type=server&scope_id=1')
 *   // => '/api/v1/overview/stream?scope_type=server&scope_id=1'
 */
function toStreamUrl(overviewUrl: string): string {
    const qIndex = overviewUrl.indexOf('?');
    if (qIndex === -1) {
        return overviewUrl.replace('/overview', '/overview/stream');
    }
    const path = overviewUrl.substring(0, qIndex);
    const query = overviewUrl.substring(qIndex);
    return path.replace('/overview', '/overview/stream') + query;
}

/**
 * Subscribe to server-sent overview events via EventSource.
 *
 * The hook derives the SSE stream URL from the given polling URL,
 * opens an EventSource connection with cookie credentials, and
 * listens for named `overview` events.  The browser-native
 * EventSource handles automatic reconnection on transient errors.
 *
 * State is keyed by the URL it arrived for, so a change of URL never
 * reports the previous scope's overview or connection state.
 *
 * @param overviewUrl  The REST polling URL (e.g. `/api/v1/overview`).
 * @returns The latest overview payload and connection status.
 */
export function useOverviewSSE(
    overviewUrl: string,
): UseOverviewSSEReturn {
    const [overviewState, setOverviewState] = useState<{
        url: string;
        overview: OverviewResponse;
    } | null>(null);
    const [connectedUrl, setConnectedUrl] = useState<string | null>(null);
    const esRef = useRef<EventSource | null>(null);

    useEffect(() => {
        const streamUrl = toStreamUrl(overviewUrl);
        const es = new EventSource(streamUrl, { withCredentials: true });
        esRef.current = es;

        es.onopen = () => {
            setConnectedUrl(overviewUrl);
        };

        es.onerror = () => {
            setConnectedUrl((current) =>
                current === overviewUrl ? null : current,
            );
        };

        es.addEventListener('overview', (event: MessageEvent) => {
            try {
                const data = JSON.parse(event.data) as OverviewResponse;
                setOverviewState({ url: overviewUrl, overview: data });
            } catch {
                // Ignore malformed payloads; the next event will
                // deliver a valid update.
            }
        });

        return () => {
            es.close();
            esRef.current = null;
            // Forget this subscription's state so that returning to
            // the same URL later starts afresh.
            setOverviewState((current) =>
                current?.url === overviewUrl ? null : current,
            );
            setConnectedUrl((current) =>
                current === overviewUrl ? null : current,
            );
        };
    }, [overviewUrl]);

    return {
        overview:
            overviewState?.url === overviewUrl ? overviewState.overview : null,
        connected: connectedUrl === overviewUrl,
    };
}
