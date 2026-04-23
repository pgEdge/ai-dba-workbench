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
 * Agentic loop module.
 *
 * Implements the core LLM tool-use loop that iteratively calls the
 * language model and executes tool requests until a final text
 * response is produced or the iteration limit is reached.
 */

import { ChatMessageData } from '../../components/ChatPanel/ChatMessage';
import { ToolActivity } from '../../components/ChatPanel/ToolStatus';
import {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
} from '../../types/llm';
import { APIMessage, ToolDefinition } from './chatTypes';

/**
 * Type alias for the fetch function signature used by the agentic
 * loop. This allows dependency injection for testing.
 */
export type FetchFunction = (
    url: string,
    init?: RequestInit,
) => Promise<Response>;

/**
 * Parameters for running the agentic LLM loop.
 */
export interface AgenticLoopParams {
    /** Current API message history including the user's new message. */
    apiMessages: APIMessage[];
    /** Available tools the LLM can call. */
    availableTools: ToolDefinition[];
    /** System prompt for the LLM. */
    systemPrompt: string;
    /** Maximum number of LLM iterations before giving up. */
    maxIterations: number;
    /** Abort signal for cancellation. */
    abortSignal: AbortSignal;
    /** Fetch function for API calls. */
    fetchFn: FetchFunction;
    /** Callback invoked when tool activity updates. */
    onToolActivity: (activities: ToolActivity[]) => void;
}

/**
 * Result of a successful agentic loop execution.
 */
export interface AgenticLoopResult {
    /** The final assistant message to display to the user. */
    finalMessage: ChatMessageData;
    /** The updated API message history after the loop completes. */
    updatedApiMessages: APIMessage[];
}

/**
 * Error message returned when the iteration limit is reached.
 */
export const ITERATION_LIMIT_MESSAGE =
    'I was unable to complete the request within the allowed number of ' +
    'steps. Please try rephrasing your question.';

/**
 * Run the agentic LLM tool-use loop.
 *
 * This function calls the LLM with the current message history. If the
 * LLM requests tool calls, it executes them and feeds the results back
 * to the LLM. This continues until either:
 *
 * 1. The LLM returns a text response without tool calls (success).
 * 2. The maximum iteration count is reached (returns error message).
 * 3. The abort signal is triggered (throws AbortError).
 * 4. An unrecoverable error occurs (throws Error).
 *
 * @param params - The loop parameters.
 * @returns The final assistant message and updated API messages.
 * @throws AbortError if the request was cancelled.
 * @throws Error if an unrecoverable error occurs.
 */
export async function runAgenticLoop(
    params: AgenticLoopParams,
): Promise<AgenticLoopResult> {
    const {
        apiMessages,
        availableTools,
        systemPrompt,
        maxIterations,
        abortSignal,
        fetchFn,
        onToolActivity,
    } = params;

    let currentMessages = [...apiMessages];
    let iterations = 0;
    const collectedActivity: ToolActivity[] = [];

    while (iterations < maxIterations) {
        if (abortSignal.aborted) {
            const abortError = new Error('Aborted');
            abortError.name = 'AbortError';
            throw abortError;
        }
        iterations++;

        // Call the LLM with current message history and tools
        const response = await fetchFn('/api/v1/llm/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                messages: currentMessages,
                tools: availableTools,
                system: systemPrompt,
            }),
            signal: abortSignal,
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`LLM request failed: ${errorText}`);
        }

        const data: LLMResponse = await response.json();

        const toolUses =
            data.content?.filter(c => c.type === 'tool_use') || [];
        const textBlocks =
            data.content?.filter(c => c.type === 'text') || [];

        if (toolUses.length === 0) {
            // No tool calls - extract final text response
            const assistantText =
                textBlocks.map(c => c.text).join('\n') || '';

            const finalMessage: ChatMessageData = {
                role: 'assistant',
                content: assistantText,
                timestamp: new Date().toISOString(),
                activity:
                    collectedActivity.length > 0
                        ? [...collectedActivity]
                        : undefined,
            };

            // Append to API history
            currentMessages = [
                ...currentMessages,
                { role: 'assistant', content: assistantText },
            ];

            return { finalMessage, updatedApiMessages: currentMessages };
        }

        // --- Tool execution phase ---

        // Append the assistant message (with tool_use blocks) to history
        currentMessages = [
            ...currentMessages,
            { role: 'assistant', content: data.content as LLMContentBlock[] },
        ];

        // Execute each tool call sequentially
        const toolResults: ToolResult[] = [];

        for (const toolUse of toolUses) {
            const toolName = toolUse.name ?? 'unknown';

            // Mark tool as running in the activity tracker
            const activity: ToolActivity = {
                name: toolName,
                status: 'running',
                startedAt: new Date().toISOString(),
            };
            collectedActivity.push(activity);
            onToolActivity([...collectedActivity]);

            try {
                const toolResponse = await fetchFn('/api/v1/mcp/tools/call', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        name: toolUse.name,
                        arguments: toolUse.input,
                    }),
                    signal: abortSignal,
                });

                if (!toolResponse.ok) {
                    const errorText = await toolResponse.text();
                    throw new Error(
                        errorText ||
                            `Tool call failed with status ${toolResponse.status}`,
                    );
                }

                const toolData: ToolCallResponse = await toolResponse.json();
                const resultText =
                    toolData.content?.[0]?.text ||
                    (toolData.isError
                        ? 'Tool execution failed'
                        : 'No data returned');

                activity.status = toolData.isError ? 'error' : 'completed';
                onToolActivity([...collectedActivity]);

                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.id ?? '',
                    content: resultText,
                    is_error: toolData.isError || undefined,
                });
            } catch (toolErr) {
                if ((toolErr as Error).name === 'AbortError') {
                    throw toolErr;
                }

                const errMsg = `Tool execution error: ${(toolErr as Error).message}`;
                activity.status = 'error';
                onToolActivity([...collectedActivity]);

                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.id ?? '',
                    content: errMsg,
                    is_error: true,
                });
            }
        }

        // Append tool results to API history and loop
        currentMessages = [
            ...currentMessages,
            { role: 'user', content: toolResults },
        ];
    }

    // Loop exhausted iterations without a final text response
    const errorMessage: ChatMessageData = {
        role: 'assistant',
        content: ITERATION_LIMIT_MESSAGE,
        timestamp: new Date().toISOString(),
        isError: true,
        activity:
            collectedActivity.length > 0
                ? [...collectedActivity]
                : undefined,
    };

    currentMessages = [
        ...currentMessages,
        { role: 'assistant', content: ITERATION_LIMIT_MESSAGE },
    ];

    return { finalMessage: errorMessage, updatedApiMessages: currentMessages };
}
