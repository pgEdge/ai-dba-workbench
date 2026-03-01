/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useState, useCallback } from 'react';
import { Box, Typography, useTheme, IconButton, Tooltip } from '@mui/material';
import PsychologyIcon from '@mui/icons-material/Psychology';
import { Chart } from '../../Chart/Chart';
import { ChartAnalysisDialog } from '../../ChartAnalysisDialog';
import { ChartAnalysisContext } from '../../Chart/types';
import TileContainer from './TileContainer';
import { ConnectionPerformance } from './types';
import { TILE_VALUE_SX, getCacheColor } from './styles';
import { useAICapabilities } from '../../../contexts/AICapabilitiesContext';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';

interface CacheHitTileProps {
    connections: ConnectionPerformance[];
    loading: boolean;
    isMultiServer: boolean;
}

/**
 * CacheHitTile shows a large cache hit ratio percentage with a
 * mini sparkline chart below it. For cluster or estate views,
 * the headline displays the worst (lowest) ratio.
 */
const CacheHitTile: React.FC<CacheHitTileProps> = ({
    connections,
    loading,
    isMultiServer,
}) => {
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();
    // Find the headline value: worst ratio for multi-server, current for single
    const headlineValue = useMemo(() => {
        if (!connections.length) {return null;}

        if (isMultiServer) {
            let worst = Infinity;
            connections.forEach(conn => {
                if (conn.cache_hit_ratio?.current !== undefined) {
                    worst = Math.min(worst, conn.cache_hit_ratio.current);
                }
            });
            return worst === Infinity ? null : worst;
        }

        return connections[0]?.cache_hit_ratio?.current ?? null;
    }, [connections, isMultiServer]);

    // Build chart data: one series per connection for multi-server,
    // or a single series for single-server views
    const chartData = useMemo(() => {
        if (!connections.length) {return null;}

        // For single server, use one series
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
        const series: Array<{ name: string; data: number[] }> = [];

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
    }, [connections, isMultiServer]);

    const hasData = headlineValue !== null;
    const color = hasData ? getCacheColor(headlineValue) : undefined;

    const [analysisOpen, setAnalysisOpen] = useState(false);
    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);
    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const analysisContext: ChartAnalysisContext | undefined = hasData ? {
        metricDescription: 'Buffer cache hit ratio showing cache effectiveness',
        connectionId: connections[0]?.connection_id,
        connectionName: connections[0]?.connection_name,
    } : undefined;

    const isCached = analysisContext ? hasCachedAnalysis(
        analysisContext.metricDescription,
        analysisContext.connectionId,
        analysisContext.databaseName,
        analysisContext.timeRange,
    ) : false;

    return (
        <TileContainer
            title="Cache Hit Ratio"
            loading={loading}
            hasData={hasData}
            headerRight={headlineValue !== null ? (
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5 }}>
                    <Typography sx={{ ...TILE_VALUE_SX, fontSize: '1.25rem', color }}>
                        {headlineValue.toFixed(1)}
                    </Typography>
                    <Typography sx={{
                        fontSize: '0.875rem',
                        fontWeight: 600,
                        color: 'text.secondary',
                    }}>
                        %
                    </Typography>
                    {isMultiServer && (
                        <Typography sx={{
                            fontSize: '0.875rem',
                            color: 'text.disabled',
                            ml: 0.5,
                        }}>
                            (worst)
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
                                    fontSize: 14,
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
                                    fontSize: 14,
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
