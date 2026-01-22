/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Alert Analysis Dialog
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying AI-powered alert analysis with
 * professional analytics aesthetic and markdown rendering
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useMemo } from 'react';
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
} from '@mui/material';
import {
    Close as CloseIcon,
    Download as DownloadIcon,
    Psychology as PsychologyIcon,
    Error as ErrorIcon,
    Warning as WarningIcon,
    Info as InfoIcon,
} from '@mui/icons-material';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useAlertAnalysis } from '../hooks/useAlertAnalysis';

// Severity colors for indicators
const SEVERITY_COLORS = {
    critical: '#EF4444',
    warning: '#F59E0B',
    info: '#3B82F6',
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

/**
 * Styled markdown content component using react-markdown
 */
const MarkdownContent = ({ content, isDark }) => {
    // Memoize markdown components to avoid re-creating on each render
    const components = useMemo(() => ({
        h1: ({ children }) => (
            <Typography
                variant="h5"
                sx={{
                    fontWeight: 600,
                    color: isDark ? '#6366F1' : '#4F46E5',
                    fontSize: '1.125rem',
                    mt: 2,
                    mb: 1,
                    pb: 0.5,
                    borderBottom: '1px solid',
                    borderColor: isDark ? alpha('#6366F1', 0.2) : alpha('#4F46E5', 0.15),
                }}
            >
                {children}
            </Typography>
        ),
        h2: ({ children }) => (
            <Typography
                variant="h6"
                sx={{
                    fontWeight: 600,
                    color: isDark ? '#6366F1' : '#4F46E5',
                    fontSize: '1rem',
                    mt: 2.5,
                    mb: 1,
                    pb: 0.5,
                    borderBottom: '1px solid',
                    borderColor: isDark ? alpha('#6366F1', 0.2) : alpha('#4F46E5', 0.15),
                }}
            >
                {children}
            </Typography>
        ),
        h3: ({ children }) => (
            <Typography
                variant="subtitle1"
                sx={{
                    fontWeight: 600,
                    color: 'text.primary',
                    fontSize: '0.9375rem',
                    mt: 2,
                    mb: 0.75,
                }}
            >
                {children}
            </Typography>
        ),
        p: ({ children }) => (
            <Typography
                variant="body2"
                sx={{
                    color: 'text.primary',
                    fontSize: '0.875rem',
                    lineHeight: 1.7,
                    my: 1,
                }}
            >
                {children}
            </Typography>
        ),
        ul: ({ children }) => (
            <Box
                component="ul"
                sx={{
                    pl: 2.5,
                    my: 1.5,
                    listStyleType: 'disc',
                    '& li': {
                        mb: 0.75,
                        fontSize: '0.875rem',
                        lineHeight: 1.6,
                        color: 'text.primary',
                    },
                }}
            >
                {children}
            </Box>
        ),
        ol: ({ children }) => (
            <Box
                component="ol"
                sx={{
                    pl: 2.5,
                    my: 1.5,
                    '& li': {
                        mb: 0.75,
                        fontSize: '0.875rem',
                        lineHeight: 1.6,
                        color: 'text.primary',
                    },
                }}
            >
                {children}
            </Box>
        ),
        li: ({ children }) => <li>{children}</li>,
        code: ({ inline, children }) =>
            inline ? (
                <Box
                    component="code"
                    sx={{
                        fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                        fontSize: '0.8125rem',
                        bgcolor: isDark ? alpha('#334155', 0.6) : alpha('#E5E7EB', 0.8),
                        color: isDark ? '#E2E8F0' : '#374151',
                        px: 0.75,
                        py: 0.25,
                        borderRadius: 0.5,
                    }}
                >
                    {children}
                </Box>
            ) : (
                <Box
                    component="pre"
                    sx={{
                        fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                        fontSize: '0.8125rem',
                        bgcolor: isDark ? alpha('#0F172A', 0.8) : alpha('#F1F5F9', 0.8),
                        color: isDark ? '#E2E8F0' : '#374151',
                        p: 2,
                        borderRadius: 1,
                        overflow: 'auto',
                        my: 1.5,
                        border: '1px solid',
                        borderColor: isDark ? '#334155' : '#E2E8F0',
                    }}
                >
                    <code>{children}</code>
                </Box>
            ),
        strong: ({ children }) => (
            <Box component="strong" sx={{ fontWeight: 600 }}>
                {children}
            </Box>
        ),
        em: ({ children }) => (
            <Box component="em" sx={{ fontStyle: 'italic' }}>
                {children}
            </Box>
        ),
        a: ({ href, children }) => (
            <Box
                component="a"
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                sx={{
                    color: isDark ? '#818CF8' : '#4F46E5',
                    textDecoration: 'none',
                    '&:hover': {
                        textDecoration: 'underline',
                    },
                }}
            >
                {children}
            </Box>
        ),
        blockquote: ({ children }) => (
            <Box
                component="blockquote"
                sx={{
                    borderLeft: '3px solid',
                    borderColor: isDark ? '#6366F1' : '#4F46E5',
                    pl: 2,
                    ml: 0,
                    my: 1.5,
                    color: isDark ? '#94A3B8' : '#64748B',
                    fontStyle: 'italic',
                }}
            >
                {children}
            </Box>
        ),
        table: ({ children }) => (
            <Box
                component="table"
                sx={{
                    width: '100%',
                    borderCollapse: 'collapse',
                    my: 1.5,
                    fontSize: '0.875rem',
                    '& th, & td': {
                        border: '1px solid',
                        borderColor: isDark ? '#334155' : '#E2E8F0',
                        p: 1,
                        textAlign: 'left',
                    },
                    '& th': {
                        bgcolor: isDark ? alpha('#334155', 0.5) : alpha('#F1F5F9', 0.8),
                        fontWeight: 600,
                    },
                }}
            >
                {children}
            </Box>
        ),
    }), [isDark]);

    if (!content) return null;

    return (
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
            {content}
        </ReactMarkdown>
    );
};

/**
 * Loading skeleton for analysis content
 */
const AnalysisSkeleton = ({ isDark }) => (
    <Box sx={{ py: 2 }}>
        {/* Summary section */}
        <Skeleton
            variant="text"
            width="30%"
            height={28}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB', mb: 1 }}
        />
        <Skeleton
            variant="text"
            width="100%"
            height={20}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
        />
        <Skeleton
            variant="text"
            width="85%"
            height={20}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB', mb: 2.5 }}
        />

        {/* Analysis section */}
        <Skeleton
            variant="text"
            width="25%"
            height={28}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB', mb: 1 }}
        />
        <Skeleton
            variant="text"
            width="100%"
            height={20}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
        />
        <Skeleton
            variant="text"
            width="90%"
            height={20}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
        />
        <Skeleton
            variant="text"
            width="75%"
            height={20}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB', mb: 2.5 }}
        />

        {/* Remediation section */}
        <Skeleton
            variant="text"
            width="35%"
            height={28}
            sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB', mb: 1 }}
        />
        {[1, 2, 3].map((i) => (
            <Box key={i} sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 }}>
                <Skeleton
                    variant="circular"
                    width={8}
                    height={8}
                    sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
                />
                <Skeleton
                    variant="text"
                    width={`${85 - i * 10}%`}
                    height={20}
                    sx={{ bgcolor: isDark ? '#334155' : '#E5E7EB' }}
                />
            </Box>
        ))}
    </Box>
);

/**
 * AlertAnalysisDialog - Dialog for displaying AI-powered alert analysis
 */
const AlertAnalysisDialog = ({ open, alert, onClose, isDark }) => {
    const { analysis, loading, error, analyze, reset } = useAlertAnalysis();

    // Trigger analysis when dialog opens with an alert
    useEffect(() => {
        if (open && alert) {
            analyze(alert);
        }
    }, [open, alert, analyze]);

    // Reset state when dialog closes
    const handleClose = () => {
        reset();
        onClose();
    };

    // Download analysis as markdown file
    const handleDownload = () => {
        if (!analysis || !alert) return;

        const timestamp = new Date().toISOString().split('T')[0];
        const filename = `alert-analysis-${alert.id || 'unknown'}-${timestamp}.md`;

        const content = `# Alert Analysis Report

## Alert Details

- **Title:** ${alert.title || 'N/A'}
- **Severity:** ${alert.severity || 'N/A'}
- **Alert Type:** ${alert.alertType || 'threshold'}
- **Metric Value:** ${alert.metricValue ?? 'N/A'}
- **Threshold:** ${alert.operator || '>'} ${alert.thresholdValue ?? 'N/A'}
- **Connection ID:** ${alert.connectionId || 'N/A'}
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

    const severityColor = SEVERITY_COLORS[alert?.severity] || SEVERITY_COLORS.info;
    const SeverityIcon = getSeverityIcon(alert?.severity);

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="md"
            fullWidth
            PaperProps={{
                sx: {
                    bgcolor: isDark ? '#0F172A' : '#F8FAFC',
                    backgroundImage: 'none',
                    borderRadius: 2,
                    border: '1px solid',
                    borderColor: isDark ? '#334155' : '#E2E8F0',
                    boxShadow: isDark
                        ? '0 25px 50px -12px rgba(0, 0, 0, 0.5)'
                        : '0 25px 50px -12px rgba(0, 0, 0, 0.15)',
                },
            }}
        >
            {/* Header */}
            <DialogTitle
                sx={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 2,
                    pb: 2,
                    borderBottom: '1px solid',
                    borderColor: isDark ? '#334155' : '#E2E8F0',
                    bgcolor: isDark ? alpha('#1E293B', 0.5) : '#FFFFFF',
                }}
            >
                {/* Icon with severity indicator */}
                <Box
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 48,
                        height: 48,
                        borderRadius: 1.5,
                        bgcolor: isDark ? alpha('#6366F1', 0.15) : alpha('#6366F1', 0.1),
                        position: 'relative',
                        flexShrink: 0,
                    }}
                >
                    <PsychologyIcon
                        sx={{
                            fontSize: 28,
                            color: isDark ? '#818CF8' : '#6366F1',
                        }}
                    />
                    {/* Severity dot */}
                    <Box
                        sx={{
                            position: 'absolute',
                            top: -4,
                            right: -4,
                            width: 14,
                            height: 14,
                            borderRadius: '50%',
                            bgcolor: severityColor,
                            border: '2px solid',
                            borderColor: isDark ? '#0F172A' : '#F8FAFC',
                            boxShadow: `0 0 8px ${alpha(severityColor, 0.5)}`,
                        }}
                    />
                </Box>

                {/* Title and metadata */}
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography
                        variant="h6"
                        sx={{
                            fontWeight: 600,
                            color: 'text.primary',
                            fontSize: '1.125rem',
                            lineHeight: 1.3,
                        }}
                    >
                        Alert Analysis
                    </Typography>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mt: 0.5 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
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
                        <Typography
                            sx={{
                                fontSize: '0.75rem',
                                color: 'text.secondary',
                            }}
                        >
                            {alert?.title || 'Alert'}
                        </Typography>
                        {alert?.time && (
                            <Typography
                                sx={{
                                    fontSize: '0.75rem',
                                    color: 'text.disabled',
                                }}
                            >
                                {alert.time}
                            </Typography>
                        )}
                    </Box>
                </Box>

                {/* Close button */}
                <IconButton
                    onClick={handleClose}
                    size="small"
                    sx={{
                        color: 'text.secondary',
                        '&:hover': {
                            bgcolor: isDark ? alpha('#94A3B8', 0.1) : alpha('#64748B', 0.1),
                        },
                    }}
                >
                    <CloseIcon sx={{ fontSize: 20 }} />
                </IconButton>
            </DialogTitle>

            {/* Content */}
            <DialogContent
                sx={{
                    p: 3,
                    bgcolor: isDark ? '#0F172A' : '#F8FAFC',
                }}
            >
                <Fade in={true} timeout={300}>
                    <Box>
                        {loading && (
                            <Box>
                                <Box
                                    sx={{
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: 1.5,
                                        mb: 2,
                                        p: 1.5,
                                        borderRadius: 1,
                                        bgcolor: isDark ? alpha('#6366F1', 0.1) : alpha('#6366F1', 0.05),
                                        border: '1px solid',
                                        borderColor: isDark ? alpha('#6366F1', 0.2) : alpha('#6366F1', 0.15),
                                    }}
                                >
                                    <Box
                                        sx={{
                                            width: 8,
                                            height: 8,
                                            borderRadius: '50%',
                                            bgcolor: '#6366F1',
                                            animation: 'pulse 1.5s ease-in-out infinite',
                                            '@keyframes pulse': {
                                                '0%, 100%': { opacity: 1 },
                                                '50%': { opacity: 0.4 },
                                            },
                                        }}
                                    />
                                    <Typography
                                        sx={{
                                            fontSize: '0.8125rem',
                                            color: isDark ? '#818CF8' : '#6366F1',
                                            fontWeight: 500,
                                        }}
                                    >
                                        Analyzing alert and gathering context...
                                    </Typography>
                                </Box>
                                <AnalysisSkeleton isDark={isDark} />
                            </Box>
                        )}

                        {error && !loading && (
                            <Box
                                sx={{
                                    p: 2.5,
                                    borderRadius: 1.5,
                                    bgcolor: isDark ? alpha('#EF4444', 0.1) : alpha('#EF4444', 0.05),
                                    border: '1px solid',
                                    borderColor: isDark ? alpha('#EF4444', 0.25) : alpha('#EF4444', 0.2),
                                }}
                            >
                                <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
                                    <ErrorIcon sx={{ fontSize: 20, color: '#EF4444', mt: 0.25 }} />
                                    <Box>
                                        <Typography
                                            sx={{
                                                fontWeight: 600,
                                                color: isDark ? '#F87171' : '#DC2626',
                                                fontSize: '0.875rem',
                                                mb: 0.5,
                                            }}
                                        >
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
                            <Box
                                sx={{
                                    p: 2.5,
                                    borderRadius: 1.5,
                                    bgcolor: isDark ? alpha('#1E293B', 0.6) : '#FFFFFF',
                                    border: '1px solid',
                                    borderColor: isDark ? '#334155' : '#E2E8F0',
                                    boxShadow: isDark
                                        ? '0 4px 6px -1px rgba(0, 0, 0, 0.2)'
                                        : '0 1px 3px 0 rgba(0, 0, 0, 0.05)',
                                }}
                            >
                                <MarkdownContent content={analysis} isDark={isDark} />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </DialogContent>

            {/* Footer */}
            <DialogActions
                sx={{
                    px: 3,
                    py: 2,
                    borderTop: '1px solid',
                    borderColor: isDark ? '#334155' : '#E2E8F0',
                    bgcolor: isDark ? alpha('#1E293B', 0.5) : '#FFFFFF',
                }}
            >
                <Button
                    onClick={handleDownload}
                    startIcon={<DownloadIcon />}
                    disabled={!analysis || loading}
                    size="small"
                    sx={{
                        color: isDark ? '#94A3B8' : '#64748B',
                        '&:hover': {
                            bgcolor: isDark ? alpha('#94A3B8', 0.1) : alpha('#64748B', 0.1),
                        },
                        '&.Mui-disabled': {
                            color: isDark ? '#475569' : '#CBD5E1',
                        },
                    }}
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
