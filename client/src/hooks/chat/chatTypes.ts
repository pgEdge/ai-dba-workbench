/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { LLMContentBlock, ToolResult, ToolInputSchema } from '../../types/llm';
import type { ChatMessageData } from '../../components/ChatPanel/ChatMessage';

// ---------------------------------------------------------------
// Internal types (API wire format - not exported from hook)
// ---------------------------------------------------------------

/**
 * API message format used in the LLM chat request body.
 */
export interface APIMessage {
    role: string;
    content: string | LLMContentBlock[] | ToolResult[];
}

/**
 * Definition of an MCP tool available to the chat.
 */
export interface ToolDefinition {
    name: string;
    description: string;
    inputSchema: ToolInputSchema;
}

/**
 * Response from creating a new conversation.
 */
export interface ConversationCreateResponse {
    id: string;
    title: string;
}

/**
 * Response from fetching a conversation's details.
 */
export interface ConversationDetail {
    id: string;
    title: string;
    messages: ChatMessageData[];
}

/**
 * Response from the compaction endpoint.
 */
export interface CompactResponse {
    messages: APIMessage[];
}
