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
import { Chip } from '@mui/material';
import renderWithTheme from '../../../../test/renderWithTheme';
import { ChannelTable, ChannelTableProps } from '../ChannelTable';
import { BaseChannel, ChannelColumnDef } from '../channelTypes';

interface TestChannel extends BaseChannel {
    extra_field: string;
}

const createMockChannel = (overrides: Partial<TestChannel> = {}): TestChannel => ({
    id: 1,
    name: 'Test Channel',
    description: 'A test channel description',
    enabled: true,
    is_estate_default: false,
    extra_field: 'extra value',
    ...overrides,
});

const createDefaultProps = (
    overrides: Partial<ChannelTableProps<TestChannel>> = {}
): ChannelTableProps<TestChannel> => ({
    channels: [],
    loading: false,
    extraColumns: [],
    testingChannelId: null,
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onToggleEnabled: vi.fn(),
    onTest: vi.fn(),
    onAdd: vi.fn(),
    emptyMessage: 'No channels configured.',
    testTooltip: 'Send test notification',
    testAriaLabel: 'send test notification',
    testingAriaLabel: 'Sending test',
    title: 'Test channels',
    error: null,
    success: null,
    onClearError: vi.fn(),
    onClearSuccess: vi.fn(),
    ...overrides,
});

describe('ChannelTable', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders loading spinner when loading is true', () => {
        const props = createDefaultProps({ loading: true });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(
            screen.getByLabelText('Loading test channels')
        ).toBeInTheDocument();
    });

    it('renders empty state message when channels is empty', () => {
        const props = createDefaultProps({
            channels: [],
            emptyMessage: 'No channels configured.',
        });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('No channels configured.')).toBeInTheDocument();
    });

    it('renders channel rows with name and description', () => {
        const channels = [
            createMockChannel({ id: 1, name: 'Email Channel', description: 'Sends emails' }),
            createMockChannel({ id: 2, name: 'Webhook Channel', description: 'Sends webhooks' }),
        ];
        const props = createDefaultProps({ channels });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('Email Channel')).toBeInTheDocument();
        expect(screen.getByText('Sends emails')).toBeInTheDocument();
        expect(screen.getByText('Webhook Channel')).toBeInTheDocument();
        expect(screen.getByText('Sends webhooks')).toBeInTheDocument();
    });

    it('renders enabled switch for each channel', () => {
        const channels = [
            createMockChannel({ id: 1, enabled: true }),
            createMockChannel({ id: 2, enabled: false }),
        ];
        const props = createDefaultProps({ channels });

        renderWithTheme(<ChannelTable {...props} />);

        const switches = screen.getAllByRole('checkbox', {
            name: 'Toggle channel enabled',
        });
        expect(switches).toHaveLength(2);
        expect(switches[0]).toBeChecked();
        expect(switches[1]).not.toBeChecked();
    });

    it('renders action buttons for each channel', () => {
        const channels = [createMockChannel()];
        const props = createDefaultProps({ channels });

        renderWithTheme(<ChannelTable {...props} />);

        expect(
            screen.getByRole('button', { name: 'send test notification' })
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: 'edit channel' })
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: 'delete channel' })
        ).toBeInTheDocument();
    });

    it('calls onToggleEnabled when switch is clicked', () => {
        const onToggleEnabled = vi.fn();
        const channel = createMockChannel();
        const props = createDefaultProps({
            channels: [channel],
            onToggleEnabled,
        });

        renderWithTheme(<ChannelTable {...props} />);

        const enabledSwitch = screen.getByRole('checkbox', {
            name: 'Toggle channel enabled',
        });
        fireEvent.click(enabledSwitch);

        expect(onToggleEnabled).toHaveBeenCalledWith(channel);
    });

    it('calls onEdit when edit button is clicked', () => {
        const onEdit = vi.fn();
        const channel = createMockChannel();
        const props = createDefaultProps({ channels: [channel], onEdit });

        renderWithTheme(<ChannelTable {...props} />);

        const editButton = screen.getByRole('button', { name: 'edit channel' });
        fireEvent.click(editButton);

        expect(onEdit).toHaveBeenCalledWith(expect.any(Object), channel);
    });

    it('calls onDelete when delete button is clicked', () => {
        const onDelete = vi.fn();
        const channel = createMockChannel();
        const props = createDefaultProps({ channels: [channel], onDelete });

        renderWithTheme(<ChannelTable {...props} />);

        const deleteButton = screen.getByRole('button', {
            name: 'delete channel',
        });
        fireEvent.click(deleteButton);

        expect(onDelete).toHaveBeenCalledWith(expect.any(Object), channel);
    });

    it('calls onTest when test button is clicked', () => {
        const onTest = vi.fn();
        const channel = createMockChannel();
        const props = createDefaultProps({ channels: [channel], onTest });

        renderWithTheme(<ChannelTable {...props} />);

        const testButton = screen.getByRole('button', {
            name: 'send test notification',
        });
        fireEvent.click(testButton);

        expect(onTest).toHaveBeenCalledWith(expect.any(Object), channel);
    });

    it('calls onAdd when Add Channel button is clicked', () => {
        const onAdd = vi.fn();
        const props = createDefaultProps({ onAdd });

        renderWithTheme(<ChannelTable {...props} />);

        const addButton = screen.getByRole('button', { name: /Add Channel/i });
        fireEvent.click(addButton);

        expect(onAdd).toHaveBeenCalled();
    });

    it('renders extra columns when provided', () => {
        const extraColumns: ChannelColumnDef<TestChannel>[] = [
            {
                label: 'Recipients',
                render: (channel) => (
                    <Chip label={channel.extra_field} size="small" />
                ),
            },
        ];
        const channels = [createMockChannel({ extra_field: '5' })];
        const props = createDefaultProps({ channels, extraColumns });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('Recipients')).toBeInTheDocument();
        expect(screen.getByText('5')).toBeInTheDocument();
    });

    it('renders error alert when error is set', () => {
        const onClearError = vi.fn();
        const props = createDefaultProps({
            error: 'Failed to load channels',
            onClearError,
        });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('Failed to load channels')).toBeInTheDocument();

        const closeButton = screen.getByRole('button', { name: /close/i });
        fireEvent.click(closeButton);

        expect(onClearError).toHaveBeenCalled();
    });

    it('renders success alert when success is set', () => {
        const onClearSuccess = vi.fn();
        const props = createDefaultProps({
            success: 'Channel created successfully',
            onClearSuccess,
        });

        renderWithTheme(<ChannelTable {...props} />);

        expect(
            screen.getByText('Channel created successfully')
        ).toBeInTheDocument();

        const closeButton = screen.getByRole('button', { name: /close/i });
        fireEvent.click(closeButton);

        expect(onClearSuccess).toHaveBeenCalled();
    });

    it('shows testing spinner for the channel being tested', () => {
        const channel = createMockChannel({ id: 42 });
        const props = createDefaultProps({
            channels: [channel],
            testingChannelId: 42,
            testingAriaLabel: 'Sending test email',
        });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByLabelText('Sending test email')).toBeInTheDocument();

        const testButton = screen.getByRole('button', {
            name: 'send test notification',
        });
        expect(testButton).toBeDisabled();
    });

    it('does not show testing spinner for channels not being tested', () => {
        const channels = [
            createMockChannel({ id: 1 }),
            createMockChannel({ id: 2, name: 'Other Channel' }),
        ];
        const props = createDefaultProps({
            channels,
            testingChannelId: 1,
        });

        renderWithTheme(<ChannelTable {...props} />);

        const testButtons = screen.getAllByRole('button', {
            name: 'send test notification',
        });
        expect(testButtons[0]).toBeDisabled();
        expect(testButtons[1]).not.toBeDisabled();
    });

    it('renders title in the header', () => {
        const props = createDefaultProps({ title: 'Email channels' });

        renderWithTheme(<ChannelTable {...props} />);

        expect(
            screen.getByRole('heading', { name: 'Email channels' })
        ).toBeInTheDocument();
    });

    it('renders estate default switch as disabled for each channel', () => {
        const channels = [createMockChannel({ is_estate_default: true })];
        const props = createDefaultProps({ channels });

        renderWithTheme(<ChannelTable {...props} />);

        const estateDefaultSwitch = screen.getByRole('checkbox', {
            name: 'Toggle estate default',
        });
        expect(estateDefaultSwitch).toBeDisabled();
        expect(estateDefaultSwitch).toBeChecked();
    });

    it('truncates long descriptions', () => {
        const longDescription =
            'This is a very long description that should be truncated when displayed in the table view to prevent overflow issues';
        const channels = [
            createMockChannel({ description: longDescription }),
        ];
        const props = createDefaultProps({ channels });

        renderWithTheme(<ChannelTable {...props} />);

        expect(
            screen.queryByText(longDescription)
        ).not.toBeInTheDocument();
        expect(screen.getByText(/This is a very long/)).toBeInTheDocument();
    });

    it('renders table headers', () => {
        const props = createDefaultProps();

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('Name')).toBeInTheDocument();
        expect(screen.getByText('Description')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Estate Default')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();
    });

    it('renders multiple extra columns in correct order', () => {
        const extraColumns: ChannelColumnDef<TestChannel>[] = [
            { label: 'Column A', render: () => 'Value A' },
            { label: 'Column B', render: () => 'Value B' },
        ];
        const channels = [createMockChannel()];
        const props = createDefaultProps({ channels, extraColumns });

        renderWithTheme(<ChannelTable {...props} />);

        expect(screen.getByText('Column A')).toBeInTheDocument();
        expect(screen.getByText('Column B')).toBeInTheDocument();
        expect(screen.getByText('Value A')).toBeInTheDocument();
        expect(screen.getByText('Value B')).toBeInTheDocument();
    });
});
