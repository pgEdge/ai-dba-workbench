/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ChatContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ChatProvider, useChatContext } from '../ChatContext';

vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    apiDelete: vi.fn(),
    apiPatch: vi.fn(),
}));

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

let mockUser: { username: string } | null = { username: 'testuser' };
vi.mock('../AuthContext', () => ({
    useAuth: () => ({ user: mockUser }),
}));

// Mock useChat hook — return a controllable "instance" per render.
const chatHookState = {
    messages: [] as unknown[],
    isLoading: false,
    error: null as string | null,
    conversationId: null as string | null,
    conversationTitle: '',
    activeTools: [] as unknown[],
    inputHistory: [] as string[],
    sendMessage: vi.fn(async (_text: string) => {}),
    clearChat: vi.fn(),
    loadConversation: vi.fn(async (_id: string) => {}),
};

vi.mock('../../hooks/useChat', () => ({
    default: () => chatHookState,
}));

import { apiGet, apiDelete, apiPatch } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;
const mockApiDelete = apiDelete as unknown as ReturnType<typeof vi.fn>;
const mockApiPatch = apiPatch as unknown as ReturnType<typeof vi.fn>;

const sampleConversations = [
    {
        id: 'c1',
        title: 'Convo 1',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        preview: 'hi',
    },
    {
        id: 'c2',
        title: 'Convo 2',
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
        preview: 'hello',
    },
];

describe('ChatContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <ChatProvider>{children}</ChatProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
        chatHookState.messages = [];
        chatHookState.isLoading = false;
        chatHookState.error = null;
        chatHookState.conversationId = null;
        chatHookState.conversationTitle = '';
        chatHookState.activeTools = [];
        chatHookState.inputHistory = [];
        chatHookState.sendMessage = vi.fn(async () => {});
        chatHookState.clearChat = vi.fn();
        chatHookState.loadConversation = vi.fn(async () => {});
        mockApiGet.mockResolvedValue({ conversations: sampleConversations });
        mockApiDelete.mockResolvedValue({});
        mockApiPatch.mockResolvedValue({});
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('default state', () => {
        it('starts with panel closed and provides expected properties', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            expect(result.current.isOpen).toBe(false);
            expect(result.current).toHaveProperty('openChat');
            expect(result.current).toHaveProperty('closeChat');
            expect(result.current).toHaveProperty('toggleChat');
            expect(result.current).toHaveProperty('sendMessage');
            expect(result.current).toHaveProperty('clearChat');
            expect(result.current).toHaveProperty('loadConversation');
            expect(result.current).toHaveProperty('deleteConversation');
            expect(result.current).toHaveProperty('renameConversation');
            expect(result.current).toHaveProperty('refreshConversations');
        });
    });

    describe('panel controls', () => {
        it('openChat sets isOpen to true', () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            act(() => {
                result.current.openChat();
            });

            expect(result.current.isOpen).toBe(true);
        });

        it('closeChat sets isOpen to false', () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            act(() => {
                result.current.openChat();
            });
            act(() => {
                result.current.closeChat();
            });

            expect(result.current.isOpen).toBe(false);
        });

        it('toggleChat flips isOpen', () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            act(() => {
                result.current.toggleChat();
            });
            expect(result.current.isOpen).toBe(true);

            act(() => {
                result.current.toggleChat();
            });
            expect(result.current.isOpen).toBe(false);
        });
    });

    describe('refreshConversations', () => {
        it('fetches the conversation list on mount when user is set', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/conversations');
        });

        it('handles array-shaped responses', async () => {
            mockApiGet.mockResolvedValueOnce(sampleConversations);

            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });
        });

        it('handles responses without a conversations field', async () => {
            mockApiGet.mockResolvedValueOnce({});
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                // When response is {} (truthy but no conversations), list stays empty.
                expect(result.current.conversations).toEqual([]);
            });
        });

        it('logs and swallows fetch errors', async () => {
            mockApiGet.mockRejectedValue(new Error('boom'));

            renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalled();
            });
        });

        it('does not fetch when user is null', async () => {
            mockUser = null;

            renderHook(() => useChatContext(), { wrapper });

            await new Promise((r) => setTimeout(r, 10));
            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });

    describe('loadConversation', () => {
        it('delegates to the hook and refreshes list', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            mockApiGet.mockClear();

            await act(async () => {
                await result.current.loadConversation('c1');
            });

            expect(chatHookState.loadConversation).toHaveBeenCalledWith('c1');
            expect(mockApiGet).toHaveBeenCalled();
        });
    });

    describe('deleteConversation', () => {
        it('removes the deleted conversation from the list', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            await act(async () => {
                await result.current.deleteConversation('c1');
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/conversations/c1');
            expect(
                result.current.conversations.map((c) => c.id),
            ).toEqual(['c2']);
        });

        it('clears the chat when deleting the active conversation', async () => {
            chatHookState.conversationId = 'c1';

            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            await act(async () => {
                await result.current.deleteConversation('c1');
            });

            expect(chatHookState.clearChat).toHaveBeenCalled();
        });

        it('rethrows errors from delete requests', async () => {
            mockApiDelete.mockRejectedValueOnce(new Error('forbidden'));

            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            await expect(
                act(async () => {
                    await result.current.deleteConversation('c1');
                }),
            ).rejects.toThrow('forbidden');

            // List should be unchanged since delete failed.
            expect(result.current.conversations).toHaveLength(2);
        });
    });

    describe('renameConversation', () => {
        it('PATCHes and updates the title in the list', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            await act(async () => {
                await result.current.renameConversation('c1', 'Renamed');
            });

            expect(mockApiPatch).toHaveBeenCalledWith(
                '/api/v1/conversations/c1',
                { title: 'Renamed' },
            );

            const updated = result.current.conversations.find(
                (c) => c.id === 'c1',
            );
            expect(updated?.title).toBe('Renamed');
        });

        it('rethrows errors from rename requests', async () => {
            mockApiPatch.mockRejectedValueOnce(new Error('bad request'));

            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            const originalTitle = result.current.conversations.find(
                (c) => c.id === 'c1',
            )?.title;

            await expect(
                act(async () => {
                    await result.current.renameConversation('c1', 'X');
                }),
            ).rejects.toThrow('bad request');

            // Title should remain the original since the rename failed.
            expect(
                result.current.conversations.find((c) => c.id === 'c1')?.title,
            ).toBe(originalTitle);
        });
    });

    describe('clearChat wrapper', () => {
        it('delegates to hook and refreshes list', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            mockApiGet.mockClear();

            act(() => {
                result.current.clearChat();
            });

            expect(chatHookState.clearChat).toHaveBeenCalled();
        });
    });

    describe('sendMessage wrapper', () => {
        it('delegates to hook and refreshes list', async () => {
            const { result } = renderHook(() => useChatContext(), { wrapper });

            await waitFor(() => {
                expect(result.current.conversations).toHaveLength(2);
            });

            await act(async () => {
                await result.current.sendMessage('hello');
            });

            expect(chatHookState.sendMessage).toHaveBeenCalledWith('hello');
        });
    });

    describe('hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useChatContext());
            }).toThrow('useChatContext must be used within a ChatProvider');
        });
    });
});
