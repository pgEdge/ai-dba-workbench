/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiPut = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

import AdminMessagingChannels from '../AdminMessagingChannels';

const slackConfig = {
    channelType: 'slack',
    platformName: 'Slack',
    webhookUrlLabel: 'Webhook URL',
};

const mattermostConfig = {
    channelType: 'mattermost',
    platformName: 'Mattermost',
    webhookUrlLabel: 'Webhook URL',
};

// API responses no longer include `webhook_url` (redacted by the
// server, issue #187). Channels indicate whether one is configured via
// the `webhook_url_set` boolean instead.
const slackChannels = [
    {
        id: 1,
        channel_type: 'slack',
        name: 'Engineering Slack',
        description: 'Eng channel',
        enabled: true,
        is_estate_default: false,
        webhook_url_set: true,
    },
    {
        id: 2,
        channel_type: 'slack',
        name: 'Ops Slack',
        description: 'Ops channel',
        enabled: false,
        is_estate_default: true,
        webhook_url_set: true,
    },
];

describe('AdminMessagingChannels', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Loading and listing', () => {
        it('shows a loading indicator and then the channel list', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            expect(screen.getByRole('progressbar')).toBeInTheDocument();

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });
            expect(screen.getByText('Ops Slack')).toBeInTheDocument();
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/notification-channels');
        });

        it('shows the empty state when no channels of the given type exist', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });
        });

        it('filters channels by channel_type', async () => {
            const mixed = [
                ...slackChannels,
                {
                    id: 3,
                    channel_type: 'mattermost',
                    name: 'Different Type',
                    description: '',
                    enabled: true,
                    is_estate_default: false,
                    webhook_url_set: true,
                },
            ];
            mockApiGet.mockResolvedValue({ notification_channels: mixed });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });
            expect(screen.queryByText('Different Type')).not.toBeInTheDocument();
        });

        it('reflects the platform name from config', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(
                <AdminMessagingChannels config={mattermostConfig} />,
            );

            await waitFor(() => {
                expect(
                    screen.getByText('No Mattermost channels configured.'),
                ).toBeInTheDocument();
            });
            expect(screen.getByText('Mattermost Channels')).toBeInTheDocument();
        });

        it('shows a fetch error message when the API rejects', async () => {
            mockApiGet.mockRejectedValue(new Error('Network down'));

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Network down')).toBeInTheDocument();
            });
        });

        it('shows a fallback fetch error when a non-Error is thrown', async () => {
            mockApiGet.mockRejectedValue('boom');

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Create dialog', () => {
        it('opens a blank dialog and creates a channel', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 99 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create Slack channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');
            const createButton = within(dialog).getByRole('button', {
                name: /Create/i,
            });
            // Required fields empty -> disabled.
            expect(createButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'New Slack' },
            });
            fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
                target: { value: 'https://hooks.slack.com/example' },
            });

            expect(createButton).not.toBeDisabled();
            await user.click(createButton);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels',
                    expect.objectContaining({
                        channel_type: 'slack',
                        name: 'New Slack',
                        webhook_url: 'https://hooks.slack.com/example',
                    }),
                );
            });
        });

        it('shows a name-required error when the user pushes through with an empty name', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));
            const dialog = screen.getByRole('dialog');

            // Type something in URL only, then clear it back to whitespace
            // (the create button is otherwise disabled).
            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: '   ' },
            });
            fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
                target: { value: 'https://x' },
            });

            const createButton = within(dialog).getByRole('button', {
                name: /Create/i,
            });
            // Despite the URL being filled, name is whitespace -> disabled.
            expect(createButton).toBeDisabled();
        });

        it('surfaces a save error from the API', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockRejectedValue(new Error('Server boom'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));
            const dialog = screen.getByRole('dialog');

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'X' },
            });
            fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
                target: { value: 'https://x' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Create/i }),
            );

            await waitFor(() => {
                expect(within(dialog).getByText('Server boom')).toBeInTheDocument();
            });
        });

        it('surfaces a fallback save error for non-Error rejections', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockRejectedValue('weird thing');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));
            const dialog = screen.getByRole('dialog');

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'X' },
            });
            fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
                target: { value: 'https://x' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Create/i }),
            );

            await waitFor(() => {
                expect(
                    within(dialog).getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Edit dialog', () => {
        it('leaves the webhook URL form field blank for an existing channel', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Edit channel: Engineering Slack'),
                ).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');
            // The URL field is empty even though the channel has one
            // configured server-side.
            expect(within(dialog).getByLabelText('Webhook URL')).toHaveValue('');
            // A helper hint communicates configured-status.
            expect(
                within(dialog).getByText(
                    /A webhook URL is configured. Leave this blank/i,
                ),
            ).toBeInTheDocument();
        });

        it('keeps the Save button enabled with the URL field blank when the channel already has a URL', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Edit channel: Engineering Slack'),
                ).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');
            const saveButton = within(dialog).getByRole('button', {
                name: /Save/i,
            });
            expect(saveButton).not.toBeDisabled();
        });

        it('omits webhook_url from the PUT body when the form field stays blank', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            const dialog = screen.getByRole('dialog');
            // Only change the description to trigger a PUT.
            fireEvent.change(within(dialog).getByLabelText('Description'), {
                target: { value: 'updated description' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [, body] = mockApiPut.mock.calls[0];
            // The webhook URL must NOT be in the body — sending an
            // empty string would clear the redacted secret server-side.
            expect(body).not.toHaveProperty('webhook_url');
            expect(body).toHaveProperty('description', 'updated description');
        });

        it('sends the webhook URL when the user types a new value', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            const dialog = screen.getByRole('dialog');
            fireEvent.change(within(dialog).getByLabelText('Webhook URL'), {
                target: { value: 'https://hooks.slack.com/rotated' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    expect.objectContaining({
                        webhook_url: 'https://hooks.slack.com/rotated',
                    }),
                );
            });
        });

        it('requires a webhook URL on edit when the channel does not have one configured', async () => {
            mockApiGet.mockResolvedValue({
                notification_channels: [
                    {
                        id: 9,
                        channel_type: 'slack',
                        name: 'No URL Slack',
                        description: 'never had one',
                        enabled: true,
                        is_estate_default: false,
                        webhook_url_set: false,
                    },
                ],
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('No URL Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            const dialog = screen.getByRole('dialog');
            const saveButton = within(dialog).getByRole('button', {
                name: /Save/i,
            });
            // Channel never had a URL set. Save must be disabled until
            // the user types one.
            expect(saveButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
                target: { value: 'https://x' },
            });
            expect(saveButton).not.toBeDisabled();
        });

        it('updates only changed non-secret fields', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            const dialog = screen.getByRole('dialog');
            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'Renamed' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    { name: 'Renamed' },
                );
            });
        });

        it('updates is_estate_default and enabled when toggled in the dialog', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            await user.click(editButtons[0]);

            const dialog = screen.getByRole('dialog');
            // Toggle the Estate Default switch (was false).
            const estateToggle = within(dialog).getByRole('checkbox', {
                name: /Toggle estate default/i,
            });
            fireEvent.click(estateToggle);

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    { is_estate_default: true },
                );
            });
        });
    });

    describe('Toggle enabled', () => {
        it('toggles the enabled state from the table', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockResolvedValue({});

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const switches = screen.getAllByRole('checkbox', {
                name: 'Toggle channel enabled',
            });
            fireEvent.click(switches[0]);

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    { enabled: false },
                );
            });
        });

        it('shows an error when the toggle PUT fails', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockRejectedValue(new Error('Toggle failed'));

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const switches = screen.getAllByRole('checkbox', {
                name: 'Toggle channel enabled',
            });
            fireEvent.click(switches[0]);

            await waitFor(() => {
                expect(screen.getByText('Toggle failed')).toBeInTheDocument();
            });
        });

        it('shows a fallback error when the toggle throws non-Error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPut.mockRejectedValue('boom');

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const switches = screen.getAllByRole('checkbox', {
                name: 'Toggle channel enabled',
            });
            fireEvent.click(switches[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Test channel', () => {
        it('sends a test notification', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1/test',
                );
            });
            await waitFor(() => {
                expect(
                    screen.getByText(/Test notification sent successfully/i),
                ).toBeInTheDocument();
            });
        });

        it('surfaces a test error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPost.mockRejectedValue(new Error('Test failed'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Test failed')).toBeInTheDocument();
            });
        });

        it('shows fallback when test throws non-Error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPost.mockRejectedValue('weird');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Failed to send test notification'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Delete channel', () => {
        it('deletes a channel via the confirmation dialog', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete channel/i,
            });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Delete Slack Channel'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                );
            });
        });

        it('cancels the delete confirmation dialog', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete channel/i,
            });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Delete Slack Channel'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Cancel/i }));

            await waitFor(() => {
                expect(
                    screen.queryByText('Delete Slack Channel'),
                ).not.toBeInTheDocument();
            });
        });

        it('shows a delete error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiDelete.mockRejectedValue(new Error('Delete failed'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete channel/i,
            });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Delete Slack Channel'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(screen.getByText('Delete failed')).toBeInTheDocument();
            });
        });

        it('shows fallback delete error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiDelete.mockRejectedValue('boom');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete channel/i,
            });
            await user.click(deleteButtons[0]);
            await waitFor(() => {
                expect(
                    screen.getByText('Delete Slack Channel'),
                ).toBeInTheDocument();
            });
            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Dialog close', () => {
        it('closes the create dialog on Cancel', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Slack channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));
            await waitFor(() => {
                expect(
                    screen.getByText('Create Slack channel'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Cancel/i }));

            await waitFor(() => {
                expect(
                    screen.queryByText('Create Slack channel'),
                ).not.toBeInTheDocument();
            });
        });
    });

    describe('Alert dismissal', () => {
        it('dismisses error alerts', async () => {
            mockApiGet.mockRejectedValue(new Error('Something broke'));

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Something broke')).toBeInTheDocument();
            });

            const closeButton = screen.getByRole('button', { name: /close/i });
            fireEvent.click(closeButton);

            await waitFor(() => {
                expect(
                    screen.queryByText('Something broke'),
                ).not.toBeInTheDocument();
            });
        });

        it('dismisses success alerts', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: slackChannels });
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminMessagingChannels config={slackConfig} />);

            await waitFor(() => {
                expect(screen.getByText('Engineering Slack')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText(/Test notification sent successfully/i),
                ).toBeInTheDocument();
            });

            const closeButtons = screen.getAllByRole('button', { name: /close/i });
            fireEvent.click(closeButtons[0]);

            await waitFor(() => {
                expect(
                    screen.queryByText(/Test notification sent successfully/i),
                ).not.toBeInTheDocument();
            });
        });
    });
});
