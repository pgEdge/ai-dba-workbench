/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Style constants and style-getter functions
 * for markdown rendering and analysis dialog components.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { alpha } from '@mui/material';
import { Theme } from '@mui/material/styles';

// ---------------------------------------------------------------------------
// Static sx constants
// ---------------------------------------------------------------------------

export const sxMonoFont = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

export const sxH3 = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1.0625rem',
    mt: 2,
    mb: 0.75,
};

export const sxParagraph = {
    color: 'text.primary',
    fontSize: '1rem',
    lineHeight: 1.7,
    my: 1,
};

export const sxList = {
    pl: 2.5,
    my: 1.5,
    '& li': {
        mb: 0.75,
        fontSize: '1rem',
        lineHeight: 1.6,
        color: 'text.primary',
    },
};

export const sxUnorderedList = {
    ...sxList,
    listStyleType: 'disc',
};

export const sxStrong = { fontWeight: 600 };
export const sxEm = { fontStyle: 'italic' };
export const sxSkeletonRow = { display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 };
export const sxSkeletonContainer = { py: 2 };

// Static sx constants shared by dialog components
export const sxContentFadeBox = { mt: 3 };
export const sxErrorFlexRow = { display: 'flex', alignItems: 'flex-start', gap: 1.5 };
export const sxTitleFlexBox = { flex: 1, minWidth: 0 };
export const sxCloseIconSize = { fontSize: 20 };

export const sxTitleTypography = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1.125rem',
    lineHeight: 1.3,
};

export const sxConfirmationActions = {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 1,
    mt: 1,
};

// ---------------------------------------------------------------------------
// Theme-dependent style-getter functions (markdown / code block)
// ---------------------------------------------------------------------------

export const getHeadingSx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.secondary.main,
    pb: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.2 : 0.15),
});

export const sxH1 = (theme: Theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1.75rem',
    mt: 2,
    mb: 1,
});

export const sxH2 = (theme: Theme) => ({
    ...getHeadingSx(theme),
    fontSize: '1.25rem',
    mt: 2.5,
    mb: 1,
});

export const getInlineCodeSx = (theme: Theme) => ({
    ...sxMonoFont,
    fontSize: '1rem',
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.6)
        : alpha(theme.palette.grey[200], 0.8),
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[200]
        : theme.palette.grey[700],
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
});

export const getCodeBlockWrapperSx = (theme: Theme) => ({
    my: 1.5,
    borderRadius: 1,
    overflow: 'hidden',
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
    '& pre': {
        margin: '0 !important',
        borderRadius: '0 !important',
    },
});

export const getCodeBlockCustomStyle = (customBackground) => ({
    margin: 0,
    padding: '1rem',
    fontSize: '1rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    background: customBackground,
});

export const getLinkSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.dark,
    textDecoration: 'none',
    '&:hover': {
        textDecoration: 'underline',
    },
});

export const getBlockquoteSx = (theme: Theme) => ({
    borderLeft: '3px solid',
    borderColor: theme.palette.secondary.main,
    pl: 2,
    ml: 0,
    my: 1.5,
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    fontStyle: 'italic',
});

export const getTableSx = (theme: Theme) => ({
    width: '100%',
    borderCollapse: 'collapse',
    my: 1.5,
    fontSize: '1rem',
    '& th, & td': {
        border: '1px solid',
        borderColor: theme.palette.mode === 'dark'
            ? theme.palette.grey[700]
            : theme.palette.grey[200],
        p: 1,
        textAlign: 'left',
    },
    '& th': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.5)
            : alpha(theme.palette.grey[100], 0.8),
        fontWeight: 600,
    },
});

// ---------------------------------------------------------------------------
// Style-getter functions (code block action buttons)
// ---------------------------------------------------------------------------

export const getCodeBlockButtonGroupSx = () => ({
    position: 'absolute',
    top: 6,
    right: 6,
    display: 'flex',
    gap: 0.5,
});

const getCodeBlockActionButtonSx = (theme: Theme) => ({
    minWidth: 0,
    width: 28,
    height: 28,
    p: 0,
    borderRadius: 0.75,
    bgcolor: alpha(
        theme.palette.background.paper,
        theme.palette.mode === 'dark' ? 0.6 : 0.8,
    ),
    color: alpha(theme.palette.secondary.main, 0.9),
    opacity: 0.8,
    transition: 'opacity 0.15s, background-color 0.15s, color 0.15s',
    '&:hover': {
        opacity: 1,
        bgcolor: alpha(
            theme.palette.background.paper,
            theme.palette.mode === 'dark' ? 0.85 : 0.95,
        ),
        color: theme.palette.secondary.main,
    },
});

export const getCopyButtonSx = (theme: Theme) => ({
    ...getCodeBlockActionButtonSx(theme),
});

export const getRunButtonSx = (theme: Theme) => ({
    ...getCodeBlockActionButtonSx(theme),
    position: 'absolute',
    top: 6,
    right: 6,
});

export const getQueryResultWrapperSx = (theme: Theme) => ({
    mt: 0,
    mb: 1.5,
    border: '1px solid',
    borderTop: 'none',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
    borderRadius: '0 0 4px 4px',
    overflow: 'hidden',
});

export const getQueryResultHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    px: 1.5,
    py: 0.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[800], 0.6)
        : alpha(theme.palette.grey[100], 0.8),
    borderBottom: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

export const getQueryErrorSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1,
    px: 1.5,
    py: 1,
    bgcolor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05,
    ),
});

export const getConfirmationPanelSx = (theme: Theme) => ({
    px: 1.5,
    py: 1.25,
    bgcolor: alpha(
        theme.palette.warning.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.06,
    ),
    borderTop: '1px solid',
    borderColor: alpha(
        theme.palette.warning.main,
        theme.palette.mode === 'dark' ? 0.25 : 0.2,
    ),
});

export const getConfirmationTitleSx = (_theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1,
    mb: 0.75,
});

export const getConfirmationTextSx = (theme: Theme) => ({
    fontSize: '1rem',
    fontWeight: 500,
    color: theme.palette.mode === 'dark'
        ? theme.palette.warning.light
        : theme.palette.warning.dark,
});

export const getConfirmationStatementSx = (theme: Theme) => ({
    fontSize: '0.875rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[300]
        : theme.palette.grey[700],
    ...sxMonoFont,
    pl: 3,
    py: 0.25,
});

// ---------------------------------------------------------------------------
// Style-getter functions (shared dialog styles)
// ---------------------------------------------------------------------------

export const getSkeletonBgSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

export const getDialogPaperSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    backgroundImage: 'none',
    borderRadius: 2,
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.divider
        : theme.palette.grey[200],
    boxShadow: theme.palette.mode === 'dark'
        ? '0 25px 50px -12px rgba(0, 0, 0, 0.5)'
        : '0 25px 50px -12px rgba(0, 0, 0, 0.15)',
});

export const getDialogTitleSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 2,
    pb: 2,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

export const getIconBoxSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 48,
    height: 48,
    borderRadius: 1.5,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.15 : 0.1
    ),
    position: 'relative',
    flexShrink: 0,
});

export const getIconColorSx = (theme: Theme) => ({
    fontSize: 28,
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
});

export const getContentSx = (theme: Theme) => ({
    p: 3,
    pt: 0,
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
});

export const getLoadingBannerSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    mb: 2,
    p: 1.5,
    borderRadius: 1,
    bgcolor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05
    ),
    border: '1px solid',
    borderColor: alpha(
        theme.palette.secondary.main,
        theme.palette.mode === 'dark' ? 0.2 : 0.15
    ),
});

export const getPulseDotSx = (theme: Theme) => ({
    width: 8,
    height: 8,
    borderRadius: '50%',
    bgcolor: theme.palette.secondary.main,
    animation: 'pulse 1.5s ease-in-out infinite',
    '@keyframes pulse': {
        '0%, 100%': { opacity: 1 },
        '50%': { opacity: 0.4 },
    },
});

export const getLoadingTextSx = (theme: Theme) => ({
    fontSize: '1rem',
    color: theme.palette.mode === 'dark'
        ? theme.palette.secondary.light
        : theme.palette.secondary.main,
    fontWeight: 500,
});

export const getErrorBoxSx = (theme: Theme) => ({
    p: 2.5,
    borderRadius: 1.5,
    bgcolor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.1 : 0.05
    ),
    border: '1px solid',
    borderColor: alpha(
        theme.palette.error.main,
        theme.palette.mode === 'dark' ? 0.25 : 0.2
    ),
});

export const getErrorTitleSx = (theme: Theme) => ({
    fontWeight: 600,
    color: theme.palette.mode === 'dark'
        ? theme.palette.error.light
        : theme.palette.error.dark,
    fontSize: '1rem',
    mb: 0.5,
});

export const getAnalysisBoxSx = (theme: Theme) => ({
    p: 2.5,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.6)
        : theme.palette.background.paper,
    border: '1px solid',
    borderColor: theme.palette.divider,
    boxShadow: theme.palette.mode === 'dark'
        ? '0 4px 6px -1px rgba(0, 0, 0, 0.2)'
        : '0 1px 3px 0 rgba(0, 0, 0, 0.05)',
});

export const getFooterSx = (theme: Theme) => ({
    px: 3,
    py: 2,
    borderTop: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

export const getDownloadButtonSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    '&:hover': {
        bgcolor: alpha(
            theme.palette.grey[400],
            0.1
        ),
    },
    '&.Mui-disabled': {
        color: theme.palette.mode === 'dark'
            ? theme.palette.grey[600]
            : theme.palette.grey[300],
    },
});

export const getCloseButtonSx = (theme: Theme) => ({
    color: 'text.secondary',
    '&:hover': {
        bgcolor: alpha(
            theme.palette.grey[400],
            0.1
        ),
    },
});
