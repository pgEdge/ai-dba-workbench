/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { alpha } from '@mui/material';
import type { Theme } from '@mui/material/styles';

/** Monospace font family for technical values. */
export const MONO_FONT = '"JetBrains Mono", "SF Mono", monospace';

/** LocalStorage key for section collapsed state. */
export const SECTION_STATE_KEY = 'serverInfoSectionState';

/** Monospace style object for sx prop. */
export const sxMono = { fontFamily: MONO_FONT };

/** Main content area styles with custom scrollbar. */
export const getContentSx = (theme: Theme) => ({
    flex: 1,
    overflow: 'auto',
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    '&::-webkit-scrollbar': { width: 6 },
    '&::-webkit-scrollbar-thumb': {
        borderRadius: 3,
        backgroundColor: theme.palette.mode === 'dark' ? '#475569' : '#D1D5DB',
    },
    '&::-webkit-scrollbar-track': {
        backgroundColor: 'transparent',
    },
});

/** Clickable section header row. */
export const getSectionHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    px: 2.5,
    py: 1,
    cursor: 'pointer',
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.3)
        : alpha(theme.palette.grey[100], 0.5),
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.background.paper, 0.5)
            : alpha(theme.palette.grey[100], 0.8),
    },
    userSelect: 'none',
});

/** Section header icon. */
export const getSectionIconSx = (theme: Theme) => ({
    fontSize: 16,
    color: theme.palette.primary.main,
});

/** Section title text. */
export const getSectionTitleSx = () => ({
    fontSize: '1rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
    color: 'text.secondary',
    flex: 1,
});

/** Section content wrapper. */
export const getSectionContentSx = (theme: Theme) => ({
    px: 2.5,
    py: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
});

/** Key-value grid container. */
export const getKvGridSx = () => ({
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
    gap: 1.5,
});

/** Key-value label. */
export const getKvLabelSx = (theme: Theme) => ({
    fontSize: '0.875rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.1em',
    lineHeight: 1,
    color: theme.palette.grey[500],
    mb: 0.25,
});

/** Key-value value. */
export const getKvValueSx = () => ({
    fontSize: '1rem',
    fontWeight: 500,
    lineHeight: 1.3,
    color: 'text.primary',
    ...sxMono,
    wordBreak: 'break-word' as const,
});

/** Linear progress bar with color based on percentage. */
export const getProgressBarSx = (theme: Theme, percentage: number) => ({
    height: 4,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : alpha(theme.palette.grey[200], 0.8),
    '& .MuiLinearProgress-bar': {
        borderRadius: 2,
        bgcolor: percentage > 90
            ? theme.palette.error.main
            : percentage > 75
                ? theme.palette.warning.main
                : theme.palette.primary.main,
    },
});

/** Database row container. */
export const getDbRowSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1.5,
    py: 1,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.5),
    '&:last-child': { borderBottom: 'none', pb: 0 },
    '&:first-of-type': { pt: 0 },
});

/** Extension chip. */
export const getExtChipSx = (theme: Theme) => ({
    display: 'inline-flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.4)
        : alpha(theme.palette.grey[200], 0.6),
    fontSize: '1rem',
    ...sxMono,
    color: 'text.secondary',
});

/** AI analysis box container. */
export const getAiBoxSx = (theme: Theme) => ({
    mt: 0.5,
    px: 1.25,
    py: 0.75,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.4)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.5),
});

/** Configuration setting row. */
export const getSettingRowSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    gap: 2,
    py: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.3),
    '&:last-child': { borderBottom: 'none' },
});
