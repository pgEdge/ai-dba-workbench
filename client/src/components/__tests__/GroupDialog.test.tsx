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
import { screen, waitFor, act, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import GroupDialog from '../GroupDialog';
import { renderWithTheme } from '../../test/renderWithTheme';

// Mock child panels to avoid fetch calls
vi.mock('../AlertOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="alert-overrides-panel">
            AlertOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ProbeOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="probe-overrides-panel">
            ProbeOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ChannelOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="channel-overrides-panel">
            ChannelOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

const getNameField = () => screen.getByRole('textbox', { name: /^name/i });
const getDescriptionField = () =>
    screen.getByRole('textbox', { name: /description/i });

describe('GroupDialog', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onSave: vi.fn(),
        mode: 'create' as const,
        group: null,
        isSuperuser: false,
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('create mode rendering', () => {
        it('renders dialog with Add Cluster Group title', () => {
            renderWithTheme(<GroupDialog {...defaultProps} />);
            expect(screen.getByText('Add Cluster Group')).toBeInTheDocument();
        });

        it('renders name field', () => {
            renderWithTheme(<GroupDialog {...defaultProps} />);
            expect(getNameField()).toBeInTheDocument();
        });

        it('renders description field', () => {
            renderWithTheme(<GroupDialog {...defaultProps} />);
            expect(getDescriptionField()).toBeInTheDocument();
        });

        it('does not render shared checkbox for non-superusers', () => {
            renderWithTheme(
                <GroupDialog {...defaultProps} isSuperuser={false} />
            );
            expect(
                screen.queryByLabelText(/share with all users/i)
            ).not.toBeInTheDocument();
        });

        it('renders shared checkbox for superusers', () => {
            renderWithTheme(
                <GroupDialog {...defaultProps} isSuperuser={true} />
            );
            expect(
                screen.getByLabelText(/share with all users/i)
            ).toBeInTheDocument();
        });

        it('renders Cancel and Save buttons', () => {
            renderWithTheme(<GroupDialog {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /cancel/i })
            ).toBeInTheDocument();
            expect(
                screen.getByRole('button', { name: /save/i })
            ).toBeInTheDocument();
        });

        it('does not render when open is false', () => {
            renderWithTheme(<GroupDialog {...defaultProps} open={false} />);
            expect(
                screen.queryByText('Add Cluster Group')
            ).not.toBeInTheDocument();
        });

        it('does not render tabs in create mode', () => {
            renderWithTheme(<GroupDialog {...defaultProps} />);
            expect(
                screen.queryByRole('tab', { name: /details/i })
            ).not.toBeInTheDocument();
        });
    });

    describe('edit mode rendering', () => {
        const editGroup = {
            id: 1,
            name: 'Production',
            description: 'Production cluster group',
            is_shared: true,
        };

        it('renders fullscreen dialog with Group Settings title', () => {
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );
            expect(
                screen.getByText('Group Settings: Production')
            ).toBeInTheDocument();
        });

        it('renders tabs in edit mode', () => {
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );
            expect(
                screen.getByRole('tab', { name: /details/i })
            ).toBeInTheDocument();
            expect(
                screen.getByRole('tab', { name: /alert overrides/i })
            ).toBeInTheDocument();
            expect(
                screen.getByRole('tab', { name: /probe configuration/i })
            ).toBeInTheDocument();
            expect(
                screen.getByRole('tab', { name: /notification channels/i })
            ).toBeInTheDocument();
        });

        it('pre-populates name field in edit mode', () => {
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );
            expect(getNameField()).toHaveValue('Production');
        });

        it('pre-populates description field in edit mode', () => {
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );
            expect(getDescriptionField()).toHaveValue(
                'Production cluster group'
            );
        });

        it('shows AlertOverridesPanel when Alert overrides tab is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );

            await user.click(
                screen.getByRole('tab', { name: /alert overrides/i })
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('alert-overrides-panel')
                ).toBeInTheDocument();
            });
        });

        it('shows ProbeOverridesPanel when Probe configuration tab is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );

            await user.click(
                screen.getByRole('tab', { name: /probe configuration/i })
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('probe-overrides-panel')
                ).toBeInTheDocument();
            });
        });

        it('shows ChannelOverridesPanel when Notification channels tab is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );

            await user.click(
                screen.getByRole('tab', { name: /notification channels/i })
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('channel-overrides-panel')
                ).toBeInTheDocument();
            });
        });

        it('renders close button in edit mode', () => {
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    mode="edit"
                    group={editGroup}
                />
            );
            expect(
                screen.getByRole('button', { name: /close edit group/i })
            ).toBeInTheDocument();
        });
    });

    describe('validation', () => {
        it('shows error when name is empty', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<GroupDialog {...defaultProps} />);

            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/name is required/i)).toBeInTheDocument();
        });

        it('clears name error when user types', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<GroupDialog {...defaultProps} />);

            await user.click(screen.getByRole('button', { name: /save/i }));
            expect(screen.getByText(/name is required/i)).toBeInTheDocument();

            await user.type(getNameField(), 'Test Group');
            expect(
                screen.queryByText(/name is required/i)
            ).not.toBeInTheDocument();
        });
    });

    describe('form submission', () => {
        it('calls onSave with trimmed form data', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(
                <GroupDialog {...defaultProps} onSave={onSave} />
            );

            await user.type(getNameField(), '  Test Group  ');
            await user.type(getDescriptionField(), '  Test description  ');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith({
                    name: 'Test Group',
                    description: 'Test description',
                    is_shared: false,
                });
            });
        });

        it('calls onClose after successful save', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            const onClose = vi.fn();
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    onSave={onSave}
                    onClose={onClose}
                />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onClose).toHaveBeenCalled();
            });
        });

        it('shows error when save fails', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockRejectedValue(new Error('Save failed'));
            renderWithTheme(
                <GroupDialog {...defaultProps} onSave={onSave} />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(screen.getByText(/save failed/i)).toBeInTheDocument();
            });
        });

        it('does not call onClose when save fails', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockRejectedValue(new Error('Save failed'));
            const onClose = vi.fn();
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    onSave={onSave}
                    onClose={onClose}
                />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(screen.getByText(/save failed/i)).toBeInTheDocument();
            });

            expect(onClose).not.toHaveBeenCalled();
        });

        it('disables form during save', async () => {
            const user = userEvent.setup({ delay: null });
            let resolvePromise: (value: unknown) => void = () => undefined;
            const savePromise = new Promise((resolve) => {
                resolvePromise = resolve;
            });
            const onSave = vi.fn().mockReturnValue(savePromise);
            renderWithTheme(
                <GroupDialog {...defaultProps} onSave={onSave} />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(getNameField()).toBeDisabled();
            });

            await act(async () => {
                resolvePromise(undefined);
                await savePromise;
            });
        });

        it('includes is_shared when superuser checks the checkbox', async () => {
            const user = userEvent.setup({ delay: null });
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    onSave={onSave}
                    isSuperuser={true}
                />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByLabelText(/share with all users/i));
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith(
                    expect.objectContaining({
                        is_shared: true,
                    })
                );
            });
        });
    });

    describe('cancel behavior', () => {
        it('calls onClose when Cancel button is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            const onClose = vi.fn();
            renderWithTheme(
                <GroupDialog {...defaultProps} onClose={onClose} />
            );

            await user.click(screen.getByRole('button', { name: /cancel/i }));

            expect(onClose).toHaveBeenCalled();
        });

        it('does not call onClose when saving', async () => {
            const user = userEvent.setup({ delay: null });
            const savePromise = new Promise(() => {});
            const onSave = vi.fn().mockReturnValue(savePromise);
            const onClose = vi.fn();
            renderWithTheme(
                <GroupDialog
                    {...defaultProps}
                    onSave={onSave}
                    onClose={onClose}
                />
            );

            await user.type(getNameField(), 'Test Group');
            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(getNameField()).toBeDisabled();
            });

            const cancelButton = screen.getByRole('button', {
                name: /cancel/i,
            });
            expect(cancelButton).toBeDisabled();
            fireEvent.click(cancelButton);

            expect(onClose).not.toHaveBeenCalled();
        });

        it('resets form when reopened', async () => {
            const user = userEvent.setup({ delay: null });
            const { rerender } = renderWithTheme(
                <GroupDialog {...defaultProps} />
            );

            await user.type(getNameField(), 'Test Group');

            // Close and reopen
            rerender(<GroupDialog {...defaultProps} open={false} />);
            rerender(<GroupDialog {...defaultProps} open={true} />);

            expect(getNameField()).toHaveValue('');
        });
    });
});
