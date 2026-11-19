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
    linkConversationActions,
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
    // Actions proposed before this chat had a saved conversation, kept
    // until a link to the conversation succeeds.
    const pendingActionIdsRef = useRef<Set<number>>(new Set());
    // Bumped whenever the chat is replaced, so a turn still running in
    // the old chat cannot add its actions to the new one.
    const chatGenerationRef = useRef(0);

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

    /** Append a message to the visible conversation. */
    const appendVisible = useCallback((message: ChatMessageData): void => {
        // Functional update, so a reply appended before an earlier update
        // has committed (for example with synchronous test mocks) is not
        // lost; the ref lets async closures read the result straight away.
        setMessages(prev => {
            const updated = [...prev, message];
            visibleMessagesRef.current = updated;
            return updated;
        });
    }, []);

    /** Record input in history (most recent first, deduped). */
    const recordInput = useCallback((text: string): void => {
        setInputHistory(prev => {
            const updated = [
                text,
                ...prev.filter(h => h !== text),
            ].slice(0, INPUT_HISTORY_MAX);
            saveInputHistory(updated);
            return updated;
        });
    }, []);

    /**
     * Link the actions proposed before this chat had a saved
     * conversation to `conversationId`. They stay pending if the link
     * fails, so the next save tries again; the server only links actions
     * that have no conversation yet, so sending one twice is harmless.
     */
    const linkPendingActions = useCallback(
        async (conversationId: string): Promise<void> => {
            const ids = [...pendingActionIdsRef.current];
            if (ids.length === 0) {
                return;
            }
            const generation = chatGenerationRef.current;
            const linked = await linkConversationActions(
                conversationId,
                ids,
                apiFetch,
            );
            if (linked && chatGenerationRef.current === generation) {
                ids.forEach(id => pendingActionIdsRef.current.delete(id));
            }
        },
        [],
    );

    /** Save the visible conversation, creating it on the first turn. */
    const persistConversation = useCallback(async (): Promise<void> => {
        try {
            let id = conversationIdRef.current;
            if (id) {
                await updateConversation(
                    id,
                    visibleMessagesRef.current,
                    apiFetch,
                );
            } else {
                id = await createConversation(
                    visibleMessagesRef.current,
                    apiFetch,
                );
                if (!id) {
                    return;
                }
                syncConversationId(id);
            }
            await linkPendingActions(id);
        } catch {
            // Non-fatal: the user can still continue chatting
        }
    }, [syncConversationId, linkPendingActions]);

    /** Compact the API history if it has grown too large. */
    const compactHistory = useCallback(async (): Promise<void> => {
        try {
            apiMessagesRef.current = await maybeCompact(
                apiMessagesRef.current,
                apiFetch,
            );
        } catch {
            // Compaction failures are non-fatal
        }
    }, []);

    /**
     * Wait for the initial tools fetch to settle, then return the
     * authoritative tool list. Without the wait, a fast user (or a
     * synchronous test runner) can invoke sendMessage between the fetch
     * starting and setAvailableTools committing, causing the closure to
     * observe the hardcoded CHAT_TOOLS fallback and bypass the empty-list
     * short circuit even when the server reported zero tools; see issue
     * #188. The ref is updated synchronously alongside setAvailableTools
     * so it reflects the latest list immediately.
     */
    const resolveTools = useCallback(async (): Promise<ToolDefinition[]> => {
        if (toolsFetchRef.current) {
            try {
                await toolsFetchRef.current;
            } catch {
                // Fetch errors are already handled inside the
                // useEffect; the ref still holds the resolved
                // (or fallback) tool list.
            }
        }
        return availableToolsRef.current;
    }, []);

    /**
     * Reply that the user has no MCP tool privileges. Without this the
     * agentic loop would invoke the LLM with an empty tool list (or,
     * worse, with the hardcoded fallback tools the user is forbidden
     * from calling), and every proposed call would fail with "Access
     * denied" until `maxIterations` is exhausted; see issue #188.
     */
    const replyNoPrivileges = useCallback((): void => {
        appendVisible({
            role: 'assistant',
            content: NO_MCP_PRIVILEGES_MESSAGE,
            timestamp: new Date().toISOString(),
            isError: true,
        });
        apiMessagesRef.current = [
            ...apiMessagesRef.current,
            { role: 'assistant', content: NO_MCP_PRIVILEGES_MESSAGE },
        ];
    }, [appendVisible]);

    /** Show a failed turn's error in the conversation. */
    const reportTurnError = useCallback((err: unknown): void => {
        const errMessage =
            (err as Error).message || 'An unexpected error occurred';
        logger.error('Chat error:', err);
        setError(errMessage);
        appendVisible({
            role: 'assistant',
            content: `Sorry, an error occurred: ${errMessage}`,
            timestamp: new Date().toISOString(),
            isError: true,
        });
    }, [appendVisible]);

    /**
     * Build the callback the agentic loop calls for each action it
     * proposes. An action proposed before the chat has a saved
     * conversation is kept pending until one exists, even if the turn
     * then fails or is aborted. A later chat ignores reports from a turn
     * started in an earlier one.
     */
    const pendingActionRecorder = useCallback(
        (turnConversationId: string | null) => {
            const generation = chatGenerationRef.current;
            return (actionId: number): void => {
                if (
                    !turnConversationId
                    && chatGenerationRef.current === generation
                ) {
                    pendingActionIdsRef.current.add(actionId);
                }
            };
        },
        [],
    );

    const sendMessage = useCallback(
        async (text: string): Promise<void> => {
            if (!text.trim()) {
                return;
            }

            // Cancel any in-flight request
            abortControllerRef.current?.abort();
            const abortController = new AbortController();
            abortControllerRef.current = abortController;

            setIsLoading(true);
            setError(null);
            setActiveTools([]);
            recordInput(text);
            appendVisible({
                role: 'user',
                content: text,
                timestamp: new Date().toISOString(),
            });

            // Capture a snapshot before modification for rollback on
            // abort, then append to the API history (wire format, no UI
            // fields).
            const apiMessagesAtTurnStart = [...apiMessagesRef.current];
            apiMessagesRef.current = [
                ...apiMessagesRef.current,
                { role: 'user', content: text },
            ];

            const currentTools = await resolveTools();
            const turnConversationId = conversationIdRef.current;

            try {
                if (currentTools.length === 0) {
                    replyNoPrivileges();
                    return;
                }

                const { finalMessage, updatedApiMessages } =
                    await runAgenticLoop({
                        apiMessages: apiMessagesRef.current,
                        availableTools: currentTools,
                        systemPrompt: SYSTEM_PROMPT,
                        maxIterations,
                        abortSignal: abortController.signal,
                        fetchFn: apiFetch,
                        onToolActivity: setActiveTools,
                        conversationId: turnConversationId,
                        onActionProposed:
                            pendingActionRecorder(turnConversationId),
                    });

                apiMessagesRef.current = updatedApiMessages;
                appendVisible(finalMessage);
                await persistConversation();
                await compactHistory();
            } catch (err) {
                if ((err as Error).name === 'AbortError') {
                    // Request was intentionally cancelled; rollback API history
                    apiMessagesRef.current = apiMessagesAtTurnStart;
                    return;
                }
                reportTurnError(err);
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
        [
            maxIterations,
            recordInput,
            appendVisible,
            resolveTools,
            replyNoPrivileges,
            pendingActionRecorder,
            persistConversation,
            compactHistory,
            reportTurnError,
        ],
    );

    // ---------------------------------------------------------------
    // New chat
    // ---------------------------------------------------------------

    /**
     * Forget the current chat's unlinked actions, and ignore any still
     * reported by a turn started in it.
     */
    const resetPendingActions = useCallback((): void => {
        chatGenerationRef.current += 1;
        pendingActionIdsRef.current = new Set();
    }, []);

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
        resetPendingActions();
        apiMessagesRef.current = [];
        visibleMessagesRef.current = [];
        // Intentionally preserve inputHistory across conversations
    }, [syncConversationId, resetPendingActions]);

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
            resetPendingActions();

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
        [syncConversationId, resetPendingActions],
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
