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

import React, { useEffect } from 'react';
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
 * Simple markdown parser for rendering analysis content
 * Handles headers, lists, bold, code, and paragraphs
 */
const renderMarkdown = (text, isDark) => {
    if (!text) return null;

    const lines = text.split('\n');
    const elements = [];
    let currentList = [];
    let currentListType = null;
    let key = 0;

    const flushList = () => {
        if (currentList.length > 0) {
            if (currentListType === 'numbered') {
                elements.push(
                    <Box
                        key={key++}
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
                        {currentList.map((item, idx) => (
                            <li key={idx}>{renderInlineMarkdown(item, isDark)}</li>
                        ))}
                    </Box>
                );
            } else {
                elements.push(
                    <Box
                        key={key++}
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
                        {currentList.map((item, idx) => (
                            <li key={idx}>{renderInlineMarkdown(item, isDark)}</li>
                        ))}
                    </Box>
                );
            }
            currentList = [];
            currentListType = null;
        }
    };

    for (const line of lines) {
        const trimmedLine = line.trim();

        // Skip empty lines but flush lists
        if (!trimmedLine) {
            flushList();
            continue;
        }

        // Headers
        if (trimmedLine.startsWith('## ')) {
            flushList();
            elements.push(
                <Typography
                    key={key++}
                    variant="h6"
                    sx={{
                        fontWeight: 600,
                        color: isDark ? '#6366F1' : '#4F46E5',
                        fontSize: '1rem',
                        mt: elements.length > 0 ? 2.5 : 0,
                        mb: 1,
                        pb: 0.5,
                        borderBottom: '1px solid',
                        borderColor: isDark ? alpha('#6366F1', 0.2) : alpha('#4F46E5', 0.15),
                    }}
                >
                    {trimmedLine.slice(3)}
                </Typography>
            );
            continue;
        }

        if (trimmedLine.startsWith('### ')) {
            flushList();
            elements.push(
                <Typography
                    key={key++}
                    variant="subtitle1"
                    sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '0.9375rem',
                        mt: 2,
                        mb: 0.75,
                    }}
                >
                    {trimmedLine.slice(4)}
                </Typography>
            );
            continue;
        }

        // Numbered list items
        const numberedMatch = trimmedLine.match(/^(\d+)\.\s+(.*)$/);
        if (numberedMatch) {
            if (currentListType !== 'numbered') {
                flushList();
            }
            currentListType = 'numbered';
            currentList.push(numberedMatch[2]);
            continue;
        }

        // Bullet list items
        if (trimmedLine.startsWith('- ') || trimmedLine.startsWith('* ')) {
            if (currentListType !== 'bullet') {
                flushList();
            }
            currentListType = 'bullet';
            currentList.push(trimmedLine.slice(2));
            continue;
        }

        // Regular paragraph
        flushList();
        elements.push(
            <Typography
                key={key++}
                variant="body2"
                sx={{
                    color: 'text.primary',
                    fontSize: '0.875rem',
                    lineHeight: 1.7,
                    my: 1,
                }}
            >
                {renderInlineMarkdown(trimmedLine, isDark)}
            </Typography>
        );
    }

    flushList();
    return elements;
};

/**
 * Render inline markdown elements (bold, code, etc.)
 */
const renderInlineMarkdown = (text, isDark) => {
    if (!text) return null;

    const parts = [];
    let remaining = text;
    let partKey = 0;

    // Process inline code first (`code`)
    while (remaining.includes('`')) {
        const startIdx = remaining.indexOf('`');
        const endIdx = remaining.indexOf('`', startIdx + 1);

        if (endIdx === -1) break;

        // Add text before code
        if (startIdx > 0) {
            parts.push(processTextForBold(remaining.slice(0, startIdx), isDark, partKey++));
        }

        // Add code span
        const code = remaining.slice(startIdx + 1, endIdx);
        parts.push(
            <Box
                key={`code-${partKey++}`}
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
                {code}
            </Box>
        );

        remaining = remaining.slice(endIdx + 1);
    }

    // Process remaining text for bold
    if (remaining) {
        parts.push(processTextForBold(remaining, isDark, partKey));
    }

    return parts.length === 1 ? parts[0] : parts;
};

/**
 * Process text for bold formatting (**text**)
 */
const processTextForBold = (text, isDark, baseKey) => {
    if (!text.includes('**')) {
        return text;
    }

    const parts = [];
    let remaining = text;
    let partKey = 0;

    while (remaining.includes('**')) {
        const startIdx = remaining.indexOf('**');
        const endIdx = remaining.indexOf('**', startIdx + 2);

        if (endIdx === -1) break;

        // Add text before bold
        if (startIdx > 0) {
            parts.push(remaining.slice(0, startIdx));
        }

        // Add bold span
        const bold = remaining.slice(startIdx + 2, endIdx);
        parts.push(
            <Box
                key={`bold-${baseKey}-${partKey++}`}
                component="strong"
                sx={{ fontWeight: 600 }}
            >
                {bold}
            </Box>
        );

        remaining = remaining.slice(endIdx + 2);
    }

    if (remaining) {
        parts.push(remaining);
    }

    return parts;
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
                                {renderMarkdown(analysis, isDark)}
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
