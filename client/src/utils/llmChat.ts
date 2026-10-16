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
 * Shared request builder for the LLM chat endpoint.
 *
 * This module is the single place in the client that knows the wire
 * contract of `POST /api/v1/llm/chat`, which the server decodes into
 * the `pgedge-go-llm-lib` request types:
 *
 * - `messages[].content` must be an array of typed content blocks, never
 *   a plain string (see {@link normaliseMessages});
 * - `tools[].input_schema` is snake_case, whereas the app's internal tool
 *   types carry camelCase `inputSchema` (see {@link normaliseTools} and
 *   issue #370, where the mismatch silently emptied every tool schema);
 * - the system prompt travels as `system_prompt`.
 *
 * Every call site must build its request through
 * {@link buildChatRequestInit} rather than hand-assembling the body, so
 * the contract cannot be got wrong per site. See issue #373.
 */

import type { Message } from '../types/llm';
import { normaliseMessages, normaliseTools } from '../types/llm';

/** Path of the LLM chat endpoint. */
export const LLM_CHAT_PATH = '/api/v1/llm/chat';

/**
 * App-internal tool definition accepted by {@link buildChatRequestInit}.
 * Both `ToolDefinition` (chat) and `AnalysisTool` (analysis loops) are
 * structurally compatible with this shape.
 */
export interface ChatRequestTool {
    name: string;
    description: string;
    inputSchema: unknown;
}

/**
 * Parameters for a single LLM chat request.
 */
export interface ChatRequestParams {
    /** Conversation history; string content is wrapped in a text block. */
    messages: Message[];
    /** Tool definitions to offer the model; omitted from the body when
     * absent or empty. */
    tools?: ChatRequestTool[];
    /** System prompt sent as `system_prompt`. */
    systemPrompt: string;
    /** Optional abort signal forwarded to `fetch`. */
    signal?: AbortSignal;
}

/**
 * Build the `RequestInit` for a `POST /api/v1/llm/chat` call, applying
 * the wire-contract normalisation described in the module comment.
 *
 * The `signal` key is only present on the returned object when a signal
 * was supplied, so callers that never abort produce the same init shape
 * they always have.
 *
 * @param params - The request parameters.
 * @returns A `RequestInit` ready to pass to `apiFetch` or `fetch`.
 */
export function buildChatRequestInit(params: ChatRequestParams): RequestInit {
    const { messages, tools, systemPrompt, signal } = params;

    const init: RequestInit = {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            messages: normaliseMessages(messages),
            tools: tools && tools.length > 0 ? normaliseTools(tools) : undefined,
            system_prompt: systemPrompt,
        }),
    };

    if (signal !== undefined) {
        init.signal = signal;
    }

    return init;
}
