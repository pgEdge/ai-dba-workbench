/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Shared markdown rendering and analysis components
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import {
    Box,
    Typography,
    useTheme,
} from '@mui/material';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import CopyCodeButton from './CopyCodeButton';
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

import RunnableCodeBlock from './RunnableCodeBlock';
import ConnectionSelectorCodeBlock from './ConnectionSelectorCodeBlock';
import AnalysisSkeleton from './AnalysisSkeleton';
import { isSqlCodeBlock, extractLanguage } from './sqlDetection';
import { createCleanTheme } from './markdownUtils';
import {
    sxH3,
    sxParagraph,
    sxUnorderedList,
    sxList,
    sxStrong,
    sxEm,
    sxH1,
    sxH2,
    getInlineCodeSx,
    getCodeBlockWrapperSx,
    getCodeBlockCustomStyle,
    getCodeBlockButtonGroupSx,
    getLinkSx,
    getBlockquoteSx,
    getTableSx,
} from './markdownStyles';

// ---------------------------------------------------------------------------
// MarkdownContent component
// ---------------------------------------------------------------------------

/**
 * Styled markdown content component using react-markdown
 */
interface MarkdownContentProps {
    content: string;
    isDark: boolean;
    connectionId?: number;
    databaseName?: string;
    serverName?: string;
    connectionMap?: Map<number, string>;
}

const MarkdownContent: React.FC<MarkdownContentProps> = ({
    content,
    isDark,
    connectionId,
    databaseName,
    serverName,
    connectionMap,
}) => {
    const theme = useTheme();

    // Memoize markdown components to avoid re-creating on each render
    const components = useMemo(() => ({
        h1: ({ children }) => (
            <Typography variant="h5" sx={sxH1(theme)}>
                {children}
            </Typography>
        ),
        h2: ({ children }) => (
            <Typography variant="h6" sx={sxH2(theme)}>
                {children}
            </Typography>
        ),
        h3: ({ children }) => (
            <Typography variant="subtitle1" sx={sxH3}>
                {children}
            </Typography>
        ),
        p: ({ children }) => (
            <Typography variant="body2" sx={sxParagraph}>
                {children}
            </Typography>
        ),
        ul: ({ children }) => (
            <Box component="ul" sx={sxUnorderedList}>
                {children}
            </Box>
        ),
        ol: ({ children }) => (
            <Box component="ol" sx={sxList}>
                {children}
            </Box>
        ),
        li: ({ children }) => <li>{children}</li>,
        code: ({ className, children, ...props }) => {
            const language = extractLanguage(className);
            const codeString = String(children).replace(/\n$/, '');

            // In react-markdown v10, the `inline` prop was removed.
            // Inline code has no language className and no newlines.
            const isInline = !className && !String(children).includes('\n');

            // Custom backgrounds for code blocks
            const customBackground = isDark
                ? theme.palette.background.paper
                : theme.palette.grey[50];

            // Create clean themes without token background colors
            const cleanTheme = isDark
                ? createCleanTheme(oneDark, theme.palette.background.paper)
                : createCleanTheme(oneLight, theme.palette.grey[50]);

            if (isInline) {
                return (
                    <Box component="code" sx={getInlineCodeSx(theme)}>
                        {children}
                    </Box>
                );
            }

            const sqlDetected = isSqlCodeBlock(className, codeString);

            if (sqlDetected) {
                // Server analysis: direct connection ID
                if (connectionId) {
                    return (
                        <RunnableCodeBlock
                            codeContent={codeString}
                            language={language}
                            isDark={isDark}
                            connectionId={connectionId}
                            databaseName={databaseName}
                            serverName={serverName}
                            syntaxTheme={cleanTheme}
                            customBackground={customBackground}
                            theme={theme}
                            isSql={true}
                            props={props}
                        />
                    );
                }

                // Cluster analysis: parse connection_id comment or show selector
                if (connectionMap && connectionMap.size > 0) {
                    const connectionIdMatch = codeString.match(/^--\s*connection_id:\s*(\d+)\s*\n/);
                    if (connectionIdMatch) {
                        const targetId = parseInt(connectionIdMatch[1], 10);
                        const targetName = connectionMap.get(targetId) || `Server ${targetId}`;
                        const strippedCode = codeString.replace(/^--\s*connection_id:\s*\d+\s*\n/, '');
                        return (
                            <RunnableCodeBlock
                                codeContent={strippedCode}
                                language={language}
                                isDark={isDark}
                                connectionId={targetId}
                                databaseName={databaseName}
                                serverName={targetName}
                                syntaxTheme={cleanTheme}
                                customBackground={customBackground}
                                theme={theme}
                                isSql={true}
                                props={props}
                            />
                        );
                    }

                    // No connection_id comment - show selector
                    return (
                        <ConnectionSelectorCodeBlock
                            codeContent={codeString}
                            language={language}
                            isDark={isDark}
                            connectionMap={connectionMap}
                            databaseName={databaseName}
                            syntaxTheme={cleanTheme}
                            customBackground={customBackground}
                            theme={theme}
                            props={props}
                        />
                    );
                }
            }

            return (
                <Box sx={{
                    ...getCodeBlockWrapperSx(theme),
                    position: 'relative',
                }}>
                    <SyntaxHighlighter
                        style={cleanTheme}
                        language={language || 'sql'}
                        PreTag="div"
                        customStyle={getCodeBlockCustomStyle(customBackground)}
                        {...props}
                    >
                        {codeString}
                    </SyntaxHighlighter>
                    <Box sx={getCodeBlockButtonGroupSx()}>
                        <CopyCodeButton code={codeString} theme={theme} />
                    </Box>
                </Box>
            );
        },
        strong: ({ children }) => (
            <Box component="strong" sx={sxStrong}>
                {children}
            </Box>
        ),
        em: ({ children }) => (
            <Box component="em" sx={sxEm}>
                {children}
            </Box>
        ),
        a: ({ href, children }) => {
            const safeHref =
                href && /^(https?:\/\/|mailto:)/i.test(href)
                    ? href
                    : undefined;
            return safeHref ? (
                <Box
                    component="a"
                    href={safeHref}
                    target="_blank"
                    rel="noopener noreferrer"
                    sx={getLinkSx(theme)}
                >
                    {children}
                </Box>
            ) : (
                <>{children}</>
            );
        },
        blockquote: ({ children }) => (
            <Box component="blockquote" sx={getBlockquoteSx(theme)}>
                {children}
            </Box>
        ),
        table: ({ children }) => (
            <Box component="table" sx={getTableSx(theme)}>
                {children}
            </Box>
        ),
    }), [isDark, theme, connectionId, databaseName, serverName, connectionMap]);

    if (!content) {return null;}

    return (
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
            {content}
        </ReactMarkdown>
    );
};

// ---------------------------------------------------------------------------
// Component exports only (non-component exports live in MarkdownExports.ts)
// ---------------------------------------------------------------------------

export {
    RunnableCodeBlock,
    ConnectionSelectorCodeBlock,
    CopyCodeButton,
    MarkdownContent,
    AnalysisSkeleton,
};

export type { MarkdownContentProps };
