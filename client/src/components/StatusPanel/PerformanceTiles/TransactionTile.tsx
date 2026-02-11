/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import { Box } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Chart } from '../../Chart/Chart';
import TileContainer from './TileContainer';
import { ConnectionPerformance, TransactionTimeSeries } from './types';

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
            grid: { top: 8, right: 4, bottom: 30, left: 4, containLabel: true },
            legend: { bottom: 2 },
            xAxis: {
                boundaryGap: false,
                axisLabel: {
                    ...axisLabelStyle,
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
                },
                {
                    type: 'value',
                    axisLabel: axisLabelStyle,
                    max: 100,
                    splitLine: { show: false },
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
                        height={165}
                        showToolbar={false}
                        showLegend
                        showTooltip
                        echartsOptions={dualAxisOptions}
                    />
                </Box>
            )}
        </TileContainer>
    );
};

export default TransactionTile;
