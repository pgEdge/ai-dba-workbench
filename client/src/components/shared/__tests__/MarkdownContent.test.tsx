/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Focused regression coverage for the issue #185 fix. The full
// MarkdownContent component renders an extensive set of markdown features
// (code blocks, runnable SQL, connection selectors, link sanitisation, ...)
// that already have integration coverage through the dialogs that mount
// it. This file pins down the specific behaviour added by the issue #185
// fix: every rendered markdown <table> sits inside a horizontally
// scrollable wrapper so wide MCP-tool tables no longer clip the right
// side of the surrounding surface.

import React from 'react';
import { screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MarkdownContent } from '../MarkdownContent';
import { renderWithTheme } from '../../../test/renderWithTheme';

// MarkdownContent imports several heavy children (RunnableCodeBlock,
// ConnectionSelectorCodeBlock, AnalysisSkeleton) that pull in API
// utilities. None of them are exercised by a markdown payload that only
// contains text and a table, so we mock them defensively to keep the
// test surface tight and the test deterministic.
vi.mock('../RunnableCodeBlock', () => ({
    default: () => <div data-testid="runnable-code-block" />,
    // The named QueryState/QueryResponse type re-exports are not used at
    // runtime and only matter for TypeScript; vi.mock returns undefined
    // for them which is fine.
}));

vi.mock('../ConnectionSelectorCodeBlock', () => ({
    default: () => <div data-testid="connection-selector" />,
}));

vi.mock('../AnalysisSkeleton', () => ({
    default: () => <div data-testid="analysis-skeleton" />,
}));

vi.mock('../CopyCodeButton', () => ({
    default: () => <button type="button">copy</button>,
}));

const TABLE_MARKDOWN = [
    '| col_a | col_b | col_c | col_d | col_e |',
    '|-------|-------|-------|-------|-------|',
    '| 1 | 2 | 3 | 4 | 5 |',
    '| 6 | 7 | 8 | 9 | 10 |',
].join('\n');

describe('MarkdownContent table overflow (issue #185)', () => {
    it('wraps markdown tables in a horizontally scrollable container', () => {
        renderWithTheme(
            <MarkdownContent content={TABLE_MARKDOWN} isDark={false} />,
        );

        const wrapper = screen.getByTestId('markdown-table-container');
        expect(wrapper).toBeInTheDocument();

        // Wide tables must scroll horizontally inside the surface, not
        // overflow it. Vertical overflow stays visible so tall tables
        // keep growing the surrounding container.
        const styles = window.getComputedStyle(wrapper);
        expect(styles.overflowX).toBe('auto');
        expect(styles.overflowY).toBe('visible');
        expect(styles.maxWidth).toBe('100%');
    });

    it('places the rendered table inside the scroll wrapper', () => {
        renderWithTheme(
            <MarkdownContent content={TABLE_MARKDOWN} isDark={false} />,
        );

        const wrapper = screen.getByTestId('markdown-table-container');
        const table = wrapper.querySelector('table');
        expect(table).not.toBeNull();
        // Confirm the cells are still part of the DOM — the previous
        // `width: 100%` rule could squash columns but never dropped
        // cells; this guards against future regressions from the new
        // `width: auto` layout.
        expect(screen.getByText('col_a')).toBeInTheDocument();
        expect(screen.getByText('col_e')).toBeInTheDocument();
        expect(screen.getByText('10')).toBeInTheDocument();
    });

    it('renders nothing when content is empty', () => {
        // The early-return branch in MarkdownContent. Worth pinning down
        // so the touched module retains its existing safety net.
        const { container } = renderWithTheme(
            <MarkdownContent content="" isDark={false} />,
        );
        expect(container.firstChild).toBeNull();
    });
});
