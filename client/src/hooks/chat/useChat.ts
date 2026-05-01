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
import type {
    ChatMessageData,
    ContentBlock,
} from '../../components/ChatPanel/ChatMessage';
import type { ToolActivity } from '../../components/ChatPanel/ToolStatus';

import type { APIMessage, ToolDefinition } from './chatTypes';
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
import {
    NO_MCP_PRIVILEGES_MESSAGE,
    runAgenticLoop,
} from './chatAgenticLoop';
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
    // Ref mirroring the latest `availableTools` value. Reading the
    // tool list through a ref inside sendMessage eliminates the
    // closure-staleness race where a fast user could click send
    // between the tools fetch starting and the resulting setState
    // committing — without the ref, sendMessage would observe the
    // hardcoded CHAT_TOOLS fallback and skip the empty-list short
    // circuit even when the user has zero MCP privileges (issue
    // #188).
    const availableToolsRef = useRef<ToolDefinition[]>(CHAT_TOOLS);
    // Ref to the in-flight tools fetch promise. sendMessage awaits
    // this before reading availableToolsRef so it always observes
    // the authoritative tool list rather than the initial fallback.
    const toolsFetchRef = useRef<Promise<void> | null>(null);

    // Keep the visible messages ref in sync with React state.
    useEffect(() => {
        visibleMessagesRef.current = messages;
    }, [messages]);

    // Keep the tools ref in sync with React state so async closures
    // (sendMessage in particular) always read the latest list.
    useEffect(() => {
        availableToolsRef.current = availableTools;
    }, [availableTools]);

    /**
     * Keep the conversation id ref in sync with React state so
     * that async closures always read the latest value.
     */
    const syncConversationId = useCallback((id: string | null) => {
        conversationIdRef.current = id;
        setCurrentConversationId(id);
    }, []);

    // Fetch available tools from the server on mount.
    //
    // The server returns the RBAC-filtered tool list for the current
    // user. Trust that list verbatim, including empty arrays: a user
    // with zero MCP privileges legitimately has no tools available,
    // and treating an empty list as "fall back to the hardcoded
    // defaults" causes the agentic loop to send tools the user is not
    // allowed to call, triggering the infinite "Access denied" loop
    // described in issue #188. We only fall back to CHAT_TOOLS when
    // the fetch itself fails (network error / non-OK response).
    useEffect(() => {
        const fetchTools = async (): Promise<void> => {
            try {
                const response = await apiFetch('/api/v1/mcp/tools');
                if (response.ok) {
                    const data = await response.json();
                    const tools = data.tools || [];
                    const mapped: ToolDefinition[] = tools.map(
                        (t: {
                            name: string;
                            description: string;
                            inputSchema: Record<string, unknown>;
                        }) => ({
                            name: t.name,
                            description: t.description,
                            inputSchema: t.inputSchema,
                        }),
                    );
                    // Update the ref synchronously alongside the
                    // state update so that any sendMessage call
                    // awaiting toolsFetchRef sees the resolved list
                    // immediately on its next read, regardless of
                    // whether React has flushed the state update.
                    availableToolsRef.current = mapped;
                    setAvailableTools(mapped);
                }
            } catch {
                // Fall back to hardcoded CHAT_TOOLS (already the
                // initial state). This preserves chat functionality
                // when the tools endpoint is transiently unreachable.
            }
        };
        // Track the in-flight fetch so sendMessage can await it
        // and avoid the closure-staleness race described above.
        toolsFetchRef.current = fetchTools();
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

            // Wait for the initial tools fetch to settle before
            // reading the tool list. Without this, a fast user (or a
            // synchronous test runner) can invoke sendMessage between
            // the fetch starting and setAvailableTools committing,
            // causing the closure to observe the hardcoded CHAT_TOOLS
            // fallback and bypass the empty-list short circuit even
            // when the server reported zero tools — see issue #188.
            if (toolsFetchRef.current) {
                try {
                    await toolsFetchRef.current;
                } catch {
                    // Fetch errors are already handled inside the
                    // useEffect; the ref still holds the resolved
                    // (or fallback) tool list.
                }
            }

            // Read the authoritative tool list from the ref. The ref
            // is updated synchronously alongside setAvailableTools so
            // it reflects the latest list immediately, even if React
            // has not yet flushed the state update.
            const currentTools = availableToolsRef.current;

            // Short-circuit when the user has no MCP tool privileges.
            // Without this guard the agentic loop would invoke the LLM
            // with an empty tool list (or, worse, with the hardcoded
            // fallback tools the user is forbidden from calling), and
            // every proposed call would fail with "Access denied". The
            // loop would then spin until `maxIterations` is exhausted
            // — see issue #188. Render a clear permission message and
            // skip the loop entirely.
            if (currentTools.length === 0) {
                const deniedMessage: ChatMessageData = {
                    role: 'assistant',
                    content: NO_MCP_PRIVILEGES_MESSAGE,
                    timestamp: new Date().toISOString(),
                    isError: true,
                };
                setMessages(prev => {
                    const updated = [...prev, deniedMessage];
                    visibleMessagesRef.current = updated;
                    return updated;
                });
                apiMessagesRef.current = [
                    ...apiMessagesRef.current,
                    {
                        role: 'assistant',
                        content: NO_MCP_PRIVILEGES_MESSAGE,
                    },
                ];
                setIsLoading(false);
                setActiveTools([]);
                if (abortControllerRef.current === abortController) {
                    abortControllerRef.current = null;
                }
                return;
            }

            try {
                // Run the agentic loop with the resolved tool list.
                const { finalMessage, updatedApiMessages } =
                    await runAgenticLoop({
                        apiMessages: apiMessagesRef.current,
                        availableTools: currentTools,
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
        // The tool list is read through availableToolsRef rather than
        // captured via closure, so availableTools is intentionally not
        // a dependency here.
        [maxIterations, syncConversationId],
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
