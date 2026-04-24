/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import renderWithTheme from '../../../../test/renderWithTheme';
import {
    ChannelDialogShell,
    type ChannelDialogShellProps,
} from '../ChannelDialogShell';

const createDefaultProps = (
    overrides: Partial<ChannelDialogShellProps> = {}
): ChannelDialogShellProps => ({
    open: true,
    onClose: vi.fn(),
    title: 'Test Dialog',
    tabs: ['Settings', 'Recipients'],
    activeTab: 0,
    onTabChange: vi.fn(),
    error: null,
    saving: false,
    onSave: vi.fn(),
    saveDisabled: false,
    saveLabel: 'Save',
    maxWidth: 'sm',
    children: <div data-testid="dialog-content">Dialog content</div>,
    ...overrides,
});

describe('ChannelDialogShell', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('does not render when open is false', () => {
        const props = createDefaultProps({ open: false });

        const { container } = renderWithTheme(<ChannelDialogShell {...props} />);

        expect(container).toBeEmptyDOMElement();
    });

    it('renders title when open', () => {
        const props = createDefaultProps({ title: 'Edit Channel: My Channel' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByText('Edit Channel: My Channel')).toBeInTheDocument();
    });

    it('renders tabs', () => {
        const props = createDefaultProps({
            tabs: ['Settings', 'Headers', 'Authentication'],
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByRole('tab', { name: 'Settings' })).toBeInTheDocument();
        expect(screen.getByRole('tab', { name: 'Headers' })).toBeInTheDocument();
        expect(
            screen.getByRole('tab', { name: 'Authentication' })
        ).toBeInTheDocument();
    });

    it('shows error alert when error is set', () => {
        const props = createDefaultProps({ error: 'Failed to save channel' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByText('Failed to save channel')).toBeInTheDocument();
    });

    it('does not show error alert when error is null', () => {
        const props = createDefaultProps({ error: null });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('calls onTabChange when tab is clicked', () => {
        const onTabChange = vi.fn();
        const props = createDefaultProps({
            tabs: ['Settings', 'Recipients'],
            activeTab: 0,
            onTabChange,
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const recipientsTab = screen.getByRole('tab', { name: 'Recipients' });
        fireEvent.click(recipientsTab);

        expect(onTabChange).toHaveBeenCalledWith(1);
    });

    it('calls onSave when save button is clicked', () => {
        const onSave = vi.fn();
        const props = createDefaultProps({ onSave, saveLabel: 'Create' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const saveButton = screen.getByRole('button', { name: 'Create' });
        fireEvent.click(saveButton);

        expect(onSave).toHaveBeenCalled();
    });

    it('calls onClose when cancel is clicked', () => {
        const onClose = vi.fn();
        const props = createDefaultProps({ onClose });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const cancelButton = screen.getByRole('button', { name: 'Cancel' });
        fireEvent.click(cancelButton);

        expect(onClose).toHaveBeenCalled();
    });

    it('shows spinner when saving', () => {
        const props = createDefaultProps({ saving: true, saveLabel: 'Save' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByLabelText('Saving')).toBeInTheDocument();
        expect(screen.queryByText('Save')).not.toBeInTheDocument();
    });

    it('disables save button when saveDisabled is true', () => {
        const props = createDefaultProps({ saveDisabled: true, saveLabel: 'Save' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const saveButton = screen.getByRole('button', { name: 'Save' });
        expect(saveButton).toBeDisabled();
    });

    it('disables save button when saving is true', () => {
        const props = createDefaultProps({ saving: true });

        renderWithTheme(<ChannelDialogShell {...props} />);

        // Find by progressbar since label text is hidden when saving
        const progressbar = screen.getByLabelText('Saving');
        const saveButton = progressbar.closest('button');
        expect(saveButton).toBeDisabled();
    });

    it('disables cancel button when saving is true', () => {
        const props = createDefaultProps({ saving: true });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const cancelButton = screen.getByRole('button', { name: 'Cancel' });
        expect(cancelButton).toBeDisabled();
    });

    it('renders children', () => {
        const props = createDefaultProps({
            children: <div data-testid="custom-content">Custom content</div>,
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByTestId('custom-content')).toBeInTheDocument();
        expect(screen.getByText('Custom content')).toBeInTheDocument();
    });

    it('renders with correct active tab', () => {
        const props = createDefaultProps({
            tabs: ['Settings', 'Recipients'],
            activeTab: 1,
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const recipientsTab = screen.getByRole('tab', { name: 'Recipients' });
        expect(recipientsTab).toHaveAttribute('aria-selected', 'true');

        const settingsTab = screen.getByRole('tab', { name: 'Settings' });
        expect(settingsTab).toHaveAttribute('aria-selected', 'false');
    });

    it('uses custom saveLabel', () => {
        const props = createDefaultProps({ saveLabel: 'Update Channel' });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(
            screen.getByRole('button', { name: 'Update Channel' })
        ).toBeInTheDocument();
    });

    it('enables save button when saveDisabled is false and not saving', () => {
        const props = createDefaultProps({
            saveDisabled: false,
            saving: false,
            saveLabel: 'Save',
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        const saveButton = screen.getByRole('button', { name: 'Save' });
        expect(saveButton).not.toBeDisabled();
    });

    it('renders single tab', () => {
        const props = createDefaultProps({
            tabs: ['Settings'],
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getByRole('tab', { name: 'Settings' })).toBeInTheDocument();
        expect(screen.getAllByRole('tab')).toHaveLength(1);
    });

    it('renders multiple tabs', () => {
        const props = createDefaultProps({
            tabs: ['Settings', 'Headers', 'Auth', 'Templates'],
        });

        renderWithTheme(<ChannelDialogShell {...props} />);

        expect(screen.getAllByRole('tab')).toHaveLength(4);
    });
});
