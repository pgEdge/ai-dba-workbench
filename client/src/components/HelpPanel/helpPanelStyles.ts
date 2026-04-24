/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { alpha, type Theme } from '@mui/material/styles';

/**
 * Static style constants (Issue 23)
 */
export const styles = {
    navItemIcon: { minWidth: 32 },
    navItemIconSize: { fontSize: 18 },
    chevronActive: { fontSize: 16 },
    sectionTitleWrapper: {
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        mb: 1.5,
        mt: 3,
    },
    sectionTitleIcon: { fontSize: 18, color: 'primary.main' },
    sectionTitleText: {
        fontSize: '1rem',
        fontWeight: 600,
        color: 'text.primary',
    },
    helpTipText: {
        fontSize: '1rem',
        color: 'text.secondary',
        lineHeight: 1.5,
    },
    featureTitle: {
        fontWeight: 600,
        fontSize: '1rem',
        color: 'text.primary',
        mb: 0.25,
    },
    featureDescription: {
        fontSize: '1rem',
        color: 'text.secondary',
        lineHeight: 1.5,
    },
    featureWrapper: { mb: 1.5 },
    shortcutKeyBase: {
        fontFamily: '"JetBrains Mono", monospace',
        fontSize: '0.875rem',
        fontWeight: 600,
        color: 'text.primary',
        px: 0.75,
        py: 0.25,
        borderRadius: 0.5,
        border: '1px solid',
    },
    shortcutRow: { display: 'flex', alignItems: 'center', gap: 1, mb: 1 },
    shortcutKeysRow: { display: 'flex', gap: 0.5 },
    shortcutDescription: { fontSize: '1rem', color: 'text.secondary' },
    drawerContent: {
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
    },
    headerWrapper: {
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        p: 2,
        borderBottom: '1px solid',
        borderColor: 'divider',
    },
    headerFlex: { flex: 1 },
    breadcrumbsOl: {
        '& .MuiBreadcrumbs-ol': { flexWrap: 'nowrap' },
    },
    breadcrumbLink: {
        fontSize: '1rem',
        color: 'text.secondary',
        textDecoration: 'none',
        '&:hover': { textDecoration: 'underline' },
    },
    breadcrumbCurrent: {
        fontSize: '1rem',
        fontWeight: 600,
        color: 'text.primary',
    },
    chevronSeparator: { fontSize: 14, color: 'text.disabled' },
    closeIconSize: { fontSize: 20 },
    backIconSize: { fontSize: 20 },
    backButton: { mr: 0.5 },
    mainContentArea: { display: 'flex', flex: 1, overflow: 'hidden' },
    navSidebar: {
        width: 180,
        borderRight: '1px solid',
        borderColor: 'divider',
        p: 1.5,
        overflowY: 'auto',
    },
    contentArea: {
        flex: 1,
        p: 3,
        overflowY: 'auto',
    },
    footerWrapper: {
        p: 2,
        borderTop: '1px solid',
        borderColor: 'divider',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
    },
    footerVersion: { fontSize: '0.875rem' },
    footerCopyright: { fontSize: '0.875rem' },
    pageHeading: { fontWeight: 600, mb: 2 },
    bodyText: { color: 'text.secondary', lineHeight: 1.6 },
    bodyTextMb3: { color: 'text.secondary', mb: 3, lineHeight: 1.6 },
    bodyTextMb2: { color: 'text.secondary', mb: 2, lineHeight: 1.6 },
    indentedBlock: { pl: 2, mb: 2 },
    severityChipsRow: { display: 'flex', gap: 1, mb: 3 },
    severityChipBase: { fontWeight: 600, fontSize: '0.875rem' },
};

/**
 * Theme-dependent style getters (Issue 22 + 23)
 */

export const getDrawerPaperSx = () => ({
    '& .MuiDrawer-paper': {
        width: { xs: '100%', sm: 560 },
        bgcolor: 'background.default',
    },
});

export const getNavItemSx = (isActive: boolean) => (theme: Theme) => ({
    borderRadius: 1,
    mb: 0.5,
    py: 0.75,
    bgcolor: isActive
        ? alpha(theme.palette.primary.main, 0.15)
        : 'transparent',
    '&:hover': {
        bgcolor: isActive
            ? alpha(theme.palette.primary.main, 0.2)
            : alpha(theme.palette.grey[500], 0.1),
    },
});

export const getNavItemIconColor = (isActive: boolean): string =>
    isActive ? 'primary.main' : 'text.secondary';

export const getNavItemLabelProps = (isActive: boolean) => ({
    fontSize: '1rem',
    fontWeight: isActive ? 600 : 500,
    color: isActive ? 'primary.main' : 'text.primary',
});

export const getHelpTipSx = (theme: Theme) => ({
    display: 'flex',
    gap: 1,
    p: 1.5,
    mt: 2,
    borderRadius: 1,
    bgcolor: alpha(theme.palette.primary.main, 0.08),
    border: '1px solid',
    borderColor: alpha(theme.palette.primary.main, 0.2),
});

export const getSeverityChipSx = (paletteKey: 'error' | 'warning' | 'info') => (theme: Theme) => ({
    ...styles.severityChipBase,
    bgcolor: alpha(theme.palette[paletteKey].main, 0.15),
    color: theme.palette[paletteKey].main,
});
