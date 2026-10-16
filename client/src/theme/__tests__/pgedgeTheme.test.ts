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

import { createPgedgeTheme, loginTheme } from '../pgedgeTheme';
import {
    compositeOver,
    contrastRatio,
    WCAG_AA_NORMAL_TEXT,
} from '../../test/contrast';

describe('createPgedgeTheme', () => {
    it('builds a light theme by default', () => {
        const theme = createPgedgeTheme();
        expect(theme.palette.mode).toBe('light');
        expect(theme.palette.primary.main).toBe('#15AABF');
    });

    it('builds a dark theme when asked', () => {
        const theme = createPgedgeTheme('dark');
        expect(theme.palette.mode).toBe('dark');
        expect(theme.palette.primary.main).toBe('#22B8CF');
    });

    describe('typography', () => {
        const theme = createPgedgeTheme();

        it('uses 16px for body1 to match the MUI standard size', () => {
            expect(theme.typography.body1.fontSize).toBe('1rem');
        });

        it('uses 14px for body2 to separate secondary text from body1', () => {
            expect(theme.typography.body2.fontSize).toBe('0.875rem');
        });

        it('keeps subtitle1 at 18px so it stays a step above body1', () => {
            expect(theme.typography.subtitle1.fontSize).toBe('1.125rem');
        });

        it('keeps subtitle2 at 16px alongside body1', () => {
            expect(theme.typography.subtitle2.fontSize).toBe('1rem');
        });

        it('keeps caption and overline at 14px', () => {
            expect(theme.typography.caption.fontSize).toBe('0.875rem');
            expect(theme.typography.overline.fontSize).toBe('0.875rem');
        });
    });
});

/**
 * Status chips across the client paint their label on an
 * `alpha(<status>.main, 0.15)` background. The label colour therefore
 * has to be measured against that composited background rather than
 * against the paper colour alone, and it must not simply be the status
 * colour itself, which leaves text and background sharing a hue at
 * roughly 2:1.
 */
describe('status chip contrast', () => {
    const CHIP_BACKGROUND_OPACITY = 0.15;

    const statuses = [
        ['success', '#22C55E'],
        ['error', '#EF4444'],
        ['warning', '#F59E0B'],
    ] as const;

    describe.each(['light', 'dark'] as const)('in %s mode', (mode) => {
        const theme = createPgedgeTheme(mode);
        const paper = theme.palette.background.paper;

        it.each(statuses)(
            'clears WCAG AA for the %s chip label',
            (status, mainColour) => {
                const background = compositeOver(
                    mainColour,
                    CHIP_BACKGROUND_OPACITY,
                    paper,
                );
                const ratio = contrastRatio(
                    theme.palette.custom.chipText[status],
                    background,
                );
                expect(ratio).toBeGreaterThanOrEqual(WCAG_AA_NORMAL_TEXT);
            },
        );

        it.each(statuses)(
            'uses the same %s label colour for MuiChip overrides',
            (status) => {
                const filled = theme.components?.MuiChip?.styleOverrides
                    ?.filled as Record<string, { color: string }>;
                const key = `&.MuiChip-color${status.charAt(0).toUpperCase()}${status.slice(1)}`;
                expect(filled[key].color).toBe(
                    theme.palette.custom.chipText[status],
                );
            },
        );

        it.each(statuses)(
            'does not reuse the %s status colour as its own label',
            (status, mainColour) => {
                expect(theme.palette.custom.chipText[status]).not.toBe(
                    mainColour,
                );
            },
        );
    });
});

describe('loginTheme', () => {
    it('is always a light theme', () => {
        expect(loginTheme.palette.mode).toBe('light');
    });
});
