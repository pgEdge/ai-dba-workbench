/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Chat compaction module.
 *
 * Provides functionality to compact long conversation histories by
 * summarizing older messages while preserving recent context.
 */

import type { APIMessage, CompactResponse } from './chatTypes';
import {
    COMPACTION_TOKEN_THRESHOLD,
    COMPACTION_MAX_TOKENS,
    COMPACTION_RECENT_WINDOW,
} from './chatConstants';
import { estimateTokenCount } from './chatHelpers';
import { logger } from '../../utils/logger';

/**
 * Type alias for the fetch function signature used by the compaction
 * module. This allows dependency injection for testing.
 */
export type FetchFunction = (
    url: string,
    init?: RequestInit,
) => Promise<Response>;

/**
 * Compact the API message history when estimated tokens exceed the
 * threshold.  Returns the compacted messages if successful, or the
 * original messages if compaction is not needed or fails.
 *
 * @param msgs - The current API message history.
 * @param fetchFn - The fetch function to use for the compaction request.
 * @returns The compacted or original messages.
 */
export async function maybeCompact(
    msgs: APIMessage[],
    fetchFn: FetchFunction,
): Promise<APIMessage[]> {
    if (estimateTokenCount(msgs) < COMPACTION_TOKEN_THRESHOLD) {
        return msgs;
    }

    try {
        const response = await fetchFn('/api/v1/chat/compact', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                messages: msgs,
                max_tokens: COMPACTION_MAX_TOKENS,
                recent_window: COMPACTION_RECENT_WINDOW,
                keep_anchors: true,
                options: {
                    preserve_tool_results: true,
                    enable_summarization: true,
                },
            }),
        });

        if (!response.ok) {
            return msgs;
        }

        const data: CompactResponse = await response.json();
        return data.messages ?? msgs;
    } catch (err) {
        logger.error('Chat compaction failed:', err);
        return msgs;
    }
}
