/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared style constants and style-getter functions for AdminPanel
 * components. All colors reference the MUI theme to ensure consistency
 * with light and dark modes.
 */

const FONT_WEIGHT_SEMIBOLD = 600;

// --- Static style constants (theme-independent) ---

export const tableHeaderCellSx = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const dialogTitleSx = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const dialogActionsSx = {
    px: 3,
    pb: 2,
};

export const sectionHeaderSx = {
    display: 'flex',
    alignItems: 'center',
    mb: 1,
};

export const sectionTitleSx = {
    flex: 1,
    fontWeight: FONT_WEIGHT_SEMIBOLD,
};

export const pageHeadingSx = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    flex: 1,
    color: 'text.primary',
};

export const loadingContainerSx = {
    display: 'flex',
    justifyContent: 'center',
    py: 8,
};

export const categoryLabelSx = {
    fontSize: '0.7rem',
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    color: 'text.secondary',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

export const subsectionLabelSx = {
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    color: 'text.secondary',
    textTransform: 'uppercase',
    fontSize: '0.75rem',
};

export const emptyRowSx = {
    py: 2,
};

export const emptyRowTextSx = {
    fontSize: '0.875rem',
};

// --- Theme-dependent style-getter functions ---

/**
 * Returns sx props for the primary action button (contained variant).
 */
export const getContainedButtonSx = (theme) => ({
    textTransform: 'none',
    fontWeight: FONT_WEIGHT_SEMIBOLD,
    bgcolor: theme.palette.primary.main,
    '&:hover': { bgcolor: theme.palette.primary.dark },
});

/**
 * Returns sx props for a text-style grant/action button.
 */
export const getTextButtonSx = (theme) => ({
    textTransform: 'none',
    color: theme.palette.primary.main,
});

/**
 * Returns sx props for the delete/revoke icon button.
 */
export const getDeleteIconSx = (theme) => ({
    color: theme.palette.error.main,
});

/**
 * Returns sx props for the success check icon.
 */
export const getSuccessIconSx = (theme) => ({
    color: theme.palette.success.main,
    fontSize: 20,
});

/**
 * Returns sx props for the inactive/cancelled icon.
 */
export const getInactiveIconSx = (theme) => ({
    color: theme.palette.grey[400],
    fontSize: 20,
});

/**
 * Returns sx props for a bordered table container.
 */
export const getTableContainerSx = (theme) => ({
    border: '1px solid',
    borderColor: theme.palette.divider,
    borderRadius: 1,
});

/**
 * Returns sx props for a radio button with primary accent.
 */
export const getRadioSx = (theme) => ({
    '&.Mui-checked': { color: theme.palette.primary.main },
});

/**
 * Returns sx props for a focused InputLabel inside FormControl.
 */
export const getFocusedLabelSx = (theme) => ({
    '&.Mui-focused': { color: theme.palette.primary.main },
});
