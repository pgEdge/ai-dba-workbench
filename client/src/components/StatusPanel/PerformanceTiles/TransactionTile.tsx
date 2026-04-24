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
import { ConnectionPerformance, TransactionTimeSeries } from './types';
import { useAICapabilities } from '../../../contexts/useAICapabilities';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';

interface TransactionTileProps {
    connections: ConnectionPerformance[];
    loading: boolean;
}

/**
 * TransactionTile shows a dual-axis chart with commits per second
 * as an area chart on the left y-axis and rollback percentage as
 * a dashed line on the right y-axis.
 */
const TransactionTile: React.FC<TransactionTileProps> = ({
    connections,
    loading,
}) => {
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();

    const { chartData, hasData, dualAxisOptions } = useMemo(() => {
        const allPoints: TransactionTimeSeries[] = [];

        connections.forEach(conn => {
            if (conn.transactions?.time_series?.length) {
                allPoints.push(...conn.transactions.time_series);
            }
        });

        if (!allPoints.length) {
            return { chartData: null, hasData: false, dualAxisOptions: {} };
        }

        allPoints.sort((a, b) => a.time.localeCompare(b.time));

        const data = {
            categories: allPoints.map(p => p.time),
            series: [
                { name: 'Commits/sec', data: allPoints.map(p => p.commits_per_sec) },
                { name: 'Rollback %', data: allPoints.map(p => p.rollback_percent) },
            ],
        };

        const axisLabelStyle = { fontSize: 14, color: theme.palette.text.secondary };

        const options = {
            grid: { top: 8, right: 4, bottom: 20, left: 4, containLabel: true },
            xAxis: {
                boundaryGap: false,
                axisLabel: {
                    ...axisLabelStyle,
                    interval: 'auto',
                    hideOverlap: true,
                    formatter: (value: string) => {
                        const d = new Date(value);
                        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
                    },
                },
            },
            yAxis: [
                {
                    type: 'value',
                    axisLabel: axisLabelStyle,
                    splitLine: { show: true },
                    splitNumber: 3,
                },
                {
                    type: 'value',
                    axisLabel: axisLabelStyle,
                    max: 100,
                    splitLine: { show: false },
                    splitNumber: 3,
                },
            ],
            series: [
                {
                    type: 'line',
                    name: 'Commits/sec',
                    data: allPoints.map(p => p.commits_per_sec),
                    yAxisIndex: 0,
                    areaStyle: { opacity: 0.3 },
                    smooth: true,
                    symbol: 'none',
                },
                {
                    type: 'line',
                    name: 'Rollback %',
                    data: allPoints.map(p => p.rollback_percent),
                    yAxisIndex: 1,
                    lineStyle: { type: 'dashed' },
                    smooth: true,
                    symbol: 'none',
                },
            ],
        };

        return { chartData: data, hasData: true, dualAxisOptions: options };
    }, [connections, theme]);

    const [analysisOpen, setAnalysisOpen] = useState(false);
    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);
    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const analysisContext: ChartAnalysisContext | undefined = hasData ? {
        metricDescription: 'Transaction commit rate and rollback percentage',
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
            title="Transactions"
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
                        showToolbar={false}
                        showLegend={false}
                        showTooltip
                        echartsOptions={dualAxisOptions}
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

export default TransactionTile;
