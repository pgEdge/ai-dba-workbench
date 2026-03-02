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

import React, { useEffect } from 'react';
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
    Warning as WarningIcon,
    Info as InfoIcon,
} from '@mui/icons-material';
import { useAlertAnalysis } from '../hooks/useAlertAnalysis';
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
import {
    getServerBadgeSx,
    getDatabaseBadgeSx,
    getDatabaseTextSx,
    sxMonoSmall,
} from './analysisStyles';
import SlideTransition from './shared/SlideTransition';
import { downloadAsMarkdown } from '../utils/downloadMarkdown';

const TOOL_LABELS = [
    'Querying metrics',
    'Fetching metric baselines',
    'Reviewing alert history',
    'Checking alert rules',
    'Checking blackouts',
    'Validating query',
    'Searching knowledgebase',
];

// Severity color getter using theme palette
const getSeverityColor = (severity: string | undefined, theme: Theme) => {
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
const getSeverityIcon = (severity: string | undefined) => {
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
// Alert-specific style constants and style-getter functions
// ---------------------------------------------------------------------------

const getSeverityDotSx = (severityColor: string, theme: Theme) => ({
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

const sxSeverityBadge = { display: 'flex', alignItems: 'center', gap: 0.5 };

const sxThresholdText = {
    fontSize: '0.875rem',
    color: 'text.disabled',
    ...sxMonoFont,
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * AlertAnalysisDialog - Dialog for displaying AI-powered alert analysis
 */
interface AlertAnalysisDialogProps {
    open: boolean;
    alert: Record<string, unknown> | null;
    onClose: () => void;
    onAnalysisComplete?: (alertId: number, analysis: string) => void;
}

const AlertAnalysisDialog: React.FC<AlertAnalysisDialogProps> = ({ open, alert, onClose, onAnalysisComplete }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const { analysis, loading, error, progressMessage, activeTools, analyze, reset } = useAlertAnalysis();

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

        downloadAsMarkdown(content, filename);
    };

    const severityColor = getSeverityColor(alert?.severity as string | undefined, theme);
    const SeverityIcon = getSeverityIcon(alert?.severity as string | undefined);

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
                        aria-label="close alert analysis"
                        sx={{ color: 'text.secondary' }}
                    >
                        <CloseIcon />
                    </IconButton>

                    {/* Icon with severity indicator */}
                    <Box sx={getIconBoxSx(theme)}>
                        <PsychologyIcon sx={getIconColorSx(theme)} />
                        <Box sx={getSeverityDotSx(severityColor, theme)} />
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
                        Alert analysis
                    </Typography>

                    {/* Severity badge */}
                    <Box sx={sxSeverityBadge}>
                        <SeverityIcon sx={{ fontSize: 14, color: severityColor }} />
                        <Typography
                            sx={{
                                fontSize: '1rem',
                                color: severityColor,
                                fontWeight: 500,
                                textTransform: 'capitalize',
                            }}
                        >
                            {alert?.severity || 'Unknown'}
                        </Typography>
                    </Box>

                    {/* Alert title */}
                    <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary' }}>
                        {alert?.title || 'Alert'}
                    </Typography>

                    {/* Server pill */}
                    {alert?.server && (
                        <Box sx={getServerBadgeSx(theme)}>
                            <Typography sx={sxMonoSmall}>
                                {alert.server}
                            </Typography>
                        </Box>
                    )}

                    {/* Database pill */}
                    {alert?.databaseName && (
                        <Box sx={getDatabaseBadgeSx(theme)}>
                            <Typography sx={getDatabaseTextSx(theme)}>
                                {alert.databaseName}
                            </Typography>
                        </Box>
                    )}

                    {/* Threshold text */}
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

                    {/* Time text */}
                    {alert?.time && (
                        <Typography sx={{ fontSize: '0.875rem', color: 'text.disabled' }}>
                            {alert.time}
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
                                    content={`# Alert Analysis: ${alert?.title || 'Alert'}\n\n${analysis}`}
                                    isDark={isDark}
                                    connectionId={alert?.connectionId as number | undefined}
                                    databaseName={alert?.databaseName as string | undefined}
                                    serverName={alert?.server as string | undefined}
                                />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </Box>
        </Dialog>
    );
};

export default AlertAnalysisDialog;
