/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared style constants for PerformanceTiles
 */

import { SxProps, Theme } from '@mui/material/styles';
import { alpha } from '@mui/material';

export const TILE_GRID_SX: SxProps<Theme> = {
    display: 'grid',
    gridTemplateColumns: 'repeat(4, 1fr)',
    gap: 2,
    mt: 2,
    mb: 2,
    '@media (max-width: 1200px)': {
        gridTemplateColumns: 'repeat(2, 1fr)',
    },
    '@media (max-width: 600px)': {
        gridTemplateColumns: '1fr',
    },
};

export const getTilePaperSx = (theme: Theme): SxProps<Theme> => ({
    position: 'relative',
    p: 2,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[800], 0.8)
        : theme.palette.grey[100],
    border: '1px solid',
    borderColor: theme.palette.divider,
    height: 220,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    minWidth: 0,
});

export const TILE_TITLE_SX: SxProps<Theme> = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    mb: 1,
};

export const TILE_VALUE_SX: SxProps<Theme> = {
    fontWeight: 700,
    fontSize: '1.75rem',
    lineHeight: 1,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

export const TILE_CONTENT_SX: SxProps<Theme> = {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    minHeight: 0,
};

export const NO_DATA_SX: SxProps<Theme> = {
    color: 'text.disabled',
    fontSize: '1rem',
    fontWeight: 500,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flex: 1,
};

export const getCacheColor = (value: number): string => {
    if (value >= 95) return '#4caf50';
    if (value >= 90) return '#ff9800';
    return '#f44336';
};

export const getXidColor = (percent: number): string => {
    if (percent < 50) return '#4caf50';
    if (percent <= 75) return '#ff9800';
    return '#f44336';
};
