/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import React, {
    createContext,
    useState,
    useCallback,
    useEffect,
    useRef,
    useMemo,
} from 'react';
import { useAuth } from './useAuth';
import { apiGet, apiDelete, apiPatch } from '../utils/apiClient';
import useChat from '../hooks/useChat';
import type { ChatMessage, ToolActivity } from '../hooks/useChat';
import { logger } from '../utils/logger';

// Re-export hook types for convenience
export type { ChatMessage, ContentBlock, ToolActivity } from '../hooks/useChat';

// ---------------------------------------------------------------
// Types
// ---------------------------------------------------------------

export interface ConversationSummary {
    id: string;
    title: string;
    connection?: string;
    created_at: string;
    updated_at: string;
    preview: string;
}

export interface ChatContextType {
    // Chat panel state
    isOpen: boolean;
    openChat: () => void;
    closeChat: () => void;
    toggleChat: () => void;

    // Current conversation
    messages: ChatMessage[];
    conversationId: string | null;
    conversationTitle: string;
    isLoading: boolean;
    error: string | null;

    // User message input history (for up/down arrow navigation)
    inputHistory: string[];

    // Actions
    sendMessage: (text: string) => Promise<void>;
    clearChat: () => void;

    // Conversation management
    conversations: ConversationSummary[];
    loadConversation: (id: string) => Promise<void>;
    deleteConversation: (id: string) => Promise<void>;
    renameConversation: (id: string, title: string) => Promise<void>;
    refreshConversations: () => Promise<void>;

    // Tool state
    activeTools: ToolActivity[];
}

// ---------------------------------------------------------------
// Context
// ---------------------------------------------------------------

const ChatContext = createContext<ChatContextType | null>(null);

// ---------------------------------------------------------------
// Provider
// ---------------------------------------------------------------

interface ChatProviderProps {
    children: React.ReactNode;
}

export const ChatProvider = ({ children }: ChatProviderProps): React.ReactElement => {
    const { user } = useAuth();

    // Panel open/closed state
    const [isOpen, setIsOpen] = useState<boolean>(false);

    // Conversation list
    const [conversations, setConversations] = useState<ConversationSummary[]>([]);

    // Mounted guard
    const isMountedRef = useRef<boolean>(true);

    // Core chat hook handles messages, agentic loop, and persistence
    const {
        messages,
        isLoading,
        error,
        conversationId,
        conversationTitle,
        activeTools,
        inputHistory,
        sendMessage,
        clearChat: clearChatHook,
        loadConversation: loadConversationHook,
    } = useChat();

    // ---------------------------------------------------------------
    // Panel controls
    // ---------------------------------------------------------------

    const openChat = useCallback((): void => {
        setIsOpen(true);
    }, []);

    const closeChat = useCallback((): void => {
        setIsOpen(false);
    }, []);

    const toggleChat = useCallback((): void => {
        setIsOpen(prev => !prev);
    }, []);

    // ---------------------------------------------------------------
    // Conversation list management
    // ---------------------------------------------------------------

    const refreshConversations = useCallback(async (): Promise<void> => {
        if (!user) {
            return;
        }

        try {
            const data = await apiGet<{ conversations: ConversationSummary[] }>(
                '/api/v1/conversations',
            );

            if (isMountedRef.current) {
                const list = Array.isArray(data)
                    ? data
                    : (data as { conversations: ConversationSummary[] })?.conversations || [];
                setConversations(list);
            }
        } catch (err) {
            logger.error('Failed to fetch conversations:', err);
            // Non-fatal: the sidebar list simply stays as-is
        }
    }, [user]);

    const loadConversation = useCallback(async (id: string): Promise<void> => {
        await loadConversationHook(id);
        // Refresh the list so updated_at and previews stay current
        await refreshConversations();
    }, [loadConversationHook, refreshConversations]);

    const deleteConversation = useCallback(async (id: string): Promise<void> => {
        try {
            await apiDelete(`/api/v1/conversations/${id}`);

            if (isMountedRef.current) {
                setConversations(prev => prev.filter(c => c.id !== id));
            }

            // If the deleted conversation is the active one, reset
            if (id === conversationId) {
                clearChatHook();
            }
        } catch (err) {
            logger.error('Failed to delete conversation:', err);
            throw err;
        }
    }, [conversationId, clearChatHook]);

    const renameConversation = useCallback(async (
        id: string,
        title: string,
    ): Promise<void> => {
        try {
            await apiPatch(`/api/v1/conversations/${id}`, { title });

            if (isMountedRef.current) {
                setConversations(prev =>
                    prev.map(c => (c.id === id ? { ...c, title } : c)),
                );
            }
        } catch (err) {
            logger.error('Failed to rename conversation:', err);
            throw err;
        }
    }, []);

    /**
     * Wrapped clearChat that also refreshes the conversation list so
     * that any newly-completed conversation shows its latest state.
     */
    const clearChat = useCallback((): void => {
        clearChatHook();
        // Fire-and-forget refresh of the sidebar conversation list
        refreshConversations().catch(() => {});
    }, [clearChatHook, refreshConversations]);

    /**
     * Wrapped sendMessage that refreshes the conversation list after
     * the exchange completes so the sidebar shows the new or updated
     * conversation.
     */
    const sendMessage_ = useCallback(async (text: string): Promise<void> => {
        await sendMessage(text);
        // Refresh after the exchange to pick up new/updated conversations
        refreshConversations().catch(() => {});
    }, [sendMessage, refreshConversations]);

    // ---------------------------------------------------------------
    // Effects
    // ---------------------------------------------------------------

    // Fetch the conversation list on mount and whenever the user changes
    useEffect(() => {
        isMountedRef.current = true;
        if (user) {
            refreshConversations();
        }
        return () => {
            isMountedRef.current = false;
        };
    }, [user, refreshConversations]);

    // ---------------------------------------------------------------
    // Context value
    // ---------------------------------------------------------------

    const value: ChatContextType = useMemo(() => ({
        // Panel state
        isOpen,
        openChat,
        closeChat,
        toggleChat,

        // Current conversation
        messages,
        conversationId,
        conversationTitle,
        isLoading,
        error,

        // Input history
        inputHistory,

        // Actions
        sendMessage: sendMessage_,
        clearChat,

        // Conversation management
        conversations,
        loadConversation,
        deleteConversation,
        renameConversation,
        refreshConversations,

        // Tool state
        activeTools,
    }), [
        isOpen,
        openChat,
        closeChat,
        toggleChat,
        messages,
        conversationId,
        conversationTitle,
        isLoading,
        error,
        inputHistory,
        sendMessage_,
        clearChat,
        conversations,
        loadConversation,
        deleteConversation,
        renameConversation,
        refreshConversations,
        activeTools,
    ]);

    return (
        <ChatContext.Provider value={value}>
            {children}
        </ChatContext.Provider>
    );
};

export default ChatContext;
