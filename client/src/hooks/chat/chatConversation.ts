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
 * Chat conversation persistence module.
 *
 * Provides functions to create, update, and fetch conversations
 * from the backend API.
 */

import type { ChatMessageData } from '../../components/ChatPanel/ChatMessage';
import type { ConversationCreateResponse, ConversationDetail } from './chatTypes';
import { logger } from '../../utils/logger';

/**
 * Type alias for the fetch function signature used by the conversation
 * module. This allows dependency injection for testing.
 */
export type FetchFunction = (
    url: string,
    init?: RequestInit,
) => Promise<Response>;

/**
 * Create a new conversation with the given messages.
 *
 * @param messages - The visible chat messages to persist.
 * @param fetchFn - The fetch function to use for the API request.
 * @returns The conversation ID if successful, or null on failure.
 */
export async function createConversation(
    messages: ChatMessageData[],
    fetchFn: FetchFunction,
): Promise<string | null> {
    try {
        const response = await fetchFn('/api/v1/conversations', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                messages,
                provider: '',
                model: '',
            }),
        });

        if (!response.ok) {
            logger.warn(
                'Failed to create conversation:',
                response.status,
                await response.text(),
            );
            return null;
        }

        const data: ConversationCreateResponse = await response.json();
        return data.id;
    } catch (err) {
        logger.error('Failed to create conversation:', err);
        return null;
    }
}

/**
 * Update an existing conversation with new messages.
 *
 * @param id - The conversation ID to update.
 * @param messages - The updated visible chat messages.
 * @param fetchFn - The fetch function to use for the API request.
 * @returns True if the update succeeded, false otherwise.
 */
export async function updateConversation(
    id: string,
    messages: ChatMessageData[],
    fetchFn: FetchFunction,
): Promise<boolean> {
    try {
        const response = await fetchFn(`/api/v1/conversations/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messages }),
        });

        if (!response.ok) {
            logger.warn(
                'Failed to update conversation:',
                response.status,
                await response.text(),
            );
            return false;
        }

        return true;
    } catch (err) {
        logger.error('Failed to update conversation:', err);
        return false;
    }
}

/**
 * Fetch an existing conversation by ID.
 *
 * @param id - The conversation ID to fetch.
 * @param fetchFn - The fetch function to use for the API request.
 * @returns The conversation detail on success.
 * @throws Error if the fetch fails.
 */
export async function fetchConversation(
    id: string,
    fetchFn: FetchFunction,
): Promise<ConversationDetail> {
    const response = await fetchFn(`/api/v1/conversations/${id}`);

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to load conversation: ${errorText}`);
    }

    return response.json();
}

/**
 * The most action IDs one link request may name. This MUST match
 * `actionMaxLinkIDs` in `server/src/internal/api/action_handlers.go`,
 * which answers a longer list with a 400.
 */
export const MAX_ACTION_IDS_PER_LINK = 100;

/**
 * Link the actions proposed during a conversation's first turn to it.
 * Those tool calls ran before the conversation had been saved, so they
 * carried no conversation ID.
 *
 * @param conversationId - The newly saved conversation.
 * @param actionIds - The IDs of the actions to link.
 * @param fetchFn - The fetch function to use for the API request.
 * @returns True if the actions were linked, false otherwise.
 */
export async function linkConversationActions(
    conversationId: string,
    actionIds: number[],
    fetchFn: FetchFunction,
): Promise<boolean> {
    if (actionIds.length === 0) {
        return true;
    }
    try {
        const response = await fetchFn(
            `/api/v1/conversations/${conversationId}/actions/link`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action_ids: actionIds }),
            },
        );

        if (!response.ok) {
            logger.warn(
                'Failed to link actions to conversation:',
                response.status,
            );
            return false;
        }
        return true;
    } catch (err) {
        logger.error('Failed to link actions to conversation:', err);
        return false;
    }
}
