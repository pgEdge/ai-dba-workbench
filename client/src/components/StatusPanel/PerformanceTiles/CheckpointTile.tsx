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
import { Box, IconButton, Tooltip } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import PsychologyIcon from '@mui/icons-material/Psychology';
import { Chart } from '../../Chart/Chart';
import { ChartAnalysisDialog } from '../../ChartAnalysisDialog';
import { ChartAnalysisContext } from '../../Chart/types';
import TileContainer from './TileContainer';
import { ConnectionPerformance, CheckpointTimeSeries } from './types';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';

interface CheckpointTileProps {
    connections: ConnectionPerformance[];
    loading: boolean;
}

/**
 * CheckpointTile shows a stacked area chart of checkpoint write
 * time and sync time over time.
 */
const CheckpointTile: React.FC<CheckpointTileProps> = ({
    connections,
    loading,
}) => {
    const theme = useTheme();

    const { chartData, hasData } = useMemo(() => {
        const allPoints: CheckpointTimeSeries[] = [];

        connections.forEach(conn => {
            if (conn.checkpoints?.time_series?.length) {
                allPoints.push(...conn.checkpoints.time_series);
            }
        });

        if (!allPoints.length) {
            return { chartData: null, hasData: false };
        }

        allPoints.sort((a, b) => a.time.localeCompare(b.time));

        const data = {
            categories: allPoints.map(p => p.time),
            series: [
                { name: 'Write Time (ms)', data: allPoints.map(p => p.write_time_ms) },
                { name: 'Sync Time (ms)', data: allPoints.map(p => p.sync_time_ms) },
            ],
        };

        return { chartData: data, hasData: true };
    }, [connections]);

    const [analysisOpen, setAnalysisOpen] = useState(false);
    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);
    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const analysisContext: ChartAnalysisContext | undefined = hasData ? {
        metricDescription: 'Checkpoint write time and sync time',
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
            title="Checkpoints"
            loading={loading}
            hasData={hasData}
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
                        stacked
                        areaFill
                        smooth
                        showToolbar={false}
                        showLegend
                        showTooltip
                        echartsOptions={{
                            grid: { top: 10, right: 10, bottom: 40, left: 10, containLabel: true },
                            legend: { bottom: 8, textStyle: { color: theme.palette.text.primary } },
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
                                splitNumber: 3,
                                axisLabel: {
                                    fontSize: 14,
                                    color: theme.palette.text.secondary,
                                    formatter: (value: number) => Math.round(value).toLocaleString(),
                                },
                            },
                        }}
                    />
                </Box>
            )}
            {analysisContext && chartData && (
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
            {analysisContext && chartData && (
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

export default CheckpointTile;
