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
import { Box, Typography } from '@mui/material';
import { Chart } from '../../Chart/Chart';
import TileContainer from './TileContainer';
import { ConnectionPerformance, CacheHitTimeSeries } from './types';
import { TILE_VALUE_SX, getCacheColor } from './styles';

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
    // Find the headline value: worst ratio for multi-server, current for single
    const headlineValue = useMemo(() => {
        if (!connections.length) return null;

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

    // Build sparkline data from the first connection (or aggregate)
    const chartData = useMemo(() => {
        const allSeries: CacheHitTimeSeries[] = [];

        connections.forEach(conn => {
            if (conn.cache_hit_ratio?.time_series?.length) {
                allSeries.push(...conn.cache_hit_ratio.time_series);
            }
        });

        if (!allSeries.length) return null;

        // Sort by time and deduplicate for multi-server
        allSeries.sort((a, b) => a.time.localeCompare(b.time));

        return {
            categories: allSeries.map(p => p.time),
            series: [{ name: 'Cache Hit %', data: allSeries.map(p => p.value) }],
        };
    }, [connections]);

    const hasData = headlineValue !== null;
    const color = hasData ? getCacheColor(headlineValue) : undefined;

    return (
        <TileContainer
            title="Cache Hit Ratio"
            loading={loading}
            hasData={hasData}
        >
            <Box sx={{ display: 'flex', flexDirection: 'column', flex: 1 }}>
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5, mb: 0.5 }}>
                    <Typography sx={{ ...TILE_VALUE_SX, color }}>
                        {headlineValue !== null ? headlineValue.toFixed(1) : '--'}
                    </Typography>
                    <Typography sx={{
                        fontSize: '1rem',
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
                            height={100}
                            smooth
                            areaFill
                            showToolbar={false}
                            showLegend={false}
                            showTooltip
                            echartsOptions={{
                                grid: { top: 4, right: 4, bottom: 4, left: 4 },
                                xAxis: { show: false, boundaryGap: false },
                                yAxis: { show: false },
                            }}
                        />
                    </Box>
                )}
            </Box>
        </TileContainer>
    );
};

export default CacheHitTile;
