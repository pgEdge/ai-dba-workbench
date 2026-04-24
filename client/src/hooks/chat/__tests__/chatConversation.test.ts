/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
    createConversation,
    updateConversation,
    fetchConversation,
    type FetchFunction,
} from '../chatConversation';
import type { ChatMessageData } from '../../../components/ChatPanel/ChatMessage';
import type { ConversationDetail } from '../chatTypes';

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

/**
 * Create a mock fetch function that returns the specified response.
 */
function createMockFetch(
    ok: boolean,
    data?: object,
    text?: string,
): FetchFunction {
    return vi.fn().mockResolvedValue({
        ok,
        json: vi.fn().mockResolvedValue(data ?? {}),
        text: vi.fn().mockResolvedValue(text ?? ''),
    });
}

/**
 * Create sample chat messages for testing.
 */
function createSampleMessages(): ChatMessageData[] {
    return [
        {
            role: 'user',
            content: 'Hello',
            timestamp: '2024-01-01T00:00:00Z',
        },
        {
            role: 'assistant',
            content: 'Hi there!',
            timestamp: '2024-01-01T00:00:01Z',
        },
    ];
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('chatConversation', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('createConversation', () => {
        it('returns conversation ID on successful creation', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true, {
                id: 'conv-123',
                title: 'New Conversation',
            });

            const result = await createConversation(messages, mockFetch);

            expect(result).toBe('conv-123');
        });

        it('calls correct endpoint with POST method', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true, { id: 'conv-123' });

            await createConversation(messages, mockFetch);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/conversations',
                expect.objectContaining({
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                }),
            );
        });

        it('sends correct request body', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true, { id: 'conv-123' });

            await createConversation(messages, mockFetch);

            const call = mockFetch.mock.calls[0];
            const body = JSON.parse(call[1]?.body as string);

            expect(body).toEqual({
                messages,
                provider: '',
                model: '',
            });
        });

        it('returns null when response is not ok', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                status: 400,
                text: vi.fn().mockResolvedValue('Bad Request'),
            });
            const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

            const result = await createConversation(messages, mockFetch);

            expect(result).toBeNull();
            expect(consoleSpy).toHaveBeenCalledWith(
                'Failed to create conversation:',
                400,
                'Bad Request',
            );
        });

        it('returns null when fetch throws error', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'));
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const result = await createConversation(messages, mockFetch);

            expect(result).toBeNull();
            expect(consoleSpy).toHaveBeenCalledWith(
                'Failed to create conversation:',
                expect.any(Error),
            );
        });

        it('returns null when JSON parsing fails', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: vi.fn().mockRejectedValue(new Error('Invalid JSON')),
            });
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const result = await createConversation(messages, mockFetch);

            expect(result).toBeNull();
            expect(consoleSpy).toHaveBeenCalled();
        });

        it('handles empty messages array', async () => {
            const mockFetch = createMockFetch(true, { id: 'conv-empty' });

            const result = await createConversation([], mockFetch);

            expect(result).toBe('conv-empty');
            const call = mockFetch.mock.calls[0];
            const body = JSON.parse(call[1]?.body as string);
            expect(body.messages).toEqual([]);
        });

        it('handles 500 server error', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                status: 500,
                text: vi.fn().mockResolvedValue('Internal Server Error'),
            });
            const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

            const result = await createConversation(messages, mockFetch);

            expect(result).toBeNull();
            expect(consoleSpy).toHaveBeenCalled();
        });
    });

    describe('updateConversation', () => {
        it('returns true on successful update', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true);

            const result = await updateConversation('conv-123', messages, mockFetch);

            expect(result).toBe(true);
        });

        it('calls correct endpoint with PUT method', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true);

            await updateConversation('conv-456', messages, mockFetch);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/conversations/conv-456',
                expect.objectContaining({
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                }),
            );
        });

        it('sends correct request body with messages only', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true);

            await updateConversation('conv-123', messages, mockFetch);

            const call = mockFetch.mock.calls[0];
            const body = JSON.parse(call[1]?.body as string);

            expect(body).toEqual({ messages });
        });

        it('returns false when response is not ok', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                status: 404,
                text: vi.fn().mockResolvedValue('Not Found'),
            });
            const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

            const result = await updateConversation('conv-123', messages, mockFetch);

            expect(result).toBe(false);
            expect(consoleSpy).toHaveBeenCalledWith(
                'Failed to update conversation:',
                404,
                'Not Found',
            );
        });

        it('returns false when fetch throws error', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'));
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

            const result = await updateConversation('conv-123', messages, mockFetch);

            expect(result).toBe(false);
            expect(consoleSpy).toHaveBeenCalledWith(
                'Failed to update conversation:',
                expect.any(Error),
            );
        });

        it('handles empty messages array', async () => {
            const mockFetch = createMockFetch(true);

            const result = await updateConversation('conv-123', [], mockFetch);

            expect(result).toBe(true);
            const call = mockFetch.mock.calls[0];
            const body = JSON.parse(call[1]?.body as string);
            expect(body.messages).toEqual([]);
        });

        it('handles 404 not found error', async () => {
            const messages = createSampleMessages();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                status: 404,
                text: vi.fn().mockResolvedValue('Conversation not found'),
            });
            const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

            const result = await updateConversation('nonexistent', messages, mockFetch);

            expect(result).toBe(false);
            expect(consoleSpy).toHaveBeenCalled();
        });

        it('uses provided conversation ID in URL', async () => {
            const messages = createSampleMessages();
            const mockFetch = createMockFetch(true);

            await updateConversation('special-id-789', messages, mockFetch);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/conversations/special-id-789',
                expect.any(Object),
            );
        });
    });

    describe('fetchConversation', () => {
        it('returns conversation detail on success', async () => {
            const expectedData: ConversationDetail = {
                id: 'conv-123',
                title: 'Test Conversation',
                messages: createSampleMessages(),
            };
            const mockFetch = createMockFetch(true, expectedData);

            const result = await fetchConversation('conv-123', mockFetch);

            expect(result).toEqual(expectedData);
        });

        it('calls correct endpoint with GET method (default)', async () => {
            const mockFetch = createMockFetch(true, {
                id: 'conv-123',
                title: 'Test',
                messages: [],
            });

            await fetchConversation('conv-456', mockFetch);

            expect(mockFetch).toHaveBeenCalledWith('/api/v1/conversations/conv-456');
        });

        it('throws error when response is not ok', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                text: vi.fn().mockResolvedValue('Conversation not found'),
            });

            await expect(
                fetchConversation('conv-123', mockFetch),
            ).rejects.toThrow('Failed to load conversation: Conversation not found');
        });

        it('throws error when fetch fails', async () => {
            const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'));

            await expect(
                fetchConversation('conv-123', mockFetch),
            ).rejects.toThrow('Network error');
        });

        it('throws error when JSON parsing fails', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: vi.fn().mockRejectedValue(new Error('Invalid JSON')),
            });

            await expect(
                fetchConversation('conv-123', mockFetch),
            ).rejects.toThrow('Invalid JSON');
        });

        it('returns conversation with empty messages', async () => {
            const expectedData: ConversationDetail = {
                id: 'conv-empty',
                title: 'Empty Chat',
                messages: [],
            };
            const mockFetch = createMockFetch(true, expectedData);

            const result = await fetchConversation('conv-empty', mockFetch);

            expect(result.messages).toEqual([]);
        });

        it('uses provided conversation ID in URL', async () => {
            const mockFetch = createMockFetch(true, {
                id: 'special-id',
                title: 'Test',
                messages: [],
            });

            await fetchConversation('special-id-999', mockFetch);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/conversations/special-id-999',
            );
        });

        it('handles 500 server error with proper error message', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                status: 500,
                text: vi.fn().mockResolvedValue('Internal Server Error'),
            });

            await expect(
                fetchConversation('conv-123', mockFetch),
            ).rejects.toThrow('Failed to load conversation: Internal Server Error');
        });

        it('handles empty error text', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                text: vi.fn().mockResolvedValue(''),
            });

            await expect(
                fetchConversation('conv-123', mockFetch),
            ).rejects.toThrow('Failed to load conversation: ');
        });
    });
});
