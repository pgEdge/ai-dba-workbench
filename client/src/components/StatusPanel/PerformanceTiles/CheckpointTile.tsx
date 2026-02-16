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
import { ConnectionPerformance, CheckpointTimeSeries } from './types';

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
                                    formatter: (value: string) => {
                                        const d = new Date(value);
                                        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
                                    },
                                },
                            },
                            yAxis: {
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
        </TileContainer>
    );
};

export default CheckpointTile;
