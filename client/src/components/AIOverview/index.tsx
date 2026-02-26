/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
    Box,
    Typography,
    Paper,
    Skeleton,
    Collapse,
    IconButton,
    alpha,
    Tooltip,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    AutoAwesome as SparkleIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Psychology as PsychologyIcon,
    Refresh as RefreshIcon,
} from '@mui/icons-material';
import { apiGet } from '../../utils/apiClient';
import { ApiError } from '../../utils/apiClient';
import { clearAnalysisCache } from '../../hooks/useServerAnalysis';
import { useOverviewSSE } from '../../hooks/useOverviewSSE';
import type { OverviewResponse } from '../../hooks/useOverviewSSE';

/**
 * Selection object describing the current scope for the overview.
 */
interface OverviewSelection {
    type: 'server' | 'cluster' | 'estate' | 'group';
    id?: number | string;
    name?: string;
    serverIds?: number[];
    [key: string]: unknown;
}

/**
 * Props accepted by the AIOverview component.
 */
interface AIOverviewProps {
    selection?: OverviewSelection | null;
    onAnalyze?: () => void;
    analysisCached?: boolean;
}

/** localStorage key for persisting collapsed state. */
const COLLAPSED_STORAGE_KEY = 'ai-overview-collapsed';

/**
 * Format a timestamp as a human-readable relative time string.
 */
function formatRelativeTime(dateStr: string): string {
    if (!dateStr) {
        return '';
    }
    const now = new Date();
    const then = new Date(dateStr);
    const diffMs = now.getTime() - then.getTime();
    const diffSecs = Math.floor(diffMs / 1000);
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffSecs < 60) {
        return 'Updated just now';
    }
    if (diffMins < 60) {
        return `Updated ${diffMins} min ago`;
    }
    if (diffHours < 24) {
        return `Updated ${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
    }
    if (diffDays < 7) {
        return `Updated ${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
    }
    return `Updated ${then.toLocaleDateString()}`;
}

/**
 * AIOverview displays an AI-generated estate summary at the top of the
 * StatusPanel.  It subscribes to real-time updates via SSE and falls
 * back to polling when the SSE connection is unavailable.  Handles
 * loading, generating, and ready states.
 */
const AIOverview: React.FC<AIOverviewProps> = ({ selection, onAnalyze, analysisCached }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';

    const [overview, setOverview] = useState<OverviewResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [refreshing, setRefreshing] = useState(false);

    // Collapsed state with localStorage persistence
    const [collapsed, setCollapsed] = useState<boolean>(() => {
        try {
            const stored = localStorage.getItem(COLLAPSED_STORAGE_KEY);
            return stored === 'true';
        } catch {
            return false;
        }
    });

    const handleToggleCollapse = useCallback(() => {
        setCollapsed(prev => {
            const next = !prev;
            try {
                localStorage.setItem(COLLAPSED_STORAGE_KEY, String(next));
            } catch {
                // Ignore localStorage errors
            }
            return next;
        });
    }, []);

    // Build the API URL based on the current selection scope
    const overviewUrl = useMemo(() => {
        if (!selection || selection.type === 'estate') {
            return '/api/v1/overview';
        }

        // For servers with a numeric ID, use scope_type/scope_id directly
        if (selection.type === 'server' && typeof selection.id === 'number') {
            return `/api/v1/overview?scope_type=server&scope_id=${encodeURIComponent(String(selection.id))}`;
        }

        // For clusters and groups, send the individual connection IDs
        // instead of the virtual string ID that the server cannot resolve
        if (
            (selection.type === 'cluster' || selection.type === 'group') &&
            selection.serverIds &&
            selection.serverIds.length > 0
        ) {
            const ids = selection.serverIds.join(',');
            const params = new URLSearchParams();
            params.set('connection_ids', ids);
            if (selection.name) {
                params.set('scope_name', selection.name);
            }
            return `/api/v1/overview?${params.toString()}`;
        }

        // Fallback: estate-wide overview
        return '/api/v1/overview';
    }, [selection?.type, selection?.id, selection?.serverIds, selection?.name]);

    const { overview: sseOverview, connected: sseConnected } = useOverviewSSE(overviewUrl);

    // Reset state synchronously when the URL changes so the stale
    // overview is never painted to the screen.
    const [lastFetchedUrl, setLastFetchedUrl] = useState(overviewUrl);

    if (overviewUrl !== lastFetchedUrl) {
        setOverview(null);
        setLoading(true);
        setLastFetchedUrl(overviewUrl);
    }

    const fetchOverview = useCallback(async (isInitial: boolean) => {
        if (isInitial) {
            setLoading(true);
        }
        setError(null);

        try {
            const data = await apiGet<OverviewResponse>(overviewUrl);
            setOverview(data);
            if (data.restart_detected) {
                clearAnalysisCache();
            }
        } catch (err) {
            if (err instanceof ApiError && err.statusCode === 401) {
                // User is not authenticated; suppress the error silently
                setError(null);
            } else {
                console.error('Failed to fetch AI overview:', err);
                setError('Unable to load AI overview');
            }
        } finally {
            setLoading(false);
        }
    }, [overviewUrl]);

    // Dedicated refresh handler for the manual refresh button.
    // Only replaces the overview when the server returns a real
    // summary so that a "generating" response never hides existing
    // content.  Errors are silently ignored.
    const handleRefresh = useCallback(async () => {
        setRefreshing(true);
        try {
            const sep = overviewUrl.includes('?') ? '&' : '?';
            const refreshUrl = `${overviewUrl}${sep}refresh=true`;
            const data = await apiGet<OverviewResponse>(refreshUrl);
            if (data.summary != null) {
                setOverview(data);
                if (data.restart_detected) {
                    clearAnalysisCache();
                }
            }
        } catch {
            // Silently ignore refresh errors
        } finally {
            setRefreshing(false);
        }
    }, [overviewUrl]);

    // SSE-driven updates: when SSE delivers data, update local state
    useEffect(() => {
        if (sseOverview) {
            setOverview(sseOverview);
            setLoading(false);
            setRefreshing(false);
            if (sseOverview.restart_detected) {
                clearAnalysisCache();
            }
        }
    }, [sseOverview]);

    // Fallback polling: if SSE isn't connected after 5 seconds, poll every 10s
    useEffect(() => {
        let fallbackInterval: ReturnType<typeof setInterval> | null = null;
        const fallbackTimeout = setTimeout(() => {
            if (!sseConnected) {
                fallbackInterval = setInterval(() => {
                    fetchOverview(false);
                }, 10_000);
            }
        }, 5000);

        return () => {
            clearTimeout(fallbackTimeout);
            if (fallbackInterval) {
                clearInterval(fallbackInterval);
            }
        };
    }, [sseConnected, fetchOverview]);

    // Determine whether the overview is stale
    const isStale = useMemo(() => {
        if (!overview?.stale_at) {
            return false;
        }
        return new Date() > new Date(overview.stale_at);
    }, [overview?.stale_at]);

    // Check whether the server is still generating the summary
    const isGenerating = overview?.status === 'generating' || (
        overview !== null && overview.summary === null
    );

    // Computed styles that depend on the theme
    const paperSx = useMemo(() => ({
        p: 1.5,
        elevation: 0,
        bgcolor: isDark
            ? alpha(theme.palette.background.paper, 0.4)
            : alpha(theme.palette.grey[50], 0.8),
        border: '1px solid',
        borderColor: theme.palette.divider,
    }), [theme, isDark]);

    const labelContainerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 0.5,
        mb: collapsed ? 0 : 0.75,
    }), [collapsed]);

    const toggleButtonSx = useMemo(() => ({
        p: 0.25,
        color: 'text.secondary',
    }), []);

    const sparkleIconSx = useMemo(() => ({
        fontSize: 16,
        color: 'primary.main',
    }), []);

    const labelSx = useMemo(() => ({
        fontSize: '1rem',
        fontWeight: 600,
        color: 'text.primary',
        lineHeight: 1,
    }), []);

    const staleBadgeSx = useMemo(() => ({
        fontSize: '0.875rem',
        fontWeight: 500,
        color: theme.palette.warning.main,
        ml: 1,
    }), [theme.palette.warning.main]);

    const showAnalyzeButton = onAnalyze && selection && (selection.type === 'server' || selection.type === 'cluster');

    // Header row shared across all states
    const headerRow = (showStale = false) => (
        <Box sx={labelContainerSx}>
            <SparkleIcon sx={sparkleIconSx} />
            <Typography sx={labelSx}>
                AI Overview
            </Typography>
            {showStale && isStale && (
                <Typography sx={staleBadgeSx}>
                    (stale)
                </Typography>
            )}
            {showAnalyzeButton && (
                <Tooltip title={analysisCached
                    ? `View cached ${selection?.type === 'cluster' ? 'cluster' : 'server'} analysis`
                    : `Analyze ${selection?.type === 'cluster' ? 'cluster' : 'server'}`
                }>
                    <IconButton
                        size="small"
                        onClick={onAnalyze}
                        aria-label="Run full analysis"
                        sx={{
                            p: 0.25,
                            color: analysisCached ? 'warning.main' : 'secondary.main',
                            '&:hover': { bgcolor: alpha(
                                analysisCached ? theme.palette.warning.main : theme.palette.secondary.main,
                                0.1,
                            ) },
                        }}
                    >
                        <PsychologyIcon sx={{ fontSize: 18 }} />
                    </IconButton>
                </Tooltip>
            )}
            <Box sx={{ flexGrow: 1 }} />
            <IconButton
                size="small"
                onClick={handleToggleCollapse}
                aria-label={collapsed ? 'Expand AI Overview' : 'Collapse AI Overview'}
                sx={toggleButtonSx}
            >
                {collapsed ? <ExpandMoreIcon sx={{ fontSize: 18 }} /> : <ExpandLessIcon sx={{ fontSize: 18 }} />}
            </IconButton>
        </Box>
    );

    // Loading state: show skeleton placeholder
    if (loading) {
        return (
            <Paper elevation={0} sx={paperSx}>
                {headerRow()}
                <Collapse in={!collapsed}>
                    <Skeleton variant="text" width="90%" height={18} />
                    <Skeleton variant="text" width="75%" height={18} />
                    <Skeleton variant="text" width="40%" height={14} sx={{ mt: 0.5 }} />
                </Collapse>
            </Paper>
        );
    }

    // Error state or no data: render nothing to avoid cluttering the UI
    if (error || !overview) {
        return null;
    }

    // Generating state: the server has not produced a summary yet
    if (isGenerating) {
        return (
            <Paper elevation={0} sx={paperSx}>
                {headerRow()}
                <Collapse in={!collapsed}>
                    <Typography
                        variant="body2"
                        sx={{ color: 'text.secondary', fontStyle: 'italic' }}
                    >
                        Generating overview...
                    </Typography>
                </Collapse>
            </Paper>
        );
    }

    // Ready state: display the summary
    return (
        <Paper elevation={0} sx={paperSx}>
            {headerRow(true)}
            <Collapse in={!collapsed}>
                <Typography
                    variant="body2"
                    sx={{
                        color: 'text.primary',
                        lineHeight: 1.5,
                        whiteSpace: 'pre-wrap',
                    }}
                >
                    {overview.summary}
                </Typography>
                {overview.generated_at && (
                    <Box sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 0.5,
                        mt: 0.75,
                    }}>
                        <Tooltip title={refreshing ? 'Refreshing...' : 'Refresh now'}>
                            <IconButton
                                size="small"
                                onClick={handleRefresh}
                                disabled={refreshing}
                                aria-label="Refresh overview"
                                sx={{
                                    p: 0.25,
                                    color: 'text.secondary',
                                    '&:hover': {
                                        bgcolor: 'action.hover',
                                    },
                                }}
                            >
                                <RefreshIcon sx={{
                                    fontSize: 14,
                                    animation: refreshing ? 'spin 1s linear infinite' : 'none',
                                    '@keyframes spin': {
                                        '0%': { transform: 'rotate(0deg)' },
                                        '100%': { transform: 'rotate(360deg)' },
                                    },
                                }} />
                            </IconButton>
                        </Tooltip>
                        <Typography
                            variant="caption"
                            sx={{ color: 'text.secondary' }}
                        >
                            {formatRelativeTime(overview.generated_at)}
                        </Typography>
                    </Box>
                )}
            </Collapse>
        </Paper>
    );
};

export default AIOverview;
