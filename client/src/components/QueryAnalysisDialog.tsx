/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Query Analysis Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying AI-powered query performance analysis
 * with professional analytics aesthetic and markdown rendering
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useRef } from 'react';
import {
    Box,
    Typography,
    Dialog,
    AppBar,
    Toolbar,
    IconButton,
    alpha,
    Fade,
    useTheme,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    Close as CloseIcon,
    Download as DownloadIcon,
    Psychology as PsychologyIcon,
    Error as ErrorIcon,
} from '@mui/icons-material';
import {
    useQueryAnalysis,
    QueryAnalysisInput,
} from '../hooks/useQueryAnalysis';
import { formatTime } from '../utils/formatters';
import { slugify } from '../utils/textHelpers';
import {
    MarkdownContent,
    AnalysisSkeleton,
} from './shared/MarkdownContent';
import {
    sxMonoFont,
    sxErrorFlexRow,
    getIconBoxSx,
    getIconColorSx,
    getLoadingBannerSx,
    getPulseDotSx,
    getLoadingTextSx,
    getErrorBoxSx,
    getErrorTitleSx,
    getAnalysisBoxSx,
    getDownloadButtonSx,
} from './shared/MarkdownExports';
import SlideTransition from './shared/SlideTransition';
import { downloadAsMarkdown } from '../utils/downloadMarkdown';

const TOOL_LABELS = [
    'Querying metrics',
    'Fetching metric baselines',
    'Querying database',
    'Inspecting schema',
    'Validating query',
    'Searching knowledgebase',
];

// ---------------------------------------------------------------------------
// Query-specific style constants and style-getter functions
// ---------------------------------------------------------------------------

const getConnectionBadgeSx = (theme: Theme) => ({
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
    fontSize: '0.875rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    ...sxMonoFont,
});

const sxMonoSmall = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    ...sxMonoFont,
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface QueryAnalysisDialogProps {
    open: boolean;
    onClose: () => void;
    isDark: boolean;
    queryText: string;
    queryId: string;
    stats: {
        calls: number;
        totalExecTime: number;
        meanExecTime: number;
        rows: number;
        sharedBlksHit: number;
        sharedBlksRead: number;
    };
    connectionId: number;
    connectionName?: string;
    databaseName: string;
}

const QueryAnalysisDialog: React.FC<QueryAnalysisDialogProps> = ({
    open,
    onClose,
    isDark,
    queryText,
    queryId,
    stats,
    connectionId,
    connectionName,
    databaseName,
}) => {
    const theme = useTheme();
    const { analysis, loading, error, progressMessage, activeTools, analyze } =
        useQueryAnalysis();

    // Trigger analysis once when dialog opens; ignore subsequent changes
    // while the dialog remains open.
    const hasTriggeredRef = useRef(false);

    useEffect(() => {
        if (open && !hasTriggeredRef.current) {
            hasTriggeredRef.current = true;
            if (!analysis) {
                const input: QueryAnalysisInput = {
                    queryText,
                    queryId,
                    calls: stats.calls,
                    totalExecTime: stats.totalExecTime,
                    meanExecTime: stats.meanExecTime,
                    rows: stats.rows,
                    sharedBlksHit: stats.sharedBlksHit,
                    sharedBlksRead: stats.sharedBlksRead,
                    connectionId,
                    databaseName,
                };
                analyze(input);
            }
        }
        if (!open) {
            hasTriggeredRef.current = false;
        }
    }, [open, analysis, queryText, queryId, stats, connectionId,
        databaseName, analyze]);

    const handleClose = () => {
        onClose();
    };

    // Download analysis as markdown file
    const handleDownload = () => {
        if (!analysis) { return; }

        const timestamp = new Date().toISOString().split('T')[0];
        const slug = slugify(queryId || 'query');
        const filename = `query-analysis-${slug}-${timestamp}.md`;

        const connectionLine = connectionName
            ? `- **Connection:** ${connectionName}\n`
            : '';
        const databaseLine = databaseName
            ? `- **Database:** ${databaseName}\n`
            : '';

        const content = `# Query Analysis Report

## Query Details

- **Query ID:** ${queryId}
${connectionLine}${databaseLine}- **Total Calls:** ${stats.calls.toLocaleString()}
- **Total Execution Time:** ${formatTime(stats.totalExecTime)}
- **Mean Execution Time:** ${formatTime(stats.meanExecTime)}

### Query Text

\`\`\`sql
${queryText}
\`\`\`

---

${analysis}

---

*Generated by pgEdge AI DBA Workbench on ${new Date().toISOString()}*
`;

        downloadAsMarkdown(content, filename);
    };

    return (
        <Dialog
            fullScreen
            open={open}
            onClose={handleClose}
            TransitionComponent={SlideTransition}
        >
            {/* AppBar Header */}
            <AppBar
                position="static"
                elevation={0}
                sx={{
                    bgcolor: 'background.paper',
                    borderBottom: '1px solid',
                    borderColor: 'divider',
                }}
            >
                <Toolbar
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 1.5,
                        flexWrap: 'wrap',
                    }}
                >
                    {/* Close button */}
                    <IconButton
                        edge="start"
                        onClick={handleClose}
                        aria-label="close query analysis"
                        sx={{ color: 'text.secondary' }}
                    >
                        <CloseIcon />
                    </IconButton>

                    {/* Icon */}
                    <Box sx={getIconBoxSx(theme)}>
                        <PsychologyIcon sx={getIconColorSx(theme)} />
                    </Box>

                    {/* Title */}
                    <Typography
                        variant="h6"
                        sx={{
                            fontWeight: 600,
                            fontSize: '1.125rem',
                            color: 'text.primary',
                            whiteSpace: 'nowrap',
                        }}
                    >
                        Query analysis
                    </Typography>

                    {/* Connection pill */}
                    {connectionName && (
                        <Box sx={getConnectionBadgeSx(theme)}>
                            <Typography sx={sxMonoSmall}>
                                {connectionName}
                            </Typography>
                        </Box>
                    )}

                    {/* Database pill */}
                    {databaseName && (
                        <Box sx={getDatabaseBadgeSx(theme)}>
                            <Typography sx={getDatabaseTextSx(theme)}>
                                {databaseName}
                            </Typography>
                        </Box>
                    )}

                    {/* Spacer */}
                    <Box sx={{ flexGrow: 1 }} />

                    {/* Download button */}
                    <IconButton
                        onClick={handleDownload}
                        disabled={!analysis || loading}
                        aria-label="download analysis"
                        sx={getDownloadButtonSx(theme)}
                    >
                        <DownloadIcon />
                    </IconButton>
                </Toolbar>
            </AppBar>

            {/* Scrollable Content */}
            <Box
                sx={{
                    flex: 1,
                    overflow: 'auto',
                    bgcolor: theme.palette.mode === 'dark'
                        ? theme.palette.background.default
                        : theme.palette.grey[50],
                    px: 3,
                    pt: 1.5,
                    pb: 3,
                    '&::-webkit-scrollbar': { width: 6 },
                    '&::-webkit-scrollbar-thumb': {
                        borderRadius: 3,
                        backgroundColor: theme.palette.mode === 'dark'
                            ? '#475569'
                            : '#D1D5DB',
                    },
                    '&::-webkit-scrollbar-track': {
                        backgroundColor: 'transparent',
                    },
                }}
            >
                <Fade in={true} timeout={300}>
                    <Box sx={{ mt: 1.5, maxWidth: 900, mx: 'auto' }}>
                        {loading && (
                            <Box>
                                <Box sx={getLoadingBannerSx(theme)}>
                                    <Box sx={getPulseDotSx(theme)} />
                                    <Box sx={{ flex: 1 }}>
                                        <Typography
                                            sx={getLoadingTextSx(theme)}
                                        >
                                            {progressMessage}
                                        </Typography>
                                        <Box sx={{
                                            display: 'flex',
                                            flexWrap: 'wrap',
                                            gap: 0.5,
                                            mt: 1,
                                        }}>
                                            {TOOL_LABELS.map(label => {
                                                const isActive =
                                                    activeTools.includes(label);
                                                return (
                                                    <Box
                                                        key={label}
                                                        sx={{
                                                            px: 1,
                                                            py: 0.25,
                                                            borderRadius: 0.75,
                                                            fontSize: '0.75rem',
                                                            fontWeight: 500,
                                                            fontFamily:
                                                                '"JetBrains Mono", "SF Mono", monospace',
                                                            border: '1px solid',
                                                            ...(isActive
                                                                ? {
                                                                    transition:
                                                                        'all 0.3s ease',
                                                                    color:
                                                                        theme.palette
                                                                            .mode ===
                                                                        'dark'
                                                                            ? theme
                                                                                .palette
                                                                                .success
                                                                                .light
                                                                            : theme
                                                                                .palette
                                                                                .success
                                                                                .main,
                                                                    borderColor:
                                                                        alpha(
                                                                            theme
                                                                                .palette
                                                                                .success
                                                                                .main,
                                                                            0.4,
                                                                        ),
                                                                    bgcolor:
                                                                        alpha(
                                                                            theme
                                                                                .palette
                                                                                .success
                                                                                .main,
                                                                            theme
                                                                                .palette
                                                                                .mode ===
                                                                            'dark'
                                                                                ? 0.15
                                                                                : 0.08,
                                                                        ),
                                                                }
                                                                : {
                                                                    transition:
                                                                        'all 2.5s ease',
                                                                    color:
                                                                        theme
                                                                            .palette
                                                                            .text
                                                                            .disabled,
                                                                    borderColor:
                                                                        alpha(
                                                                            theme
                                                                                .palette
                                                                                .divider,
                                                                            0.5,
                                                                        ),
                                                                    bgcolor:
                                                                        'transparent',
                                                                }),
                                                        }}
                                                    >
                                                        {label}
                                                    </Box>
                                                );
                                            })}
                                        </Box>
                                    </Box>
                                </Box>
                                <AnalysisSkeleton />
                            </Box>
                        )}

                        {error && !loading && (
                            <Box sx={getErrorBoxSx(theme)}>
                                <Box sx={sxErrorFlexRow}>
                                    <ErrorIcon sx={{
                                        fontSize: 20,
                                        color: theme.palette.error.main,
                                        mt: 0.25,
                                    }} />
                                    <Box>
                                        <Typography
                                            sx={getErrorTitleSx(theme)}
                                        >
                                            Analysis Failed
                                        </Typography>
                                        <Typography
                                            sx={{
                                                color: 'text.secondary',
                                                fontSize: '1rem',
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
                                    content={
                                        `# Query Analysis\n\n${analysis}`
                                    }
                                    isDark={isDark}
                                    connectionId={connectionId}
                                    databaseName={databaseName}
                                    serverName={connectionName}
                                />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </Box>
        </Dialog>
    );
};

export { QueryAnalysisDialog };
export default QueryAnalysisDialog;
