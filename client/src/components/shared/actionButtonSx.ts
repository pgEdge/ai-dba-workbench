/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Button styles for the proposed action block
 * and its override dialog, chosen so every label clears WCAG AA.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { Theme } from '@mui/material/styles';

/**
 * A plain control (Retry, Cancel) in the neutral treatment the contrast
 * guidelines give: `primary.main` text reads at only 2.67:1 on the
 * light surface, so the label takes `text.primary` (14.68:1 light,
 * 13.35:1 dark) and an outlined button's edge `grey.500` (4.83:1 light,
 * 3.07:1 dark). See `.claude/react-expert/color-contrast-guidelines.md`.
 */
export function neutralButtonSx(theme: Theme) {
    return {
        textTransform: 'none',
        color: theme.palette.text.primary,
        borderColor: theme.palette.grey[500],
    } as const;
}

/**
 * "Run anyway" sits on the error fill, where white reads at 3.76:1, so
 * its label is the darkest grey (4.71:1 on `error.main`). The hover
 * shade, `error.dark`, is too dark for that grey (3.67:1) but takes
 * white at 4.83:1, so the label turns white on hover.
 */
export function runAnywayButtonSx(theme: Theme) {
    return {
        textTransform: 'none',
        color: theme.palette.grey[900],
        '&:hover': { color: theme.palette.common.white },
    } as const;
}
