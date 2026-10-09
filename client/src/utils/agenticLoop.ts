/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { apiFetch } from './apiClient';
import type { AnalysisTool } from './mcpTools';
import { getToolDisplayName } from './toolDisplayNames';
import { stripPreamble } from './textHelpers';
import type {
    LLMContentBlock,
    LLMResponse,
    Message,
    ToolCallResponse,
    ToolResult,
} from '../types/llm';
import { LLM_CHAT_PATH, buildChatRequestInit } from './llmChat';
import { extractSqlCodeBlocks } from './sqlValidation';
import type { SqlBlockValidator } from './sqlValidation';

export interface AgenticLoopOptions {
    /** Initial messages (typically a single user message). */
    messages: Message[];
    /** Tool definitions available to the LLM. */
    tools: AnalysisTool[];
    /** System prompt sent with every LLM call. */
    systemPrompt: string;
    /** Maximum number of LLM round-trips before aborting. */
    maxIterations: number;
    /** Called when the set of active tools changes. */
    onActiveTools?: (toolNames: string[]) => void;
    /** Called with a human-readable progress message. */
    onProgress?: (message: string) => void;
    /**
     * Optional validator used for a single SQL self-repair round once
     * the loop has produced its final text. Omitting it, or having it
     * resolve to null, leaves the text untouched.
     */
    validateSqlBlocks?: SqlBlockValidator;
}

/**
 * Prompt used for the single SQL self-repair round.
 */
export const SQL_REPAIR_INSTRUCTION =
    'Some of the SQL in your report failed validation against the target '
    + 'database. PostgreSQL reported the errors below when planning it '
    + 'with EXPLAIN. Correct every failing statement, verifying object '
    + 'and column names with get_schema_info where one is needed, and '
    + 'reply with the COMPLETE corrected report in the same format. Do '
    + 'not comment on the corrections or apologise; return the report '
    + 'only.';

/**
 * Validate the SQL blocks in `text` and, when any fail, ask the model
 * once for a corrected report.
 *
 * Exactly one extra LLM round-trip is made, deliberately outside the
 * `maxIterations` budget: that budget bounds the tool-calling loop, and
 * a repair round that could be starved by it would leave the user with
 * the broken SQL the feature exists to prevent. If the model answers
 * with tool calls instead of text, or with nothing at all, the original
 * text is returned unchanged.
 */
async function repairInvalidSql(
    text: string,
    options: AgenticLoopOptions,
): Promise<string> {
    const { validateSqlBlocks, messages, tools, systemPrompt, onProgress } =
        options;

    if (!validateSqlBlocks || !text.trim()) {
        return text;
    }

    const blocks = extractSqlCodeBlocks(text);
    if (blocks.length === 0) {
        return text;
    }

    const results = await Promise.all(
        blocks.map(block =>
            Promise.resolve()
                .then(() => validateSqlBlocks(block))
                .catch(() => null),
        ),
    );

    const failures: string[] = [];
    results.forEach((result, index) => {
        if (!result) {
            return;
        }
        const errors = result.statements
            .filter(statement => statement.status === 'invalid')
            .map(statement =>
                `  ${statement.query}\n    ERROR: ${statement.error}`);
        if (errors.length > 0) {
            failures.push(
                `Block ${index + 1}:\n${errors.join('\n')}`,
            );
        }
    });

    if (failures.length === 0) {
        return text;
    }

    onProgress?.('Correcting SQL...');

    const repairMessages: Message[] = [
        ...messages,
        { role: 'assistant', content: text },
        {
            role: 'user',
            content: `${SQL_REPAIR_INSTRUCTION}\n\n${failures.join('\n\n')}`,
        },
    ];

    try {
        const response = await apiFetch(
            LLM_CHAT_PATH,
            buildChatRequestInit({
                messages: repairMessages,
                tools,
                systemPrompt,
            }),
        );
        if (!response.ok) {
            return text;
        }

        const data: LLMResponse = await response.json();
        const usedTools = data.content?.some(c => c.type === 'tool_use');
        if (usedTools) {
            return text;
        }

        const repaired = data.content
            ?.filter(c => c.type === 'text')
            .map(c => c.text)
            .join('\n') || '';

        return repaired.trim() ? stripPreamble(repaired) : text;
    } catch {
        // A failing repair round must never lose the analysis.
        return text;
    }
}

/**
 * Run an agentic tool loop: repeatedly call the LLM, execute any
 * requested tools, feed results back, and repeat until the LLM
 * returns a final text response or the iteration limit is reached.
 *
 * Returns the final text response with any conversational preamble
 * stripped.
 */
export async function runAgenticLoop(
    options: AgenticLoopOptions,
): Promise<string> {
    const {
        messages,
        tools,
        systemPrompt,
        maxIterations,
        onActiveTools,
        onProgress,
    } = options;

    let iterations = 0;

    while (iterations < maxIterations) {
        iterations++;

        const response = await apiFetch(
            LLM_CHAT_PATH,
            buildChatRequestInit({ messages, tools, systemPrompt }),
        );

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Analysis request failed: ${errorText}`);
        }

        const data: LLMResponse = await response.json();
        const toolUses = data.content?.filter(
            c => c.type === 'tool_use',
        ) || [];

        if (toolUses.length === 0) {
            // Final text response
            const textContent = data.content
                ?.filter(c => c.type === 'text')
                .map(c => c.text)
                .join('\n') || '';
            const finalText = stripPreamble(textContent);
            const repaired = await repairInvalidSql(finalText, options);
            onActiveTools?.([]);
            return repaired;
        }

        // Add assistant message with tool-use blocks
        messages.push({
            role: 'assistant',
            content: data.content as LLMContentBlock[],
        });

        // Update progress with tool names
        const toolNames = toolUses.map(
            t => getToolDisplayName(t.tool_use?.name || '') || 'unknown tool',
        );
        const uniqueNames = [...new Set(toolNames)];
        onActiveTools?.(uniqueNames);
        onProgress?.(
            uniqueNames.length === 1
                ? `${uniqueNames[0]}...`
                : `Running ${uniqueNames.length} tools...`,
        );

        // Execute each tool call
        const toolResults: ToolResult[] = [];
        for (const toolUse of toolUses) {
            try {
                const toolResponse = await apiFetch('/api/v1/mcp/tools/call', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        name: toolUse.tool_use?.name,
                        arguments: toolUse.tool_use?.input,
                    }),
                });

                const toolData: ToolCallResponse =
                    await toolResponse.json();
                const resultText =
                    toolData.content?.[0]?.text ||
                    (toolData.isError
                        ? `Error: ${toolData.content?.[0]?.text}`
                        : 'No data returned');

                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.tool_use?.id ?? '',
                    text: resultText,
                });
            } catch (toolErr) {
                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.tool_use?.id ?? '',
                    text: `Tool execution error: ${(toolErr as Error).message}`,
                    is_error: true,
                });
            }
        }

        messages.push({ role: 'user', content: toolResults });
        onProgress?.('Analyzing results...');
    }

    throw new Error('Analysis exceeded maximum iterations');
}
