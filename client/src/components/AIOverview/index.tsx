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
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { AutoAwesome as SparkleIcon } from '@mui/icons-material';
import { apiGet } from '../../utils/apiClient';
import { ApiError } from '../../utils/apiClient';

/**
 * Shape of the API response from GET /api/v1/overview.
 */
interface OverviewResponse {
    status?: string;
    summary: string | null;
    generated_at: string;
    stale_at: string;
    snapshot?: Record<string, unknown>;
}

/**
 * Props accepted by the AIOverview component.
 */
interface AIOverviewProps {
    mode?: 'light' | 'dark';
}

/** Refresh interval in milliseconds (30 seconds). */
const REFRESH_INTERVAL_MS = 30_000;

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
 * StatusPanel.  It fetches from GET /api/v1/overview, auto-refreshes
 * every 30 seconds, and handles loading, generating, and ready states.
 */
const AIOverview: React.FC<AIOverviewProps> = ({ mode = 'light' }) => {
    const theme = useTheme();
    const isDark = mode === 'dark';

    const [overview, setOverview] = useState<OverviewResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const fetchOverview = useCallback(async (isInitial: boolean) => {
        if (isInitial) {
            setLoading(true);
        }
        setError(null);

        try {
            const data = await apiGet<OverviewResponse>('/api/v1/overview');
            setOverview(data);
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
    }, []);

    // Fetch on mount and set up auto-refresh
    useEffect(() => {
        fetchOverview(true);

        const intervalId = setInterval(() => {
            fetchOverview(false);
        }, REFRESH_INTERVAL_MS);

        return () => clearInterval(intervalId);
    }, [fetchOverview]);

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
        borderLeft: `3px solid ${alpha(theme.palette.primary.main, 0.3)}`,
        bgcolor: isDark
            ? alpha(theme.palette.primary.main, 0.08)
            : alpha(theme.palette.primary.main, 0.04),
        border: `1px solid ${alpha(theme.palette.primary.main, 0.12)}`,
        borderLeftWidth: '3px',
        borderLeftColor: alpha(theme.palette.primary.main, 0.3),
    }), [theme, isDark]);

    const labelContainerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 0.5,
        mb: 0.75,
    }), []);

    const sparkleIconSx = useMemo(() => ({
        fontSize: 16,
        color: theme.palette.primary.main,
    }), [theme.palette.primary.main]);

    const labelSx = useMemo(() => ({
        fontSize: '0.6875rem',
        fontWeight: 600,
        color: theme.palette.primary.main,
        letterSpacing: '0.05em',
        textTransform: 'uppercase',
        lineHeight: 1,
    }), [theme.palette.primary.main]);

    const staleBadgeSx = useMemo(() => ({
        fontSize: '0.5625rem',
        fontWeight: 500,
        color: theme.palette.warning.main,
        ml: 1,
    }), [theme.palette.warning.main]);

    // Loading state: show skeleton placeholder
    if (loading) {
        return (
            <Paper elevation={0} sx={paperSx}>
                <Box sx={labelContainerSx}>
                    <SparkleIcon sx={sparkleIconSx} />
                    <Typography sx={labelSx}>
                        AI Overview
                    </Typography>
                </Box>
                <Skeleton variant="text" width="90%" height={18} />
                <Skeleton variant="text" width="75%" height={18} />
                <Skeleton variant="text" width="40%" height={14} sx={{ mt: 0.5 }} />
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
                <Box sx={labelContainerSx}>
                    <SparkleIcon sx={sparkleIconSx} />
                    <Typography sx={labelSx}>
                        AI Overview
                    </Typography>
                </Box>
                <Typography
                    variant="body2"
                    sx={{ color: 'text.secondary', fontStyle: 'italic' }}
                >
                    Generating estate overview...
                </Typography>
            </Paper>
        );
    }

    // Ready state: display the summary
    return (
        <Paper elevation={0} sx={paperSx}>
            <Box sx={labelContainerSx}>
                <SparkleIcon sx={sparkleIconSx} />
                <Typography sx={labelSx}>
                    AI Overview
                </Typography>
                {isStale && (
                    <Typography sx={staleBadgeSx}>
                        (stale)
                    </Typography>
                )}
            </Box>
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
                <Typography
                    variant="caption"
                    sx={{
                        color: 'text.secondary',
                        display: 'block',
                        mt: 0.75,
                    }}
                >
                    {formatRelativeTime(overview.generated_at)}
                </Typography>
            )}
        </Paper>
    );
};

export default AIOverview;
