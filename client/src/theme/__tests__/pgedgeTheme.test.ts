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

describe('loginTheme', () => {
    it('is always a light theme', () => {
        expect(loginTheme.palette.mode).toBe('light');
    });
});
