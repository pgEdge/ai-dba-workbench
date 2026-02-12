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
 * Shared style constants for Dashboard components
 */

import { SxProps, Theme } from '@mui/material/styles';
import { alpha } from '@mui/material';

/** Section container */
export const SECTION_CONTAINER_SX: SxProps<Theme> = {
    mb: 2,
};

/** Section header (collapsible) */
export const SECTION_HEADER_SX: SxProps<Theme> = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    py: 0.5,
    '&:hover': { opacity: 0.8 },
};

/** Section title */
export const SECTION_TITLE_SX: SxProps<Theme> = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1rem',
};

/** KPI tile grid (similar to TILE_GRID_SX but more flexible) */
export const KPI_GRID_SX: SxProps<Theme> = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
    gap: 2,
    mb: 2,
};

/** Metric chart container */
export const CHART_SECTION_SX: SxProps<Theme> = {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, 1fr)',
    gap: 2,
    mb: 2,
    '@media (max-width: 900px)': {
        gridTemplateColumns: '1fr',
    },
};

/** Overlay container (positioned within the status panel) */
export const OVERLAY_CONTAINER_SX: SxProps<Theme> = {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 10,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
};

/** Overlay header bar */
export const OVERLAY_HEADER_SX: SxProps<Theme> = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    px: 2,
    py: 1.5,
    borderBottom: '1px solid',
    borderColor: 'divider',
};

/** Overlay title */
export const OVERLAY_TITLE_SX: SxProps<Theme> = {
    fontWeight: 600,
    fontSize: '1.1rem',
    flex: 1,
};

/** Overlay content area */
export const OVERLAY_CONTENT_SX: SxProps<Theme> = {
    flex: 1,
    overflow: 'auto',
    p: 3,
};

/** Theme-dependent dashboard tile paper style */
export const getDashboardTileSx = (theme: Theme): SxProps<Theme> => ({
    p: 2,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[800], 0.8)
        : theme.palette.grey[100],
    border: '1px solid',
    borderColor: theme.palette.divider,
    cursor: 'pointer',
    transition: 'border-color 0.2s, box-shadow 0.2s',
    '&:hover': {
        borderColor: theme.palette.primary.main,
        boxShadow: `0 0 0 1px ${alpha(theme.palette.primary.main, 0.3)}`,
    },
});

/** KPI tile label */
export const KPI_LABEL_SX: SxProps<Theme> = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    mb: 0.5,
};

/** KPI tile value */
export const KPI_VALUE_SX: SxProps<Theme> = {
    fontWeight: 700,
    fontSize: '1.75rem',
    lineHeight: 1,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

/** KPI tile unit */
export const KPI_UNIT_SX: SxProps<Theme> = {
    fontSize: '0.875rem',
    fontWeight: 500,
    color: 'text.secondary',
    ml: 0.5,
};

/** KPI trend container */
export const KPI_TREND_SX: SxProps<Theme> = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
    mt: 0.5,
};

/** Leaderboard row */
export const LEADERBOARD_ROW_SX: SxProps<Theme> = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    py: 0.75,
    px: 1,
    '&:not(:last-child)': {
        borderBottom: '1px solid',
        borderColor: 'divider',
    },
};

/** Leaderboard name */
export const LEADERBOARD_NAME_SX: SxProps<Theme> = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
    fontWeight: 500,
    color: 'text.primary',
};

/** Leaderboard value */
export const LEADERBOARD_VALUE_SX: SxProps<Theme> = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
    fontWeight: 600,
    color: 'text.secondary',
};

/** Time range selector container */
export const TIME_RANGE_CONTAINER_SX: SxProps<Theme> = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
};
