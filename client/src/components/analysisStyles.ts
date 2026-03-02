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
 * Shared style constants for analysis dialog components.
 * Extracted from QueryAnalysisDialog, AlertAnalysisDialog,
 * ChartAnalysisDialog, and ServerAnalysisDialog to eliminate
 * duplication.
 */

import { alpha } from '@mui/material';
import { Theme } from '@mui/material/styles';
import { sxMonoFont } from './shared/MarkdownExports';

/** Grey badge for connection or server pills. */
export const getConnectionBadgeSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: alpha(
        theme.palette.grey[500],
        theme.palette.mode === 'dark' ? 0.2 : 0.1
    ),
});

/** Alias for getConnectionBadgeSx (used in server/alert contexts). */
export const getServerBadgeSx = getConnectionBadgeSx;

/** Secondary-color badge for database pills. */
export const getDatabaseBadgeSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.1
    ),
});

/** Secondary-color monospace text for database names. */
export const getDatabaseTextSx = (theme: Theme) => ({
    fontSize: '0.875rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    ...sxMonoFont,
});

/** Small monospace text for badge labels. */
export const sxMonoSmall = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    ...sxMonoFont,
};
