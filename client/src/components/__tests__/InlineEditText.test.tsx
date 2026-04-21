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
import { screen, waitFor, fireEvent, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import InlineEditText from '../InlineEditText';
import { renderWithTheme } from '../../test/renderWithTheme';

describe('InlineEditText', () => {
    const defaultProps = {
        value: 'Test Value',
        onSave: vi.fn(),
        canEdit: true,
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('display mode', () => {
        it('renders the value as text', () => {
            renderWithTheme(<InlineEditText {...defaultProps} />);
            expect(screen.getByText('Test Value')).toBeInTheDocument();
        });

        it('renders Typography element', () => {
            renderWithTheme(<InlineEditText {...defaultProps} />);
            const text = screen.getByText('Test Value');
            expect(text.tagName).toBe('P');
        });

        it('shows cursor text when canEdit is true', () => {
            renderWithTheme(<InlineEditText {...defaultProps} canEdit={true} />);
            const text = screen.getByText('Test Value');
            expect(text).toHaveStyle({ cursor: 'text' });
        });

        it('shows default cursor when canEdit is false', () => {
            renderWithTheme(<InlineEditText {...defaultProps} canEdit={false} />);
            const text = screen.getByText('Test Value');
            expect(text).toHaveStyle({ cursor: 'default' });
        });
    });

    describe('entering edit mode', () => {
        it('enters edit mode on double-click when canEdit is true', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<InlineEditText {...defaultProps} canEdit={true} />);

            await user.dblClick(screen.getByText('Test Value'));

            expect(
                screen.getByRole('textbox')
            ).toBeInTheDocument();
        });

        it('does not enter edit mode on double-click when canEdit is false', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<InlineEditText {...defaultProps} canEdit={false} />);

            await user.dblClick(screen.getByText('Test Value'));

            expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
        });

        it('pre-populates input with current value', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<InlineEditText {...defaultProps} />);

            await user.dblClick(screen.getByText('Test Value'));

            expect(screen.getByRole('textbox')).toHaveValue('Test Value');
        });

        it('focuses and selects input text', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<InlineEditText {...defaultProps} />);

            await user.dblClick(screen.getByText('Test Value'));

            const input = screen.getByRole('textbox');
            expect(document.activeElement).toBe(input);
        });
    });

    describe('saving changes', () => {
        it('calls onSave with trimmed value on Enter', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, '  New Value  {Enter}');

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith('New Value');
            });
        });

        it('calls onSave on blur', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value');
            fireEvent.blur(input);

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith('New Value');
            });
        });

        it('exits edit mode after successful save', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
            });
        });

        it('does not call onSave if value unchanged', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.type(input, '{Enter}');

            expect(onSave).not.toHaveBeenCalled();
        });

        it('shows error when value is empty', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, '{Enter}');

            expect(screen.getByText(/name cannot be empty/i)).toBeInTheDocument();
            expect(onSave).not.toHaveBeenCalled();
        });
    });

    describe('canceling', () => {
        it('cancels edit on Escape', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn();
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.type(input, 'Changed');
            fireEvent.keyDown(input, { key: 'Escape' });

            await waitFor(() => {
                expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
            });
            expect(onSave).not.toHaveBeenCalled();
        });

        it('restores original value after cancel', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<InlineEditText {...defaultProps} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'Changed Value');
            fireEvent.keyDown(input, { key: 'Escape' });

            await waitFor(() => {
                expect(screen.getByText('Test Value')).toBeInTheDocument();
            });
        });
    });

    describe('error handling', () => {
        it('shows error message when save fails', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockRejectedValue(new Error('Save failed'));
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.getByText(/save failed/i)).toBeInTheDocument();
            });
        });

        it('stays in edit mode when save fails', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockRejectedValue(new Error('Save failed'));
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.getByText(/save failed/i)).toBeInTheDocument();
            });
            expect(screen.getByRole('textbox')).toBeInTheDocument();
        });

        it('shows generic error for non-Error exceptions', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockRejectedValue('String error');
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.getByText(/failed to save/i)).toBeInTheDocument();
            });
        });
    });

    describe('loading state', () => {
        it('shows spinner while saving', async () => {
            const user = userEvent.setup({ delay: null });
            let resolvePromise: (value: unknown) => void = () => undefined;
            const savePromise = new Promise((resolve) => {
                resolvePromise = resolve;
            });
            const onSave = vi.fn().mockReturnValue(savePromise);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.getByLabelText('Saving')).toBeInTheDocument();
            });

            await act(async () => {
                resolvePromise(undefined);
                await savePromise;
            });
        });

        it('disables input while saving', async () => {
            const user = userEvent.setup({ delay: null });
            let resolvePromise: (value: unknown) => void = () => undefined;
            const savePromise = new Promise((resolve) => {
                resolvePromise = resolve;
            });
            const onSave = vi.fn().mockReturnValue(savePromise);
            renderWithTheme(<InlineEditText {...defaultProps} onSave={onSave} />);

            await user.dblClick(screen.getByText('Test Value'));
            const input = screen.getByRole('textbox');
            await user.clear(input);
            await user.type(input, 'New Value{Enter}');

            await waitFor(() => {
                expect(screen.getByRole('textbox')).toBeDisabled();
            });

            await act(async () => {
                resolvePromise(undefined);
                await savePromise;
            });
        });
    });

    describe('prop updates', () => {
        it('updates displayed value when prop changes', () => {
            const { rerender } = renderWithTheme(
                <InlineEditText {...defaultProps} value="Original" />
            );

            expect(screen.getByText('Original')).toBeInTheDocument();

            rerender(<InlineEditText {...defaultProps} value="Updated" />);

            expect(screen.getByText('Updated')).toBeInTheDocument();
        });

        it('does not update input value while editing', async () => {
            const user = userEvent.setup({ delay: null });
            const { rerender } = renderWithTheme(
                <InlineEditText {...defaultProps} value="Original" />
            );

            await user.dblClick(screen.getByText('Original'));

            rerender(<InlineEditText {...defaultProps} value="Updated" />);

            expect(screen.getByRole('textbox')).toHaveValue('Original');
        });
    });
});
