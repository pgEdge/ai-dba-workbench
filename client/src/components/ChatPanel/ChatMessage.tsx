/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import { Box, Typography, Chip, alpha, useTheme } from '@mui/material';
import { Theme } from '@mui/material/styles';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { ThemeMode } from '../../types/theme';
import { ToolActivity } from './ToolStatus';
import { getToolDisplayName } from '../../utils/toolDisplayNames';
import { createCleanTheme, extractLanguage } from '../shared/MarkdownContent';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ContentBlock {
    type: 'text' | 'tool_use' | 'tool_result';
    text?: string;
    name?: string;
    content?: string;
}

export interface ChatMessageData {
    role: 'user' | 'assistant' | 'system';
    content: string | ContentBlock[];
    timestamp?: string;
    isError?: boolean;
    activity?: ToolActivity[];
}

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const sxMonoFont = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

const sxTimestamp = {
    fontSize: '0.875rem',
    color: 'text.disabled',
    mt: 0.5,
};

const sxToolChipsRow = {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 0.5,
    mt: 0.75,
};

const sxToolChip = {
    height: 20,
    fontSize: '1rem',
    fontWeight: 500,
};

const getMessageContainerSx = (role: ChatMessageData['role']) => ({
    display: 'flex',
    flexDirection: 'column',
    alignItems: role === 'user'
        ? 'flex-end'
        : role === 'system'
            ? 'center'
            : 'flex-start',
    mb: 1.5,
    px: 1,
});

const getUserBubbleSx = (theme: Theme) => ({
    maxWidth: '85%',
    px: 2,
    py: 1.25,
    borderRadius: 2,
    borderBottomRightRadius: 4,
    bgcolor: alpha(theme.palette.primary.main, 0.15),
    color: theme.palette.text.primary,
});

const getAssistantBubbleSx = (theme: Theme) => ({
    maxWidth: '92%',
    px: 2,
    py: 1.25,
    borderRadius: 2,
    borderBottomLeftRadius: 4,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.6)
        : theme.palette.background.paper,
    border: '1px solid',
    borderColor: theme.palette.divider,
});

const getSystemBubbleSx = (theme: Theme) => ({
    maxWidth: '90%',
    px: 2,
    py: 0.75,
    borderRadius: 2,
    bgcolor: alpha(theme.palette.grey[500], 0.08),
});

const getErrorBubbleSx = (theme: Theme) => ({
    maxWidth: '85%',
    px: 2,
    py: 1.25,
    borderRadius: 2,
    borderBottomLeftRadius: 4,
    bgcolor: alpha(theme.palette.error.main, 0.08),
    border: '1px solid',
    borderColor: alpha(theme.palette.error.main, 0.2),
});

const getUserTextSx = {
    fontSize: '1rem',
    lineHeight: 1.6,
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
};

const getSystemTextSx = (theme: Theme) => ({
    fontSize: '1.125rem',
    fontStyle: 'italic',
    color: theme.palette.text.secondary,
    lineHeight: 1.5,
});

const getErrorTextSx = (theme: Theme) => ({
    fontSize: '1rem',
    lineHeight: 1.6,
    color: theme.palette.mode === 'dark'
        ? theme.palette.error.light
        : theme.palette.error.dark,
});

// Markdown style getters (following AlertAnalysisDialog pattern)
const getMdH1Sx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.secondary.main,
    fontSize: '1.125rem',
    mt: 1.5,
    mb: 0.75,
    pb: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.15,
    ),
});

const getMdH2Sx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.secondary.main,
    fontSize: '1.0625rem',
    mt: 1.5,
    mb: 0.75,
    pb: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.15,
    ),
});

const sxMdH3 = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1rem',
    mt: 1.5,
    mb: 0.5,
};

const sxMdParagraph = {
    color: 'text.primary',
    fontSize: '1.125rem',
    lineHeight: 1.7,
    my: 0.75,
};

const sxMdList = {
    pl: 2.5,
    my: 1,
    '& li': {
        mb: 0.5,
        fontSize: '1.125rem',
        lineHeight: 1.6,
        color: 'text.primary',
    },
};

const sxMdUnorderedList = {
    ...sxMdList,
    listStyleType: 'disc',
};

const sxMdStrong = { fontWeight: 600 };
const sxMdEm = { fontStyle: 'italic' };

const getMdInlineCodeSx = (theme: Theme) => ({
    ...sxMonoFont,
    fontSize: '1rem',
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.6)
        : alpha(theme.palette.grey[200], 0.8),
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[200]
        : theme.palette.grey[700],
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
});

const getMdCodeBlockWrapperSx = (theme: Theme) => ({
    my: 1,
    borderRadius: 1,
    overflow: 'hidden',
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
    '& pre': {
        margin: '0 !important',
        borderRadius: '0 !important',
    },
});

const getMdCodeBlockCustomStyle = (customBackground: string) => ({
    margin: 0,
    padding: '0.75rem',
    fontSize: '1rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    background: customBackground,
});

const getMdLinkSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.dark,
    textDecoration: 'none',
    '&:hover': {
        textDecoration: 'underline',
    },
});

const getMdBlockquoteSx = (theme: Theme) => ({
    borderLeft: '3px solid',
    borderColor: theme.palette.secondary.main,
    pl: 1.5,
    ml: 0,
    my: 1,
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    fontStyle: 'italic',
});

const getMdTableSx = (theme: Theme) => ({
    width: '100%',
    borderCollapse: 'collapse',
    my: 1,
    fontSize: '1.125rem',
    '& th, & td': {
        border: '1px solid',
        borderColor: theme.palette.mode === 'dark'
            ? theme.palette.grey[700]
            : theme.palette.grey[200],
        p: 0.75,
        textAlign: 'left',
    },
    '& th': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.5)
            : alpha(theme.palette.grey[100], 0.8),
        fontWeight: 600,
    },
});

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

interface ChatMessageProps {
    message: ChatMessageData;
    mode: ThemeMode;
}

const ChatMessage: React.FC<ChatMessageProps> = ({ message, mode }) => {
    const theme = useTheme();
    const isDark = mode === 'dark';

    const textContent = getContentText(message.content);

    // Memoize markdown components following AlertAnalysisDialog pattern
    const markdownComponents = useMemo(
        () => ({
            h1: ({ children }: { children?: React.ReactNode }) => (
                <Typography variant="h5" sx={getMdH1Sx(theme)}>
                    {children}
                </Typography>
            ),
            h2: ({ children }: { children?: React.ReactNode }) => (
                <Typography variant="h6" sx={getMdH2Sx(theme)}>
                    {children}
                </Typography>
            ),
            h3: ({ children }: { children?: React.ReactNode }) => (
                <Typography variant="subtitle1" sx={sxMdH3}>
                    {children}
                </Typography>
            ),
            p: ({ children }: { children?: React.ReactNode }) => (
                <Typography variant="body2" sx={sxMdParagraph}>
                    {children}
                </Typography>
            ),
            ul: ({ children }: { children?: React.ReactNode }) => (
                <Box component="ul" sx={sxMdUnorderedList}>
                    {children}
                </Box>
            ),
            ol: ({ children }: { children?: React.ReactNode }) => (
                <Box component="ol" sx={sxMdList}>
                    {children}
                </Box>
            ),
            li: ({ children }: { children?: React.ReactNode }) => (
                <li>{children}</li>
            ),
            code: ({
                className,
                children,
                node,
                ...props
            }: {
                className?: string;
                children?: React.ReactNode;
                node?: { position?: unknown };
                [key: string]: unknown;
            }) => {
                // In react-markdown v10+, inline code is NOT wrapped in <pre>.
                // Fenced code blocks are rendered as <pre><code>...</code></pre>.
                // We detect inline by checking if there's no language class and
                // the content doesn't contain newlines.
                const language = extractLanguage(className);
                const codeString = String(children).replace(/\n$/, '');
                const isInline = !language && !codeString.includes('\n');

                if (isInline) {
                    return (
                        <Box component="code" sx={getMdInlineCodeSx(theme)}>
                            {children}
                        </Box>
                    );
                }

                const customBackground = isDark
                    ? theme.palette.background.paper
                    : theme.palette.grey[50];

                const cleanTheme = isDark
                    ? createCleanTheme(oneDark, theme.palette.background.paper)
                    : createCleanTheme(oneLight, theme.palette.grey[50]);

                return (
                    <Box sx={getMdCodeBlockWrapperSx(theme)}>
                        <SyntaxHighlighter
                            style={cleanTheme}
                            language={language || 'text'}
                            PreTag="div"
                            customStyle={getMdCodeBlockCustomStyle(
                                customBackground,
                            )}
                            {...props}
                        >
                            {codeString}
                        </SyntaxHighlighter>
                    </Box>
                );
            },
            pre: ({ children }: { children?: React.ReactNode }) => (
                <>{children}</>
            ),
            strong: ({ children }: { children?: React.ReactNode }) => (
                <Box component="strong" sx={sxMdStrong}>
                    {children}
                </Box>
            ),
            em: ({ children }: { children?: React.ReactNode }) => (
                <Box component="em" sx={sxMdEm}>
                    {children}
                </Box>
            ),
            a: ({
                href,
                children,
            }: {
                href?: string;
                children?: React.ReactNode;
            }) => (
                <Box
                    component="a"
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer"
                    sx={getMdLinkSx(theme)}
                >
                    {children}
                </Box>
            ),
            blockquote: ({ children }: { children?: React.ReactNode }) => (
                <Box component="blockquote" sx={getMdBlockquoteSx(theme)}>
                    {children}
                </Box>
            ),
            table: ({ children }: { children?: React.ReactNode }) => (
                <Box component="table" sx={getMdTableSx(theme)}>
                    {children}
                </Box>
            ),
        }),
        [isDark, theme],
    );

    // Render tool activity chips
    const renderToolActivity = () => {
        if (!message.activity || message.activity.length === 0) {
            return null;
        }
        return (
            <Box sx={sxToolChipsRow}>
                {message.activity.map((tool, idx) => (
                    <Chip
                        key={`${tool.name}-${idx}`}
                        label={getToolDisplayName(tool.name)}
                        size="small"
                        color={
                            tool.status === 'error'
                                ? 'warning'
                                : tool.status === 'completed'
                                    ? 'success'
                                    : 'primary'
                        }
                        variant="outlined"
                        sx={sxToolChip}
                    />
                ))}
            </Box>
        );
    };

    // Format an ISO timestamp for display (includes date for old chat context)
    const formatTimestamp = (ts: string): string => {
        const date = new Date(ts);
        return date.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
        });
    };

    // Error message rendering
    if (message.isError) {
        return (
            <Box sx={getMessageContainerSx('assistant')}>
                <Box sx={getErrorBubbleSx(theme)}>
                    <Typography sx={getErrorTextSx(theme)}>
                        {textContent}
                    </Typography>
                </Box>
                {message.timestamp && (
                    <Typography sx={sxTimestamp}>
                        {formatTimestamp(message.timestamp)}
                    </Typography>
                )}
            </Box>
        );
    }

    // System message rendering
    if (message.role === 'system') {
        return (
            <Box sx={getMessageContainerSx('system')}>
                <Box sx={getSystemBubbleSx(theme)}>
                    <Typography sx={getSystemTextSx(theme)}>
                        {textContent}
                    </Typography>
                </Box>
            </Box>
        );
    }

    // User message rendering
    if (message.role === 'user') {
        return (
            <Box sx={getMessageContainerSx('user')}>
                <Box sx={getUserBubbleSx(theme)}>
                    <Typography sx={getUserTextSx}>{textContent}</Typography>
                </Box>
                {message.timestamp && (
                    <Typography sx={sxTimestamp}>
                        {formatTimestamp(message.timestamp)}
                    </Typography>
                )}
            </Box>
        );
    }

    // Assistant message rendering with markdown
    return (
        <Box sx={getMessageContainerSx('assistant')}>
            <Box sx={getAssistantBubbleSx(theme)}>
                <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={markdownComponents as Record<string, React.ComponentType>}
                >
                    {textContent}
                </ReactMarkdown>
                {renderToolActivity()}
            </Box>
            {message.timestamp && (
                <Typography sx={sxTimestamp}>
                    {formatTimestamp(message.timestamp)}
                </Typography>
            )}
        </Box>
    );
};

export default ChatMessage;
