/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Chart Analysis Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying AI-powered chart data analysis with
 * professional analytics aesthetic and markdown rendering
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
import { useChartAnalysis, ChartAnalysisInput } from '../hooks/useChartAnalysis';
import { ChartData, ChartAnalysisContext } from './Chart/types';
import {
    MarkdownContent,
    AnalysisSkeleton,
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
} from './shared/MarkdownContent';
import SlideTransition from './shared/SlideTransition';
import { downloadAsMarkdown } from '../utils/downloadMarkdown';

// ---------------------------------------------------------------------------
// Chart-specific style constants and style-getter functions
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
// Helper: slugify a string for use in filenames
// ---------------------------------------------------------------------------

const slugify = (text: string): string =>
    text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');

const TOOL_LABELS = [
    'Preparing chart data',
    'Fetching server context',
    'Fetching timeline events',
    'Analyzing data',
];

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ChartAnalysisDialogProps {
    open: boolean;
    onClose: () => void;
    isDark: boolean;
    analysisContext: ChartAnalysisContext;
    chartData: ChartData;
}

const ChartAnalysisDialog: React.FC<ChartAnalysisDialogProps> = ({
    open,
    onClose,
    isDark,
    analysisContext,
    chartData,
}) => {
    const theme = useTheme();
    const { analysis, loading, error, progressMessage, activeTools, analyze } = useChartAnalysis();

    // Trigger analysis once when dialog opens; ignore subsequent chartData
    // changes from dashboard polling while the dialog remains open.
    const hasTriggeredRef = useRef(false);

    useEffect(() => {
        if (open && !hasTriggeredRef.current) {
            hasTriggeredRef.current = true;
            if (!analysis) {
                const input: ChartAnalysisInput = {
                    metricDescription: analysisContext.metricDescription,
                    connectionId: analysisContext.connectionId,
                    connectionName: analysisContext.connectionName,
                    databaseName: analysisContext.databaseName,
                    timeRange: analysisContext.timeRange,
                    data: chartData,
                };
                analyze(input);
            }
        }
        if (!open) {
            hasTriggeredRef.current = false;
        }
    }, [open, analysis, analysisContext, chartData, analyze]);

    const handleClose = () => {
        onClose();
    };

    // Download analysis as markdown file
    const handleDownload = () => {
        if (!analysis) { return; }

        const timestamp = new Date().toISOString().split('T')[0];
        const slug = slugify(analysisContext.metricDescription || 'chart');
        const filename = `chart-analysis-${slug}-${timestamp}.md`;

        const connectionLine = analysisContext.connectionName
            ? `- **Connection:** ${analysisContext.connectionName}\n`
            : '';
        const databaseLine = analysisContext.databaseName
            ? `- **Database:** ${analysisContext.databaseName}\n`
            : '';
        const timeRangeLine = analysisContext.timeRange
            ? `- **Time Range:** ${analysisContext.timeRange}\n`
            : '';

        const content = `# Chart Analysis Report

## Chart Details

- **Metric:** ${analysisContext.metricDescription || 'N/A'}
${connectionLine}${databaseLine}${timeRangeLine}
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
                        aria-label="close chart analysis"
                        sx={{ color: 'text.secondary' }}
                    >
                        <CloseIcon />
                    </IconButton>

                    {/* Icon (no severity dot) */}
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
                        Chart analysis
                    </Typography>

                    {/* Metric description */}
                    <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary' }}>
                        {analysisContext.metricDescription || 'Chart'}
                    </Typography>

                    {/* Connection pill */}
                    {analysisContext.connectionName && (
                        <Box sx={getConnectionBadgeSx(theme)}>
                            <Typography sx={sxMonoSmall}>
                                {analysisContext.connectionName}
                            </Typography>
                        </Box>
                    )}

                    {/* Database pill */}
                    {analysisContext.databaseName && (
                        <Box sx={getDatabaseBadgeSx(theme)}>
                            <Typography sx={getDatabaseTextSx(theme)}>
                                {analysisContext.databaseName}
                            </Typography>
                        </Box>
                    )}

                    {/* Time range */}
                    {analysisContext.timeRange && (
                        <Typography sx={{ fontSize: '0.875rem', color: 'text.disabled' }}>
                            {analysisContext.timeRange}
                        </Typography>
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
                        backgroundColor: theme.palette.mode === 'dark' ? '#475569' : '#D1D5DB',
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
                                        <Typography sx={getLoadingTextSx(theme)}>
                                            {progressMessage}
                                        </Typography>
                                        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mt: 1 }}>
                                            {TOOL_LABELS.map(label => {
                                                const isActive = activeTools.includes(label);
                                                return (
                                                    <Box
                                                        key={label}
                                                        sx={{
                                                            px: 1,
                                                            py: 0.25,
                                                            borderRadius: 0.75,
                                                            fontSize: '0.75rem',
                                                            fontWeight: 500,
                                                            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                                                            border: '1px solid',
                                                            ...(isActive
                                                                ? {
                                                                    transition: 'all 0.3s ease',
                                                                    color: theme.palette.mode === 'dark'
                                                                        ? theme.palette.success.light
                                                                        : theme.palette.success.main,
                                                                    borderColor: alpha(theme.palette.success.main, 0.4),
                                                                    bgcolor: alpha(theme.palette.success.main, theme.palette.mode === 'dark' ? 0.15 : 0.08),
                                                                }
                                                                : {
                                                                    transition: 'all 2.5s ease',
                                                                    color: theme.palette.text.disabled,
                                                                    borderColor: alpha(theme.palette.divider, 0.5),
                                                                    bgcolor: 'transparent',
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
                                    <ErrorIcon sx={{ fontSize: 20, color: theme.palette.error.main, mt: 0.25 }} />
                                    <Box>
                                        <Typography sx={getErrorTitleSx(theme)}>
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
                                    content={`# Chart Analysis: ${analysisContext.metricDescription || 'Chart'}\n\n${analysis}`}
                                    isDark={isDark}
                                    connectionId={analysisContext.connectionId}
                                    databaseName={analysisContext.databaseName}
                                    serverName={analysisContext.connectionName}
                                />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </Box>
        </Dialog>
    );
};

export { ChartAnalysisDialog };
export default ChartAnalysisDialog;
