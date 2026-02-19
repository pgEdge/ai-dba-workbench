/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback } from 'react';
import { Box, Typography, LinearProgress, IconButton, Tooltip } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import PsychologyIcon from '@mui/icons-material/Psychology';
import TileContainer from './TileContainer';
import { ChartAnalysisDialog } from '../../ChartAnalysisDialog';
import { ChartData, ChartAnalysisContext } from '../../Chart/types';
import { ConnectionPerformance, XidAgeEntry } from './types';
import { getXidColor } from './styles';
import { useAICapabilities } from '../../../contexts/AICapabilitiesContext';
import { hasCachedAnalysis } from '../../../hooks/useChartAnalysis';

interface DatabaseAgeTileProps {
    connections: ConnectionPerformance[];
    loading: boolean;
    isMultiServer: boolean;
}

interface AgeRow {
    serverName?: string;
    databaseName: string;
    age: number;
    percent: number;
}

/**
 * DatabaseAgeTile displays per-database XID age as colored
 * horizontal progress bars. Color shifts from green to yellow
 * to red as the percentage increases.
 */
const DatabaseAgeTile: React.FC<DatabaseAgeTileProps> = ({
    connections,
    loading,
    isMultiServer,
}) => {
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();
    const [analysisOpen, setAnalysisOpen] = useState(false);

    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);

    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const rows: AgeRow[] = [];

    connections.forEach(conn => {
        conn.xid_age?.forEach((entry: XidAgeEntry) => {
            rows.push({
                serverName: isMultiServer ? conn.connection_name : undefined,
                databaseName: entry.database_name,
                age: entry.age,
                percent: entry.percent,
            });
        });
    });

    // Sort by percent descending so the most urgent entries appear first
    rows.sort((a, b) => b.percent - a.percent);

    const analysisChartData: ChartData | null = rows.length > 0 ? {
        categories: rows.map(r => r.databaseName),
        series: [{ name: 'XID Age %', data: rows.map(r => r.percent) }],
    } : null;

    const analysisContext: ChartAnalysisContext | undefined = rows.length > 0 ? {
        metricDescription: 'Transaction ID (XID) age as percentage of wraparound limit per database',
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
            title="XID Age"
            loading={loading}
            hasData={rows.length > 0}
        >
            <Box sx={{
                flex: 1,
                overflow: 'auto',
                maxHeight: 170,
                pr: 1,
                '&::-webkit-scrollbar': { width: '3px' },
                '&::-webkit-scrollbar-thumb': {
                    borderRadius: '3px',
                    bgcolor: 'action.disabled',
                },
                '&::-webkit-scrollbar-track': {
                    bgcolor: 'transparent',
                },
            }}>
                {rows.map((row, idx) => {
                    const color = getXidColor(row.percent);
                    const label = isMultiServer
                        ? `${row.serverName} / ${row.databaseName}`
                        : row.databaseName;

                    return (
                        <Box key={idx} sx={{ mb: 0.75 }}>
                            <Box sx={{
                                display: 'flex',
                                justifyContent: 'space-between',
                                alignItems: 'baseline',
                                mb: 0.25,
                            }}>
                                <Typography sx={{
                                    fontSize: '0.875rem',
                                    fontWeight: 500,
                                    color: 'text.primary',
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    flex: 1,
                                    mr: 1,
                                }}>
                                    {label}
                                </Typography>
                                <Typography sx={{
                                    fontSize: '0.875rem',
                                    fontWeight: 600,
                                    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
                                    color,
                                    flexShrink: 0,
                                }}>
                                    {row.percent.toFixed(1)}%
                                </Typography>
                            </Box>
                            <LinearProgress
                                variant="determinate"
                                value={Math.min(row.percent, 100)}
                                sx={{
                                    height: 6,
                                    borderRadius: 3,
                                    bgcolor: 'action.hover',
                                    '& .MuiLinearProgress-bar': {
                                        borderRadius: 3,
                                        bgcolor: color,
                                    },
                                }}
                            />
                        </Box>
                    );
                })}
            </Box>
            {aiEnabled && analysisContext && analysisChartData && (
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
            {aiEnabled && analysisContext && analysisChartData && (
                <ChartAnalysisDialog
                    open={analysisOpen}
                    onClose={handleAnalysisClose}
                    isDark={theme.palette.mode === 'dark'}
                    analysisContext={analysisContext}
                    chartData={analysisChartData}
                />
            )}
        </TileContainer>
    );
};

export default DatabaseAgeTile;
