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
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

import RunnableCodeBlock from './RunnableCodeBlock';
import ConnectionSelectorCodeBlock from './ConnectionSelectorCodeBlock';
import AnalysisSkeleton from './AnalysisSkeleton';
import { isSqlCodeBlock, extractLanguage } from './sqlDetection';
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
    getLinkSx,
    getBlockquoteSx,
    getTableSx,
} from './markdownStyles';

/**
 * Create a modified Prism theme that removes token background colors.
 * This prevents subtle background color conflicts with our custom backgrounds.
 */
const createCleanTheme = (
    baseTheme: Record<string, unknown>,
    customBackground: string,
): Record<string, unknown> => {
    const cleanTheme: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(baseTheme)) {
        if (typeof value === 'object' && value !== null) {
            // Remove background from token styles, keep other properties
            const {
                background: _background,
                backgroundColor: _backgroundColor,
                ...rest
            } = value as Record<string, unknown>;
            cleanTheme[key] = rest;
        } else {
            cleanTheme[key] = value;
        }
    }
    // Set the base code block background
    const preKey = 'pre[class*="language-"]';
    const codeKey = 'code[class*="language-"]';
    if (cleanTheme[preKey] && typeof cleanTheme[preKey] === 'object') {
        (cleanTheme[preKey] as Record<string, unknown>).background =
            customBackground;
    }
    if (cleanTheme[codeKey] && typeof cleanTheme[codeKey] === 'object') {
        (cleanTheme[codeKey] as Record<string, unknown>).background =
            'transparent';
    }
    return cleanTheme;
};

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
                <Box sx={getCodeBlockWrapperSx(theme)}>
                    <SyntaxHighlighter
                        style={cleanTheme}
                        language={language || 'sql'}
                        PreTag="div"
                        customStyle={getCodeBlockCustomStyle(customBackground)}
                        {...props}
                    >
                        {codeString}
                    </SyntaxHighlighter>
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
        a: ({ href, children }) => (
            <Box
                component="a"
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                sx={getLinkSx(theme)}
            >
                {children}
            </Box>
        ),
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
// Re-exports: all consumers can continue importing from this file
// ---------------------------------------------------------------------------

// Components
export {
    RunnableCodeBlock,
    ConnectionSelectorCodeBlock,
    MarkdownContent,
    AnalysisSkeleton,
};

// Utility functions
export { createCleanTheme };
export {
    extractExecutableSQL,
    isSqlCodeBlock,
    extractLanguage,
    SQL_KEYWORDS_RE,
    SQL_STATEMENT_KEYWORDS,
} from './sqlDetection';

// Style constants and getters
export {
    sxMonoFont,
    sxH3,
    sxParagraph,
    sxList,
    sxUnorderedList,
    sxStrong,
    sxEm,
    sxConfirmationActions,
    sxContentFadeBox,
    sxErrorFlexRow,
    sxTitleFlexBox,
    sxCloseIconSize,
    sxTitleTypography,
    getHeadingSx,
    sxH1,
    sxH2,
    getInlineCodeSx,
    getCodeBlockWrapperSx,
    getCodeBlockCustomStyle,
    getLinkSx,
    getBlockquoteSx,
    getTableSx,
    getRunButtonSx,
    getQueryResultWrapperSx,
    getQueryResultHeaderSx,
    getQueryErrorSx,
    getConfirmationPanelSx,
    getConfirmationTitleSx,
    getConfirmationTextSx,
    getConfirmationStatementSx,
    getSkeletonBgSx,
    getDialogPaperSx,
    getDialogTitleSx,
    getIconBoxSx,
    getIconColorSx,
    getContentSx,
    getLoadingBannerSx,
    getPulseDotSx,
    getLoadingTextSx,
    getErrorBoxSx,
    getErrorTitleSx,
    getAnalysisBoxSx,
    getFooterSx,
    getDownloadButtonSx,
    getCloseButtonSx,
} from './markdownStyles';

// Type exports
export type { MarkdownContentProps };
export type { QueryResponse, StatementResult, QueryState } from './RunnableCodeBlock';
