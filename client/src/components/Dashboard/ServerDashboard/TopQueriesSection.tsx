/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import QueryStatsIcon from '@mui/icons-material/QueryStats';
import { useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/AuthContext';
import { useDashboard } from '../../../contexts/DashboardContext';
import CollapsibleSection from '../CollapsibleSection';
import { ServerSectionProps, TopQueryRow } from './types';

/** Maximum characters to display before truncating a query */
const MAX_QUERY_LENGTH = 80;

/** Table container styles */
const TABLE_CONTAINER_SX = {
    overflowX: 'auto' as const,
    mb: 2,
};

/** Table header row */
const TABLE_HEADER_SX = {
    display: 'grid',
    gridTemplateColumns: '2fr 0.7fr 1fr 1fr 0.7fr',
    gap: 1,
    px: 1.5,
    py: 1,
    borderBottom: '2px solid',
    borderColor: 'divider',
};

/** Table header cell */
const HEADER_CELL_SX = {
    fontSize: '0.75rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
    color: 'text.secondary',
};

/** Table row */
const TABLE_ROW_SX = {
    display: 'grid',
    gridTemplateColumns: '2fr 0.7fr 1fr 1fr 0.7fr',
    gap: 1,
    px: 1.5,
    py: 1,
    cursor: 'pointer',
    borderBottom: '1px solid',
    borderColor: 'divider',
    transition: 'background-color 0.15s',
    '&:hover': {
        bgcolor: 'action.hover',
    },
    '&:last-child': {
        borderBottom: 'none',
    },
};

/** Query text cell */
const QUERY_CELL_SX = {
    fontSize: '0.8125rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    whiteSpace: 'nowrap' as const,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    color: 'text.primary',
};

/** Numeric cell */
const NUMERIC_CELL_SX = {
    fontSize: '0.8125rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontWeight: 500,
    textAlign: 'right' as const,
    color: 'text.primary',
};

/**
 * Format a duration in milliseconds.
 */
const formatTime = (ms: number): string => {
    if (ms < 1) { return `${(ms * 1000).toFixed(0)} us`; }
    if (ms < 1000) { return `${ms.toFixed(1)} ms`; }
    if (ms < 60000) { return `${(ms / 1000).toFixed(2)} s`; }
    return `${(ms / 60000).toFixed(1)} min`;
};

/**
 * Format large numbers with separators.
 */
const formatNumber = (num: number): string => {
    if (num >= 1_000_000) { return `${(num / 1_000_000).toFixed(1)}M`; }
    if (num >= 1_000) { return `${(num / 1_000).toFixed(1)}K`; }
    return num.toLocaleString();
};

/**
 * Truncate a query string and clean up whitespace.
 */
const truncateQuery = (query: string, maxLen: number): string => {
    if (!query) { return ''; }
    const cleaned = query.replace(/\s+/g, ' ').trim();
    if (cleaned.length <= maxLen) { return cleaned; }
    return cleaned.substring(0, maxLen) + '...';
};

/**
 * Top Queries section displays the most resource-intensive queries
 * from pg_stat_statements, sorted by total execution time. Each
 * row is clickable to drill down into query detail via overlay.
 */
const TopQueriesSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { user } = useAuth();
    const { refreshTrigger, pushOverlay } = useDashboard();
    const theme = useTheme();

    const [queries, setQueries] = useState<TopQueryRow[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            connection_id: connectionId.toString(),
            limit: '10',
            order_by: 'total_exec_time',
            order: 'desc',
        });
        const url = `/api/v1/metrics/top-queries?${params.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await fetch(url, {
                credentials: 'include',
            });

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch top queries: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json() as TopQueryRow[];
                setQueries(Array.isArray(result) ? result : []);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching top queries:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch top queries'
                );
                setQueries([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchData, refreshTrigger]);

    const handleQueryClick = useCallback((query: TopQueryRow): void => {
        pushOverlay({
            level: 'object',
            title: truncateQuery(query.query, 60),
            entityId: query.queryid,
            entityName: query.query,
            objectName: query.queryid,
            objectType: 'query',
            connectionId,
            connectionName,
        });
    }, [pushOverlay, connectionId, connectionName]);

    const headerRowSx = useMemo(() => ({
        ...TABLE_HEADER_SX,
        borderColor: theme.palette.divider,
    }), [theme.palette.divider]);

    return (
        <CollapsibleSection title="Top Queries" icon={<QueryStatsIcon sx={{ fontSize: 16 }} />} defaultExpanded>
            {loading && queries.length === 0 && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                    <CircularProgress size={24} />
                </Box>
            )}

            {error && (
                <Typography
                    variant="body2"
                    color="error"
                    sx={{ textAlign: 'center', py: 2 }}
                >
                    {error}
                </Typography>
            )}

            {!loading && !error && queries.length === 0 && (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ textAlign: 'center', py: 3 }}
                >
                    No query statistics available. Is the pg_stat_statements extension installed?
                </Typography>
            )}

            {queries.length > 0 && (
                <Box sx={TABLE_CONTAINER_SX}>
                    <Box sx={headerRowSx}>
                        <Typography sx={HEADER_CELL_SX}>
                            Query
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Calls
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Total Time
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Mean Time
                        </Typography>
                        <Typography sx={{
                            ...HEADER_CELL_SX,
                            textAlign: 'right',
                        }}>
                            Rows
                        </Typography>
                    </Box>

                    {queries.map((query, index) => (
                        <Box
                            key={query.queryid || index}
                            sx={TABLE_ROW_SX}
                            onClick={() => handleQueryClick(query)}
                            tabIndex={0}
                            role="button"
                            aria-label={`View details for query: ${truncateQuery(query.query, 40)}`}
                            onKeyDown={(e: React.KeyboardEvent) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault();
                                    handleQueryClick(query);
                                }
                            }}
                        >
                            <Typography
                                sx={QUERY_CELL_SX}
                                title={query.query}
                            >
                                {truncateQuery(
                                    query.query, MAX_QUERY_LENGTH
                                )}
                            </Typography>
                            <Typography sx={NUMERIC_CELL_SX}>
                                {formatNumber(query.calls)}
                            </Typography>
                            <Typography sx={NUMERIC_CELL_SX}>
                                {formatTime(query.total_exec_time)}
                            </Typography>
                            <Typography sx={NUMERIC_CELL_SX}>
                                {formatTime(query.mean_exec_time)}
                            </Typography>
                            <Typography sx={NUMERIC_CELL_SX}>
                                {formatNumber(query.rows)}
                            </Typography>
                        </Box>
                    ))}
                </Box>
            )}
        </CollapsibleSection>
    );
};

export default TopQueriesSection;
