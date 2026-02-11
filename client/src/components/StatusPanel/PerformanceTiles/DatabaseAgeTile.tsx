/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Box, Typography, LinearProgress } from '@mui/material';
import TileContainer from './TileContainer';
import { ConnectionPerformance, XidAgeEntry } from './types';
import { getXidColor } from './styles';

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
                                    fontSize: '0.6875rem',
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
                                    fontSize: '0.625rem',
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
        </TileContainer>
    );
};

export default DatabaseAgeTile;
