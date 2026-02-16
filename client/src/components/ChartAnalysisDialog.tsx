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
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
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
    sxContentFadeBox,
    sxErrorFlexRow,
    sxTitleFlexBox,
    sxCloseIconSize,
    sxTitleTypography,
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
} from './shared/MarkdownContent';

// ---------------------------------------------------------------------------
// Chart-specific style constants and style-getter functions
// ---------------------------------------------------------------------------

const sxMetadataRow = {
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    mt: 0.5,
    flexWrap: 'wrap',
};

const sxMetadataSecondRow = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    mt: 0.75,
    flexWrap: 'wrap',
};

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
    const { analysis, loading, error, analyze } = useChartAnalysis();

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
                {/* Icon (no severity dot) */}
                <Box sx={getIconBoxSx(theme)}>
                    <PsychologyIcon sx={getIconColorSx(theme)} />
                </Box>

                {/* Title and metadata */}
                <Box sx={sxTitleFlexBox}>
                    <Typography variant="h6" sx={sxTitleTypography}>
                        Chart analysis
                    </Typography>
                    {/* First row: metric description */}
                    <Box sx={sxMetadataRow}>
                        <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary' }}>
                            {analysisContext.metricDescription || 'Chart'}
                        </Typography>
                    </Box>
                    {/* Second row: connection name, database name, time range */}
                    <Box sx={sxMetadataSecondRow}>
                        {analysisContext.connectionName && (
                            <Box sx={getConnectionBadgeSx(theme)}>
                                <Typography sx={sxMonoSmall}>
                                    {analysisContext.connectionName}
                                </Typography>
                            </Box>
                        )}
                        {analysisContext.databaseName && (
                            <Box sx={getDatabaseBadgeSx(theme)}>
                                <Typography sx={getDatabaseTextSx(theme)}>
                                    {analysisContext.databaseName}
                                </Typography>
                            </Box>
                        )}
                        {analysisContext.timeRange && (
                            <Typography sx={{ fontSize: '0.875rem', color: 'text.disabled' }}>
                                {analysisContext.timeRange}
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
                                        Analyzing data and identifying patterns...
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
                                    content={analysis}
                                    isDark={isDark}
                                    connectionId={analysisContext.connectionId}
                                    databaseName={analysisContext.databaseName}
                                    serverName={analysisContext.connectionName}
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

export { ChartAnalysisDialog };
export default ChartAnalysisDialog;
