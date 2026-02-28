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
 */
export interface LLMContentBlock {
    type: string;
    text?: string;
    id?: string;
    name?: string;
    input?: Record<string, unknown>;
}

/**
 * Top-level response envelope from the LLM chat endpoint.
 */
export interface LLMResponse {
    content?: LLMContentBlock[];
    stop_reason?: string;
}

/**
 * Response from executing a single MCP tool call.
 */
export interface ToolCallResponse {
    content?: Array<{ text?: string }>;
    isError?: boolean;
}

/**
 * A tool result message sent back to the LLM after tool execution.
 */
export interface ToolResult {
    type: 'tool_result';
    tool_use_id: string;
    content: string;
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
 * JSON Schema fragment describing the input parameters of an MCP tool.
 */
export interface ToolInputSchema {
    type: string;
    properties: Record<string, { type: string; description: string }>;
    required: string[];
}
