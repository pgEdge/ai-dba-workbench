/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { screen } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ChatMessage, { type ChatMessageData } from '../ChatMessage';
import { renderWithTheme } from '../../../test/renderWithTheme';

// ChatMessage's formatTimestamp helper calls `Date.toLocaleString(undefined,
// {...})` so the rendered string depends on the test runner's locale. CI runs
// on an en-US image and emits "Jan 15, 2024, 10:30 AM"; a contributor on a
// machine set to en-GB (or any day-first locale) sees "15 Jan 2024, 10:30".
// To keep the assertions locale-independent, compute the expected display
// string here with the same ISO input and the same Intl options the
// component uses, rather than hard-coding a locale-specific pattern.
const TIMESTAMP_ISO = '2024-01-15T10:30:00Z';

const expectedFormattedTimestamp = (iso: string): string =>
    new Date(iso).toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
    });

// Mock toolDisplayNames
vi.mock('../../../utils/toolDisplayNames', () => ({
    getToolDisplayName: (name: string) => name,
}));

// Mock MarkdownExports
vi.mock('../../shared/MarkdownExports', () => ({
    createCleanTheme: () => ({}),
    extractLanguage: (className?: string) =>
        className?.replace('language-', '') || '',
}));

// Mock CopyCodeButton
vi.mock('../../shared/CopyCodeButton', () => ({
    default: () => <button data-testid="copy-button">Copy</button>,
}));

// Mock markdownStyles
vi.mock('../../shared/markdownStyles', () => ({
    getCodeBlockButtonGroupSx: () => ({}),
}));

describe('ChatMessage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('user messages', () => {
        it('renders user message text', () => {
            const message: ChatMessageData = {
                role: 'user',
                content: 'Hello, how are you?',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Hello, how are you?')).toBeInTheDocument();
        });

        it('renders user message with timestamp', () => {
            const message: ChatMessageData = {
                role: 'user',
                content: 'Hello',
                timestamp: TIMESTAMP_ISO,
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText(expectedFormattedTimestamp(TIMESTAMP_ISO)),
            ).toBeInTheDocument();
        });
    });

    describe('assistant messages', () => {
        it('renders assistant message text', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'I can help you with that.',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText('I can help you with that.')
            ).toBeInTheDocument();
        });

        it('renders markdown in assistant messages', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: '**Bold text** and *italic text*',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Bold text')).toBeInTheDocument();
            expect(screen.getByText(/italic text/)).toBeInTheDocument();
        });

        it('renders code blocks in assistant messages', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: '```sql\nSELECT * FROM users;\n```',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            // The code block renders the SQL statement; check for presence
            // using a partial text match since syntax highlighting may split it
            expect(
                screen.getByText(/SELECT/i)
            ).toBeInTheDocument();
        });

        it('renders timestamp for assistant messages', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Response',
                timestamp: TIMESTAMP_ISO,
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText(expectedFormattedTimestamp(TIMESTAMP_ISO)),
            ).toBeInTheDocument();
        });
    });

    describe('system messages', () => {
        it('renders system message text', () => {
            const message: ChatMessageData = {
                role: 'system',
                content: 'New conversation started',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText('New conversation started')
            ).toBeInTheDocument();
        });

        it('does not render timestamp for system messages', () => {
            const message: ChatMessageData = {
                role: 'system',
                content: 'System message',
                timestamp: TIMESTAMP_ISO,
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            // Year is the most stable anchor across locales for a rendered
            // timestamp from this ISO input.
            expect(screen.queryByText(/2024/)).not.toBeInTheDocument();
        });
    });

    describe('error messages', () => {
        it('renders error message with special styling', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'An error occurred',
                isError: true,
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('An error occurred')).toBeInTheDocument();
        });

        it('renders error timestamp', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Error message',
                isError: true,
                timestamp: TIMESTAMP_ISO,
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText(expectedFormattedTimestamp(TIMESTAMP_ISO)),
            ).toBeInTheDocument();
        });
    });

    describe('content blocks', () => {
        it('renders content from text blocks', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: [
                    { type: 'text', text: 'First block' },
                    { type: 'text', text: 'Second block' },
                ],
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(
                screen.getByText(/First block.*Second block/s)
            ).toBeInTheDocument();
        });

        it('ignores non-text blocks', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: [
                    { type: 'text', text: 'Text content' },
                    { type: 'tool_use', name: 'some_tool' },
                ],
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Text content')).toBeInTheDocument();
            expect(screen.queryByText('some_tool')).not.toBeInTheDocument();
        });
    });

    describe('tool activity', () => {
        it('renders tool activity chips', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Response with tools',
                activity: [
                    { name: 'get_server_info', status: 'completed' },
                    { name: 'run_query', status: 'completed' },
                ],
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('get_server_info')).toBeInTheDocument();
            expect(screen.getByText('run_query')).toBeInTheDocument();
        });

        it('does not render activity when empty', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Response',
                activity: [],
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            // Should render the message without chips container
            expect(screen.getByText('Response')).toBeInTheDocument();
        });
    });

    describe('theme modes', () => {
        it('renders in dark mode', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Dark mode message',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Dark mode message')).toBeInTheDocument();
        });

        it('renders in light mode', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Light mode message',
            };
            renderWithTheme(<ChatMessage message={message} mode="light" />);

            expect(screen.getByText('Light mode message')).toBeInTheDocument();
        });
    });

    describe('markdown features', () => {
        it('renders headers', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: '# Heading 1\n## Heading 2\n### Heading 3',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Heading 1')).toBeInTheDocument();
            expect(screen.getByText('Heading 2')).toBeInTheDocument();
            expect(screen.getByText('Heading 3')).toBeInTheDocument();
        });

        it('renders lists', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: '- Item 1\n- Item 2\n- Item 3',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('Item 1')).toBeInTheDocument();
            expect(screen.getByText('Item 2')).toBeInTheDocument();
            expect(screen.getByText('Item 3')).toBeInTheDocument();
        });

        it('renders links with safe href', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: '[Safe link](https://example.com)',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            const link = screen.getByText('Safe link');
            expect(link).toHaveAttribute('href', 'https://example.com');
            expect(link).toHaveAttribute('target', '_blank');
            expect(link).toHaveAttribute('rel', 'noopener noreferrer');
        });

        it('renders inline code', () => {
            const message: ChatMessageData = {
                role: 'assistant',
                content: 'Use the `SELECT` statement',
            };
            renderWithTheme(<ChatMessage message={message} mode="dark" />);

            expect(screen.getByText('SELECT')).toBeInTheDocument();
        });
    });
});
