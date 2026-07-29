/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import FormControl from '@mui/material/FormControl';
import FormControlLabel from '@mui/material/FormControlLabel';
import IconButton from '@mui/material/IconButton';
import InputLabel from '@mui/material/InputLabel';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import type { SelectChangeEvent } from '@mui/material/Select';
import Switch from '@mui/material/Switch';
import {
    QueryStats as QueryStatsIcon,
    ChevronLeft as ChevronLeftIcon,
    ChevronRight as ChevronRightIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useDashboard } from '../../../contexts/useDashboard';
import { useDatabaseSummaries } from '../../../hooks/useDatabaseSummaries';
import CollapsibleSection from '../CollapsibleSection';
import { formatTime, formatNumber } from '../../../utils/formatters';
import {
    DASHBOARD_CONTROL_TEXT_SX,
    DASHBOARD_TAB_CHIP_TEXT_SX,
} from '../../../theme/tokens';
import type { ServerSectionProps, TopQueryRow } from './types';
import { logger } from '../../../utils/logger';

/** Maximum characters to display before truncating a query */
const MAX_QUERY_LENGTH = 80;

/** Page sizes offered by the rows-per-page selector */
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

/** Default number of rows fetched per page */
const DEFAULT_PAGE_SIZE = 20;

/** Sentinel value used by the database filter for "no filter" */
const ALL_DATABASES = '';

/** Table container styles */
const TABLE_CONTAINER_SX = {
    overflowX: 'auto' as const,
    mb: 2,
};

/** Table header row */
const TABLE_HEADER_SX = {
    display: 'grid',
    gridTemplateColumns: '0.8fr 2fr 0.7fr 1fr 1fr 0.7fr',
    gap: 1,
    px: 1.5,
    py: 1,
    borderBottom: '2px solid',
    borderColor: 'divider',
};

/** Table header cell */
const HEADER_CELL_SX = {
    fontSize: '0.875rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
    color: 'text.secondary',
};

/** Table row */
const TABLE_ROW_SX = {
    display: 'grid',
    gridTemplateColumns: '0.8fr 2fr 0.7fr 1fr 1fr 0.7fr',
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
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    whiteSpace: 'nowrap' as const,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    color: 'text.primary',
};

/** Numeric cell */
const NUMERIC_CELL_SX = {
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontWeight: 500,
    textAlign: 'right' as const,
    color: 'text.primary',
};

/** Header controls container (database filter plus toggle) */
const HEADER_CONTROLS_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
};

/** Database filter control width */
const DB_FILTER_SX = {
    minWidth: 160,
    '& .MuiInputBase-root': DASHBOARD_CONTROL_TEXT_SX,
    '& .MuiInputLabel-root': DASHBOARD_CONTROL_TEXT_SX,
};

/** Footer bar holding the page-size selector and the pager */
const PAGER_BAR_SX = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    flexWrap: 'wrap' as const,
    gap: 1,
    pt: 1,
    borderTop: '1px solid',
    borderColor: 'divider',
};

/** Page-size chip row */
const PAGE_SIZE_GROUP_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    flexWrap: 'wrap' as const,
};

/** Page-size chip (inactive) */
const PAGE_SIZE_CHIP_SX = {
    ...DASHBOARD_TAB_CHIP_TEXT_SX,
    px: 1.5,
    py: 0.5,
    borderRadius: 1,
    border: '1px solid',
    borderColor: 'divider',
    cursor: 'pointer',
    transition: 'all 0.15s',
    bgcolor: 'transparent',
    color: 'text.secondary',
    fontFamily: 'inherit',
    '&:hover': {
        borderColor: 'primary.main',
        color: 'primary.main',
    },
};

/** Page-size chip (active) */
const PAGE_SIZE_CHIP_ACTIVE_SX = {
    ...PAGE_SIZE_CHIP_SX,
    bgcolor: 'primary.main',
    color: 'primary.contrastText',
    borderColor: 'primary.main',
    '&:hover': {
        bgcolor: 'primary.dark',
        borderColor: 'primary.dark',
    },
};

/** Pager controls container */
const PAGER_CONTROLS_SX = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
};

/** Pager status text */
const PAGER_STATUS_SX = {
    ...DASHBOARD_CONTROL_TEXT_SX,
    color: 'text.secondary',
    whiteSpace: 'nowrap' as const,
};

/**
 * Truncate a query string and clean up whitespace.
 */
const truncateQuery = (query: string, maxLen: number): string => {
    if (!query) { return ''; }
    const cleaned = query.replace(/\s+/g, ' ').trim();
    if (cleaned.length <= maxLen) { return cleaned; }
    return `${cleaned.substring(0, maxLen)}...`;
};

/**
 * Read the total row count advertised by the server in the
 * `X-Total-Count` response header. Returns null when the header is
 * absent or cannot be parsed as a non-negative integer, in which case
 * the caller falls back to short-page detection for the pager.
 */
const readTotalCount = (headers?: Headers): number | null => {
    let raw: string | null | undefined;
    try {
        raw = headers?.get('X-Total-Count');
    } catch {
        return null;
    }
    if (raw === null || raw === undefined) { return null; }
    const parsed = Number.parseInt(raw, 10);
    if (!Number.isFinite(parsed) || parsed < 0) { return null; }
    return parsed;
};

/**
 * Top Queries section displays the most resource-intensive queries
 * from pg_stat_statements, sorted by total execution time. Results
 * are paged server-side and can be filtered to a single database.
 * Each row is clickable to drill down into query detail via overlay.
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
    const [hideCollectorQueries, setHideCollectorQueries] = useState<boolean>(true);
    const [page, setPage] = useState<number>(0);
    const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE);
    const [databaseFilter, setDatabaseFilter] = useState<string>(ALL_DATABASES);
    const [totalCount, setTotalCount] = useState<number | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const userRef = useRef(user);
    userRef.current = user;

    const isLoggedIn = !!user;

    // Reset the paging and filter state during render when the
    // selected connection changes, so the next fetch never runs with
    // an offset or database name belonging to the previous server.
    const [renderedConnectionId, setRenderedConnectionId] =
        useState<number>(connectionId);
    if (renderedConnectionId !== connectionId) {
        setRenderedConnectionId(connectionId);
        setPage(0);
        setDatabaseFilter(ALL_DATABASES);
        setTotalCount(null);
    }

    // The database list drives the filter control only, so it tracks
    // the connection rather than the dashboard refresh cycle.
    const { databases } = useDatabaseSummaries(connectionId);

    const databaseNames = useMemo(
        () => databases.map(db => db.database_name).filter(Boolean),
        [databases],
    );

    // A single-database connection gains nothing from a filter, so the
    // control only appears once there is a genuine choice to make.
    const showDatabaseFilter = databaseNames.length > 1;

    const fetchData = useCallback(async (): Promise<void> => {
        if (!userRef.current) { return; }

        const params = new URLSearchParams({
            connection_id: connectionId.toString(),
            limit: pageSize.toString(),
            offset: (page * pageSize).toString(),
            order_by: 'total_exec_time',
            order: 'desc',
        });
        if (hideCollectorQueries) {
            params.set('exclude_collector', 'true');
        }
        if (databaseFilter !== ALL_DATABASES) {
            params.set('database_name', databaseFilter);
        }
        const url = `/api/v1/metrics/top-queries?${params.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await apiFetch(url);

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch top queries: ${response.status}`
                );
            }

            const total = readTotalCount(response.headers);
            const result = await response.json() as TopQueryRow[];

            if (isMountedRef.current) {
                const rows = Array.isArray(result) ? result : [];
                setQueries(rows);
                setTotalCount(total);
                initialLoadDoneRef.current = true;

                // A refresh (or a shrinking result set) can leave the
                // user stranded past the end of the data; step back to
                // the last page that still exists.
                if (rows.length === 0 && page > 0) {
                    setPage(
                        total === null
                            ? page - 1
                            : Math.max(0, Math.ceil(total / pageSize) - 1)
                    );
                }
            }
        } catch (err) {
            logger.error('Error fetching top queries:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch top queries'
                );
                setQueries([]);
                setTotalCount(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [connectionId, hideCollectorQueries, page, pageSize, databaseFilter]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId]);

    useEffect(() => {
        isMountedRef.current = true;

        if (isLoggedIn) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [isLoggedIn, fetchData, refreshTrigger]);

    const handleQueryClick = useCallback((query: TopQueryRow): void => {
        pushOverlay({
            level: 'object',
            title: `${query.database_name}: ${truncateQuery(query.query, 50)}`,
            entityId: query.queryid,
            entityName: query.query,
            objectName: query.queryid,
            objectType: 'query',
            connectionId,
            connectionName,
            databaseName: query.database_name,
        });
    }, [pushOverlay, connectionId, connectionName]);

    const handleToggleCollector = useCallback((
        event: React.ChangeEvent<HTMLInputElement>
    ): void => {
        setHideCollectorQueries(event.target.checked);
        setPage(0);
    }, []);

    const handleDatabaseChange = useCallback((
        event: SelectChangeEvent
    ): void => {
        setDatabaseFilter(event.target.value);
        setPage(0);
    }, []);

    const handlePageSizeChange = useCallback((size: number): void => {
        setPageSize(size);
        setPage(0);
    }, []);

    const handlePreviousPage = useCallback((): void => {
        setPage(prev => Math.max(0, prev - 1));
    }, []);

    const handleNextPage = useCallback((): void => {
        setPage(prev => prev + 1);
    }, []);

    const headerRowSx = useMemo(() => ({
        ...TABLE_HEADER_SX,
        borderColor: theme.palette.divider,
    }), [theme.palette.divider]);

    const firstRowNumber = page * pageSize + 1;
    const lastRowNumber = page * pageSize + queries.length;
    const hasPreviousPage = page > 0;
    const hasNextPage = totalCount === null
        ? queries.length === pageSize
        : lastRowNumber < totalCount;

    const rangeLabel = totalCount === null
        ? `Showing ${firstRowNumber}–${lastRowNumber}`
        : `Showing ${firstRowNumber}–${lastRowNumber} of ${totalCount}`;

    const isFiltered = databaseFilter !== ALL_DATABASES;
    const showPager = !error && (queries.length > 0 || hasPreviousPage);

    return (
        <CollapsibleSection
            title="Top Queries"
            icon={<QueryStatsIcon sx={{ fontSize: 16 }} />}
            defaultExpanded
            headerRight={
                <Box sx={HEADER_CONTROLS_SX}>
                    {showDatabaseFilter && (
                        <FormControl size="small" sx={DB_FILTER_SX}>
                            <InputLabel id="top-queries-database-filter-label">
                                Database
                            </InputLabel>
                            <Select
                                labelId="top-queries-database-filter-label"
                                id="top-queries-database-filter"
                                label="Database"
                                value={databaseFilter}
                                onChange={handleDatabaseChange}
                            >
                                <MenuItem value={ALL_DATABASES}>
                                    All databases
                                </MenuItem>
                                {databaseNames.map(name => (
                                    <MenuItem key={name} value={name}>
                                        {name}
                                    </MenuItem>
                                ))}
                            </Select>
                        </FormControl>
                    )}
                    <FormControlLabel
                        control={
                            <Switch
                                size="small"
                                checked={hideCollectorQueries}
                                onChange={handleToggleCollector}
                            />
                        }
                        label="Hide monitoring queries"
                        labelPlacement="start"
                        slotProps={{
                            typography: {
                                sx: {
                                    ...DASHBOARD_CONTROL_TEXT_SX,
                                    color: 'text.secondary',
                                },
                            },
                        }}
                        sx={{ mr: 0, gap: 1 }}
                    />
                </Box>
            }
        >
            {loading && queries.length === 0 && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                    <CircularProgress size={24} aria-label="Loading queries" />
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
                    {isFiltered
                        ? `No query statistics available for ${databaseFilter}.`
                        : 'No query statistics available. '
                            + 'Is the pg_stat_statements extension installed?'}
                </Typography>
            )}

            {queries.length > 0 && (
                <Box sx={TABLE_CONTAINER_SX}>
                    <Box sx={headerRowSx}>
                        <Typography sx={HEADER_CELL_SX}>
                            Database
                        </Typography>
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
                            onClick={() => { handleQueryClick(query); }}
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
                                sx={{
                                    fontSize: '0.875rem',
                                    whiteSpace: 'nowrap' as const,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    color: 'text.primary',
                                }}
                                title={query.database_name}
                            >
                                {query.database_name}
                            </Typography>
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

            {showPager && (
                <Box
                    component="nav"
                    aria-label="Top queries pagination"
                    sx={PAGER_BAR_SX}
                >
                    <Box
                        sx={PAGE_SIZE_GROUP_SX}
                        role="group"
                        aria-label="Rows per page"
                    >
                        <Typography sx={PAGER_STATUS_SX}>
                            Rows per page
                        </Typography>
                        {PAGE_SIZE_OPTIONS.map(size => (
                            <Box
                                key={size}
                                component="button"
                                type="button"
                                sx={size === pageSize
                                    ? PAGE_SIZE_CHIP_ACTIVE_SX
                                    : PAGE_SIZE_CHIP_SX}
                                aria-pressed={size === pageSize}
                                aria-label={`Show ${size} rows per page`}
                                onClick={() => { handlePageSizeChange(size); }}
                            >
                                {size}
                            </Box>
                        ))}
                    </Box>

                    <Box sx={PAGER_CONTROLS_SX}>
                        <Typography
                            sx={PAGER_STATUS_SX}
                            aria-live="polite"
                        >
                            {rangeLabel}
                        </Typography>
                        <IconButton
                            size="small"
                            aria-label="Previous page of queries"
                            disabled={!hasPreviousPage}
                            onClick={handlePreviousPage}
                        >
                            <ChevronLeftIcon fontSize="small" />
                        </IconButton>
                        <IconButton
                            size="small"
                            aria-label="Next page of queries"
                            disabled={!hasNextPage}
                            onClick={handleNextPage}
                        >
                            <ChevronRightIcon fontSize="small" />
                        </IconButton>
                    </Box>
                </Box>
            )}
        </CollapsibleSection>
    );
};

export default TopQueriesSection;
