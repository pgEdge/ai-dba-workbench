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
 * Shared style constants and style-getter functions for AdminPanel
 * components. All colors reference the MUI theme to ensure consistency
 * with light and dark modes.
 */

import type { SxProps, Theme } from '@mui/material/styles';

const FONT_WEIGHT_SEMIBOLD = 600;

// --- Static style constants (theme-independent) ---

export const tableHeaderCellSx: SxProps<Theme> = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const dialogTitleSx: SxProps<Theme> = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const dialogActionsSx: SxProps<Theme> = {
    px: 3,
    pb: 2,
};

export const sectionHeaderSx: SxProps<Theme> = {
    display: 'flex',
    alignItems: 'center',
    mb: 1,
};

export const sectionTitleSx: SxProps<Theme> = {
    flex: 1,
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const pageHeadingSx: SxProps<Theme> = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    flex: 1,
    color: 'text.primary',
};

export const loadingContainerSx: SxProps<Theme> = {
    display: 'flex',
    justifyContent: 'center',
    py: 8,
};

export const categoryLabelSx: SxProps<Theme> = {
    fontSize: '0.875rem',
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    color: 'text.secondary',
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
};

export const subsectionLabelSx: SxProps<Theme> = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    color: 'text.secondary',
    textTransform: 'uppercase',
    fontSize: '0.875rem',
};

export const emptyRowSx: SxProps<Theme> = {
    py: 2,
};

export const emptyRowTextSx: SxProps<Theme> = {
    fontSize: '1rem',
};

// --- Theme-dependent style-getter functions ---

/**
 * Returns sx props for the primary action button (contained variant).
 */
export const getContainedButtonSx = (theme: Theme): SxProps<Theme> => ({
    textTransform: 'none',
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    bgcolor: theme.palette.primary.main,
    '&:hover': { bgcolor: theme.palette.primary.dark },
});

/**
 * Returns sx props for a text-style grant/action button.
 */
export const getTextButtonSx = (theme: Theme): SxProps<Theme> => ({
    textTransform: 'none',
    color: theme.palette.primary.main,
});

/**
 * Returns sx props for the delete/revoke icon button.
 */
export const getDeleteIconSx = (theme: Theme): SxProps<Theme> => ({
    color: theme.palette.error.main,
});

/**
 * Returns sx props for the success check icon.
 */
export const getSuccessIconSx = (theme: Theme): SxProps<Theme> => ({
    color: theme.palette.success.main,
    fontSize: 20,
});

/**
 * Returns sx props for the inactive/cancelled icon.
 */
export const getInactiveIconSx = (theme: Theme): SxProps<Theme> => ({
    color: theme.palette.grey[400],
    fontSize: 20,
});

/**
 * Returns sx props for a bordered table container.
 */
export const getTableContainerSx = (theme: Theme): SxProps<Theme> => ({
    border: '1px solid',
    borderColor: theme.palette.divider,
    borderRadius: 1,
});

/**
 * Returns sx props for a radio button with primary accent.
 */
export const getRadioSx = (theme: Theme): SxProps<Theme> => ({
    '&.Mui-checked': { color: theme.palette.primary.main },
});

/**
 * Returns sx props for a focused InputLabel inside FormControl.
 */
export const getFocusedLabelSx = (theme: Theme): SxProps<Theme> => ({
    '&.Mui-focused': { color: theme.palette.primary.main },
});
