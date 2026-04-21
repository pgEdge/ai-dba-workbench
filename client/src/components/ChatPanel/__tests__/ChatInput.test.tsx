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
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ChatInput from '../ChatInput';
import { renderWithTheme } from '../../../test/renderWithTheme';

// Helper to get the native textarea element inside the MUI TextField
const getNativeTextarea = (container: HTMLElement) => {
    return container.querySelector('textarea') as HTMLTextAreaElement;
};

describe('ChatInput', () => {
    const defaultProps = {
        onSend: vi.fn(),
        disabled: false,
        inputHistory: [] as string[],
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders the text field', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByLabelText(/chat message input/i)
            ).toBeInTheDocument();
        });

        it('renders send button', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /send message/i })
            ).toBeInTheDocument();
        });

        it('renders hint text', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByText(/enter to send/i)
            ).toBeInTheDocument();
        });

        it('shows placeholder when enabled', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByPlaceholderText(/ask ellie a question/i)
            ).toBeInTheDocument();
        });

        it('shows waiting placeholder when disabled', () => {
            renderWithTheme(<ChatInput {...defaultProps} disabled={true} />);
            expect(
                screen.getByPlaceholderText(/waiting for response/i)
            ).toBeInTheDocument();
        });
    });

    describe('disabled state', () => {
        it('disables text field when disabled', () => {
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} disabled={true} />
            );
            const textarea = getNativeTextarea(container);
            expect(textarea).toBeDisabled();
        });

        it('disables send button when disabled', () => {
            renderWithTheme(<ChatInput {...defaultProps} disabled={true} />);
            expect(
                screen.getByRole('button', { name: /send message/i })
            ).toBeDisabled();
        });

        it('disables send button when input is empty', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /send message/i })
            ).toBeDisabled();
        });
    });

    describe('sending messages', () => {
        it('calls onSend when clicking send button with text', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: 'Hello world' } });
            fireEvent.click(
                screen.getByRole('button', { name: /send message/i })
            );

            expect(onSend).toHaveBeenCalledWith('Hello world');
        });

        it('calls onSend when pressing Enter', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: 'Hello world' } });
            fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });

            expect(onSend).toHaveBeenCalledWith('Hello world');
        });

        it('does not call onSend when pressing Shift+Enter', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: 'Hello' } });
            fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true });

            expect(onSend).not.toHaveBeenCalled();
        });

        it('trims whitespace from message', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, {
                target: { value: '  Hello world  ' },
            });
            fireEvent.click(
                screen.getByRole('button', { name: /send message/i })
            );

            expect(onSend).toHaveBeenCalledWith('Hello world');
        });

        it('clears input after sending', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: 'Hello world' } });
            fireEvent.click(
                screen.getByRole('button', { name: /send message/i })
            );

            await waitFor(() => {
                expect(textarea).toHaveValue('');
            });
        });

        it('does not send empty messages', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: '   ' } });
            fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });

            expect(onSend).not.toHaveBeenCalled();
        });

        it('does not send when disabled', async () => {
            const onSend = vi.fn();
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} onSend={onSend} disabled={true} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, {
                target: { value: 'Should not send' },
            });
            fireEvent.keyDown(textarea, { key: 'Enter' });

            expect(onSend).not.toHaveBeenCalled();
        });
    });

    describe('input history navigation', () => {
        // Note: Testing arrow key navigation for input history is complex
        // because it requires precise cursor positioning which is not well
        // supported in jsdom for textarea elements. These features are
        // covered by manual testing and integration tests.

        it('has inputHistory prop available', () => {
            const inputHistory = ['First message', 'Second message'];
            renderWithTheme(
                <ChatInput {...defaultProps} inputHistory={inputHistory} />
            );

            // Component renders without error with inputHistory
            expect(
                screen.getByLabelText(/chat message input/i)
            ).toBeInTheDocument();
        });

        it('accepts text input via change events', () => {
            const inputHistory = ['Previous message'];
            const { container } = renderWithTheme(
                <ChatInput {...defaultProps} inputHistory={inputHistory} />
            );

            const textarea = getNativeTextarea(container);
            fireEvent.change(textarea, { target: { value: 'New text' } });

            expect(textarea).toHaveValue('New text');
        });
    });

    describe('accessibility', () => {
        it('has accessible label for input', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByLabelText(/chat message input/i)
            ).toBeInTheDocument();
        });

        it('has accessible label for send button', () => {
            renderWithTheme(<ChatInput {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /send message/i })
            ).toBeInTheDocument();
        });
    });
});
