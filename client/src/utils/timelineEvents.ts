/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { apiGet } from './apiClient';
import type { TimelineEvent } from '../components/EventTimeline/types';

/**
 * Format an array of timeline events into a human-readable text block
 * suitable for inclusion in an LLM prompt.
 */
function formatEvents(
    events: TimelineEvent[],
    label: string,
): string {
    const lines = events.map(e => {
        const time = new Date(e.occurred_at).toLocaleString();
        const summary = e.summary ? `: ${e.summary}` : '';
        return `  [${time}] ${e.event_type} - ${e.title}${summary}`;
    });
    return `\nTimeline Events (${label}):\n${lines.join('\n')}`;
}

/**
 * Fetch timeline events for a connection using a 24-hour window
 * centered on the given reference time (defaults to now).
 */
export async function fetchTimelineEventsCentered(
    connectionId: number,
    referenceTime?: string | Date,
): Promise<string> {
    const center = referenceTime ? new Date(referenceTime) : new Date();
    const startTime = new Date(center.getTime() - 12 * 60 * 60 * 1000);
    const endTime = new Date(center.getTime() + 12 * 60 * 60 * 1000);

    const params = new URLSearchParams({
        start_time: startTime.toISOString(),
        end_time: endTime.toISOString(),
        connection_id: String(connectionId),
        limit: '100',
    });

    const data = await apiGet<{ events?: TimelineEvent[] }>(
        `/api/v1/timeline/events?${params}`,
    ).catch(() => null);
    if (!data) { return ''; }

    const events = data.events;
    if (!events || events.length === 0) { return ''; }

    return formatEvents(events, '24h window');
}

/**
 * Fetch timeline events for a connection using a relative time range
 * string (e.g. '1h', '6h', '24h', '7d', '30d'). The window extends
 * from `now - range` to `now`.
 */
export async function fetchTimelineEventsForRange(
    connectionId: number,
    timeRange: string | undefined,
): Promise<string> {
    const now = new Date();
    let startTime: Date;
    switch (timeRange) {
        case '1h': startTime = new Date(now.getTime() - 60 * 60 * 1000); break;
        case '6h': startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000); break;
        case '24h': startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000); break;
        case '7d': startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000); break;
        case '30d': startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000); break;
        default: startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000); break;
    }

    const params = new URLSearchParams({
        start_time: startTime.toISOString(),
        end_time: now.toISOString(),
        connection_id: String(connectionId),
        limit: '100',
    });

    const data = await apiGet<{ events?: TimelineEvent[] }>(
        `/api/v1/timeline/events?${params}`,
    ).catch(() => null);
    if (!data) { return ''; }

    const events = data.events;
    if (!events || events.length === 0) { return ''; }

    return formatEvents(events, timeRange || '24h');
}
