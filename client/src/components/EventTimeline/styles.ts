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
import { Theme } from '@mui/material/styles';

// ---- Tooltip styles ----
export const tooltipPaddingSx = { p: 0.5 };
export const tooltipClusterTitleSx = { fontSize: '0.875rem', fontWeight: 600 };
export const tooltipClusterItemSx = { fontSize: '0.875rem', color: 'grey.300' };
export const tooltipClusterMoreSx = { fontSize: '0.875rem', color: 'grey.400' };
export const tooltipSingleTitleSx = { fontSize: '0.875rem', fontWeight: 600 };
export const tooltipSingleTimeSx = { fontSize: '0.875rem', color: 'grey.300' };
export const tooltipSingleServerSx = { fontSize: '0.875rem', color: 'grey.400' };

// ---- Cluster badge styles ----
const clusterBadgeBaseSx = {
    position: 'absolute',
    top: -6,
    right: -6,
    minWidth: 16,
    height: 16,
    px: 0.5,
    borderRadius: '8px',
    color: 'common.white',
    fontSize: '0.875rem',
    fontWeight: 700,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    lineHeight: 1,
};

export const getClusterBadgeSx = (theme: Theme) => ({
    ...clusterBadgeBaseSx,
    bgcolor: theme.palette.grey[600],
});

// ---- Detail section styles ----
export const sectionLabelSx = {
    fontSize: '0.875rem',
    fontWeight: 600,
    color: 'text.secondary',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    mb: 0.5,
};

export const sectionLabelShortSx = {
    ...sectionLabelSx,
    mb: 0.25,
};

export const getCodeBlockSx = (theme: Theme) => ({
    p: 1,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.grey[100],
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
    maxHeight: 300,
    overflow: 'auto',
});

export const getCodeBlockSmallSx = (theme: Theme) => ({
    ...getCodeBlockSx(theme),
    fontSize: '0.875rem',
});

export const settingNameSx = {
    color: 'primary.main',
    fontWeight: 600,
    fontFamily: 'inherit',
    fontSize: 'inherit',
};

export const settingValueSx = {
    color: 'text.secondary',
    fontFamily: 'inherit',
    fontSize: 'inherit',
};

export const hbaRuleTextSx = {
    fontFamily: 'inherit',
    fontSize: 'inherit',
    color: 'text.primary',
};

export const falsePositiveChipSx = (theme: Theme) => ({
    ml: 1,
    height: 16,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    bgcolor: alpha(theme.palette.warning.main, 0.15),
    color: theme.palette.warning.main,
    '& .MuiChip-label': { px: 0.5 },
});

export const metricValueMonoSx = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '1rem',
    fontWeight: 600,
};

export const metricUnitSx = {
    fontWeight: 400,
    color: 'text.secondary',
    fontSize: '0.875rem',
    ml: 0.5,
};

export const thresholdSx = {
    fontWeight: 400,
    color: 'text.secondary',
    fontSize: '0.875rem',
};

export const severityChipSx = (color) => ({
    height: 18,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
    bgcolor: alpha(color, 0.15),
    color: color,
    '& .MuiChip-label': { px: 0.75 },
});

export const restartCodeBlockSx = (theme: Theme) => ({
    p: 1,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.grey[100],
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
});

export const databaseNameSx = (theme: Theme) => ({
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '1rem',
    fontWeight: 500,
    color: theme.palette.secondary.main,
});

export const ackNameSx = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '1rem',
    fontWeight: 500,
    color: 'text.primary',
};

export const ackMessageSx = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    mt: 0.5,
    fontStyle: 'italic',
};

export const expandableShowMoreBaseSx = {
    mt: 0.5,
    pt: 0.5,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    gap: 0.5,
    color: 'primary.main',
    fontSize: '0.875rem',
    fontWeight: 500,
    '&:hover': {
        textDecoration: 'underline',
    },
};

export const expandIconSmallSx = { fontSize: 14 };

// ---- Collapsible card styles ----
export const getCollapsibleCardSx = (theme: Theme) => ({
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.3)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : theme.palette.divider,
    flexShrink: 0,
});

export const getCollapsibleHeaderHoverSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    p: 1.5,
    cursor: 'pointer',
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.2)
            : alpha(theme.palette.divider, 0.3),
    },
});

export const collapsibleTitleSx = {
    fontWeight: 600,
    fontSize: '1rem',
    color: 'text.primary',
    lineHeight: 1.3,
};

export const collapsibleTimeSx = {
    fontSize: '0.875rem',
    color: 'text.secondary',
};

export const serverDisabledSx = { color: 'text.disabled', fontSize: 'inherit' };

export const collapseToggleSx = {
    p: 0.25,
    color: 'text.secondary',
};

export const collapseContentSx = { px: 1.5, pb: 1.5 };

export const summarySx = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    lineHeight: 1.4,
    mb: 1,
};

// ---- Detail panel styles ----
export const getDetailPanelSx = (theme: Theme) => ({
    mt: 2,
    p: 2,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.6)
        : theme.palette.background.paper,
    border: '1px solid',
    borderColor: theme.palette.divider,
    boxShadow: theme.palette.mode === 'dark'
        ? 'inset 0 1px 2px rgba(0, 0, 0, 0.2)'
        : 'inset 0 1px 2px rgba(0, 0, 0, 0.05)',
});

export const getDetailPanelHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    mb: 2,
    pb: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
});

export const detailPanelTitleSx = {
    fontWeight: 600,
    fontSize: '1rem',
    color: 'text.primary',
};

export const detailPanelSubtitleSx = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    mt: 0.25,
};

export const getCloseButtonSx = (theme: Theme) => ({
    p: 0.5,
    color: 'text.secondary',
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[700], 0.5)
            : alpha(theme.palette.divider, 0.5),
    },
});

export const closeIconSx = { fontSize: 18 };

export const clusterListSx = {
    display: 'flex',
    flexDirection: 'column',
    gap: 1.5,
    maxHeight: 400,
    overflow: 'auto',
    pb: 0.5,
};

// ---- Timeline canvas styles ----
export const timelineCanvasContainerSx = {
    position: 'relative',
    height: 80,
    mt: 1,
    mx: 1,
};

export const timeAxisSx = {
    position: 'absolute',
    bottom: 0,
    left: 16,
    right: 16,
    height: 20,
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'flex-end',
};

export const getTickMarkSx = (theme: Theme) => ({
    width: 1,
    height: 4,
    bgcolor: theme.palette.grey[theme.palette.mode === 'dark' ? 600 : 300],
    mb: 0.25,
});

export const tickLabelSx = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    whiteSpace: 'nowrap',
};

export const getTimelineTrackSx = (theme: Theme) => ({
    position: 'absolute',
    top: 24,
    left: 0,
    right: 0,
    height: 32,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.3)
        : alpha(theme.palette.divider, 0.5),
    border: '1px solid',
    borderColor: theme.palette.divider,
});

// ---- Header styles ----
export const headerContainerSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    flexWrap: 'wrap',
};

export const headerTitleGroupSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    cursor: 'pointer',
    '&:hover': { opacity: 0.8 },
};

export const headerTitleIconSx = { fontSize: 16, color: 'primary.main' };

export const headerTitleTextSx = {
    fontWeight: 600,
    color: 'text.primary',
    fontSize: '1rem',
};

export const headerExpandSx = { p: 0.25 };
export const expandIconMedSx = { fontSize: 16 };

export const filterChipsSx = { display: 'flex', gap: 0.5, flexWrap: 'wrap' };

export const getToggleGroupSx = (theme: Theme) => ({
    height: 24,
    border: '1px solid',
    borderColor: theme.palette.divider,
    borderRadius: 1,
    '& .MuiToggleButton-root': {
        px: 1,
        py: 0,
        fontSize: '0.875rem',
        fontWeight: 600,
        textTransform: 'none',
        color: 'text.secondary',
        border: 'none',
        borderRadius: 0,
        '&.Mui-selected': {
            bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1),
            color: 'primary.main',
            '&:hover': {
                bgcolor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.2 : 0.15),
            },
        },
        '&:hover': {
            bgcolor: theme.palette.mode === 'dark'
                ? alpha(theme.palette.grey[700], 0.5)
                : alpha(theme.palette.divider, 0.5),
        },
        '&:first-of-type': {
            borderTopLeftRadius: 3,
            borderBottomLeftRadius: 3,
        },
        '&:last-of-type': {
            borderTopRightRadius: 3,
            borderBottomRightRadius: 3,
        },
    },
});

export const getEventCountChipSx = (theme, eventCount) => ({
    height: 18,
    fontSize: '0.875rem',
    fontWeight: 600,
    bgcolor: eventCount > 0
        ? alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.15 : 0.1)
        : alpha(theme.palette.grey[500], theme.palette.mode === 'dark' ? 0.2 : 0.1),
    color: eventCount > 0 ? 'primary.main' : 'text.secondary',
    '& .MuiChip-label': { px: 0.5 },
});

export const getFilterChipSx = (theme, isSelected, color) => ({
    height: 20,
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
    bgcolor: isSelected
        ? alpha(color, theme.palette.mode === 'dark' ? 0.2 : 0.15)
        : 'transparent',
    color: isSelected ? color : 'text.disabled',
    border: '1px solid',
    borderColor: isSelected
        ? alpha(color, 0.4)
        : theme.palette.divider,
    '& .MuiChip-label': { px: 0.5 },
    '&:hover': {
        bgcolor: alpha(color, theme.palette.mode === 'dark' ? 0.15 : 0.1),
    },
});

// ---- Loading skeleton styles ----
export const loadingSkeletonRowSx = { display: 'flex', alignItems: 'center', gap: 1, mb: 1 };

export const loadingSkeletonBarSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.grey[700]
        : theme.palette.grey[200],
});

export const loadingSkeletonTimeSx = { display: 'flex', justifyContent: 'space-between', mt: 0.5 };

// ---- Empty state styles ----
export const getEmptyStateSx = (theme: Theme) => ({
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    py: 3,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.2)
        : alpha(theme.palette.grey[100], 0.5),
    border: '1px dashed',
    borderColor: theme.palette.divider,
    mt: 1,
});

export const emptyStateTitleSx = {
    color: 'text.secondary',
    fontSize: '1rem',
    fontWeight: 500,
};

export const emptyStateSubtitleSx = {
    color: 'text.disabled',
    fontSize: '0.875rem',
};

// ---- Outer container ----
export const getOuterContainerSx = (theme: Theme) => ({
    mt: 2,
    p: 1.5,
    borderRadius: 1.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.4)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: theme.palette.divider,
});
