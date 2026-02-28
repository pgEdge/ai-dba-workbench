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
import { AnalysisTool } from './mcpTools';
import { getToolDisplayName } from './toolDisplayNames';
import { stripPreamble } from './textHelpers';
import {
    LLMContentBlock,
    LLMResponse,
    Message,
    ToolCallResponse,
    ToolResult,
} from '../types/llm';

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

        const response = await apiFetch('/api/v1/llm/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                messages,
                tools: tools.length > 0 ? tools : undefined,
                system: systemPrompt,
            }),
        });

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
            onActiveTools?.([]);
            return stripPreamble(textContent);
        }

        // Add assistant message with tool-use blocks
        messages.push({
            role: 'assistant',
            content: data.content as LLMContentBlock[],
        });

        // Update progress with tool names
        const toolNames = toolUses.map(
            t => getToolDisplayName(t.name || '') || 'unknown tool',
        );
        const uniqueNames = [...new Set(toolNames)];
        onActiveTools?.(uniqueNames);
        onProgress?.(
            uniqueNames.length === 1
                ? uniqueNames[0] + '...'
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
                        name: toolUse.name,
                        arguments: toolUse.input,
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
                    tool_use_id: toolUse.id ?? '',
                    content: resultText,
                });
            } catch (toolErr) {
                toolResults.push({
                    type: 'tool_result',
                    tool_use_id: toolUse.id ?? '',
                    content: `Tool execution error: ${(toolErr as Error).message}`,
                    is_error: true,
                });
            }
        }

        messages.push({ role: 'user', content: toolResults });
        onProgress?.('Analyzing results...');
    }

    throw new Error('Analysis exceeded maximum iterations');
}
