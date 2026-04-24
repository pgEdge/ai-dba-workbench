/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useRef, useEffect } from 'react';
import { apiFetch } from '../../utils/apiClient';
import { useAICapabilities } from '../../contexts/useAICapabilities';
import {
    ChatMessageData,
    ContentBlock,
} from '../../components/ChatPanel/ChatMessage';
import { ToolActivity } from '../../components/ChatPanel/ToolStatus';

import { APIMessage, ToolDefinition } from './chatTypes';
import { INPUT_HISTORY_MAX, SYSTEM_PROMPT, CHAT_TOOLS } from './chatConstants';
import {
    toAPIMessages,
    loadInputHistory,
    saveInputHistory,
} from './chatHelpers';
import { maybeCompact } from './chatCompaction';
import {
    createConversation,
    updateConversation,
    fetchConversation,
} from './chatConversation';
import { runAgenticLoop } from './chatAgenticLoop';
import { logger } from '../../utils/logger';

// Re-export types that consuming modules import from this hook.
// These aliases ensure backward compatibility with modules that
// imported types from useChat rather than from the component files.
export type ChatMessage = ChatMessageData;
export type { ContentBlock };
export type { ToolActivity };

// ---------------------------------------------------------------
// Return type
// ---------------------------------------------------------------

export interface UseChatReturn {
    messages: ChatMessageData[];
    activeTools: ToolActivity[];
    currentConversationId: string | null;
    inputHistory: string[];
    isLoading: boolean;
    error: string | null;

    sendMessage: (text: string) => Promise<void>;
    newChat: () => void;
    loadConversation: (id: string) => Promise<void>;

    // Backward-compatible aliases for consuming modules that use
    // the previous hook interface (ChatContext, ChatPanel).
    conversationId: string | null;
    conversationTitle: string;
    clearChat: () => void;
}

// ---------------------------------------------------------------
// Hook
// ---------------------------------------------------------------

/**
 * Core chat hook implementing the agentic LLM tool-use loop.
 *
 * Manages the conversation message history, tool execution state,
 * conversation persistence, token compaction, and input history
 * for arrow-key navigation.
 */
export function useChat(): UseChatReturn {
    const { maxIterations } = useAICapabilities();
    const [messages, setMessages] = useState<ChatMessageData[]>([]);
    const [activeTools, setActiveTools] = useState<ToolActivity[]>([]);
    const [currentConversationId, setCurrentConversationId] = useState<
        string | null
    >(null);
    const [inputHistory, setInputHistory] = useState<string[]>(
        loadInputHistory,
    );
    const [availableTools, setAvailableTools] =
        useState<ToolDefinition[]>(CHAT_TOOLS);
    const [isLoading, setIsLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [conversationTitle, setConversationTitle] = useState<string>(
        'New Chat',
    );

    // Internal refs for state that should not trigger re-renders.
    // The API message history is kept separate from visible messages
    // because compaction may replace it without affecting the UI.
    const apiMessagesRef = useRef<APIMessage[]>([]);
    const conversationIdRef = useRef<string | null>(null);
    const abortControllerRef = useRef<AbortController | null>(null);
    // Ref to track visible messages within the sendMessage closure
    // without depending on the `messages` state value.
    const visibleMessagesRef = useRef<ChatMessageData[]>([]);

    // Keep the visible messages ref in sync with React state.
    useEffect(() => {
        visibleMessagesRef.current = messages;
    }, [messages]);

    /**
     * Keep the conversation id ref in sync with React state so
     * that async closures always read the latest value.
     */
    const syncConversationId = useCallback((id: string | null) => {
        conversationIdRef.current = id;
        setCurrentConversationId(id);
    }, []);

    // Fetch available tools from the server on mount
    useEffect(() => {
        const fetchTools = async () => {
            try {
                const response = await apiFetch('/api/v1/mcp/tools');
                if (response.ok) {
                    const data = await response.json();
                    const tools = data.tools || [];
                    if (tools.length > 0) {
                        setAvailableTools(
                            tools.map(
                                (t: {
                                    name: string;
                                    description: string;
                                    inputSchema: Record<
                                        string,
                                        unknown
                                    >;
                                }) => ({
                                    name: t.name,
                                    description: t.description,
                                    inputSchema: t.inputSchema,
                                }),
                            ),
                        );
                    }
                }
            } catch {
                // Fall back to hardcoded CHAT_TOOLS (already the
                // initial state)
            }
        };
        fetchTools();
    }, []);

    // ---------------------------------------------------------------
    // Send message (agentic loop orchestrator)
    // ---------------------------------------------------------------

    const sendMessage = useCallback(
        async (text: string): Promise<void> => {
            if (!text.trim()) {
                return;
            }

            // Cancel any in-flight request
            if (abortControllerRef.current) {
                abortControllerRef.current.abort();
            }
            const abortController = new AbortController();
            abortControllerRef.current = abortController;

            setIsLoading(true);
            setError(null);
            setActiveTools([]);

            // Record input in history (most recent first, deduped)
            setInputHistory(prev => {
                const updated = [
                    text,
                    ...prev.filter(h => h !== text),
                ].slice(0, INPUT_HISTORY_MAX);
                saveInputHistory(updated);
                return updated;
            });

            const timestamp = new Date().toISOString();
            const userMessage: ChatMessageData = {
                role: 'user',
                content: text,
                timestamp,
            };

            // Append to visible messages using functional update to
            // avoid capturing stale `messages` from the closure.
            setMessages(prev => {
                const updated = [...prev, userMessage];
                visibleMessagesRef.current = updated;
                return updated;
            });

            // Append to API message history (wire format, no UI fields)
            const userAPIMessage: APIMessage = {
                role: 'user',
                content: text,
            };
            // Capture snapshot before modification for rollback on abort
            const apiMessagesAtTurnStart = [...apiMessagesRef.current];
            apiMessagesRef.current = [
                ...apiMessagesRef.current,
                userAPIMessage,
            ];

            try {
                // Run the agentic loop
                const { finalMessage, updatedApiMessages } =
                    await runAgenticLoop({
                        apiMessages: apiMessagesRef.current,
                        availableTools,
                        systemPrompt: SYSTEM_PROMPT,
                        maxIterations,
                        abortSignal: abortController.signal,
                        fetchFn: apiFetch,
                        onToolActivity: setActiveTools,
                    });

                apiMessagesRef.current = updatedApiMessages;

                // Append the assistant reply to visible messages using
                // functional update to handle cases where the prior state
                // update hasn't committed yet (e.g., in tests with sync mocks).
                setMessages(prev => {
                    const updated = [...prev, finalMessage];
                    visibleMessagesRef.current = updated;
                    return updated;
                });

                // --- Conversation persistence ---
                // Use the ref which was updated in the setMessages callback
                try {
                    if (!conversationIdRef.current) {
                        const newId = await createConversation(
                            visibleMessagesRef.current,
                            apiFetch,
                        );
                        if (newId) {
                            syncConversationId(newId);
                        }
                    } else {
                        await updateConversation(
                            conversationIdRef.current,
                            visibleMessagesRef.current,
                            apiFetch,
                        );
                    }
                } catch {
                    // Non-fatal: the user can still continue chatting
                }

                // --- Compaction ---
                try {
                    apiMessagesRef.current = await maybeCompact(
                        apiMessagesRef.current,
                        apiFetch,
                    );
                } catch {
                    // Compaction failures are non-fatal
                }
            } catch (err) {
                if ((err as Error).name === 'AbortError') {
                    // Request was intentionally cancelled; rollback API history
                    apiMessagesRef.current = apiMessagesAtTurnStart;
                    return;
                }

                const errMessage =
                    (err as Error).message ||
                    'An unexpected error occurred';
                logger.error('Chat error:', err);
                setError(errMessage);

                // Add an error message to the visible conversation
                const errorAssistantMessage: ChatMessageData = {
                    role: 'assistant',
                    content: `Sorry, an error occurred: ${errMessage}`,
                    timestamp: new Date().toISOString(),
                    isError: true,
                };
                setMessages(prev => [...prev, errorAssistantMessage]);
            } finally {
                setIsLoading(false);
                setActiveTools([]);
                if (abortControllerRef.current === abortController) {
                    abortControllerRef.current = null;
                }
            }
        },
        [availableTools, maxIterations, syncConversationId],
    );

    // ---------------------------------------------------------------
    // New chat
    // ---------------------------------------------------------------

    const newChat = useCallback((): void => {
        // Cancel any in-flight request
        if (abortControllerRef.current) {
            abortControllerRef.current.abort();
            abortControllerRef.current = null;
        }

        setMessages([]);
        setActiveTools([]);
        setError(null);
        setIsLoading(false);
        setConversationTitle('New Chat');
        syncConversationId(null);
        apiMessagesRef.current = [];
        visibleMessagesRef.current = [];
        // Intentionally preserve inputHistory across conversations
    }, [syncConversationId]);

    // ---------------------------------------------------------------
    // Load existing conversation
    // ---------------------------------------------------------------

    const loadConversation = useCallback(
        async (id: string): Promise<void> => {
            // Cancel any in-flight send operation
            if (abortControllerRef.current) {
                abortControllerRef.current.abort();
                abortControllerRef.current = null;
            }

            setIsLoading(true);
            setError(null);
            setActiveTools([]);

            try {
                const data = await fetchConversation(id, apiFetch);

                setMessages(data.messages || []);
                visibleMessagesRef.current = data.messages || [];
                setConversationTitle(data.title || 'Conversation');
                syncConversationId(id);

                // Rebuild the internal API message array from the
                // loaded conversation so the agentic loop can
                // continue naturally.
                apiMessagesRef.current = toAPIMessages(
                    data.messages || [],
                );
            } catch (err) {
                const errMessage =
                    (err as Error).message ||
                    'Failed to load conversation';
                logger.error('Failed to load conversation:', err);
                setError(errMessage);
            } finally {
                setIsLoading(false);
            }
        },
        [syncConversationId],
    );

    // ---------------------------------------------------------------
    // Return value
    // ---------------------------------------------------------------

    return {
        messages,
        activeTools,
        currentConversationId,
        inputHistory,
        isLoading,
        error,

        sendMessage,
        newChat,
        loadConversation,

        // Backward-compatible aliases
        conversationId: currentConversationId,
        conversationTitle,
        clearChat: newChat,
    };
}

export default useChat;
