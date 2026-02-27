/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { SxProps } from '@mui/material';
import { Theme } from '@mui/material/styles';

/**
 * Returns sx overrides for select and combo box components to bypass
 * the MUI notch measurement issue caused by Slide transitions.
 * Hides the fieldset legend and gives the floating label a solid
 * background so it covers the border line reliably.
 *
 * @param bgcolor - Theme palette path for the label background;
 *                  defaults to 'background.paper'.
 */
export const getSelectFieldSx = (
    bgcolor: string = 'background.paper',
): SxProps<Theme> => ({
    '& .MuiOutlinedInput-notchedOutline legend': {
        width: 0,
    },
    '& .MuiInputLabel-shrink': {
        bgcolor,
        px: 0.75,
        ml: -0.25,
    },
    '& .MuiOutlinedInput-root.Mui-disabled .MuiOutlinedInput-notchedOutline': {
        borderColor: 'divider',
    },
});

/**
 * Standard sx overrides for select fields on background.paper surfaces
 * (e.g. inside Dialog components).
 */
export const SELECT_FIELD_SX = getSelectFieldSx('background.paper');

/**
 * Standard sx overrides for select fields on background.default surfaces
 * (e.g. main page panels, fullscreen dialog content areas).
 */
export const SELECT_FIELD_DEFAULT_BG_SX = getSelectFieldSx('background.default');
