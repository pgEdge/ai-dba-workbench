/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { LLM_CHAT_PATH, buildChatRequestInit } from '../llmChat';

const parseBody = (init: RequestInit) =>
    JSON.parse(init.body as string) as Record<string, unknown>;

describe('LLM_CHAT_PATH', () => {
    it('points at the chat endpoint', () => {
        expect(LLM_CHAT_PATH).toBe('/api/v1/llm/chat');
    });
});

describe('buildChatRequestInit', () => {
    it('sets the method and JSON content type', () => {
        const init = buildChatRequestInit({
            messages: [],
            systemPrompt: 'sys',
        });

        expect(init.method).toBe('POST');
        expect(init.headers).toEqual({ 'Content-Type': 'application/json' });
    });

    it('wraps string message content in a text block', () => {
        const init = buildChatRequestInit({
            messages: [{ role: 'user', content: 'Hello' }],
            systemPrompt: 'sys',
        });

        expect(parseBody(init).messages).toEqual([
            { role: 'user', content: [{ type: 'text', text: 'Hello' }] },
        ]);
    });

    it('passes block-array message content through unchanged', () => {
        const blocks = [
            { type: 'text', text: 'Already blocks' },
            {
                type: 'tool_result',
                tool_use_id: 't1',
                text: 'ok',
            },
        ];
        const init = buildChatRequestInit({
            messages: [{ role: 'assistant', content: blocks }],
            systemPrompt: 'sys',
        });

        expect(parseBody(init).messages).toEqual([
            { role: 'assistant', content: blocks },
        ]);
    });

    it('renames camelCase inputSchema to snake_case input_schema (issue #370)', () => {
        const schema = {
            type: 'object',
            properties: { id: { type: 'string', description: 'An id' } },
            required: ['id'],
        };
        const init = buildChatRequestInit({
            messages: [{ role: 'user', content: 'Hi' }],
            tools: [{ name: 'lookup', description: 'Look up', inputSchema: schema }],
            systemPrompt: 'sys',
        });

        const body = parseBody(init);
        expect(body.tools).toEqual([
            { name: 'lookup', description: 'Look up', input_schema: schema },
        ]);
        const [tool] = body.tools as Array<Record<string, unknown>>;
        expect(tool.inputSchema).toBeUndefined();
    });

    it('omits the tools key when tools is undefined', () => {
        const init = buildChatRequestInit({
            messages: [{ role: 'user', content: 'Hi' }],
            systemPrompt: 'sys',
        });

        expect('tools' in parseBody(init)).toBe(false);
    });

    it('omits the tools key when tools is empty', () => {
        const init = buildChatRequestInit({
            messages: [{ role: 'user', content: 'Hi' }],
            tools: [],
            systemPrompt: 'sys',
        });

        expect('tools' in parseBody(init)).toBe(false);
    });

    it('sends the system prompt as system_prompt', () => {
        const init = buildChatRequestInit({
            messages: [],
            systemPrompt: 'Be helpful',
        });

        const body = parseBody(init);
        expect(body.system_prompt).toBe('Be helpful');
        expect(body.system).toBeUndefined();
        expect(body.systemPrompt).toBeUndefined();
    });

    it('forwards the abort signal when supplied', () => {
        const controller = new AbortController();
        const init = buildChatRequestInit({
            messages: [],
            systemPrompt: 'sys',
            signal: controller.signal,
        });

        expect(init.signal).toBe(controller.signal);
    });

    it('leaves the signal key absent when no signal is supplied', () => {
        const init = buildChatRequestInit({
            messages: [],
            systemPrompt: 'sys',
        });

        expect('signal' in init).toBe(false);
        expect(init).toEqual({
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messages: [], system_prompt: 'sys' }),
        });
    });
});
