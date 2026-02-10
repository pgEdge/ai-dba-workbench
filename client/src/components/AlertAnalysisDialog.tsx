/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Alert Analysis Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying AI-powered alert analysis with
 * professional analytics aesthetic and markdown rendering
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useMemo, useState, useCallback } from 'react';
import {
    Box,
    Typography,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    IconButton,
    alpha,
    Skeleton,
    Fade,
    useTheme,
    CircularProgress,
    Tooltip,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    Close as CloseIcon,
    Download as DownloadIcon,
    Psychology as PsychologyIcon,
    Error as ErrorIcon,
    Warning as WarningIcon,
    Info as InfoIcon,
    PlayArrow as PlayArrowIcon,
} from '@mui/icons-material';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { useAlertAnalysis } from '../hooks/useAlertAnalysis';

/**
 * Create a modified Prism theme that removes token background colors.
 * This prevents subtle background color conflicts with our custom backgrounds.
 */
const createCleanTheme = (baseTheme, customBackground) => {
    const cleanTheme = {};
    for (const [key, value] of Object.entries(baseTheme)) {
        if (typeof value === 'object' && value !== null) {
            // Remove background from token styles, keep other properties
            const { background: _background, backgroundColor: _backgroundColor, ...rest } = value;
            cleanTheme[key] = rest;
        } else {
            cleanTheme[key] = value;
        }
    }
    // Set the base code block background
    if (cleanTheme['pre[class*="language-"]']) {
        cleanTheme['pre[class*="language-"]'].background = customBackground;
    }
    if (cleanTheme['code[class*="language-"]']) {
        cleanTheme['code[class*="language-"]'].background = 'transparent';
    }
    return cleanTheme;
};

// Severity color getter using theme palette
const getSeverityColor = (severity, theme) => {
    switch (severity) {
        case 'critical':
            return theme.palette.error.main;
        case 'warning':
            return theme.palette.warning.main;
        default:
            return theme.palette.info.main;
    }
};

// Get severity icon
const getSeverityIcon = (severity) => {
    switch (severity) {
        case 'critical':
            return ErrorIcon;
        case 'warning':
            return WarningIcon;
        default:
            return InfoIcon;
    }
};

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const sxMonoFont = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

const sxH3 = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '0.9375rem',
    mt: 2,
    mb: 0.75,
};

const sxParagraph = {
    color: 'text.primary',
    fontSize: '0.875rem',
    lineHeight: 1.7,
    my: 1,
};

const sxList = {
    pl: 2.5,
    my: 1.5,
    '& li': {
        mb: 0.75,
        fontSize: '0.875rem',
        lineHeight: 1.6,
        color: 'text.primary',
    },
};

const sxUnorderedList = {
    ...sxList,
    listStyleType: 'disc',
};

const sxStrong = { fontWeight: 600 };
const sxEm = { fontStyle: 'italic' };
const sxSkeletonRow = { display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 };
const sxContentFadeBox = { mt: 3 };
const sxSkeletonContainer = { py: 2 };
const sxErrorFlexRow = { display: 'flex', alignItems: 'flex-start', gap: 1.5 };
const sxTitleFlexBox = { flex: 1, minWidth: 0 };
const sxCloseIconSize = { fontSize: 20 };

const getHeadingSx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.secondary.main,
    pb: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.2 : 0.15),
});

const sxH1 = (theme: Theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1.125rem',
    mt: 2,
    mb: 1,
});

const sxH2 = (theme: Theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1rem',
    mt: 2.5,
    mb: 1,
});

const getInlineCodeSx = (theme: Theme) => ({
    ...sxMonoFont,
    fontSize: '0.8125rem',
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

const getCodeBlockWrapperSx = (theme: Theme) => ({
    my: 1.5,
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

const getCodeBlockCustomStyle = (customBackground) => ({
    margin: 0,
    padding: '1rem',
    fontSize: '0.8125rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    background: customBackground,
});

const getLinkSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.dark,
    textDecoration: 'none',
    '&:hover': {
        textDecoration: 'underline',
    },
});

const getBlockquoteSx = (theme: Theme) => ({
    borderLeft: '3px solid',
    borderColor: theme.palette.secondary.main,
    pl: 2,
    ml: 0,
    my: 1.5,
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    fontStyle: 'italic',
});

const getTableSx = (theme: Theme) => ({
    width: '100%',
    borderCollapse: 'collapse',
    my: 1.5,
    fontSize: '0.875rem',
    '& th, & td': {
        border: '1px solid',
        borderColor: theme.palette.mode === 'dark'
            ? theme.palette.grey[700]
            : theme.palette.grey[200],
        p: 1,
        textAlign: 'left',
    },
    '& th': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.5)
            : alpha(theme.palette.grey[100], 0.8),
        fontWeight: 600,
    },
});

const getSkeletonBgSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

const getDialogPaperSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    backgroundImage: 'none',
    borderRadius: 2,
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.divider
        : theme.palette.grey[200],
    boxShadow: theme.palette.mode === 'dark'
        ? '0 25px 50px -12px rgba(0, 0, 0, 0.5)'
        : '0 25px 50px -12px rgba(0, 0, 0, 0.15)',
});

const getDialogTitleSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 2,
    pb: 2,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

const getIconBoxSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 48,
    height: 48,
    borderRadius: 1.5,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.15 : 0.1
    ),
    position: 'relative',
    flexShrink: 0,
});

const getIconColorSx = (theme: Theme) => ({
    fontSize: 28,
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
});

const getSeverityDotSx = (severityColor, theme) => ({
    position: 'absolute',
    top: -4,
    right: -4,
    width: 14,
    height: 14,
    borderRadius: '50%',
    bgcolor: severityColor,
    border: '2px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    boxShadow: `0 0 8px ${alpha(severityColor, 0.5)}`,
});

const sxTitleTypography = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1.125rem',
    lineHeight: 1.3,
};

const sxMetadataRow = {
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    mt: 0.5,
    flexWrap: 'wrap',
};

const sxSeverityBadge = { display: 'flex', alignItems: 'center', gap: 0.5 };

const getServerBadgeSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: alpha(
        theme.palette.grey[500],
        theme.palette.mode === 'dark' ? 0.2 : 0.1
    ),
});

const getDatabaseBadgeSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.1
    ),
});

const getDatabaseTextSx = (theme: Theme) => ({
    fontSize: '0.6875rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    ...sxMonoFont,
});

const getCloseButtonSx = (theme: Theme) => ({
    color: 'text.secondary',
    '&:hover': {
        bgcolor: alpha(
            theme.palette.grey[400],
            0.1
        ),
    },
});

const getContentSx = (theme: Theme) => ({
    p: 3,
    pt: 0,
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
});

const getLoadingBannerSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    mb: 2,
    p: 1.5,
    borderRadius: 1,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05
    ),
    border: '1px solid',
    borderColor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.15
    ),
});

const getPulseDotSx = (theme: Theme) => ({
    width: 8,
    height: 8,
    borderRadius: '50%',
    bgcolor: theme.palette.secondary.main,
    animation: 'pulse 1.5s ease-in-out infinite',
    '@keyframes pulse': {
        '0%, 100%': { opacity: 1 },
        '50%': { opacity: 0.4 },
    },
});

const getLoadingTextSx = (theme: Theme) => ({
    fontSize: '0.8125rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    fontWeight: 500,
});

const getErrorBoxSx = (theme: Theme) => ({
    p: 2.5,
    borderRadius: 1.5,
    bgcolor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05
    ),
    border: '1px solid',
    borderColor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.25 : 0.2
    ),
});

const getErrorTitleSx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.mode === 'dark'
        ? theme.palette.error.light
        : theme.palette.error.dark,
    fontSize: '0.875rem',
    mb: 0.5,
});

const getAnalysisBoxSx = (theme: Theme) => ({
    p: 2.5,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.6)
        : theme.palette.background.paper,
    border: '1px solid',
    borderColor: theme.palette.divider,
    boxShadow: theme.palette.mode === 'dark'
        ? '0 4px 6px -1px rgba(0, 0, 0, 0.2)'
        : '0 1px 3px 0 rgba(0, 0, 0, 0.05)',
});

const getFooterSx = (theme: Theme) => ({
    px: 3,
    py: 2,
    borderTop: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

const getDownloadButtonSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    '&:hover': {
        bgcolor: alpha(
            theme.palette.grey[400],
            0.1
        ),
    },
    '&.Mui-disabled': {
        color: theme.palette.mode === 'dark'
            ? theme.palette.grey[600]
            : theme.palette.grey[300],
    },
});

const sxMetadataSecondRow = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mt: 0.75,
    flexWrap: 'wrap',
};

const sxMonoSmall = {
    fontSize: '0.6875rem',
    color: 'text.secondary',
    ...sxMonoFont,
};

const sxThresholdText = {
    fontSize: '0.6875rem',
    color: 'text.disabled',
    ...sxMonoFont,
};

// ---------------------------------------------------------------------------
// SQL detection helpers
// ---------------------------------------------------------------------------

const SQL_KEYWORDS_RE = /^(SELECT|WITH|SHOW|EXPLAIN|SET|ALTER|CREATE|DROP|INSERT|UPDATE|DELETE|VACUUM|ANALYZE|REINDEX|CLUSTER)\b/i;

/**
 * Regex matching the start of a SQL statement keyword.
 * Used by extractExecutableSQL to identify valid SQL chunks.
 */
const SQL_STATEMENT_KEYWORDS = /^\s*(SELECT|WITH|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|EXPLAIN|SET|VACUUM|REINDEX|GRANT|REVOKE|TRUNCATE|CLUSTER|REFRESH|COMMENT|TABLE|ANALYZE)\b/i;

/**
 * Extract only executable SQL from a code block.
 *
 * The LLM sometimes mixes configuration file entries or shell commands
 * into SQL code blocks.  This function splits the content on semicolons,
 * keeps only the chunks that contain a recognised SQL keyword, and
 * reassembles the result.
 */
const extractExecutableSQL = (code: string): string => {
    const parts = code.split(';');
    const sqlParts: string[] = [];

    for (const part of parts) {
        const trimmed = part.trim();
        if (!trimmed) continue;

        // Strip comment-only lines so we can inspect the real content
        const contentLines = trimmed
            .split('\n')
            .filter((line) => {
                const t = line.trim();
                return t && !t.startsWith('--');
            });

        const content = contentLines.join('\n').trim();
        if (content && SQL_STATEMENT_KEYWORDS.test(content)) {
            sqlParts.push(trimmed);
        }
    }

    return sqlParts.map((p) => p + ';').join('\n\n');
};

/**
 * Determine whether a code block contains SQL.
 * Returns true when the block has a `language-sql` class, or when it has
 * no language tag and the trimmed content starts with a common SQL keyword.
 */
const isSqlCodeBlock = (className: string | undefined, content: string): boolean => {
    const langMatch = /language-(\w+)/.exec(className || '');
    if (langMatch) {
        return langMatch[1].toLowerCase() === 'sql';
    }
    // No language tag -- check for SQL keyword at start
    if (!className) {
        return SQL_KEYWORDS_RE.test(content.trim());
    }
    return false;
};

/**
 * Return the language string extracted from a className, or empty string.
 */
const extractLanguage = (className: string | undefined): string => {
    const match = /language-(\w+)/.exec(className || '');
    return match ? match[1] : '';
};

// ---------------------------------------------------------------------------
// Query result types
// ---------------------------------------------------------------------------

interface StatementResult {
    columns?: string[];
    rows?: string[][];
    row_count?: number;
    truncated?: boolean;
    query: string;
    error?: string;
}

interface QueryResponse {
    results?: StatementResult[];
    total_statements?: number;
    requires_confirmation?: boolean;
    write_statements?: string[];
    confirmation_message?: string;
}

interface QueryState {
    loading: boolean;
    response: QueryResponse | null;
    error: string | null;
    pendingConfirmation: boolean;
    writeStatements: string[];
}

// ---------------------------------------------------------------------------
// Style helpers for RunnableCodeBlock
// ---------------------------------------------------------------------------

const getRunButtonSx = (theme: Theme) => ({
    position: 'absolute',
    top: 6,
    right: 6,
    minWidth: 0,
    width: 28,
    height: 28,
    p: 0,
    borderRadius: 0.75,
    bgcolor: alpha(
        theme.palette.background.paper,
        theme.palette.mode === 'dark' ? 0.6 : 0.8,
    ),
    color: alpha(theme.palette.secondary.main, 0.7),
    opacity: 0.5,
    transition: 'opacity 0.15s, background-color 0.15s, color 0.15s',
    '&:hover': {
        opacity: 1,
        bgcolor: alpha(
            theme.palette.background.paper,
            theme.palette.mode === 'dark' ? 0.85 : 0.95,
        ),
        color: theme.palette.secondary.main,
    },
});

const getQueryResultWrapperSx = (theme: Theme) => ({
    mt: 0,
    mb: 1.5,
    border: '1px solid',
    borderTop: 'none',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
    borderRadius: '0 0 4px 4px',
    overflow: 'hidden',
});

const getQueryResultHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    px: 1.5,
    py: 0.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[800], 0.6)
        : alpha(theme.palette.grey[100], 0.8),
    borderBottom: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

const getQueryErrorSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1,
    px: 1.5,
    py: 1,
    bgcolor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05,
    ),
});

const getConfirmationPanelSx = (theme: Theme) => ({
    px: 1.5,
    py: 1.25,
    bgcolor: alpha(
        theme.palette.warning.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.06,
    ),
    borderTop: '1px solid',
    borderColor: alpha(
        theme.palette.warning.main,
        theme.palette.mode === 'dark' ? 0.25 : 0.2,
    ),
});

const getConfirmationTitleSx = (_theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1,
    mb: 0.75,
});

const getConfirmationTextSx = (theme: Theme) => ({
    fontSize: '0.8125rem',
    fontWeight: 500,
    color: theme.palette.mode === 'dark'
        ? theme.palette.warning.light
        : theme.palette.warning.dark,
});

const getConfirmationStatementSx = (theme: Theme) => ({
    fontSize: '0.75rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[300]
        : theme.palette.grey[700],
    ...sxMonoFont,
    pl: 3,
    py: 0.25,
});

const sxConfirmationActions = {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 1,
    mt: 1,
};

// ---------------------------------------------------------------------------
// Components
// ---------------------------------------------------------------------------

/**
 * RunnableCodeBlock - a code block wrapper that adds a Run button for SQL
 * and displays query results inline beneath the code.
 */
interface RunnableCodeBlockProps {
    codeContent: string;
    language: string;
    isDark: boolean;
    connectionId: number;
    databaseName?: string;
    serverName?: string;
    syntaxTheme: Record<string, unknown>;
    customBackground: string;
    theme: Theme;
    isSql: boolean;
    props: Record<string, unknown>;
}

const RunnableCodeBlock: React.FC<RunnableCodeBlockProps> = ({
    codeContent,
    language,
    isDark: _isDark,
    connectionId,
    databaseName,
    serverName,
    syntaxTheme,
    customBackground,
    theme,
    isSql,
    props,
}) => {
    const [queryState, setQueryState] = useState<QueryState>({
        loading: false,
        response: null,
        error: null,
        pendingConfirmation: false,
        writeStatements: [],
    });

    // Store the cleaned SQL so the confirmation "Execute" button can
    // re-send the same payload with confirmed=true.
    const [cleanedSQL, setCleanedSQL] = useState<string>('');

    const handleRun = useCallback(async (confirmed = false) => {
        // On the initial run, extract executable SQL from the code block.
        // When re-running after confirmation, reuse the previously cleaned SQL.
        const sql = confirmed ? cleanedSQL : extractExecutableSQL(codeContent);
        if (!confirmed) {
            setCleanedSQL(sql);
        }

        if (!sql.trim()) {
            setQueryState({
                loading: false,
                response: null,
                error: 'No executable SQL found in this code block',
                pendingConfirmation: false,
                writeStatements: [],
            });
            return;
        }

        setQueryState({
            loading: true,
            response: null,
            error: null,
            pendingConfirmation: false,
            writeStatements: [],
        });

        try {
            const body: Record<string, unknown> = { query: sql };
            if (databaseName) {
                body.database_name = databaseName;
            }
            if (confirmed) {
                body.confirmed = true;
            }

            const response = await fetch(
                `/api/v1/connections/${connectionId}/query`,
                {
                    method: 'POST',
                    credentials: 'include',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body),
                },
            );

            if (!response.ok) {
                let errorMessage = `Query failed with status ${response.status}`;
                try {
                    const errorData = await response.json();
                    if (errorData.error) {
                        errorMessage = errorData.error;
                    }
                } catch {
                    // If JSON parsing fails, try plain text
                    const errorText = await response.text();
                    if (errorText) {
                        errorMessage = errorText;
                    }
                }
                throw new Error(errorMessage);
            }

            const data: QueryResponse = await response.json();

            if (data.requires_confirmation) {
                setQueryState({
                    loading: false,
                    response: null,
                    error: null,
                    pendingConfirmation: true,
                    writeStatements: data.write_statements || [],
                });
                return;
            }

            setQueryState({
                loading: false,
                response: data,
                error: null,
                pendingConfirmation: false,
                writeStatements: [],
            });
        } catch (err) {
            setQueryState({
                loading: false,
                response: null,
                error: (err as Error).message,
                pendingConfirmation: false,
                writeStatements: [],
            });
        }
    }, [codeContent, connectionId, databaseName, cleanedSQL]);

    const handleDismiss = useCallback(() => {
        setQueryState({
            loading: false,
            response: null,
            error: null,
            pendingConfirmation: false,
            writeStatements: [],
        });
    }, []);

    const tooltipTitle = serverName
        ? databaseName
            ? `Run on ${serverName}/${databaseName}`
            : `Run on ${serverName}`
        : 'Run query';

    return (
        <>
            <Box sx={{
                ...getCodeBlockWrapperSx(theme),
                position: 'relative',
                // Remove bottom border radius when results, errors, or
                // a confirmation prompt are showing
                ...((queryState.response || queryState.error || queryState.pendingConfirmation) ? {
                    borderBottomLeftRadius: 0,
                    borderBottomRightRadius: 0,
                    mb: 0,
                } : {}),
            }}>
                <SyntaxHighlighter
                    style={syntaxTheme}
                    language={language || 'sql'}
                    PreTag="div"
                    customStyle={getCodeBlockCustomStyle(customBackground)}
                    {...props}
                >
                    {codeContent}
                </SyntaxHighlighter>
                {isSql && (
                    <Tooltip title={tooltipTitle} placement="left">
                        <span style={{ position: 'absolute', top: 6, right: 6 }}>
                            <IconButton
                                size="small"
                                onClick={() => handleRun()}
                                disabled={queryState.loading}
                                sx={getRunButtonSx(theme)}
                            >
                                {queryState.loading ? (
                                    <CircularProgress
                                        size={14}
                                        sx={{ color: alpha(theme.palette.secondary.main, 0.7) }}
                                    />
                                ) : (
                                    <PlayArrowIcon sx={{ fontSize: 18 }} />
                                )}
                            </IconButton>
                        </span>
                    </Tooltip>
                )}
            </Box>

            {/* Write-statement confirmation prompt */}
            {queryState.pendingConfirmation && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getConfirmationPanelSx(theme)}>
                        <Box sx={getConfirmationTitleSx(theme)}>
                            <WarningIcon sx={{
                                fontSize: 18,
                                color: theme.palette.warning.main,
                                mt: 0.125,
                                flexShrink: 0,
                            }} />
                            <Typography sx={getConfirmationTextSx(theme)}>
                                This code block contains statements that modify the database:
                            </Typography>
                        </Box>
                        {queryState.writeStatements.map((stmt, idx) => (
                            <Typography key={idx} sx={getConfirmationStatementSx(theme)}>
                                {'\u2022'} {stmt}
                            </Typography>
                        ))}
                        <Box sx={sxConfirmationActions}>
                            <Button
                                size="small"
                                onClick={handleDismiss}
                                sx={{ textTransform: 'none' }}
                            >
                                Cancel
                            </Button>
                            <Button
                                size="small"
                                variant="contained"
                                color="warning"
                                onClick={() => handleRun(true)}
                                sx={{ textTransform: 'none' }}
                            >
                                Execute
                            </Button>
                        </Box>
                    </Box>
                </Box>
            )}

            {/* Query results - multiple statement results */}
            {queryState.response && queryState.response.results && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryResultHeaderSx(theme)}>
                        <Typography sx={{
                            fontSize: '0.75rem',
                            color: 'text.secondary',
                            ...sxMonoFont,
                        }}>
                            {queryState.response.total_statements} statement{queryState.response.total_statements !== 1 ? 's' : ''}
                        </Typography>
                        <IconButton
                            size="small"
                            onClick={handleDismiss}
                            sx={{ p: 0.25, color: 'text.secondary' }}
                        >
                            <CloseIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                    {queryState.response.results.map((result, idx) => (
                        <Box key={idx}>
                            {/* Query label */}
                            <Typography sx={{
                                fontSize: '0.6875rem',
                                color: 'text.disabled',
                                ...sxMonoFont,
                                px: 1.5,
                                py: 0.5,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                borderTop: idx > 0 ? '1px solid' : 'none',
                                borderColor: theme.palette.mode === 'dark'
                                    ? theme.palette.grey[700]
                                    : theme.palette.grey[200],
                            }}>
                                {result.query}
                            </Typography>

                            {/* Statement error */}
                            {result.error && (
                                <Box sx={getQueryErrorSx(theme)}>
                                    <ErrorIcon sx={{
                                        fontSize: 16,
                                        color: theme.palette.error.main,
                                        mt: 0.25,
                                        flexShrink: 0,
                                    }} />
                                    <Typography sx={{
                                        fontSize: '0.75rem',
                                        color: theme.palette.mode === 'dark'
                                            ? theme.palette.error.light
                                            : theme.palette.error.dark,
                                        flex: 1,
                                        ...sxMonoFont,
                                        wordBreak: 'break-word',
                                    }}>
                                        {result.error}
                                    </Typography>
                                </Box>
                            )}

                            {/* Statement results */}
                            {result.columns && result.rows && (
                                <>
                                    <Box sx={{ overflowX: 'auto' }}>
                                        <Box component="table" sx={getTableSx(theme)}>
                                            <thead>
                                                <tr>
                                                    {result.columns.map((col, i) => (
                                                        <th key={i}>
                                                            <Typography sx={{
                                                                fontSize: '0.75rem',
                                                                fontWeight: 600,
                                                                ...sxMonoFont,
                                                                whiteSpace: 'nowrap',
                                                            }}>
                                                                {col}
                                                            </Typography>
                                                        </th>
                                                    ))}
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {result.rows.map((row, ri) => (
                                                    <tr key={ri}>
                                                        {row.map((cell, ci) => (
                                                            <td key={ci}>
                                                                <Typography sx={{
                                                                    fontSize: '0.75rem',
                                                                    ...sxMonoFont,
                                                                    whiteSpace: 'nowrap',
                                                                }}>
                                                                    {cell}
                                                                </Typography>
                                                            </td>
                                                        ))}
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </Box>
                                    </Box>
                                    {result.truncated && (
                                        <Typography sx={{
                                            fontSize: '0.6875rem',
                                            color: 'text.disabled',
                                            px: 1.5,
                                            py: 0.5,
                                        }}>
                                            Results limited to {result.row_count} rows
                                        </Typography>
                                    )}
                                    {!result.truncated && result.row_count !== undefined && (
                                        <Typography sx={{
                                            fontSize: '0.6875rem',
                                            color: 'text.disabled',
                                            px: 1.5,
                                            py: 0.5,
                                        }}>
                                            {result.row_count} row{result.row_count !== 1 ? 's' : ''}
                                        </Typography>
                                    )}
                                </>
                            )}
                        </Box>
                    ))}
                </Box>
            )}

            {/* Top-level fetch error (network failure, non-200 status) */}
            {queryState.error && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryErrorSx(theme)}>
                        <ErrorIcon sx={{
                            fontSize: 16,
                            color: theme.palette.error.main,
                            mt: 0.25,
                            flexShrink: 0,
                        }} />
                        <Typography sx={{
                            fontSize: '0.75rem',
                            color: theme.palette.mode === 'dark'
                                ? theme.palette.error.light
                                : theme.palette.error.dark,
                            flex: 1,
                            ...sxMonoFont,
                            wordBreak: 'break-word',
                        }}>
                            {queryState.error}
                        </Typography>
                        <IconButton
                            size="small"
                            onClick={handleDismiss}
                            sx={{ p: 0.25, color: 'text.secondary', flexShrink: 0 }}
                        >
                            <CloseIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                </Box>
            )}
        </>
    );
};

/**
 * Styled markdown content component using react-markdown
 */
interface MarkdownContentProps {
    content: string;
    isDark: boolean;
    connectionId?: number;
    databaseName?: string;
    serverName?: string;
}

const MarkdownContent: React.FC<MarkdownContentProps> = ({
    content,
    isDark,
    connectionId,
    databaseName,
    serverName,
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
        code: ({ inline, className, children, ...props }) => {
            const language = extractLanguage(className);
            const codeString = String(children).replace(/\n$/, '');

            // Custom backgrounds for code blocks
            const customBackground = isDark
                ? theme.palette.background.paper
                : theme.palette.grey[50];

            // Create clean themes without token background colors
            const cleanTheme = isDark
                ? createCleanTheme(oneDark, theme.palette.background.paper)
                : createCleanTheme(oneLight, theme.palette.grey[50]);

            if (inline) {
                return (
                    <Box component="code" sx={getInlineCodeSx(theme)}>
                        {children}
                    </Box>
                );
            }

            const sqlDetected = isSqlCodeBlock(className, codeString);

            if (sqlDetected && connectionId) {
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
    }), [isDark, theme, connectionId, databaseName, serverName]);

    if (!content) {return null;}

    return (
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
            {content}
        </ReactMarkdown>
    );
};

/**
 * Loading skeleton for analysis content
 */
const AnalysisSkeleton = () => {
    const theme = useTheme();
    const skeletonBg = getSkeletonBgSx(theme);

    return (
        <Box sx={sxSkeletonContainer}>
            {/* Summary section */}
            <Skeleton variant="text" width="30%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            <Skeleton variant="text" width="100%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="85%" height={20} sx={{ ...skeletonBg, mb: 2.5 }} />

            {/* Analysis section */}
            <Skeleton variant="text" width="25%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            <Skeleton variant="text" width="100%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="90%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="75%" height={20} sx={{ ...skeletonBg, mb: 2.5 }} />

            {/* Remediation section */}
            <Skeleton variant="text" width="35%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            {[1, 2, 3].map((i) => (
                <Box key={i} sx={sxSkeletonRow}>
                    <Skeleton variant="circular" width={8} height={8} sx={skeletonBg} />
                    <Skeleton variant="text" width={`${85 - i * 10}%`} height={20} sx={skeletonBg} />
                </Box>
            ))}
        </Box>
    );
};

/**
 * AlertAnalysisDialog - Dialog for displaying AI-powered alert analysis
 */
interface AlertAnalysisDialogProps {
    open: boolean;
    alert: Record<string, unknown> | null;
    onClose: () => void;
    onAnalysisComplete?: (alertId: number, analysis: string) => void;
    isDark: boolean;
}

const AlertAnalysisDialog: React.FC<AlertAnalysisDialogProps> = ({ open, alert, onClose, onAnalysisComplete, isDark }) => {
    const theme = useTheme();
    const { analysis, loading, error, analyze, reset } = useAlertAnalysis();

    // Trigger analysis when dialog opens with an alert
    useEffect(() => {
        if (open && alert) {
            analyze(alert);
        }
    }, [open, alert, analyze]);

    // Notify parent when analysis completes so the alert list updates
    useEffect(() => {
        if (analysis && alert?.id != null && onAnalysisComplete) {
            onAnalysisComplete(alert.id as number, analysis);
        }
    }, [analysis, alert, onAnalysisComplete]);

    // Reset state when dialog closes
    const handleClose = () => {
        reset();
        onClose();
    };

    // Download analysis as markdown file
    const handleDownload = () => {
        if (!analysis || !alert) {return;}

        const timestamp = new Date().toISOString().split('T')[0];
        const filename = `alert-analysis-${alert.id || 'unknown'}-${timestamp}.md`;

        // Build optional fields
        const serverLine = alert.server ? `- **Server:** ${alert.server}\n` : '';
        const databaseLine = alert.databaseName ? `- **Database:** ${alert.databaseName}\n` : '';
        const unit = alert.metricUnit || '';
        const metricDisplay = alert.metricValue !== undefined
            ? `${alert.metricValue}${unit ? ` ${unit}` : ''}`
            : 'N/A';
        const thresholdDisplay = alert.thresholdValue !== undefined
            ? `${alert.operator || '>'} ${alert.thresholdValue}${unit ? ` ${unit}` : ''}`
            : 'N/A';

        const content = `# Alert Analysis Report

## Alert Details

- **Title:** ${alert.title || 'N/A'}
- **Severity:** ${alert.severity || 'N/A'}
${serverLine}${databaseLine}- **Alert Type:** ${alert.alertType || 'threshold'}
- **Metric Value:** ${metricDisplay}
- **Threshold:** ${thresholdDisplay}
- **Triggered At:** ${alert.triggeredAt || alert.time || 'N/A'}

---

${analysis}

---

*Generated by pgEdge AI DBA Workbench on ${new Date().toISOString()}*
`;

        const blob = new Blob([content], { type: 'text/markdown' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
    };

    const severityColor = getSeverityColor(alert?.severity, theme);
    const SeverityIcon = getSeverityIcon(alert?.severity);

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="md"
            fullWidth
            PaperProps={{
                sx: getDialogPaperSx(theme),
            }}
        >
            {/* Header */}
            <DialogTitle sx={getDialogTitleSx(theme)}>
                {/* Icon with severity indicator */}
                <Box sx={getIconBoxSx(theme)}>
                    <PsychologyIcon sx={getIconColorSx(theme)} />
                    {/* Severity dot */}
                    <Box sx={getSeverityDotSx(severityColor, theme)} />
                </Box>

                {/* Title and metadata */}
                <Box sx={sxTitleFlexBox}>
                    <Typography variant="h6" sx={sxTitleTypography}>
                        Alert Analysis
                    </Typography>
                    {/* First row: severity, title, time */}
                    <Box sx={sxMetadataRow}>
                        <Box sx={sxSeverityBadge}>
                            <SeverityIcon sx={{ fontSize: 14, color: severityColor }} />
                            <Typography
                                sx={{
                                    fontSize: '0.8125rem',
                                    color: severityColor,
                                    fontWeight: 500,
                                    textTransform: 'capitalize',
                                }}
                            >
                                {alert?.severity || 'Unknown'}
                            </Typography>
                        </Box>
                        <Typography sx={{ fontSize: '0.75rem', color: 'text.secondary' }}>
                            {alert?.title || 'Alert'}
                        </Typography>
                        {alert?.time && (
                            <Typography sx={{ fontSize: '0.75rem', color: 'text.disabled' }}>
                                {alert.time}
                            </Typography>
                        )}
                    </Box>
                    {/* Second row: server, database, threshold info */}
                    <Box sx={sxMetadataSecondRow}>
                        {alert?.server && (
                            <Box sx={getServerBadgeSx(theme)}>
                                <Typography sx={sxMonoSmall}>
                                    {alert.server}
                                </Typography>
                            </Box>
                        )}
                        {alert?.databaseName && (
                            <Box sx={getDatabaseBadgeSx(theme)}>
                                <Typography sx={getDatabaseTextSx(theme)}>
                                    {alert.databaseName}
                                </Typography>
                            </Box>
                        )}
                        {alert?.metricValue !== undefined && alert?.thresholdValue !== undefined && (
                            <Typography sx={sxThresholdText}>
                                {typeof alert.metricValue === 'number'
                                    ? alert.metricValue.toLocaleString(undefined, { maximumFractionDigits: 2 })
                                    : alert.metricValue}
                                {alert.metricUnit && ` ${alert.metricUnit}`}
                                {' '}{alert.operator === '>' ? '>' : alert.operator === '<' ? '<' : '='}{' '}
                                {typeof alert.thresholdValue === 'number'
                                    ? alert.thresholdValue.toLocaleString(undefined, { maximumFractionDigits: 2 })
                                    : alert.thresholdValue}
                                {alert.metricUnit && ` ${alert.metricUnit}`}
                            </Typography>
                        )}
                    </Box>
                </Box>

                {/* Close button */}
                <IconButton
                    onClick={handleClose}
                    size="small"
                    sx={getCloseButtonSx(theme)}
                >
                    <CloseIcon sx={sxCloseIconSize} />
                </IconButton>
            </DialogTitle>

            {/* Content */}
            <DialogContent sx={getContentSx(theme)}>
                <Fade in={true} timeout={300}>
                    <Box sx={sxContentFadeBox}>
                        {loading && (
                            <Box>
                                <Box sx={getLoadingBannerSx(theme)}>
                                    <Box sx={getPulseDotSx(theme)} />
                                    <Typography sx={getLoadingTextSx(theme)}>
                                        Analyzing alert and gathering context...
                                    </Typography>
                                </Box>
                                <AnalysisSkeleton />
                            </Box>
                        )}

                        {error && !loading && (
                            <Box sx={getErrorBoxSx(theme)}>
                                <Box sx={sxErrorFlexRow}>
                                    <ErrorIcon sx={{ fontSize: 20, color: theme.palette.error.main, mt: 0.25 }} />
                                    <Box>
                                        <Typography sx={getErrorTitleSx(theme)}>
                                            Analysis Failed
                                        </Typography>
                                        <Typography
                                            sx={{
                                                color: 'text.secondary',
                                                fontSize: '0.8125rem',
                                            }}
                                        >
                                            {error}
                                        </Typography>
                                    </Box>
                                </Box>
                            </Box>
                        )}

                        {analysis && !loading && (
                            <Box sx={getAnalysisBoxSx(theme)}>
                                <MarkdownContent
                                    content={analysis}
                                    isDark={isDark}
                                    connectionId={alert?.connectionId as number | undefined}
                                    databaseName={alert?.databaseName as string | undefined}
                                    serverName={alert?.server as string | undefined}
                                />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </DialogContent>

            {/* Footer */}
            <DialogActions sx={getFooterSx(theme)}>
                <Button
                    onClick={handleDownload}
                    startIcon={<DownloadIcon />}
                    disabled={!analysis || loading}
                    size="small"
                    sx={getDownloadButtonSx(theme)}
                >
                    Download
                </Button>
                <Button
                    onClick={handleClose}
                    variant="contained"
                    size="small"
                >
                    Close
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default AlertAnalysisDialog;
