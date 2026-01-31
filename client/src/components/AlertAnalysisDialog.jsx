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
    useTheme,
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
            const { background, backgroundColor, ...rest } = value;
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

const getHeadingSx = (theme) => ({
    fontWeight: 600,
    color: theme.palette.secondary.main,
    pb: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.2 : 0.15),
});

const sxH1 = (theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1.125rem',
    mt: 2,
    mb: 1,
});

const sxH2 = (theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1rem',
    mt: 2.5,
    mb: 1,
});

const getInlineCodeSx = (theme) => ({
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

const getCodeBlockWrapperSx = (theme) => ({
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

const getLinkSx = (theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.dark,
    textDecoration: 'none',
    '&:hover': {
        textDecoration: 'underline',
    },
});

const getBlockquoteSx = (theme) => ({
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

const getTableSx = (theme) => ({
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

const getSkeletonBgSx = (theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

const getDialogPaperSx = (theme) => ({
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

const getDialogTitleSx = (theme) => ({
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

const getIconBoxSx = (theme) => ({
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

const getIconColorSx = (theme) => ({
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

const getServerBadgeSx = (theme) => ({
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

const getDatabaseBadgeSx = (theme) => ({
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

const getDatabaseTextSx = (theme) => ({
    fontSize: '0.6875rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    ...sxMonoFont,
});

const getCloseButtonSx = (theme) => ({
    color: 'text.secondary',
    '&:hover': {
        bgcolor: alpha(
            theme.palette.grey[400],
            0.1
        ),
    },
});

const getContentSx = (theme) => ({
    p: 3,
    pt: 0,
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
});

const getLoadingBannerSx = (theme) => ({
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

const getPulseDotSx = (theme) => ({
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

const getLoadingTextSx = (theme) => ({
    fontSize: '0.8125rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    fontWeight: 500,
});

const getErrorBoxSx = (theme) => ({
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

const getErrorTitleSx = (theme) => ({
    fontWeight: 600,
    color: theme.palette.mode === 'dark'
        ? theme.palette.error.light
        : theme.palette.error.dark,
    fontSize: '0.875rem',
    mb: 0.5,
});

const getAnalysisBoxSx = (theme) => ({
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

const getFooterSx = (theme) => ({
    px: 3,
    py: 2,
    borderTop: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

const getDownloadButtonSx = (theme) => ({
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
// Components
// ---------------------------------------------------------------------------

/**
 * Styled markdown content component using react-markdown
 */
const MarkdownContent = ({ content, isDark }) => {
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
            const match = /language-(\w+)/.exec(className || '');
            const language = match ? match[1] : '';

            // Custom backgrounds for code blocks
            const customBackground = isDark
                ? theme.palette.background.paper
                : theme.palette.grey[50];

            // Create clean themes without token background colors
            const cleanTheme = isDark
                ? createCleanTheme(oneDark, theme.palette.background.paper)
                : createCleanTheme(oneLight, theme.palette.grey[50]);

            return inline ? (
                <Box component="code" sx={getInlineCodeSx(theme)}>
                    {children}
                </Box>
            ) : (
                <Box sx={getCodeBlockWrapperSx(theme)}>
                    <SyntaxHighlighter
                        style={cleanTheme}
                        language={language || 'sql'}
                        PreTag="div"
                        customStyle={getCodeBlockCustomStyle(customBackground)}
                        {...props}
                    >
                        {String(children).replace(/\n$/, '')}
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
    }), [isDark, theme]);

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
const AlertAnalysisDialog = ({ open, alert, onClose, isDark }) => {
    const theme = useTheme();
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
                                <MarkdownContent content={analysis} isDark={isDark} />
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
