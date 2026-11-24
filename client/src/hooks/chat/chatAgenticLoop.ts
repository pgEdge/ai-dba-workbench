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

import type { Dispatch } from 'react';
import type { ChatMessageData } from '../../components/ChatPanel/ChatMessage';
import type { ToolActivity } from '../../components/ChatPanel/ToolStatus';
import { extractToolTarget } from '../../components/ChatPanel/chatSqlTarget';
import type {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
} from '../../types/llm';
import { parseProposeActionResult } from '../../types/actions';
import { LLM_CHAT_PATH, buildChatRequestInit } from '../../utils/llmChat';
import { hasText } from '../../utils/textHelpers';
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
    /**
     * The saved conversation's ID, when it has one. Sent with each tool
     * call so an action a tool proposes is tied to the conversation.
     */
    conversationId?: string | null;
    /**
     * Called with the ID of each action a `propose_action` call creates,
     * as soon as its result arrives, so the caller can link it to a
     * conversation later even if this turn then fails or is aborted.
     */
    onActionProposed?: Dispatch<number>;
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

/** A loop result ending the turn with `content` as the assistant reply. */
function finishTurn(
    content: string,
    collectedActivity: ToolActivity[],
    currentMessages: APIMessage[],
    isError: boolean,
): AgenticLoopResult {
    const finalMessage: ChatMessageData = {
        role: 'assistant',
        content,
        timestamp: new Date().toISOString(),
        activity:
            collectedActivity.length > 0 ? [...collectedActivity] : undefined,
    };
    if (isError) {
        finalMessage.isError = true;
    }
    return {
        finalMessage,
        updatedApiMessages: [
            ...currentMessages,
            { role: 'assistant', content },
        ],
    };
}

/** True when the model asked only for tools the user cannot call. */
function allToolsUnknown(
    toolUses: LLMContentBlock[],
    availableTools: ToolDefinition[],
): boolean {
    const availableNames = new Set(availableTools.map(t => t.name));
    return toolUses.every(t => !availableNames.has(t.tool_use?.name ?? ''));
}

/**
 * Build the running activity entry for a tool call. The connection the
 * call targeted is recorded so SQL in the final reply can be run on the
 * same server and database.
 */
function buildToolActivity(
    toolName: string,
    toolUse: LLMContentBlock,
): ToolActivity {
    const activity: ToolActivity = {
        name: toolName,
        status: 'running',
        startedAt: new Date().toISOString(),
        ...extractToolTarget(toolUse.tool_use?.input),
    };
    const revises = toolUse.tool_use?.input?.revises_action_id;
    if (
        toolName === 'propose_action'
        && typeof revises === 'number'
        && Number.isSafeInteger(revises)
        && revises > 0
    ) {
        activity.revisesActionId = revises;
    }
    return activity;
}

/** The text of a tool's result and whether the tool reported an error. */
interface ToolOutcome {
    text: string;
    isError: boolean;
}

/** Turn a tool call's HTTP response into its outcome. */
async function toolOutcomeOf(toolResponse: Response): Promise<ToolOutcome> {
    if (!toolResponse.ok) {
        const errorText = await toolResponse.text();
        const message = hasText(errorText)
            ? errorText
            : `Tool call failed with status ${toolResponse.status}`;
        return { text: `Tool execution error: ${message}`, isError: true };
    }

    const toolData: ToolCallResponse = await toolResponse.json();
    const text = toolData.content?.[0]?.text;
    const fallback = toolData.isError
        ? 'Tool execution failed'
        : 'No data returned';
    return {
        text: hasText(text) ? text : fallback,
        isError: Boolean(toolData.isError),
    };
}

/**
 * Call one tool on the server. Transport and HTTP failures become error
 * outcomes; an abort is rethrown on purpose, for `runAgenticLoop` to
 * propagate and the chat hook to catch, so the turn stops.
 */
async function callTool(
    toolUse: LLMContentBlock,
    params: AgenticLoopParams,
): Promise<ToolOutcome> {
    const body: Record<string, unknown> = {
        name: toolUse.tool_use?.name,
        arguments: toolUse.tool_use?.input,
    };
    if (params.conversationId) {
        body.conversation_id = params.conversationId;
    }
    try {
        const toolResponse = await params.fetchFn('/api/v1/mcp/tools/call', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
            signal: params.abortSignal,
        });
        return await toolOutcomeOf(toolResponse);
    } catch (toolErr) {
        if ((toolErr as Error).name === 'AbortError') {
            throw toolErr;
        }
        return {
            text: `Tool execution error: ${(toolErr as Error).message}`,
            isError: true,
        };
    }
}

/**
 * Count a failed (tool, error) pair towards the repeated-failure circuit
 * breaker, returning true once it has failed often enough to trip it.
 * See issue #268.
 */
function recordToolFailure(
    failureCounts: Map<string, number>,
    toolName: string,
    errorText: string,
): boolean {
    const signature = `${toolName} ${errorText}`;
    const count = (failureCounts.get(signature) ?? 0) + 1;
    failureCounts.set(signature, count);
    return count >= MAX_REPEATED_TOOL_FAILURES;
}

/** Throw an AbortError when the turn has been cancelled. */
function throwIfAborted(abortSignal: AbortSignal): void {
    if (abortSignal.aborted) {
        const abortError = new Error('Aborted');
        abortError.name = 'AbortError';
        throw abortError;
    }
}

/** Call the LLM once and return its parsed response. */
async function callLLM(
    params: AgenticLoopParams,
    messages: APIMessage[],
): Promise<LLMResponse> {
    const response = await params.fetchFn(
        LLM_CHAT_PATH,
        buildChatRequestInit({
            messages,
            tools: params.availableTools,
            systemPrompt: params.systemPrompt,
            signal: params.abortSignal,
        }),
    );
    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`LLM request failed: ${errorText}`);
    }
    return response.json();
}

/** The tool that tripped the circuit breaker, and its error text. */
interface TrippedFailure {
    toolName: string;
    errorText: string;
}

/**
 * Record a successful `propose_action` result on its activity entry and
 * report the new action straight away, so the caller learns of it even
 * if the turn later fails or is aborted.
 */
function recordProposal(
    activity: ToolActivity,
    resultText: string,
    params: AgenticLoopParams,
): void {
    const proposal = parseProposeActionResult(resultText);
    if (proposal) {
        activity.proposal = proposal;
        params.onActionProposed?.(proposal.action_id);
    }
}

/**
 * Execute one iteration's tool calls in order, recording activity as it
 * goes. Returns the tool results and the failure that tripped the
 * circuit breaker, if any.
 */
async function runToolCalls(
    toolUses: LLMContentBlock[],
    params: AgenticLoopParams,
    collectedActivity: ToolActivity[],
    failureCounts: Map<string, number>,
): Promise<{ toolResults: ToolResult[]; tripped: TrippedFailure | null }> {
    const toolResults: ToolResult[] = [];
    let tripped: TrippedFailure | null = null;

    for (const toolUse of toolUses) {
        const toolName = toolUse.tool_use?.name ?? 'unknown';
        const activity = buildToolActivity(toolName, toolUse);
        collectedActivity.push(activity);
        params.onToolActivity([...collectedActivity]);

        const outcome = await callTool(toolUse, params);

        activity.status = outcome.isError ? 'error' : 'completed';
        if (toolName === 'propose_action' && !outcome.isError) {
            recordProposal(activity, outcome.text, params);
        }
        params.onToolActivity([...collectedActivity]);

        toolResults.push({
            type: 'tool_result',
            tool_use_id: toolUse.tool_use?.id ?? '',
            text: outcome.text,
            is_error: outcome.isError || undefined,
        });

        if (
            outcome.isError
            && recordToolFailure(failureCounts, toolName, outcome.text)
        ) {
            tripped = { toolName, errorText: outcome.text };
        }
    }

    return { toolResults, tripped };
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
    let currentMessages = [...params.apiMessages];
    const collectedActivity: ToolActivity[] = [];

    // Repeated-failure circuit breaker state (issue #268). Keyed by a
    // signature combining the tool name and the error text, this counts
    // how many times each distinct tool-error has been seen across loop
    // iterations. Only error results are counted.
    const failureCounts = new Map<string, number>();

    for (let iteration = 0; iteration < params.maxIterations; iteration++) {
        throwIfAborted(params.abortSignal);

        const data = await callLLM(params, currentMessages);
        const toolUses =
            data.content?.filter(c => c.type === 'tool_use') || [];

        if (toolUses.length === 0) {
            // No tool calls - the text blocks are the final reply
            const assistantText = (data.content ?? [])
                .filter(c => c.type === 'text')
                .map(c => c.text)
                .join('\n');
            return finishTurn(
                assistantText, collectedActivity, currentMessages, false,
            );
        }

        // Defensive check: if the LLM proposes only tool calls that are
        // not in the available tool list, bail out rather than feeding
        // back N "unknown tool" errors. This typically indicates the
        // user lacks MCP privileges for the tools the model is reaching
        // for (the system prompt mentions all tools by name; RBAC may
        // filter them out of `availableTools`). See issue #188.
        if (allToolsUnknown(toolUses, params.availableTools)) {
            return finishTurn(
                UNKNOWN_TOOL_MESSAGE, collectedActivity, currentMessages, true,
            );
        }

        // Append the assistant message (with tool_use blocks) to history
        currentMessages = [
            ...currentMessages,
            { role: 'assistant', content: data.content as LLMContentBlock[] },
        ];

        const { toolResults, tripped } = await runToolCalls(
            toolUses, params, collectedActivity, failureCounts,
        );

        // Append tool results to API history and loop
        currentMessages = [
            ...currentMessages,
            { role: 'user', content: toolResults },
        ];

        // Repeated-failure circuit breaker (issue #268). If a tool has
        // now failed with the same error MAX_REPEATED_TOOL_FAILURES
        // times, stop looping and return a clear error rather than
        // letting the LLM retry until the iteration limit is reached.
        if (tripped !== null) {
            return finishTurn(
                buildRepeatedToolFailureMessage(
                    tripped.toolName, tripped.errorText,
                ),
                collectedActivity,
                currentMessages,
                true,
            );
        }
    }

    // Loop exhausted iterations without a final text response
    return finishTurn(
        ITERATION_LIMIT_MESSAGE, collectedActivity, currentMessages, true,
    );
}
