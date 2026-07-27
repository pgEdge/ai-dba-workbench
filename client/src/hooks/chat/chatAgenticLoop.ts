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

import type { ChatMessageData } from '../../components/ChatPanel/ChatMessage';
import type { ToolActivity } from '../../components/ChatPanel/ToolStatus';
import type {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
} from '../../types/llm';
import { normaliseMessages, normaliseTools } from '../../types/llm';
import type { APIMessage, ToolDefinition } from './chatTypes';

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
 * Error message rendered when the user has no MCP tool privileges.
 *
 * The chat short-circuits before invoking the agentic loop when the
 * server reports an empty tool list. Without this guard, the LLM would
 * keep proposing tool calls that all fail with "Access denied", causing
 * the loop to spin until `maxIterations` is exhausted. See issue #188.
 */
export const NO_MCP_PRIVILEGES_MESSAGE =
    "You don't have permission to use any of the tools I need to answer " +
    'questions like this. Ask your administrator to grant you the ' +
    'relevant MCP privileges and try again.';

/**
 * Error message returned when the LLM proposes a tool call that is not
 * in the available tool list. This guards against drift between the
 * system prompt (which mentions tools by name) and the RBAC-filtered
 * tool list the user is actually permitted to call.
 */
export const UNKNOWN_TOOL_MESSAGE =
    'The model attempted to call tools that are not available to you. ' +
    'This usually means your account lacks the necessary MCP ' +
    'privileges. Please contact your administrator.';

/**
 * Threshold for the repeated-failure circuit breaker.
 *
 * When the same tool fails with the same error this many times across
 * loop iterations, the loop stops rather than churning to the iteration
 * limit. The 3rd identical failure trips the breaker. See issue #268.
 */
export const MAX_REPEATED_TOOL_FAILURES = 3;

/**
 * Build the user-facing message returned when the repeated-failure
 * circuit breaker trips.
 *
 * Some tools (for example `query_database` or `get_schema_info`) pass
 * the LLM's pre-flight validation via `test_query` but then fail at
 * execution time with an "Access denied"-style error. The LLM responds
 * by retrying slightly different SQL, which validates and fails again,
 * looping until the iteration limit is exhausted. This builder produces
 * a clear error that names the failing tool, surfaces the underlying
 * error text, and points the user at a likely permissions or
 * connection-access cause. See issue #268.
 *
 * @param toolName - The name of the tool that failed repeatedly.
 * @param errorText - The underlying error text from the failed calls.
 * @returns A clear, user-facing error message.
 */
export function buildRepeatedToolFailureMessage(
    toolName: string,
    errorText: string,
): string {
    return (
        `The "${toolName}" tool failed repeatedly with the same error, ` +
        'so I stopped retrying. The error was:\n\n' +
        `${errorText}\n\n` +
        'This is often a permissions or connection-access problem. ' +
        'Ask your administrator to confirm you have access to this ' +
        'connection and the relevant MCP privileges, then try again.'
    );
}

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

    // Repeated-failure circuit breaker state (issue #268). Keyed by a
    // signature combining the tool name and the error text, this counts
    // how many times each distinct tool-error has been seen across loop
    // iterations. Only error results (is_error truthy) are counted.
    const failureCounts = new Map<string, number>();

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
                messages: normaliseMessages(currentMessages),
                tools: normaliseTools(availableTools),
                system_prompt: systemPrompt,
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

        // Defensive check: if the LLM proposes only tool calls that are
        // not in the available tool list, bail out rather than feeding
        // back N "unknown tool" errors. This typically indicates the
        // user lacks MCP privileges for the tools the model is reaching
        // for (the system prompt mentions all tools by name; RBAC may
        // filter them out of `availableTools`). See issue #188.
        if (toolUses.length > 0) {
            const availableNames = new Set(
                availableTools.map(t => t.name),
            );
            const allUnknown = toolUses.every(
                t => !availableNames.has(t.tool_use?.name ?? ''),
            );
            if (allUnknown) {
                const finalMessage: ChatMessageData = {
                    role: 'assistant',
                    content: UNKNOWN_TOOL_MESSAGE,
                    timestamp: new Date().toISOString(),
                    isError: true,
                    activity:
                        collectedActivity.length > 0
                            ? [...collectedActivity]
                            : undefined,
                };
                currentMessages = [
                    ...currentMessages,
                    { role: 'assistant', content: UNKNOWN_TOOL_MESSAGE },
                ];
                return {
                    finalMessage,
                    updatedApiMessages: currentMessages,
                };
            }
        }

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

        // The tool name and error text of the failure that tripped the
        // circuit breaker, if any, recorded during this iteration.
        let trippedToolName: string | null = null;
        let trippedErrorText = '';

        for (const toolUse of toolUses) {
            const toolName = toolUse.tool_use?.name ?? 'unknown';

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
                        name: toolUse.tool_use?.name,
                        arguments: toolUse.tool_use?.input,
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
                    tool_use_id: toolUse.tool_use?.id ?? '',
                    text: resultText,
                    is_error: toolData.isError || undefined,
                });

                // Track repeated failures of this (tool, error) pair so
                // the loop can break out rather than spinning until the
                // iteration limit. Only error results count. See #268.
                if (toolData.isError) {
                    const signature = `${toolName} ${resultText}`;
                    const count = (failureCounts.get(signature) ?? 0) + 1;
                    failureCounts.set(signature, count);
                    if (count >= MAX_REPEATED_TOOL_FAILURES) {
                        trippedToolName = toolName;
                        trippedErrorText = resultText;
                    }
                }
            } catch (toolErr) {
                if ((toolErr as Error).name === 'AbortError') {
                    throw toolErr;
                }

                const errMsg = `Tool execution error: ${(toolErr as Error).message}`;
                activity.status = 'error';
                onToolActivity([...collectedActivity]);

                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.tool_use?.id ?? '',
                    text: errMsg,
                    is_error: true,
                });

                // The catch path always produces an error result, so
                // count it toward the circuit breaker as well. See #268.
                const signature = `${toolName} ${errMsg}`;
                const count = (failureCounts.get(signature) ?? 0) + 1;
                failureCounts.set(signature, count);
                if (count >= MAX_REPEATED_TOOL_FAILURES) {
                    trippedToolName = toolName;
                    trippedErrorText = errMsg;
                }
            }
        }

        // Append tool results to API history and loop
        currentMessages = [
            ...currentMessages,
            { role: 'user', content: toolResults },
        ];

        // Repeated-failure circuit breaker (issue #268). If a tool has
        // now failed with the same error MAX_REPEATED_TOOL_FAILURES
        // times, stop looping and return a clear error rather than
        // letting the LLM retry until the iteration limit is reached.
        if (trippedToolName !== null) {
            const breakerMessage = buildRepeatedToolFailureMessage(
                trippedToolName,
                trippedErrorText,
            );
            const finalMessage: ChatMessageData = {
                role: 'assistant',
                content: breakerMessage,
                timestamp: new Date().toISOString(),
                isError: true,
                activity:
                    collectedActivity.length > 0
                        ? [...collectedActivity]
                        : undefined,
            };
            currentMessages = [
                ...currentMessages,
                { role: 'assistant', content: breakerMessage },
            ];
            return { finalMessage, updatedApiMessages: currentMessages };
        }
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
