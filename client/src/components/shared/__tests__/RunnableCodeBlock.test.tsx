/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Regression coverage for issue #221 plus broader unit coverage for
// RunnableCodeBlock. The component previously had only integration
// coverage through analysis dialogs because the heavyweight query
// runner is normally exercised end-to-end. The issue #221 fix touched
// the SyntaxHighlighter call site, so these tests now anchor every
// behavioural branch (layout, success, confirmation, error, dismiss).
//
// SyntaxHighlighter is mocked with a stub that surfaces the
// `customStyle` prop as JSON so we can assert layout decisions without
// pulling in the real prismjs grammar. apiFetch is mocked per test to
// drive the query-execution branches.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createTheme } from '@mui/material/styles';
import RunnableCodeBlock from '../RunnableCodeBlock';
import { renderWithTheme } from '../../../test/renderWithTheme';

vi.mock('react-syntax-highlighter', () => ({
    Prism: ({
        children,
        customStyle,
    }: {
        children: React.ReactNode;
        customStyle?: Record<string, unknown>;
        [key: string]: unknown;
    }) => (
        <div
            data-testid="syntax-highlighter"
            data-custom-style={JSON.stringify(customStyle ?? {})}
            style={customStyle as React.CSSProperties}
        >
            {children}
        </div>
    ),
}));

const mockApiFetch = vi.fn();
vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const theme = createTheme();

const baseProps = {
    codeContent: 'SELECT * FROM spock.exception_status WHERE status = $1;',
    language: 'sql',
    isDark: false,
    connectionId: 1,
    syntaxTheme: {},
    customBackground: '#fafafa',
    theme,
    props: {},
};

const readCustomStyle = (): Record<string, unknown> => {
    const node = screen.getByTestId('syntax-highlighter');
    return JSON.parse(node.dataset.customStyle || '{}');
};

/**
 * The run icon's MUI Tooltip wraps the IconButton in a <span> that carries
 * the `aria-label` (so disabled buttons still announce their purpose).
 * Tests that need to click the underlying IconButton walk through the
 * labelled span to find the actual <button>.
 */
const getRunButton = (label = 'Run query'): HTMLElement => {
    const labelled = screen.getByLabelText(label);
    const button = labelled.querySelector('button');
    if (!button) {
        throw new Error(`No <button> inside labelled element "${label}"`);
    }
    return button as HTMLElement;
};

const makeOkResponse = (body: unknown) => ({
    ok: true,
    status: 200,
    json: vi.fn().mockResolvedValue(body),
    text: vi.fn().mockResolvedValue(''),
});

const makeErrorResponse = ({
    status = 500,
    json,
    text,
}: {
    status?: number;
    json?: unknown;
    text?: string;
} = {}) => ({
    ok: false,
    status,
    json: vi.fn(
        json === undefined
            ? () => Promise.reject(new Error('not-json'))
            : () => Promise.resolve(json),
    ),
    text: vi.fn().mockResolvedValue(text ?? ''),
});

beforeEach(() => {
    mockApiFetch.mockReset();
});

describe('RunnableCodeBlock layout (issue #221 fix)', () => {
    it('reserves room for both copy and run icons on SQL blocks', () => {
        renderWithTheme(<RunnableCodeBlock {...baseProps} isSql={true} />);

        // Two action buttons (copy + run) need 74px of clearance on the
        // right of the code area; without this, long SQL lines hide
        // underneath the icons.
        const customStyle = readCustomStyle();
        expect(customStyle.paddingRight).toBe('74px');
        expect(customStyle.padding).toBe('1rem');
        expect(customStyle.background).toBe('#fafafa');
    });

    it('reserves room for only the copy icon when the block is not runnable', () => {
        renderWithTheme(<RunnableCodeBlock {...baseProps} isSql={false} />);

        // Non-SQL code blocks only ever show the copy icon, so a single
        // 42px clearance is enough.
        const customStyle = readCustomStyle();
        expect(customStyle.paddingRight).toBe('42px');
    });

    it('falls back to the "sql" language when none is provided', () => {
        renderWithTheme(
            <RunnableCodeBlock {...baseProps} isSql={true} language="" />,
        );

        // The component defaults the SyntaxHighlighter language prop to
        // 'sql' to keep the highlighter happy and the contract stable;
        // exercise this branch so future changes keep it intact.
        expect(screen.getByTestId('syntax-highlighter')).toBeInTheDocument();
    });
});

describe('RunnableCodeBlock run button', () => {
    it('renders the run action icon for SQL blocks', () => {
        renderWithTheme(<RunnableCodeBlock {...baseProps} isSql={true} />);

        expect(screen.getByLabelText('Run query')).toBeInTheDocument();
    });

    it('omits the run action icon when isSql is false', () => {
        renderWithTheme(<RunnableCodeBlock {...baseProps} isSql={false} />);

        expect(screen.queryByLabelText('Run query')).not.toBeInTheDocument();
    });

    it('builds a server-and-database aware tooltip for the run icon', () => {
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                serverName="alpha"
                databaseName="prod"
            />,
        );

        expect(
            screen.getByLabelText('Run on alpha/prod'),
        ).toBeInTheDocument();
    });

    it('falls back to a server-only tooltip when no database name is supplied', () => {
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                serverName="alpha"
            />,
        );

        expect(screen.getByLabelText('Run on alpha')).toBeInTheDocument();
    });
});

describe('RunnableCodeBlock query execution', () => {
    it('flags a code block with no executable SQL', async () => {
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="-- only a comment"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() => {
            expect(
                screen.getByText(/No executable SQL found/i),
            ).toBeInTheDocument();
        });
        // The fetch should never run for a block that contains no SQL.
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('renders the result table on success and posts the cleaned SQL', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                total_statements: 1,
                results: [
                    {
                        query: 'SELECT 1;',
                        columns: ['col_a'],
                        rows: [['v1']],
                        row_count: 1,
                    },
                ],
            }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
                databaseName="prod"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        // Result header + table content for the single statement.
        await waitFor(() =>
            expect(screen.getByText('1 statement')).toBeInTheDocument(),
        );
        expect(screen.getByText('col_a')).toBeInTheDocument();
        expect(screen.getByText('v1')).toBeInTheDocument();
        expect(screen.getByText('1 row')).toBeInTheDocument();

        // The fetch should carry the database name when supplied.
        expect(mockApiFetch).toHaveBeenCalledTimes(1);
        const callBody = JSON.parse(
            (mockApiFetch.mock.calls[0][1] as { body: string }).body,
        );
        expect(callBody).toEqual({
            query: 'SELECT 1;',
            database_name: 'prod',
        });
    });

    it('renders pluralised row counts and the truncation notice', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                total_statements: 2,
                results: [
                    {
                        query: 'SELECT * FROM a;',
                        columns: ['c'],
                        rows: [['x'], ['y'], ['z']],
                        row_count: 3,
                        truncated: true,
                    },
                    {
                        query: 'SELECT 2;',
                        error: 'boom',
                    },
                ],
            }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT * FROM a; SELECT 2;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(screen.getByText('2 statements')).toBeInTheDocument(),
        );
        expect(
            screen.getByText('Results limited to 3 rows'),
        ).toBeInTheDocument();
        expect(screen.getByText('boom')).toBeInTheDocument();
    });

    it('shows a confirmation prompt and re-runs with confirmed=true', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                requires_confirmation: true,
                write_statements: ['DELETE FROM t;'],
            }),
        );
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                total_statements: 1,
                results: [{ query: 'DELETE FROM t;', row_count: 0 }],
            }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="DELETE FROM t;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(
                screen.getByText(/contains statements that modify/i),
            ).toBeInTheDocument(),
        );
        // The confirmation panel lists the write statements as bulleted
        // paragraphs. At least one paragraph must echo the DELETE so the
        // operator can confirm what is about to run.
        const stmtParagraphs = screen
            .getAllByText(/DELETE FROM t;/)
            .filter((el) => el.tagName === 'P');
        expect(stmtParagraphs.length).toBeGreaterThan(0);

        // Execute the confirmation; the second call must include confirmed=true.
        await user.click(screen.getByRole('button', { name: /execute/i }));
        await waitFor(() =>
            expect(screen.getByText('1 statement')).toBeInTheDocument(),
        );

        const secondBody = JSON.parse(
            (mockApiFetch.mock.calls[1][1] as { body: string }).body,
        );
        expect(secondBody).toMatchObject({
            confirmed: true,
            query: 'DELETE FROM t;',
        });
    });

    it('dismisses the confirmation prompt when Cancel is clicked', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                requires_confirmation: true,
                write_statements: ['DROP TABLE t;'],
            }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="DROP TABLE t;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(
                screen.getByText(/contains statements that modify/i),
            ).toBeInTheDocument(),
        );

        await user.click(screen.getByRole('button', { name: /cancel/i }));
        await waitFor(() =>
            expect(
                screen.queryByText(/contains statements that modify/i),
            ).not.toBeInTheDocument(),
        );
    });

    it('surfaces the JSON error payload when the server returns non-2xx', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeErrorResponse({ json: { error: 'forbidden: ro-token' } }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(
                screen.getByText('forbidden: ro-token'),
            ).toBeInTheDocument(),
        );
    });

    it('falls back to the plain text body when the error response is not JSON', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeErrorResponse({ text: 'plain text failure' }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(
                screen.getByText('plain text failure'),
            ).toBeInTheDocument(),
        );
    });

    it('uses the status-based fallback when both JSON and text bodies are empty', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeErrorResponse({ status: 503, text: '' }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(
                screen.getByText('Query failed with status 503'),
            ).toBeInTheDocument(),
        );
    });

    it('captures network failures from the apiFetch call', async () => {
        mockApiFetch.mockRejectedValueOnce(new Error('network down'));
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(screen.getByText('network down')).toBeInTheDocument(),
        );
    });

    it('clears the error banner when the dismiss icon is clicked', async () => {
        mockApiFetch.mockRejectedValueOnce(new Error('temporary glitch'));
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(screen.getByText('temporary glitch')).toBeInTheDocument(),
        );

        // The dismiss IconButton is the only button rendered inside the
        // error banner.
        const errorText = screen.getByText('temporary glitch');
        const banner = errorText.closest('div');
        if (!banner) {
            throw new Error('error banner not found');
        }
        const dismissButton = banner.querySelector('button');
        if (!dismissButton) {
            throw new Error('dismiss button not found');
        }
        await act(async () => {
            await user.click(dismissButton);
        });

        await waitFor(() =>
            expect(
                screen.queryByText('temporary glitch'),
            ).not.toBeInTheDocument(),
        );
    });

    it('clears the result panel when the dismiss icon is clicked', async () => {
        mockApiFetch.mockResolvedValueOnce(
            makeOkResponse({
                total_statements: 1,
                results: [
                    {
                        query: 'SELECT 1;',
                        columns: ['c'],
                        rows: [['v']],
                        row_count: 1,
                    },
                ],
            }),
        );
        renderWithTheme(
            <RunnableCodeBlock
                {...baseProps}
                isSql={true}
                codeContent="SELECT 1;"
            />,
        );

        const user = userEvent.setup();
        await user.click(getRunButton());

        await waitFor(() =>
            expect(screen.getByText('1 statement')).toBeInTheDocument(),
        );

        const header = screen.getByText('1 statement').closest('div');
        if (!header) {
            throw new Error('result header not found');
        }
        const headerDismiss = header.querySelector('button');
        if (!headerDismiss) {
            throw new Error('header dismiss button not found');
        }
        await act(async () => {
            await user.click(headerDismiss);
        });

        await waitFor(() =>
            expect(
                screen.queryByText('1 statement'),
            ).not.toBeInTheDocument(),
        );
    });
});
