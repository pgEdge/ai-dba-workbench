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
import { useMemo } from 'react';
import { Box, Paper, Typography, Skeleton } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { getTilePaperSx, TILE_TITLE_SX, TILE_CONTENT_SX, NO_DATA_SX } from './styles';

interface TileContainerProps {
    title: string;
    headerRight?: React.ReactNode;
    loading: boolean;
    hasData: boolean;
    children: React.ReactNode;
}

/**
 * Shared Paper wrapper for each performance tile.
 * Shows a skeleton placeholder when loading and a "No data"
 * message when the tile has no data to display.
 */
const TileContainer: React.FC<TileContainerProps> = ({
    title,
    headerRight,
    loading,
    hasData,
    children,
}) => {
    const theme = useTheme();
    const paperSx = useMemo(() => getTilePaperSx(theme), [theme]);

    return (
        <Paper elevation={0} sx={paperSx}>
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1, mb: 1 }}>
                <Typography sx={{ ...TILE_TITLE_SX, mb: 0 }}>{title}</Typography>
                {headerRight}
            </Box>
            <Box sx={TILE_CONTENT_SX}>
                {loading ? (
                    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <Skeleton variant="rounded" width="60%" height={28} />
                        <Skeleton variant="rounded" width="100%" height={60} />
                        <Skeleton variant="rounded" width="80%" height={20} />
                    </Box>
                ) : !hasData ? (
                    <Typography sx={NO_DATA_SX}>No data</Typography>
                ) : (
                    children
                )}
            </Box>
        </Paper>
    );
};

export default TileContainer;
