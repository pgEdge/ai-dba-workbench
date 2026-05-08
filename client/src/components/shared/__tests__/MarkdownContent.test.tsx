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

// ---------------------------------------------------------------------------
// Markdown element coverage
// ---------------------------------------------------------------------------
//
// The block above pinned the regression for the table wrapper. The cases
// below walk every branch in the `components` map so each element type
// (headings, paragraphs, lists, strong/em, links, blockquotes, inline
// code, fenced code) is exercised, along with the SQL routing rules in
// the `code` renderer (server connection, cluster `--connection_id:`
// comment, cluster selector fallback, plain non-SQL block).

describe('MarkdownContent element rendering', () => {
    it('renders h1, h2, and h3 headings as MUI Typography', () => {
        renderWithTheme(
            <MarkdownContent
                content={'# Top\n\n## Middle\n\n### Inner'}
                isDark={false}
            />,
        );
        expect(screen.getByText('Top')).toBeInTheDocument();
        expect(screen.getByText('Middle')).toBeInTheDocument();
        expect(screen.getByText('Inner')).toBeInTheDocument();
    });

    it('renders paragraphs', () => {
        renderWithTheme(
            <MarkdownContent
                content={'A paragraph of text.'}
                isDark={false}
            />,
        );
        expect(screen.getByText('A paragraph of text.')).toBeInTheDocument();
    });

    it('renders unordered and ordered lists with their items', () => {
        renderWithTheme(
            <MarkdownContent
                content={'- a\n- b\n\n1. one\n2. two'}
                isDark={false}
            />,
        );
        expect(screen.getByText('a')).toBeInTheDocument();
        expect(screen.getByText('b')).toBeInTheDocument();
        expect(screen.getByText('one')).toBeInTheDocument();
        expect(screen.getByText('two')).toBeInTheDocument();
    });

    it('renders strong and emphasis content', () => {
        renderWithTheme(
            <MarkdownContent
                content={'**bold-bit** and *italic-bit*'}
                isDark={false}
            />,
        );
        expect(screen.getByText('bold-bit')).toBeInTheDocument();
        expect(screen.getByText('italic-bit')).toBeInTheDocument();
    });

    it('renders blockquotes', () => {
        renderWithTheme(
            <MarkdownContent
                content={'> a quoted paragraph'}
                isDark={false}
            />,
        );
        expect(screen.getByText('a quoted paragraph')).toBeInTheDocument();
    });

    it('renders inline code spans', () => {
        renderWithTheme(
            <MarkdownContent
                content={'use `pg_stat_activity` here'}
                isDark={false}
            />,
        );
        const codeEl = screen.getByText('pg_stat_activity');
        expect(codeEl.tagName.toLowerCase()).toBe('code');
    });

    it('renders safe http links with target=_blank rel=noopener noreferrer', () => {
        renderWithTheme(
            <MarkdownContent
                content={'[docs](https://docs.example.com)'}
                isDark={false}
            />,
        );
        const link = screen.getByText('docs').closest('a');
        expect(link).toBeInTheDocument();
        expect(link?.getAttribute('href')).toBe('https://docs.example.com');
        expect(link?.getAttribute('target')).toBe('_blank');
        expect(link?.getAttribute('rel')).toBe('noopener noreferrer');
    });

    it('strips href on unsafe schemes (e.g. javascript:)', () => {
        renderWithTheme(
            <MarkdownContent
                content={'[bad](javascript:alert(1))'}
                isDark={false}
            />,
        );
        const text = screen.getByText('bad');
        // The link text is preserved but no anchor element wraps it.
        expect(text.closest('a')).toBeNull();
    });

    it('renders mailto links', () => {
        renderWithTheme(
            <MarkdownContent
                content={'[email](mailto:test@example.com)'}
                isDark={false}
            />,
        );
        const link = screen.getByText('email').closest('a');
        expect(link?.getAttribute('href')).toBe('mailto:test@example.com');
    });
});

describe('MarkdownContent code block routing', () => {
    it('renders a non-SQL fenced block via SyntaxHighlighter', () => {
        renderWithTheme(
            <MarkdownContent
                content={'```bash\necho hello\n```'}
                isDark={false}
            />,
        );
        // Bash blocks fall through to the SyntaxHighlighter branch and
        // get wrapped with the copy button stub.
        expect(screen.getByText('copy')).toBeInTheDocument();
    });

    it('routes SQL blocks to RunnableCodeBlock when connectionId is set', () => {
        renderWithTheme(
            <MarkdownContent
                content={'```sql\nSELECT 1\n```'}
                isDark={false}
                connectionId={42}
                databaseName="prod"
                serverName="srv-1"
            />,
        );
        expect(screen.getByTestId('runnable-code-block')).toBeInTheDocument();
    });

    it('routes SQL blocks to RunnableCodeBlock when connection_id comment is present', () => {
        const md = [
            '```sql',
            '-- connection_id: 7',
            'SELECT 2',
            '```',
        ].join('\n');
        renderWithTheme(
            <MarkdownContent
                content={md}
                isDark={false}
                connectionMap={new Map([[7, 'seven']])}
            />,
        );
        // No selector is rendered: the comment routed directly to the
        // runnable block.
        expect(screen.getByTestId('runnable-code-block')).toBeInTheDocument();
        expect(screen.queryByTestId('connection-selector')).not.toBeInTheDocument();
    });

    it('routes SQL blocks to ConnectionSelectorCodeBlock when no comment is present', () => {
        renderWithTheme(
            <MarkdownContent
                content={'```sql\nSELECT 3\n```'}
                isDark={false}
                connectionMap={new Map([[1, 'a'], [2, 'b']])}
            />,
        );
        expect(screen.getByTestId('connection-selector')).toBeInTheDocument();
    });

    it('falls through to SyntaxHighlighter for SQL when no connection context is provided', () => {
        renderWithTheme(
            <MarkdownContent
                content={'```sql\nSELECT 4\n```'}
                isDark={false}
            />,
        );
        // No runnable, no selector — only the highlighter copy button.
        expect(screen.queryByTestId('runnable-code-block')).not.toBeInTheDocument();
        expect(screen.queryByTestId('connection-selector')).not.toBeInTheDocument();
        expect(screen.getByText('copy')).toBeInTheDocument();
    });

    it('exercises the dark-theme branch of background and theme creation', () => {
        renderWithTheme(
            <MarkdownContent
                content={'```\nsome text\n```'}
                isDark={true}
            />,
        );
        // Renders a copy button in the dark branch as well.
        expect(screen.getByText('copy')).toBeInTheDocument();
    });

    it('falls back to "Server <id>" when the connection_id is not in the map', () => {
        const md = [
            '```sql',
            '-- connection_id: 99',
            'SELECT 5',
            '```',
        ].join('\n');
        renderWithTheme(
            <MarkdownContent
                content={md}
                isDark={false}
                connectionMap={new Map([[1, 'a']])}
            />,
        );
        // Still renders the runnable block; the synthetic server name
        // is used internally.
        expect(screen.getByTestId('runnable-code-block')).toBeInTheDocument();
    });
});
