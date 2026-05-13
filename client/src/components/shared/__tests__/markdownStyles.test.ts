/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Regression coverage for issue #221. The Remediation Steps panel rendered
// SQL code blocks with absolutely positioned copy/run icons in the
// top-right corner, but the code area had a uniform 1rem padding which let
// long SQL lines slide underneath the icons. The fix reserves horizontal
// space on the right of the code area sized to the number of action
// buttons that overlay it. These tests pin down the padding calculation
// and the customStyle shape that feeds SyntaxHighlighter.

import { describe, it, expect } from 'vitest';
import {
    getCodeBlockRightPadding,
    getCodeBlockCustomStyle,
} from '../markdownStyles';

describe('getCodeBlockRightPadding (issue #221)', () => {
    it('returns the default 1rem when no action buttons overlay the block', () => {
        // A non-decorated code block keeps the original symmetric padding so
        // we do not introduce a visible right-side gutter without reason.
        expect(getCodeBlockRightPadding(0)).toBe('1rem');
    });

    it('reserves enough room for a single 28px action button', () => {
        // Layout: 6px offset + one 28px button + 8px gutter = 42px.
        expect(getCodeBlockRightPadding(1)).toBe('42px');
    });

    it('reserves enough room for two action buttons with the inter-button gap', () => {
        // Layout: 6px offset + 2 * 28px buttons + 1 * 4px gap + 8px gutter
        // = 74px. This is the case that issue #221 originally regressed:
        // both the copy and run icons sit over the right edge of the SQL.
        expect(getCodeBlockRightPadding(2)).toBe('74px');
    });

    it('scales linearly with additional buttons', () => {
        // Defensive: future panels may add more action buttons. The helper
        // must keep clearing each extra 28px button and its preceding gap.
        expect(getCodeBlockRightPadding(3)).toBe('106px');
    });

    it('clamps negative button counts to the no-button default', () => {
        // Treat a nonsensical caller value as zero rather than computing a
        // negative padding that would shrink the code area.
        expect(getCodeBlockRightPadding(-1)).toBe('1rem');
    });
});

describe('getCodeBlockCustomStyle (issue #221)', () => {
    it('preserves the existing padding and font defaults', () => {
        const style = getCodeBlockCustomStyle('#fff');
        expect(style.margin).toBe(0);
        expect(style.padding).toBe('1rem');
        expect(style.fontSize).toBe('1rem');
        expect(style.fontFamily).toBe(
            '"JetBrains Mono", "SF Mono", monospace',
        );
        expect(style.background).toBe('#fff');
    });

    it('defaults paddingRight to 1rem when no button count is supplied', () => {
        // Callers that never had action buttons (legacy paths) must keep
        // their symmetric padding so this change is a no-op for them.
        const style = getCodeBlockCustomStyle('#fff');
        expect(style.paddingRight).toBe('1rem');
    });

    it('uses the single-button clearance when one button overlays the block', () => {
        const style = getCodeBlockCustomStyle('#fff', 1);
        expect(style.paddingRight).toBe('42px');
    });

    it('uses the two-button clearance for SQL code blocks (the issue #221 case)', () => {
        const style = getCodeBlockCustomStyle('#fff', 2);
        expect(style.paddingRight).toBe('74px');
    });

    it('forwards the supplied background colour verbatim', () => {
        // The background colour is theme-driven and must not be touched by
        // the padding logic.
        const style = getCodeBlockCustomStyle('rgb(10, 20, 30)', 2);
        expect(style.background).toBe('rgb(10, 20, 30)');
    });
});
