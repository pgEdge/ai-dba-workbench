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
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { renderWithTheme } from '../../test/renderWithTheme';

describe('DeleteConfirmationDialog', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onConfirm: vi.fn(),
        title: 'Delete Item',
        message: 'Are you sure you want to delete',
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders dialog when open is true', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(screen.getByText('Delete Item')).toBeInTheDocument();
        });

        it('does not render dialog when open is false', () => {
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} open={false} />
            );
            expect(screen.queryByText('Delete Item')).not.toBeInTheDocument();
        });

        it('renders the title', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(screen.getByText('Delete Item')).toBeInTheDocument();
        });

        it('renders the message', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(
                screen.getByText(/Are you sure you want to delete/i)
            ).toBeInTheDocument();
        });

        it('renders the item name when provided', () => {
            renderWithTheme(
                <DeleteConfirmationDialog
                    {...defaultProps}
                    itemName="Test Server"
                />
            );
            expect(screen.getByText('Test Server')).toBeInTheDocument();
        });

        it('renders warning icon', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(screen.getByTestId('WarningIcon')).toBeInTheDocument();
        });

        it('renders Cancel button', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /cancel/i })
            ).toBeInTheDocument();
        });

        it('renders Delete button', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /delete/i })
            ).toBeInTheDocument();
        });
    });

    describe('loading state', () => {
        it('shows Deleting... text when loading', () => {
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} loading={true} />
            );
            expect(
                screen.getByRole('button', { name: /deleting/i })
            ).toBeInTheDocument();
        });

        it('shows progress indicator when loading', () => {
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} loading={true} />
            );
            expect(screen.getByLabelText('Deleting')).toBeInTheDocument();
        });

        it('disables Cancel button when loading', () => {
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} loading={true} />
            );
            expect(
                screen.getByRole('button', { name: /cancel/i })
            ).toBeDisabled();
        });

        it('disables Delete button when loading', () => {
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} loading={true} />
            );
            expect(
                screen.getByRole('button', { name: /deleting/i })
            ).toBeDisabled();
        });
    });

    describe('interactions', () => {
        it('calls onClose when Cancel button is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            const onClose = vi.fn();
            renderWithTheme(
                <DeleteConfirmationDialog {...defaultProps} onClose={onClose} />
            );

            await user.click(screen.getByRole('button', { name: /cancel/i }));

            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('calls onConfirm when Delete button is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            const onConfirm = vi.fn();
            renderWithTheme(
                <DeleteConfirmationDialog
                    {...defaultProps}
                    onConfirm={onConfirm}
                />
            );

            await user.click(screen.getByRole('button', { name: /delete/i }));

            expect(onConfirm).toHaveBeenCalledTimes(1);
        });

        it('handles async onConfirm', async () => {
            const user = userEvent.setup({ delay: null });
            const onConfirm = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(
                <DeleteConfirmationDialog
                    {...defaultProps}
                    onConfirm={onConfirm}
                />
            );

            await user.click(screen.getByRole('button', { name: /delete/i }));

            await waitFor(() => {
                expect(onConfirm).toHaveBeenCalledTimes(1);
            });
        });

        it('does not call onConfirm when it is undefined', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <DeleteConfirmationDialog
                    {...defaultProps}
                    onConfirm={undefined}
                />
            );

            // Should not throw when clicking Delete
            await user.click(screen.getByRole('button', { name: /delete/i }));
        });
    });

    describe('accessibility', () => {
        it('has accessible title', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            expect(
                screen.getByRole('heading', { name: /delete item/i })
            ).toBeInTheDocument();
        });

        it('has aria-labelledby pointing to title', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            const dialog = screen.getByRole('dialog');
            expect(dialog).toHaveAttribute(
                'aria-labelledby',
                'delete-dialog-title'
            );
        });

        it('has aria-describedby pointing to description', () => {
            renderWithTheme(<DeleteConfirmationDialog {...defaultProps} />);
            const dialog = screen.getByRole('dialog');
            expect(dialog).toHaveAttribute(
                'aria-describedby',
                'delete-dialog-description'
            );
        });
    });
});
