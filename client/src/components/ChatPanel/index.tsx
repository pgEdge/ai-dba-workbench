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
    useState,
    useRef,
    useEffect,
    useCallback,
    useMemo,
} from 'react';
import {
    Box,
    Typography,
    IconButton,
    Tooltip,
    alpha,
} from '@mui/material';
import { Theme, useTheme } from '@mui/material/styles';
import {
    Close as CloseIcon,
    Add as NewChatIcon,
    History as HistoryIcon,
    Download as DownloadIcon,
} from '@mui/icons-material';
import { ThemeMode } from '../../types/theme';
import { useChatContext } from '../../contexts/ChatContext';
import ChatMessage, { ChatMessageData, ContentBlock } from './ChatMessage';
import ChatInput from './ChatInput';
import ToolStatus, { ToolActivity } from './ToolStatus';
import ThinkingIndicator from './ThinkingIndicator';
import ConversationHistory from './ConversationHistory';
import { getDownloadButtonSx } from '../shared/MarkdownContent';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_WIDTH = 450;
const MIN_WIDTH = 350;
const MAX_WIDTH = 800;
const STORAGE_KEY = 'chat-panel-width';

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const getPanelContainerSx =
    (width: number, open: boolean) => (theme: Theme) => ({
        width: open ? width : 0,
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'row',
        bgcolor: theme.palette.background.default,
        ...(open ? {
            borderLeft: '1px solid',
            borderColor: theme.palette.divider,
            boxShadow: theme.palette.mode === 'dark'
                ? '-4px 0 24px rgba(0, 0, 0, 0.3)'
                : '-4px 0 24px rgba(0, 0, 0, 0.08)',
        } : {}),
        overflow: 'hidden',
    });

const getResizeHandleSx = (isResizing: boolean) => (theme: Theme) => ({
    position: 'absolute' as const,
    left: 0,
    top: 0,
    bottom: 0,
    width: 4,
    cursor: 'col-resize',
    zIndex: 2,
    bgcolor: isResizing
        ? alpha(theme.palette.primary.main, 0.4)
        : 'transparent',
    transition: 'background-color 0.15s',
    '&:hover': {
        bgcolor: alpha(theme.palette.primary.main, 0.3),
    },
});

const panelContentSx = {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    minWidth: 0,
};

const getHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    px: 2,
    py: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
    flexShrink: 0,
});

const headerTitleSx = {
    flex: 1,
    fontWeight: 600,
    fontSize: '1.0625rem',
    color: 'text.primary',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const getHeaderIconButtonSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    p: 0.75,
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.primary.main, 0.08)
            : alpha(theme.palette.primary.main, 0.04),
        color: theme.palette.primary.main,
    },
});

const getActiveHistoryButtonSx = (theme: Theme) => ({
    ...getHeaderIconButtonSx(theme),
    bgcolor: alpha(theme.palette.primary.main, 0.12),
    color: theme.palette.primary.main,
    '&:hover': {
        bgcolor: alpha(theme.palette.primary.main, 0.2),
        color: theme.palette.primary.main,
    },
});

const headerIconSx = { fontSize: 22 };

const bodySx = {
    flex: 1,
    display: 'flex',
    flexDirection: 'row',
    overflow: 'hidden',
    position: 'relative' as const,
};

const chatAreaSx = {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    minWidth: 0,
};

const messageListSx = {
    flex: 1,
    overflowY: 'auto',
    overflowX: 'hidden',
    py: 2,
    px: 1,
};

const emptyStateSx = {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 1.5,
    p: 3,
};

const emptyTitleSx = {
    fontWeight: 600,
    color: 'text.secondary',
    fontSize: '1.125rem',
};

const emptySubtitleSx = {
    color: 'text.disabled',
    fontSize: '1.125rem',
    textAlign: 'center',
    maxWidth: 280,
    lineHeight: 1.5,
};

const inputAreaSx = {
    flexShrink: 0,
};

const errorBannerSx = {
    px: 2,
    py: 0.75,
    borderTop: '1px solid',
    borderColor: 'error.main',
};

const errorBannerTextSx = {
    fontSize: '1rem',
    color: 'error.main',
    lineHeight: 1.4,
};

// ---------------------------------------------------------------------------
// Type mapping helpers
// ---------------------------------------------------------------------------

/**
 * The useChat hook and the ToolStatus component now share the
 * same ToolActivity type, so no field mapping is required.
 */
type HookToolActivity = ToolActivity;
const mapToolActivity = (tool: HookToolActivity): ToolActivity => tool;

// ---------------------------------------------------------------------------
// Helper: extract text from content blocks
// ---------------------------------------------------------------------------

const getContentText = (content: string | ContentBlock[]): string => {
    if (typeof content === 'string') {return content;}
    return content
        .filter((block) => block.type === 'text' && block.text)
        .map((block) => block.text)
        .join('\n');
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ChatPanelProps {
    open: boolean;
    onClose: () => void;
    mode: ThemeMode;
}

const ChatPanel: React.FC<ChatPanelProps> = ({
    open,
    onClose,
    mode,
}) => {
    // Panel width with localStorage persistence
    const [panelWidth, setPanelWidth] = useState<number>(() => {
        const saved = localStorage.getItem(STORAGE_KEY);
        if (saved) {
            const parsed = parseInt(saved, 10);
            if (!isNaN(parsed) && parsed >= MIN_WIDTH && parsed <= MAX_WIDTH) {
                return parsed;
            }
        }
        return DEFAULT_WIDTH;
    });

    const theme = useTheme();
    const [isResizing, setIsResizing] = useState(false);
    const [showHistory, setShowHistory] = useState(false);

    // Chat state from context
    const {
        messages: rawMessages,
        activeTools: rawActiveTools,
        conversations,
        conversationId: currentConversationId,
        inputHistory,
        isLoading,
        error,
        sendMessage,
        clearChat,
        loadConversation,
        deleteConversation,
        renameConversation,
        refreshConversations,
    } = useChatContext();

    // Map hook ToolActivity to component ToolActivity format
    const activeTools: ToolActivity[] = useMemo(
        () => (rawActiveTools as unknown as HookToolActivity[])
            .map(mapToolActivity),
        [rawActiveTools],
    );

    // Map hook ChatMessage to component ChatMessageData format
    const messages: ChatMessageData[] = useMemo(
        () => rawMessages.map((msg): ChatMessageData => {
            const hookActivity = msg.activity as
                unknown as HookToolActivity[] | undefined;
            return {
                role: msg.role,
                content: msg.content as ChatMessageData['content'],
                timestamp: msg.timestamp,
                isError: msg.isError,
                activity: hookActivity?.map(mapToolActivity),
            };
        }),
        [rawMessages],
    );

    const messageListRef = useRef<HTMLDivElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);

    // -----------------------------------------------------------------------
    // Resize logic
    // -----------------------------------------------------------------------

    const handleResizeStart = useCallback(
        (event: React.MouseEvent) => {
            event.preventDefault();
            setIsResizing(true);

            const startX = event.clientX;
            const startWidth = panelWidth;

            const handleMouseMove = (e: MouseEvent) => {
                const deltaX = startX - e.clientX;
                const newWidth = Math.min(
                    MAX_WIDTH,
                    Math.max(MIN_WIDTH, startWidth + deltaX),
                );
                setPanelWidth(newWidth);
            };

            const handleMouseUp = () => {
                setIsResizing(false);
                document.removeEventListener('mousemove', handleMouseMove);
                document.removeEventListener('mouseup', handleMouseUp);
                document.body.style.cursor = '';
                document.body.style.userSelect = '';

                // Persist width after drag ends
                setPanelWidth((w) => {
                    localStorage.setItem(STORAGE_KEY, String(w));
                    return w;
                });
            };

            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
            document.addEventListener('mousemove', handleMouseMove);
            document.addEventListener('mouseup', handleMouseUp);
        },
        [panelWidth],
    );

    // -----------------------------------------------------------------------
    // Auto-scroll to bottom on new messages
    // -----------------------------------------------------------------------

    useEffect(() => {
        if (messageListRef.current) {
            messageListRef.current.scrollTop =
                messageListRef.current.scrollHeight;
        }
    }, [messages]);

    // -----------------------------------------------------------------------
    // Refresh conversations when panel opens
    // -----------------------------------------------------------------------

    useEffect(() => {
        if (open) {
            refreshConversations();
        }
    }, [open, refreshConversations]);

    // -----------------------------------------------------------------------
    // Handlers wired to ChatContext
    // -----------------------------------------------------------------------

    const handleSend = useCallback(
        (message: string) => {
            sendMessage(message);
        },
        [sendMessage],
    );

    const handleNewChat = useCallback(() => {
        clearChat();
    }, [clearChat]);

    const handleSelectConversation = useCallback(
        (id: string) => {
            loadConversation(id);
        },
        [loadConversation],
    );

    const handleDeleteConversation = useCallback(
        (id: string) => {
            deleteConversation(id);
        },
        [deleteConversation],
    );

    const handleRenameConversation = useCallback(
        (id: string, title: string) => {
            renameConversation(id, title);
        },
        [renameConversation],
    );

    const handleClearConversations = useCallback(() => {
        const ids = conversations.map(c => c.id);
        for (const id of ids) {
            deleteConversation(id).catch((err) => {
                console.error('Failed to delete conversation:', id, err);
            });
        }
    }, [conversations, deleteConversation]);

    const handleRefreshConversations = useCallback(() => {
        refreshConversations();
    }, [refreshConversations]);

    // Download conversation as markdown file
    const handleDownload = useCallback(() => {
        if (messages.length === 0) {return;}

        const timestamp = new Date().toISOString().split('T')[0];
        const filename = `ellie-chat-${timestamp}.md`;

        const currentConversation = conversations.find(
            (c) => c.id === currentConversationId,
        );
        const title = currentConversation?.title || 'Conversation';

        const lines: string[] = [
            `# Ellie Chat - ${title}`,
            '',
            `**Date:** ${timestamp}`,
            '',
        ];

        for (const msg of messages) {
            if (msg.role === 'system') {continue;}
            const heading = msg.role === 'user' ? '## User' : '## Ellie';
            const text = getContentText(msg.content);
            lines.push(heading, '', text, '', '---', '');
        }

        const content = lines.join('\n');
        const blob = new Blob([content], { type: 'text/markdown' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
    }, [messages, conversations, currentConversationId]);

    // -----------------------------------------------------------------------
    // Render
    // -----------------------------------------------------------------------

    return (
        <Box
            ref={panelRef}
            sx={getPanelContainerSx(panelWidth, open)}
            role="complementary"
            aria-label="AI Chat Panel"
        >
            {/* Resize handle */}
            <Box
                sx={getResizeHandleSx(isResizing)}
                onMouseDown={handleResizeStart}
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize chat panel"
            />

            {/* Panel content */}
            <Box sx={panelContentSx}>
                {/* Header */}
                <Box sx={getHeaderSx}>
                    <Tooltip title="Conversation history">
                        <IconButton
                            onClick={() => setShowHistory((prev) => !prev)}
                            size="small"
                            sx={showHistory
                                ? getActiveHistoryButtonSx
                                : getHeaderIconButtonSx}
                            aria-label="Toggle conversation history"
                        >
                            <HistoryIcon sx={headerIconSx} />
                        </IconButton>
                    </Tooltip>

                    <Typography sx={headerTitleSx}>Ask Ellie</Typography>

                    <Tooltip title="New conversation">
                        <IconButton
                            onClick={handleNewChat}
                            size="small"
                            sx={getHeaderIconButtonSx}
                            aria-label="Start new conversation"
                        >
                            <NewChatIcon sx={headerIconSx} />
                        </IconButton>
                    </Tooltip>

                    <Tooltip title="Download conversation">
                        <span>
                            <IconButton
                                onClick={handleDownload}
                                disabled={
                                    messages.length === 0 || isLoading
                                }
                                size="small"
                                sx={getDownloadButtonSx(theme)}
                                aria-label="Download conversation"
                            >
                                <DownloadIcon sx={headerIconSx} />
                            </IconButton>
                        </span>
                    </Tooltip>

                    <Tooltip title="Close">
                        <IconButton
                            onClick={onClose}
                            size="small"
                            sx={getHeaderIconButtonSx}
                            aria-label="Close chat panel"
                        >
                            <CloseIcon sx={headerIconSx} />
                        </IconButton>
                    </Tooltip>
                </Box>

                {/* Body */}
                <Box sx={bodySx}>
                    {/* Conversation history sidebar */}
                    {showHistory && (
                        <ConversationHistory
                            conversations={conversations}
                            currentId={currentConversationId}
                            onSelect={handleSelectConversation}
                            onDelete={handleDeleteConversation}
                            onRename={handleRenameConversation}
                            onClear={handleClearConversations}
                            onRefresh={handleRefreshConversations}
                            onClose={() => setShowHistory(false)}
                        />
                    )}

                    {/* Chat area */}
                    <Box sx={chatAreaSx}>
                        {/* Message list */}
                        {messages.length === 0 ? (
                            <Box sx={emptyStateSx}>
                                <Typography sx={emptyTitleSx}>
                                    Ask Ellie
                                </Typography>
                                <Typography sx={emptySubtitleSx}>
                                    Ask questions about your PostgreSQL
                                    databases, get performance advice, or
                                    troubleshoot issues.
                                </Typography>
                            </Box>
                        ) : (
                            <Box
                                ref={messageListRef}
                                sx={messageListSx}
                            >
                                {messages.map((msg, idx) => (
                                    <ChatMessage
                                        key={idx}
                                        message={msg}
                                        mode={mode}
                                    />
                                ))}
                            </Box>
                        )}

                        {/* Input area */}
                        <Box sx={inputAreaSx}>
                            <ThinkingIndicator
                                visible={
                                    isLoading
                                    && !activeTools.some(
                                        t => t.status === 'running',
                                    )
                                }
                            />
                            {activeTools.length > 0 && (
                                <ToolStatus tools={activeTools} />
                            )}
                            {error && (
                                <Box sx={errorBannerSx}>
                                    <Typography sx={errorBannerTextSx}>
                                        {error}
                                    </Typography>
                                </Box>
                            )}
                            <ChatInput
                                onSend={handleSend}
                                disabled={isLoading}
                                inputHistory={inputHistory}
                            />
                        </Box>
                    </Box>
                </Box>
            </Box>
        </Box>
    );
};

export default ChatPanel;
