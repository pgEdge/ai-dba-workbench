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
import { useAICapabilities } from '../../contexts/AICapabilitiesContext';
import {
    ChatMessageData,
    ContentBlock,
} from '../../components/ChatPanel/ChatMessage';
import { ToolActivity } from '../../components/ChatPanel/ToolStatus';
import {
    LLMContentBlock,
    LLMResponse,
    ToolCallResponse,
    ToolResult,
} from '../../types/llm';

import {
    APIMessage,
    ToolDefinition,
    ConversationCreateResponse,
    ConversationDetail,
    CompactResponse,
} from './chatTypes';
import {
    COMPACTION_TOKEN_THRESHOLD,
    COMPACTION_MAX_TOKENS,
    COMPACTION_RECENT_WINDOW,
    INPUT_HISTORY_MAX,
    SYSTEM_PROMPT,
    CHAT_TOOLS,
} from './chatConstants';
import {
    estimateTokenCount,
    toAPIMessages,
    loadInputHistory,
    saveInputHistory,
} from './chatHelpers';

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
    // Compaction
    // ---------------------------------------------------------------

    /**
     * Compact the API message history when estimated tokens exceed
     * the threshold.  The compacted messages replace the in-memory
     * history; the visible UI messages are not affected.
     */
    const maybeCompact = useCallback(
        async (msgs: APIMessage[]): Promise<APIMessage[]> => {
            if (estimateTokenCount(msgs) < COMPACTION_TOKEN_THRESHOLD) {
                return msgs;
            }

            try {
                const response = await apiFetch('/api/v1/chat/compact', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        messages: msgs,
                        max_tokens: COMPACTION_MAX_TOKENS,
                        recent_window: COMPACTION_RECENT_WINDOW,
                        keep_anchors: true,
                        options: {
                            preserve_tool_results: true,
                            enable_summarization: true,
                        },
                    }),
                });

                if (!response.ok) {
                    return msgs;
                }

                const data: CompactResponse = await response.json();
                return data.messages ?? msgs;
            } catch (err) {
                console.error('Chat compaction failed:', err);
                return msgs;
            }
        },
        [],
    );

    // ---------------------------------------------------------------
    // Agentic loop
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
            apiMessagesRef.current = [
                ...apiMessagesRef.current,
                userAPIMessage,
            ];

            let iterations = 0;
            let finalAssistantMessage: ChatMessageData | null = null;
            const collectedActivity: ToolActivity[] = [];

            try {
                while (iterations < maxIterations) {
                    if (abortController.signal.aborted) {
                        return;
                    }
                    iterations++;

                    // Call the LLM with current message history and tools
                    const response = await apiFetch('/api/v1/llm/chat', {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json',
                        },
                        body: JSON.stringify({
                            messages: apiMessagesRef.current,
                            tools: availableTools,
                            system: SYSTEM_PROMPT,
                        }),
                        signal: abortController.signal,
                    });

                    if (!response.ok) {
                        const errorText = await response.text();
                        throw new Error(
                            `LLM request failed: ${errorText}`,
                        );
                    }

                    const data: LLMResponse = await response.json();

                    const toolUses =
                        data.content?.filter(
                            c => c.type === 'tool_use',
                        ) || [];
                    const textBlocks =
                        data.content?.filter(
                            c => c.type === 'text',
                        ) || [];

                    if (toolUses.length === 0) {
                        // No tool calls - extract final text response
                        const assistantText =
                            textBlocks
                                .map(c => c.text)
                                .join('\n') || '';

                        finalAssistantMessage = {
                            role: 'assistant',
                            content: assistantText,
                            timestamp: new Date().toISOString(),
                            activity:
                                collectedActivity.length > 0
                                    ? [...collectedActivity]
                                    : undefined,
                        };

                        // Append to API history
                        apiMessagesRef.current = [
                            ...apiMessagesRef.current,
                            {
                                role: 'assistant',
                                content: assistantText,
                            },
                        ];

                        break;
                    }

                    // --- Tool execution phase ---

                    // Append the assistant message (with tool_use blocks)
                    // to the API message history
                    apiMessagesRef.current = [
                        ...apiMessagesRef.current,
                        {
                            role: 'assistant',
                            content:
                                data.content as LLMContentBlock[],
                        },
                    ];

                    // Execute each tool call sequentially
                    const toolResults: ToolResult[] = [];

                    for (const toolUse of toolUses) {
                        const toolName = toolUse.name ?? 'unknown';

                        // Mark tool as running in the activity tracker
                        const activity: ToolActivity = {
                            name: toolName,
                            status: 'running',
                            startedAt: new Date().toISOString(),
                        };
                        collectedActivity.push(activity);
                        setActiveTools([...collectedActivity]);

                        try {
                            const toolResponse = await apiFetch(
                                '/api/v1/mcp/tools/call',
                                {
                                    method: 'POST',
                                    headers: {
                                        'Content-Type':
                                            'application/json',
                                    },
                                    body: JSON.stringify({
                                        name: toolUse.name,
                                        arguments: toolUse.input,
                                    }),
                                    signal: abortController.signal,
                                },
                            );

                            const toolData: ToolCallResponse =
                                await toolResponse.json();
                            const resultText =
                                toolData.content?.[0]?.text ||
                                (toolData.isError
                                    ? 'Tool execution failed'
                                    : 'No data returned');

                            activity.status = toolData.isError
                                ? 'error'
                                : 'completed';
                            setActiveTools([...collectedActivity]);

                            toolResults.push({
                                type: 'tool_result',
                                tool_use_id: toolUse.id ?? '',
                                content: resultText,
                                is_error:
                                    toolData.isError || undefined,
                            });
                        } catch (toolErr) {
                            if (
                                (toolErr as Error).name ===
                                'AbortError'
                            ) {
                                throw toolErr;
                            }

                            const errMsg = `Tool execution error: ${(toolErr as Error).message}`;
                            activity.status = 'error';
                            setActiveTools([...collectedActivity]);

                            toolResults.push({
                                type: 'tool_result',
                                tool_use_id: toolUse.id ?? '',
                                content: errMsg,
                                is_error: true,
                            });
                        }
                    }

                    // Append tool results to API history and loop
                    apiMessagesRef.current = [
                        ...apiMessagesRef.current,
                        { role: 'user', content: toolResults },
                    ];
                }

                // If the loop exhausted iterations without a final text
                // response, surface an error to the user.
                if (!finalAssistantMessage) {
                    finalAssistantMessage = {
                        role: 'assistant',
                        content:
                            'I was unable to complete the request within the ' +
                            'allowed number of steps. Please try rephrasing ' +
                            'your question.',
                        timestamp: new Date().toISOString(),
                        isError: true,
                        activity:
                            collectedActivity.length > 0
                                ? [...collectedActivity]
                                : undefined,
                    };
                    apiMessagesRef.current = [
                        ...apiMessagesRef.current,
                        {
                            role: 'assistant',
                            content:
                                finalAssistantMessage.content as string,
                        },
                    ];
                }

                // Append the assistant reply to visible messages.
                // Read the latest visible messages from the ref to
                // avoid stale closure issues.
                const finalVisibleMessages = [
                    ...visibleMessagesRef.current,
                    finalAssistantMessage,
                ];
                setMessages(finalVisibleMessages);
                visibleMessagesRef.current = finalVisibleMessages;

                // --- Conversation persistence ---

                try {
                    if (!conversationIdRef.current) {
                        // Create a new conversation on first response
                        const createResponse = await apiFetch(
                            '/api/v1/conversations',
                            {
                                method: 'POST',
                                headers: {
                                    'Content-Type':
                                        'application/json',
                                },
                                body: JSON.stringify({
                                    messages: finalVisibleMessages,
                                    provider: '',
                                    model: '',
                                }),
                            },
                        );

                        if (createResponse.ok) {
                            const createData: ConversationCreateResponse =
                                await createResponse.json();
                            syncConversationId(createData.id);
                        } else {
                            console.warn(
                                'Failed to create conversation:',
                                createResponse.status,
                                await createResponse.text(),
                            );
                        }
                    } else {
                        // Update the existing conversation
                        const updateResponse = await apiFetch(
                            `/api/v1/conversations/${conversationIdRef.current}`,
                            {
                                method: 'PUT',
                                headers: {
                                    'Content-Type':
                                        'application/json',
                                },
                                body: JSON.stringify({
                                    messages: finalVisibleMessages,
                                }),
                            },
                        );
                        if (!updateResponse.ok) {
                            console.warn(
                                'Failed to update conversation:',
                                updateResponse.status,
                                await updateResponse.text(),
                            );
                        }
                    }
                } catch (saveErr) {
                    console.error(
                        'Failed to persist conversation:',
                        saveErr,
                    );
                    // Non-fatal: the user can still continue chatting
                }

                // --- Compaction ---

                try {
                    apiMessagesRef.current = await maybeCompact(
                        apiMessagesRef.current,
                    );
                } catch {
                    // Compaction failures are non-fatal
                }
            } catch (err) {
                if ((err as Error).name === 'AbortError') {
                    // Request was intentionally cancelled
                    return;
                }

                const errMessage =
                    (err as Error).message ||
                    'An unexpected error occurred';
                console.error('Chat error:', err);
                setError(errMessage);

                // Add an error message to the visible conversation
                const errorAssistantMessage: ChatMessageData = {
                    role: 'assistant',
                    content: `Sorry, an error occurred: ${errMessage}`,
                    timestamp: new Date().toISOString(),
                    isError: true,
                    activity:
                        collectedActivity.length > 0
                            ? [...collectedActivity]
                            : undefined,
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
        [availableTools, maxIterations, syncConversationId, maybeCompact],
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
            setIsLoading(true);
            setError(null);
            setActiveTools([]);

            try {
                const response = await apiFetch(
                    `/api/v1/conversations/${id}`,
                );

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error(
                        `Failed to load conversation: ${errorText}`,
                    );
                }

                const data: ConversationDetail =
                    await response.json();

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
                console.error('Failed to load conversation:', err);
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
