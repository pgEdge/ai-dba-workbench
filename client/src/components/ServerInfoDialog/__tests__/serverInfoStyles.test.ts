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
import { createTheme } from '@mui/material/styles';
import {
    MONO_FONT,
    SECTION_STATE_KEY,
    sxMono,
    getContentSx,
    getSectionHeaderSx,
    getSectionIconSx,
    getSectionTitleSx,
    getSectionContentSx,
    getKvGridSx,
    getKvLabelSx,
    getKvValueSx,
    getProgressBarSx,
    getDbRowSx,
    getExtChipSx,
    getAiBoxSx,
    getSettingRowSx,
} from '../serverInfoStyles';

describe('serverInfoStyles', () => {
    const lightTheme = createTheme({ palette: { mode: 'light' } });
    const darkTheme = createTheme({ palette: { mode: 'dark' } });

    // -----------------------------------------------------------------------
    // Constants
    // -----------------------------------------------------------------------

    describe('constants', () => {
        it('MONO_FONT is a string containing monospace font family', () => {
            expect(typeof MONO_FONT).toBe('string');
            expect(MONO_FONT).toContain('monospace');
            expect(MONO_FONT).toContain('JetBrains Mono');
        });

        it('SECTION_STATE_KEY is a string', () => {
            expect(typeof SECTION_STATE_KEY).toBe('string');
            expect(SECTION_STATE_KEY).toBe('serverInfoSectionState');
        });

        it('sxMono contains fontFamily property', () => {
            expect(sxMono).toHaveProperty('fontFamily', MONO_FONT);
        });
    });

    // -----------------------------------------------------------------------
    // getContentSx
    // -----------------------------------------------------------------------

    describe('getContentSx', () => {
        it('returns an object with style properties for light theme', () => {
            const styles = getContentSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('flex', 1);
            expect(styles).toHaveProperty('overflow', 'auto');
            expect(styles).toHaveProperty('bgcolor');
            expect(styles).toHaveProperty('&::-webkit-scrollbar');
        });

        it('returns an object with style properties for dark theme', () => {
            const styles = getContentSx(darkTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('flex', 1);
            expect(styles).toHaveProperty('overflow', 'auto');
            expect(styles).toHaveProperty('bgcolor');
        });

        it('uses different bgcolor for light vs dark theme', () => {
            const lightStyles = getContentSx(lightTheme);
            const darkStyles = getContentSx(darkTheme);
            expect(lightStyles.bgcolor).not.toBe(darkStyles.bgcolor);
        });
    });

    // -----------------------------------------------------------------------
    // getSectionHeaderSx
    // -----------------------------------------------------------------------

    describe('getSectionHeaderSx', () => {
        it('returns an object with header styles', () => {
            const styles = getSectionHeaderSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('display', 'flex');
            expect(styles).toHaveProperty('alignItems', 'center');
            expect(styles).toHaveProperty('cursor', 'pointer');
            expect(styles).toHaveProperty('borderBottom', '1px solid');
            expect(styles).toHaveProperty('userSelect', 'none');
        });

        it('includes hover styles', () => {
            const styles = getSectionHeaderSx(lightTheme);
            expect(styles).toHaveProperty('&:hover');
        });

        it('uses theme-dependent background colors', () => {
            const lightStyles = getSectionHeaderSx(lightTheme);
            const darkStyles = getSectionHeaderSx(darkTheme);
            expect(lightStyles.bgcolor).toBeDefined();
            expect(darkStyles.bgcolor).toBeDefined();
        });
    });

    // -----------------------------------------------------------------------
    // getSectionIconSx
    // -----------------------------------------------------------------------

    describe('getSectionIconSx', () => {
        it('returns an object with icon styles', () => {
            const styles = getSectionIconSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('fontSize', 16);
            expect(styles).toHaveProperty('color');
        });

        it('uses theme primary color', () => {
            const styles = getSectionIconSx(lightTheme);
            expect(styles.color).toBe(lightTheme.palette.primary.main);
        });
    });

    // -----------------------------------------------------------------------
    // getSectionTitleSx
    // -----------------------------------------------------------------------

    describe('getSectionTitleSx', () => {
        it('returns an object with title styles', () => {
            const styles = getSectionTitleSx();
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('fontSize', '1rem');
            expect(styles).toHaveProperty('fontWeight', 700);
            expect(styles).toHaveProperty('textTransform', 'uppercase');
            expect(styles).toHaveProperty('letterSpacing', '0.08em');
            expect(styles).toHaveProperty('flex', 1);
        });
    });

    // -----------------------------------------------------------------------
    // getSectionContentSx
    // -----------------------------------------------------------------------

    describe('getSectionContentSx', () => {
        it('returns an object with content wrapper styles', () => {
            const styles = getSectionContentSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('px', 2.5);
            expect(styles).toHaveProperty('py', 1.5);
            expect(styles).toHaveProperty('borderBottom', '1px solid');
            expect(styles).toHaveProperty('borderColor');
        });
    });

    // -----------------------------------------------------------------------
    // getKvGridSx
    // -----------------------------------------------------------------------

    describe('getKvGridSx', () => {
        it('returns an object with grid styles', () => {
            const styles = getKvGridSx();
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('display', 'grid');
            expect(styles).toHaveProperty('gridTemplateColumns');
            expect(styles).toHaveProperty('gap', 1.5);
        });

        it('uses auto-fill for responsive grid', () => {
            const styles = getKvGridSx();
            expect(styles.gridTemplateColumns).toContain('auto-fill');
            expect(styles.gridTemplateColumns).toContain('minmax');
        });
    });

    // -----------------------------------------------------------------------
    // getKvLabelSx
    // -----------------------------------------------------------------------

    describe('getKvLabelSx', () => {
        it('returns an object with label styles', () => {
            const styles = getKvLabelSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('fontSize', '0.875rem');
            expect(styles).toHaveProperty('fontWeight', 700);
            expect(styles).toHaveProperty('textTransform', 'uppercase');
            expect(styles).toHaveProperty('letterSpacing', '0.1em');
            expect(styles).toHaveProperty('lineHeight', 1);
        });

        it('uses theme grey color', () => {
            const styles = getKvLabelSx(lightTheme);
            expect(styles.color).toBe(lightTheme.palette.grey[500]);
        });
    });

    // -----------------------------------------------------------------------
    // getKvValueSx
    // -----------------------------------------------------------------------

    describe('getKvValueSx', () => {
        it('returns an object with value styles', () => {
            const styles = getKvValueSx();
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('fontSize', '1rem');
            expect(styles).toHaveProperty('fontWeight', 500);
            expect(styles).toHaveProperty('lineHeight', 1.3);
            expect(styles).toHaveProperty('fontFamily', MONO_FONT);
            expect(styles).toHaveProperty('wordBreak', 'break-word');
        });
    });

    // -----------------------------------------------------------------------
    // getProgressBarSx
    // -----------------------------------------------------------------------

    describe('getProgressBarSx', () => {
        it('returns an object with progress bar styles', () => {
            const styles = getProgressBarSx(lightTheme, 50);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('height', 4);
            expect(styles).toHaveProperty('borderRadius', 2);
            expect(styles).toHaveProperty('bgcolor');
            expect(styles).toHaveProperty('& .MuiLinearProgress-bar');
        });

        it('uses primary color for percentage <= 75', () => {
            const styles = getProgressBarSx(lightTheme, 50);
            const barStyles = styles['& .MuiLinearProgress-bar'];
            expect(barStyles.bgcolor).toBe(lightTheme.palette.primary.main);
        });

        it('uses warning color for percentage > 75 and <= 90', () => {
            const styles = getProgressBarSx(lightTheme, 80);
            const barStyles = styles['& .MuiLinearProgress-bar'];
            expect(barStyles.bgcolor).toBe(lightTheme.palette.warning.main);
        });

        it('uses error color for percentage > 90', () => {
            const styles = getProgressBarSx(lightTheme, 95);
            const barStyles = styles['& .MuiLinearProgress-bar'];
            expect(barStyles.bgcolor).toBe(lightTheme.palette.error.main);
        });

        it('uses different background for dark theme', () => {
            const lightStyles = getProgressBarSx(lightTheme, 50);
            const darkStyles = getProgressBarSx(darkTheme, 50);
            expect(lightStyles.bgcolor).not.toBe(darkStyles.bgcolor);
        });
    });

    // -----------------------------------------------------------------------
    // getDbRowSx
    // -----------------------------------------------------------------------

    describe('getDbRowSx', () => {
        it('returns an object with database row styles', () => {
            const styles = getDbRowSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('display', 'flex');
            expect(styles).toHaveProperty('alignItems', 'flex-start');
            expect(styles).toHaveProperty('gap', 1.5);
            expect(styles).toHaveProperty('py', 1);
            expect(styles).toHaveProperty('borderBottom', '1px solid');
        });

        it('includes pseudo-selectors for first and last items', () => {
            const styles = getDbRowSx(lightTheme);
            expect(styles).toHaveProperty('&:last-child');
            expect(styles).toHaveProperty('&:first-of-type');
        });
    });

    // -----------------------------------------------------------------------
    // getExtChipSx
    // -----------------------------------------------------------------------

    describe('getExtChipSx', () => {
        it('returns an object with extension chip styles', () => {
            const styles = getExtChipSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('display', 'inline-flex');
            expect(styles).toHaveProperty('alignItems', 'center');
            expect(styles).toHaveProperty('gap', 0.5);
            expect(styles).toHaveProperty('borderRadius', 0.5);
            expect(styles).toHaveProperty('fontFamily', MONO_FONT);
        });

        it('uses theme-dependent background', () => {
            const lightStyles = getExtChipSx(lightTheme);
            const darkStyles = getExtChipSx(darkTheme);
            expect(lightStyles.bgcolor).toBeDefined();
            expect(darkStyles.bgcolor).toBeDefined();
        });
    });

    // -----------------------------------------------------------------------
    // getAiBoxSx
    // -----------------------------------------------------------------------

    describe('getAiBoxSx', () => {
        it('returns an object with AI box styles', () => {
            const styles = getAiBoxSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('mt', 0.5);
            expect(styles).toHaveProperty('px', 1.25);
            expect(styles).toHaveProperty('py', 0.75);
            expect(styles).toHaveProperty('borderRadius', 1);
            expect(styles).toHaveProperty('border', '1px solid');
        });

        it('uses theme-dependent background', () => {
            const lightStyles = getAiBoxSx(lightTheme);
            const darkStyles = getAiBoxSx(darkTheme);
            expect(lightStyles.bgcolor).toBeDefined();
            expect(darkStyles.bgcolor).toBeDefined();
        });
    });

    // -----------------------------------------------------------------------
    // getSettingRowSx
    // -----------------------------------------------------------------------

    describe('getSettingRowSx', () => {
        it('returns an object with setting row styles', () => {
            const styles = getSettingRowSx(lightTheme);
            expect(typeof styles).toBe('object');
            expect(styles).toHaveProperty('display', 'flex');
            expect(styles).toHaveProperty('alignItems', 'baseline');
            expect(styles).toHaveProperty('justifyContent', 'space-between');
            expect(styles).toHaveProperty('gap', 2);
            expect(styles).toHaveProperty('py', 0.5);
            expect(styles).toHaveProperty('borderBottom', '1px solid');
        });

        it('includes last-child pseudo-selector', () => {
            const styles = getSettingRowSx(lightTheme);
            expect(styles).toHaveProperty('&:last-child');
        });
    });
});
