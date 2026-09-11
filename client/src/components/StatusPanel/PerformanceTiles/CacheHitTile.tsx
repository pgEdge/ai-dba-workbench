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
import { useMemo, useState, useCallback } from 'react';
import { Box, Typography, useTheme, IconButton, Tooltip } from '@mui/material';
import { Psychology as PsychologyIcon } from '@mui/icons-material';
import { Chart } from '../../Chart/Chart';
import { ChartAnalysisDialog } from '../../ChartAnalysisDialog';
import type { ChartAnalysisContext } from '../../Chart/types';
import TileContainer from './TileContainer';
import type { ConnectionPerformance, DatabaseCacheHitData } from './types';
import { TILE_VALUE_SX, getCacheColor } from './styles';
import { useAICapabilities } from '../../../contexts/useAICapabilities';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';
import { CHART_AXIS_LABEL_FONTSIZE } from '../../../theme';

interface CacheHitTileProps {
    connections: ConnectionPerformance[];
    loading: boolean;
    isMultiServer: boolean;
    /** Per-database cache hit data for single-server view */
    databaseData?: DatabaseCacheHitData[];
}

/**
 * Description passed to the AI analysis dialog. The caveat matters
 * because the ratio only sees shared_buffers: a block read may still
 * be served from the OS page cache at memory speed.
 */
const CACHE_HIT_DESCRIPTION =
    'Buffer cache hit ratio per interval showing shared_buffers '
    + 'effectiveness (null buckets had no block access). blks_hit '
    + 'counts shared_buffers hits only; a block read may still be '
    + 'served from the OS page cache at memory speed, so a lower ratio '
    + 'does not by itself mean slow I/O.';

/**
 * Narrow a `current` value to a number. The server sends null when the
 * latest interval saw no block access, and older payloads may omit the
 * field entirely; neither is a candidate for the headline.
 */
const hasCurrent = (value: number | null | undefined): value is number =>
    typeof value === 'number';

/**
 * Find the database with the worst (lowest) cache hit ratio.
 * Returns database name and value, or null if no database has one.
 */
const findWorstDatabase = (
    databases: DatabaseCacheHitData[]
): { name: string; value: number } | null => {
    let worst: { name: string; value: number } | null = null;
    databases.forEach(db => {
        const current = db.cache_hit_ratio?.current;
        if (hasCurrent(current) && (worst === null || current < worst.value)) {
            worst = { name: db.database_name, value: current };
        }
    });
    return worst;
};

/**
 * CacheHitTile shows a large cache hit ratio percentage with a
 * mini sparkline chart below it. Ratios are per interval; a null
 * `current` (no block access in the latest interval) is skipped when
 * choosing the headline, and null buckets in the series are drawn as
 * gaps rather than as 0%.
 *
 * For single-server view with database data:
 *   - Shows one series per database
 *   - Headline displays the worst (lowest) ratio with database name
 *
 * For cluster or estate views:
 *   - Shows one series per server
 *   - Headline displays the worst (lowest) server ratio
 */
const CacheHitTile: React.FC<CacheHitTileProps> = ({
    connections,
    loading,
    isMultiServer,
    databaseData,
}) => {
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();

    // Determine if we should use per-database view
    const usePerDatabaseView = !isMultiServer && databaseData && databaseData.length > 0;

    // Find the headline value and optional label
    const headlineInfo = useMemo(() => {
        if (usePerDatabaseView && databaseData) {
            const worst = findWorstDatabase(databaseData);
            if (worst) {
                return {
                    value: worst.value,
                    label: worst.name,
                    showLabel: databaseData.length > 1,
                };
            }
            return null;
        }

        // Multi-server or fallback: use server-level data
        if (!connections.length) {return null;}

        if (isMultiServer) {
            let worstValue = Infinity;
            let worstName = '';
            connections.forEach(conn => {
                const current = conn.cache_hit_ratio?.current;
                if (hasCurrent(current) && current < worstValue) {
                    worstValue = current;
                    worstName = conn.connection_name || `Server ${conn.connection_id}`;
                }
            });
            if (worstValue === Infinity) {return null;}
            return {
                value: worstValue,
                label: worstName,
                showLabel: connections.length > 1,
            };
        }

        // Single server without database data - fallback to connection data
        const current = connections[0]?.cache_hit_ratio?.current;
        if (!hasCurrent(current)) {return null;}
        return { value: current, label: null, showLabel: false };
    }, [connections, isMultiServer, usePerDatabaseView, databaseData]);

    // Build chart data
    const chartData = useMemo(() => {
        if (usePerDatabaseView && databaseData && databaseData.length > 0) {
            // Per-database series for single-server view
            let categories: string[] = [];
            const series: { name: string; data: (number | null)[] }[] = [];

            databaseData.forEach(db => {
                const ts = db.cache_hit_ratio?.time_series;
                if (!ts?.length) {return;}

                if (categories.length === 0) {
                    categories = ts.map(p => p.time);
                }

                series.push({
                    name: db.database_name,
                    data: ts.map(p => p.value),
                });
            });

            if (series.length === 0) {return null;}
            return { categories, series };
        }

        // Multi-server or fallback view
        if (!connections.length) {return null;}

        // For single server without database data, use one series
        if (!isMultiServer || connections.length === 1) {
            const conn = connections[0];
            const ts = conn?.cache_hit_ratio?.time_series;
            if (!ts?.length) {return null;}

            return {
                categories: ts.map(p => p.time),
                series: [{ name: 'Cache Hit %', data: ts.map(p => p.value) }],
            };
        }

        // For multi-server, create one series per connection
        let categories: string[] = [];
        const series: { name: string; data: (number | null)[] }[] = [];

        connections.forEach(conn => {
            const ts = conn.cache_hit_ratio?.time_series;
            if (!ts?.length) {return;}

            if (categories.length === 0) {
                categories = ts.map(p => p.time);
            }

            series.push({
                name: conn.connection_name || `Server ${conn.connection_id}`,
                data: ts.map(p => p.value),
            });
        });

        if (series.length === 0) {return null;}

        return { categories, series };
    }, [connections, isMultiServer, usePerDatabaseView, databaseData]);

    const hasData = headlineInfo !== null;
    const color = hasData ? getCacheColor(headlineInfo.value) : undefined;

    const [analysisOpen, setAnalysisOpen] = useState(false);
    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);
    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const analysisContext: ChartAnalysisContext | undefined = hasData ? {
        metricDescription: CACHE_HIT_DESCRIPTION,
        connectionId: connections[0]?.connection_id,
        connectionName: connections[0]?.connection_name,
    } : undefined;

    const isCached = analysisContext ? hasCachedAnalysis(
        analysisContext.metricDescription,
        analysisContext.connectionId,
        analysisContext.databaseName,
        analysisContext.timeRange,
    ) : false;

    // Determine the label to show (database name for single-server, "(worst)" for multi)
    const showWorstLabel = headlineInfo?.showLabel;
    const labelText = isMultiServer ? '(worst)' : headlineInfo?.label;

    return (
        <TileContainer
            title="Cache Hit Ratio"
            loading={loading}
            hasData={hasData}
            headerRight={headlineInfo !== null ? (
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5 }}>
                    <Typography sx={{ ...TILE_VALUE_SX, fontSize: '1.25rem', color }}>
                        {headlineInfo.value.toFixed(1)}
                    </Typography>
                    <Typography sx={{
                        fontSize: '0.875rem',
                        fontWeight: 600,
                        color: 'text.secondary',
                    }}>
                        %
                    </Typography>
                    {showWorstLabel && labelText && (
                        <Typography sx={{
                            fontSize: '0.875rem',
                            color: 'text.disabled',
                            ml: 0.5,
                            maxWidth: isMultiServer ? 'none' : 100,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }}>
                            {isMultiServer ? labelText : `(${labelText})`}
                        </Typography>
                    )}
                </Box>
            ) : undefined}
        >
            {chartData && (
                <Box sx={{
                    flex: 1,
                    minHeight: 0,
                    '& > .MuiPaper-root': {
                        p: 0,
                        boxShadow: 'none',
                        bgcolor: 'transparent',
                    },
                }}>
                    <Chart
                        type="line"
                        data={chartData}
                        height={150}
                        smooth
                        showToolbar={false}
                        showLegend={false}
                        showTooltip
                        echartsOptions={{
                            grid: { top: 8, right: 8, bottom: 20, left: 8, containLabel: true },
                            xAxis: {
                                boundaryGap: false,
                                axisLabel: {
                                    fontSize: CHART_AXIS_LABEL_FONTSIZE,
                                    color: theme.palette.text.secondary,
                                    interval: 'auto',
                                    hideOverlap: true,
                                    formatter: (value: string) => {
                                        const d = new Date(value);
                                        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
                                    },
                                },
                            },
                            yAxis: {
                                scale: true,
                                splitNumber: 3,
                                axisLabel: {
                                    fontSize: CHART_AXIS_LABEL_FONTSIZE,
                                    color: theme.palette.text.secondary,
                                    formatter: (value: number) => `${Math.round(value)}%`,
                                },
                                splitLine: {
                                    lineStyle: { opacity: 0.3 },
                                },
                            },
                        }}
                    />
                </Box>
            )}
            {aiEnabled && analysisContext && chartData && (
                <Tooltip title="AI Analysis">
                    <IconButton
                        size="small"
                        color={isCached ? 'warning' : 'secondary'}
                        onClick={handleAnalyzeClick}
                        sx={{
                            position: 'absolute',
                            top: 8,
                            right: 8,
                            zIndex: 1,
                        }}
                    >
                        <PsychologyIcon sx={{ fontSize: 16 }} />
                    </IconButton>
                </Tooltip>
            )}
            {aiEnabled && analysisContext && chartData && (
                <ChartAnalysisDialog
                    open={analysisOpen}
                    onClose={handleAnalysisClose}
                    isDark={theme.palette.mode === 'dark'}
                    analysisContext={analysisContext}
                    chartData={chartData}
                />
            )}
        </TileContainer>
    );
};

export default CacheHitTile;
