/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Base Analysis Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Shared shell component that provides the common chrome for all
 * analysis dialogs (loading, error, content states, AppBar, scrollbar
 * styling, tool badges).
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
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
import {
    Close as CloseIcon,
    Download as DownloadIcon,
    Error as ErrorIcon,
} from '@mui/icons-material';
import {
    MarkdownContent,
    AnalysisSkeleton,
} from './MarkdownContent';
import {
    sxErrorFlexRow,
    getIconBoxSx,
    getLoadingBannerSx,
    getPulseDotSx,
    getLoadingTextSx,
    getErrorBoxSx,
    getErrorTitleSx,
    getAnalysisBoxSx,
    getDownloadButtonSx,
} from './MarkdownExports';
import SlideTransition from './SlideTransition';

// ---------------------------------------------------------------------------
// Props interface
// ---------------------------------------------------------------------------

export interface MarkdownContentPropsForBase {
    isDark: boolean;
    connectionId?: number;
    databaseName?: string;
    serverName?: string;
    connectionMap?: Map<number, string>;
}

export interface BaseAnalysisDialogProps {
    open: boolean;
    onClose: () => void;
    title: string;
    icon: React.ReactNode;
    toolLabels: string[];
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    onDownload: () => void;
    onReset?: () => void;
    toolbarContent?: React.ReactNode;
    markdownContentProps: MarkdownContentPropsForBase;
    /** Full markdown content including title prefix; passed directly to MarkdownContent */
    markdownContent?: string;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * BaseAnalysisDialog - Shared shell for all analysis dialogs
 *
 * Provides the common UI scaffolding:
 * - Full-screen Dialog with SlideTransition
 * - AppBar with close button, icon, title, toolbar content slot, download button
 * - Content area with scrollbar styling
 * - Loading state: banner with pulsing dot, progress text, tool badges, skeleton
 * - Error state: error box with icon and message
 * - Analysis state: MarkdownContent with forwarded props
 */
const BaseAnalysisDialog: React.FC<BaseAnalysisDialogProps> = ({
    open,
    onClose,
    title,
    icon,
    toolLabels,
    analysis,
    loading,
    error,
    progressMessage,
    activeTools,
    onDownload,
    onReset,
    toolbarContent,
    markdownContentProps,
    markdownContent,
}) => {
    const theme = useTheme();

    const handleClose = () => {
        onReset?.();
        onClose();
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
                        aria-label="close analysis"
                        sx={{ color: 'text.secondary' }}
                    >
                        <CloseIcon />
                    </IconButton>

                    {/* Icon box */}
                    <Box sx={getIconBoxSx(theme)}>
                        {icon}
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
                        {title}
                    </Typography>

                    {/* Toolbar content slot */}
                    {toolbarContent}

                    {/* Spacer */}
                    <Box sx={{ flexGrow: 1 }} />

                    {/* Download button */}
                    <IconButton
                        onClick={onDownload}
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
                                        <Typography sx={getLoadingTextSx(theme)}>
                                            {progressMessage}
                                        </Typography>
                                        <Box sx={{
                                            display: 'flex',
                                            flexWrap: 'wrap',
                                            gap: 0.5,
                                            mt: 1,
                                        }}>
                                            {toolLabels.map(label => {
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
                                                            fontFamily:
                                                                '"JetBrains Mono", "SF Mono", monospace',
                                                            border: '1px solid',
                                                            ...(isActive
                                                                ? {
                                                                    transition: 'all 0.3s ease',
                                                                    color:
                                                                        theme.palette.mode === 'dark'
                                                                            ? theme.palette.success.light
                                                                            : theme.palette.success.main,
                                                                    borderColor: alpha(
                                                                        theme.palette.success.main,
                                                                        0.4,
                                                                    ),
                                                                    bgcolor: alpha(
                                                                        theme.palette.success.main,
                                                                        theme.palette.mode === 'dark'
                                                                            ? 0.15
                                                                            : 0.08,
                                                                    ),
                                                                }
                                                                : {
                                                                    transition: 'all 2.5s ease',
                                                                    color: theme.palette.text.disabled,
                                                                    borderColor: alpha(
                                                                        theme.palette.divider,
                                                                        0.5,
                                                                    ),
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
                                    <ErrorIcon sx={{
                                        fontSize: 20,
                                        color: theme.palette.error.main,
                                        mt: 0.25,
                                    }} />
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
                                    content={markdownContent || analysis}
                                    isDark={markdownContentProps.isDark}
                                    connectionId={markdownContentProps.connectionId}
                                    databaseName={markdownContentProps.databaseName}
                                    serverName={markdownContentProps.serverName}
                                    connectionMap={markdownContentProps.connectionMap}
                                />
                            </Box>
                        )}
                    </Box>
                </Fade>
            </Box>
        </Dialog>
    );
};

export default BaseAnalysisDialog;
export { BaseAnalysisDialog };
