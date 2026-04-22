/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { createTheme, Theme } from '@mui/material/styles';
import {
    styles,
    getDrawerPaperSx,
    getNavItemSx,
    getNavItemIconColor,
    getNavItemLabelProps,
    getHelpTipSx,
    getSeverityChipSx,
} from '../helpPanelStyles';

describe('helpPanelStyles', () => {
    describe('styles object', () => {
        it('has navItemIcon style', () => {
            expect(styles.navItemIcon).toEqual({ minWidth: 32 });
        });

        it('has navItemIconSize style', () => {
            expect(styles.navItemIconSize).toEqual({ fontSize: 18 });
        });

        it('has chevronActive style', () => {
            expect(styles.chevronActive).toEqual({ fontSize: 16 });
        });

        it('has sectionTitleWrapper style', () => {
            expect(styles.sectionTitleWrapper).toEqual({
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                mb: 1.5,
                mt: 3,
            });
        });

        it('has sectionTitleIcon style', () => {
            expect(styles.sectionTitleIcon).toEqual({
                fontSize: 18,
                color: 'primary.main',
            });
        });

        it('has sectionTitleText style', () => {
            expect(styles.sectionTitleText).toEqual({
                fontSize: '1rem',
                fontWeight: 600,
                color: 'text.primary',
            });
        });

        it('has helpTipText style', () => {
            expect(styles.helpTipText).toEqual({
                fontSize: '1rem',
                color: 'text.secondary',
                lineHeight: 1.5,
            });
        });

        it('has featureTitle style', () => {
            expect(styles.featureTitle).toEqual({
                fontWeight: 600,
                fontSize: '1rem',
                color: 'text.primary',
                mb: 0.25,
            });
        });

        it('has featureDescription style', () => {
            expect(styles.featureDescription).toEqual({
                fontSize: '1rem',
                color: 'text.secondary',
                lineHeight: 1.5,
            });
        });

        it('has featureWrapper style', () => {
            expect(styles.featureWrapper).toEqual({ mb: 1.5 });
        });

        it('has shortcutKeyBase style', () => {
            expect(styles.shortcutKeyBase).toHaveProperty('fontFamily');
            expect(styles.shortcutKeyBase).toHaveProperty('fontSize');
            expect(styles.shortcutKeyBase).toHaveProperty('fontWeight');
        });

        it('has shortcutRow style', () => {
            expect(styles.shortcutRow).toEqual({
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                mb: 1,
            });
        });

        it('has shortcutKeysRow style', () => {
            expect(styles.shortcutKeysRow).toEqual({
                display: 'flex',
                gap: 0.5,
            });
        });

        it('has shortcutDescription style', () => {
            expect(styles.shortcutDescription).toEqual({
                fontSize: '1rem',
                color: 'text.secondary',
            });
        });

        it('has drawerContent style', () => {
            expect(styles.drawerContent).toEqual({
                display: 'flex',
                flexDirection: 'column',
                height: '100%',
            });
        });

        it('has headerWrapper style', () => {
            expect(styles.headerWrapper).toHaveProperty('display');
            expect(styles.headerWrapper).toHaveProperty('alignItems');
            expect(styles.headerWrapper).toHaveProperty('p');
        });

        it('has headerFlex style', () => {
            expect(styles.headerFlex).toEqual({ flex: 1 });
        });

        it('has breadcrumbsOl style', () => {
            expect(styles.breadcrumbsOl).toHaveProperty('& .MuiBreadcrumbs-ol');
        });

        it('has breadcrumbLink style', () => {
            expect(styles.breadcrumbLink).toHaveProperty('fontSize');
            expect(styles.breadcrumbLink).toHaveProperty('color');
            expect(styles.breadcrumbLink).toHaveProperty('textDecoration');
        });

        it('has breadcrumbCurrent style', () => {
            expect(styles.breadcrumbCurrent).toEqual({
                fontSize: '1rem',
                fontWeight: 600,
                color: 'text.primary',
            });
        });

        it('has closeIconSize style', () => {
            expect(styles.closeIconSize).toEqual({ fontSize: 20 });
        });

        it('has backIconSize style', () => {
            expect(styles.backIconSize).toEqual({ fontSize: 20 });
        });

        it('has backButton style', () => {
            expect(styles.backButton).toEqual({ mr: 0.5 });
        });

        it('has mainContentArea style', () => {
            expect(styles.mainContentArea).toEqual({
                display: 'flex',
                flex: 1,
                overflow: 'hidden',
            });
        });

        it('has navSidebar style', () => {
            expect(styles.navSidebar).toHaveProperty('width');
            expect(styles.navSidebar).toHaveProperty('borderRight');
        });

        it('has contentArea style', () => {
            expect(styles.contentArea).toEqual({
                flex: 1,
                p: 3,
                overflowY: 'auto',
            });
        });

        it('has footerWrapper style', () => {
            expect(styles.footerWrapper).toHaveProperty('p');
            expect(styles.footerWrapper).toHaveProperty('borderTop');
        });

        it('has pageHeading style', () => {
            expect(styles.pageHeading).toEqual({ fontWeight: 600, mb: 2 });
        });

        it('has bodyText style', () => {
            expect(styles.bodyText).toEqual({
                color: 'text.secondary',
                lineHeight: 1.6,
            });
        });

        it('has bodyTextMb3 style', () => {
            expect(styles.bodyTextMb3).toEqual({
                color: 'text.secondary',
                mb: 3,
                lineHeight: 1.6,
            });
        });

        it('has bodyTextMb2 style', () => {
            expect(styles.bodyTextMb2).toEqual({
                color: 'text.secondary',
                mb: 2,
                lineHeight: 1.6,
            });
        });

        it('has indentedBlock style', () => {
            expect(styles.indentedBlock).toEqual({ pl: 2, mb: 2 });
        });

        it('has severityChipsRow style', () => {
            expect(styles.severityChipsRow).toEqual({
                display: 'flex',
                gap: 1,
                mb: 3,
            });
        });

        it('has severityChipBase style', () => {
            expect(styles.severityChipBase).toEqual({
                fontWeight: 600,
                fontSize: '0.875rem',
            });
        });
    });

    describe('getDrawerPaperSx', () => {
        it('returns an object with MuiDrawer-paper styles', () => {
            const result = getDrawerPaperSx();
            expect(result).toHaveProperty('& .MuiDrawer-paper');
            expect(result['& .MuiDrawer-paper']).toHaveProperty('width');
            expect(result['& .MuiDrawer-paper']).toHaveProperty('bgcolor');
        });

        it('has responsive width values', () => {
            const result = getDrawerPaperSx();
            expect(result['& .MuiDrawer-paper'].width).toEqual({
                xs: '100%',
                sm: 560,
            });
        });
    });

    describe('getNavItemSx', () => {
        const lightTheme = createTheme({ palette: { mode: 'light' } });
        const darkTheme = createTheme({ palette: { mode: 'dark' } });

        it('returns a function', () => {
            const result = getNavItemSx(true);
            expect(typeof result).toBe('function');
        });

        it('returns object with borderRadius', () => {
            const styleFn = getNavItemSx(false);
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('borderRadius', 1);
        });

        it('returns object with mb', () => {
            const styleFn = getNavItemSx(false);
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('mb', 0.5);
        });

        it('returns object with py', () => {
            const styleFn = getNavItemSx(false);
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('py', 0.75);
        });

        it('returns transparent bgcolor when not active', () => {
            const styleFn = getNavItemSx(false);
            const result = styleFn(lightTheme);
            expect(result.bgcolor).toBe('transparent');
        });

        it('returns primary-based bgcolor when active', () => {
            const styleFn = getNavItemSx(true);
            const result = styleFn(lightTheme);
            expect(result.bgcolor).not.toBe('transparent');
            expect(typeof result.bgcolor).toBe('string');
        });

        it('has hover styles', () => {
            const styleFn = getNavItemSx(false);
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('&:hover');
            expect(result['&:hover']).toHaveProperty('bgcolor');
        });

        it('applies different hover style when active', () => {
            const activeFn = getNavItemSx(true);
            const inactiveFn = getNavItemSx(false);
            const activeResult = activeFn(lightTheme);
            const inactiveResult = inactiveFn(lightTheme);
            expect(activeResult['&:hover'].bgcolor).not.toBe(
                inactiveResult['&:hover'].bgcolor
            );
        });

        it('works with dark theme', () => {
            const styleFn = getNavItemSx(true);
            const result = styleFn(darkTheme);
            expect(result).toHaveProperty('bgcolor');
            expect(result).toHaveProperty('&:hover');
        });
    });

    describe('getNavItemIconColor', () => {
        it('returns primary.main when active', () => {
            expect(getNavItemIconColor(true)).toBe('primary.main');
        });

        it('returns text.secondary when not active', () => {
            expect(getNavItemIconColor(false)).toBe('text.secondary');
        });
    });

    describe('getNavItemLabelProps', () => {
        it('returns object with fontSize', () => {
            const result = getNavItemLabelProps(false);
            expect(result).toHaveProperty('fontSize', '1rem');
        });

        it('returns fontWeight 600 when active', () => {
            const result = getNavItemLabelProps(true);
            expect(result.fontWeight).toBe(600);
        });

        it('returns fontWeight 500 when not active', () => {
            const result = getNavItemLabelProps(false);
            expect(result.fontWeight).toBe(500);
        });

        it('returns primary.main color when active', () => {
            const result = getNavItemLabelProps(true);
            expect(result.color).toBe('primary.main');
        });

        it('returns text.primary color when not active', () => {
            const result = getNavItemLabelProps(false);
            expect(result.color).toBe('text.primary');
        });
    });

    describe('getHelpTipSx', () => {
        const lightTheme = createTheme({ palette: { mode: 'light' } });
        const darkTheme = createTheme({ palette: { mode: 'dark' } });

        it('returns object with display flex', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('display', 'flex');
        });

        it('returns object with gap', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('gap', 1);
        });

        it('returns object with padding', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('p', 1.5);
        });

        it('returns object with margin top', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('mt', 2);
        });

        it('returns object with borderRadius', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('borderRadius', 1);
        });

        it('returns object with bgcolor', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('bgcolor');
            expect(typeof result.bgcolor).toBe('string');
        });

        it('returns object with border', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('border', '1px solid');
        });

        it('returns object with borderColor', () => {
            const result = getHelpTipSx(lightTheme);
            expect(result).toHaveProperty('borderColor');
            expect(typeof result.borderColor).toBe('string');
        });

        it('works with dark theme', () => {
            const result = getHelpTipSx(darkTheme);
            expect(result).toHaveProperty('bgcolor');
            expect(result).toHaveProperty('borderColor');
        });
    });

    describe('getSeverityChipSx', () => {
        const lightTheme = createTheme({ palette: { mode: 'light' } });
        const darkTheme = createTheme({ palette: { mode: 'dark' } });

        it('returns a function', () => {
            const result = getSeverityChipSx('error');
            expect(typeof result).toBe('function');
        });

        it('includes severityChipBase styles', () => {
            const styleFn = getSeverityChipSx('error');
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('fontWeight', 600);
            expect(result).toHaveProperty('fontSize', '0.875rem');
        });

        it('returns error-colored styles for error palette', () => {
            const styleFn = getSeverityChipSx('error');
            const result = styleFn(lightTheme);
            expect(result).toHaveProperty('bgcolor');
            expect(result).toHaveProperty('color');
            expect(result.color).toBe(lightTheme.palette.error.main);
        });

        it('returns warning-colored styles for warning palette', () => {
            const styleFn = getSeverityChipSx('warning');
            const result = styleFn(lightTheme);
            expect(result.color).toBe(lightTheme.palette.warning.main);
        });

        it('returns info-colored styles for info palette', () => {
            const styleFn = getSeverityChipSx('info');
            const result = styleFn(lightTheme);
            expect(result.color).toBe(lightTheme.palette.info.main);
        });

        it('works with dark theme for error', () => {
            const styleFn = getSeverityChipSx('error');
            const result = styleFn(darkTheme);
            expect(result.color).toBe(darkTheme.palette.error.main);
        });

        it('works with dark theme for warning', () => {
            const styleFn = getSeverityChipSx('warning');
            const result = styleFn(darkTheme);
            expect(result.color).toBe(darkTheme.palette.warning.main);
        });

        it('works with dark theme for info', () => {
            const styleFn = getSeverityChipSx('info');
            const result = styleFn(darkTheme);
            expect(result.color).toBe(darkTheme.palette.info.main);
        });
    });
});
