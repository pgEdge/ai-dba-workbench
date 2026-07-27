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
 * Content block returned by the LLM API. Represents text output,
 * tool-use requests, or other structured content from the model.
 *
 * Text blocks carry `text`. Tool-use blocks from the server nest the
 * call details under `tool_use` ({@link LLMToolUse}); the legacy flat
 * `id`/`name`/`input` fields are retained only for backward-compatible
 * local construction and are no longer populated by the server for
 * tool-use responses.
 */
export interface LLMContentBlock {
    type: string;
    text?: string;
    tool_use?: LLMToolUse;
    id?: string;
    name?: string;
    input?: Record<string, unknown>;
}

/**
 * Nested tool-use payload carried by a `tool_use` content block in a
 * library `llm/proxy` chat response.
 */
export interface LLMToolUse {
    id: string;
    name: string;
    input?: Record<string, unknown>;
}

/**
 * Token usage accounting returned by the LLM chat endpoint.
 */
export interface LLMUsage {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
    cache_creation_input_tokens?: number;
    cache_read_input_tokens?: number;
}

/**
 * Top-level response envelope from the LLM chat endpoint.
 */
export interface LLMResponse {
    content?: LLMContentBlock[];
    stop_reason?: string;
    usage?: LLMUsage;
}

/**
 * Response from executing a single MCP tool call.
 */
export interface ToolCallResponse {
    content?: { text?: string }[];
    isError?: boolean;
}

/**
 * A tool result message sent back to the LLM after tool execution.
 */
export interface ToolResult {
    type: 'tool_result';
    tool_use_id: string;
    text: string;
    is_error?: boolean;
}

/**
 * A single message in an LLM conversation. The content field carries
 * plain text for user messages and structured blocks for assistant or
 * tool-result turns.
 */
export interface Message {
    role: string;
    content: string | LLMContentBlock[] | ToolResult[];
}

/**
 * Normalise a message's content into the typed-block array shape
 * required by the library `llm/proxy` chat endpoint. Plain strings are
 * wrapped in a single `text` block; block arrays (assistant content,
 * tool results) are passed through unchanged.
 *
 * The server rejects plain-string content, so every outgoing message
 * must pass through this helper before it is serialised.
 *
 * @param content - The message content to normalise.
 * @returns A typed-block array suitable for the wire.
 */
export function toContentBlocks(
    content: string | LLMContentBlock[] | ToolResult[],
): LLMContentBlock[] | ToolResult[] {
    if (typeof content === 'string') {
        return [{ type: 'text', text: content }];
    }
    return content;
}

/**
 * Normalise an array of messages so every message carries typed-block
 * content. See {@link toContentBlocks}.
 *
 * @param messages - The messages to normalise.
 * @returns Messages with block-array content.
 */
export function normaliseMessages(messages: Message[]): Message[] {
    return messages.map(m => ({
        role: m.role,
        content: toContentBlocks(m.content),
    }));
}

/**
 * JSON Schema fragment describing the input parameters of an MCP tool.
 */
export interface ToolInputSchema {
    type: string;
    properties: Record<string, { type: string; description: string }>;
    required: string[];
}

/**
 * Wire shape of a tool definition sent to the library `llm/proxy` chat
 * endpoint. The endpoint decodes the request body into the vendored
 * `pgedge-go-llm-lib` package's `llm.Tool` struct, which tags its input
 * schema field `json:"input_schema"` (snake_case); the app's own
 * internal tool types use camelCase `inputSchema` instead, so this
 * shape exists purely to describe the post-normalisation wire format.
 */
export interface LLMTool {
    name: string;
    description: string;
    input_schema: unknown;
}

/**
 * Normalise an array of app-internal tool definitions into the
 * snake_case wire shape required by the library `llm/proxy` chat
 * endpoint. See {@link LLMTool}.
 *
 * The app's internal tool types (`ToolDefinition`, `AnalysisTool`)
 * carry a camelCase `inputSchema` field, but the vendored
 * `pgedge-go-llm-lib` decodes `tools[].input_schema` (snake_case).
 * Without this rename, `encoding/json` silently drops the schema for
 * every tool, since an underscore is not a case-only difference and
 * Go's JSON decoder has no fallback match. See issue #370.
 *
 * @param tools - The app-internal tool definitions to normalise.
 * @returns Tool definitions with a snake_case `input_schema` field.
 */
export function normaliseTools(
    tools: Array<{
        name: string;
        description: string;
        inputSchema: unknown;
    }>,
): LLMTool[] {
    return tools.map(t => ({
        name: t.name,
        description: t.description,
        input_schema: t.inputSchema,
    }));
}
