/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { Theme } from '@mui/material/styles';
import { SxProps } from '@mui/material';

/**
 * Common text field styling with hover and focus states.
 */
export const textFieldSx: SxProps<Theme> = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&:hover .MuiOutlinedInput-notchedOutline': {
            borderColor: 'grey.400',
        },
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: 'primary.main',
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: 'primary.main',
    },
};

/**
 * Dialog paper styling with rounded corners.
 */
export const dialogPaperSx: SxProps<Theme> = {
    borderRadius: 2,
};

/**
 * Dialog title styling.
 */
export const dialogTitleSx: SxProps<Theme> = {
    fontWeight: 600,
    color: 'text.primary',
    pb: 1,
};

/**
 * Section label styling for form sections.
 */
export const sectionLabelSx: SxProps<Theme> = {
    color: 'text.secondary',
    mb: 1,
    mt: 1,
    textTransform: 'uppercase',
    fontSize: '0.75rem',
    letterSpacing: '0.05em',
};

/**
 * Options section label styling with additional top margin.
 */
export const optionsSectionLabelSx: SxProps<Theme> = {
    ...sectionLabelSx,
    mt: 2,
};

/**
 * SSL accordion container styling.
 */
export const sslAccordionSx: SxProps<Theme> = {
    mt: 2,
    '&:before': { display: 'none' },
    border: '1px solid',
    borderColor: 'grey.200',
    borderRadius: '8px !important',
};

/**
 * Accordion summary styling.
 */
export const accordionSummarySx: SxProps<Theme> = {
    minHeight: 48,
    '&.Mui-expanded': { minHeight: 48 },
};

/**
 * SSL section label styling.
 */
export const sslLabelSx: SxProps<Theme> = {
    color: 'text.secondary',
    textTransform: 'uppercase',
    fontSize: '0.75rem',
    letterSpacing: '0.05em',
};

/**
 * SSL mode input label styling.
 */
export const sslModeLabelSx: SxProps<Theme> = {
    '&.Mui-focused': { color: 'primary.main' },
};

/**
 * Checkbox styling with primary color when checked.
 */
export const checkboxSx: SxProps<Theme> = {
    '&.Mui-checked': {
        color: 'primary.main',
    },
};

/**
 * Form control label styling.
 */
export const formControlLabelSx: SxProps<Theme> = {
    '& .MuiFormControlLabel-label': {
        fontSize: '0.875rem',
        color: 'text.primary',
    },
};

/**
 * Cancel button styling.
 */
export const cancelButtonSx: SxProps<Theme> = {
    color: 'text.secondary',
    textTransform: 'none',
    fontWeight: 500,
};

/**
 * Returns save button styling with theme-aware colors.
 */
export const getSaveButtonSx = (theme: Theme): SxProps<Theme> => ({
    textTransform: 'none',
    fontWeight: 600,
    minWidth: 80,
    background: theme.palette.primary.main,
    boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
    '&:hover': {
        background: theme.palette.primary.dark,
        boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
    },
    '&.Mui-disabled': {
        background: theme.palette.grey[200],
        color: theme.palette.grey[400],
    },
});

/**
 * Dialog actions styling.
 */
export const dialogActionsSx: SxProps<Theme> = {
    px: 3,
    pb: 2,
};
