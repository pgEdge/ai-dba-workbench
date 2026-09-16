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

import AdminSlackChannels from '../AdminSlackChannels';
import AdminMattermostChannels from '../AdminMattermostChannels';

/**
 * The Slack and Mattermost panels are thin descriptor wrappers around
 * AdminMessagingChannels. These tests pin the wiring each one
 * contributes: its channel type, its platform name, and its single
 * required webhook-URL secret. The shared behaviour itself is covered
 * by AdminMessagingChannels.test.tsx.
 */
describe.each([
    {
        name: 'AdminSlackChannels',
        Component: AdminSlackChannels,
        channelType: 'slack',
        platformName: 'Slack',
        webhookUrl: 'https://hooks.slack.com/services/T000/B000/xxx',
    },
    {
        name: 'AdminMattermostChannels',
        Component: AdminMattermostChannels,
        channelType: 'mattermost',
        platformName: 'Mattermost',
        webhookUrl: 'https://mattermost.example.com/hooks/abc123',
    },
])('$name', ({ Component, channelType, platformName, webhookUrl }) => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiGet.mockResolvedValue({ notification_channels: [] });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders the platform heading and empty state', async () => {
        renderWithTheme(<Component />);

        await waitFor(() => {
            expect(
                screen.getByText(`No ${platformName} channels configured.`),
            ).toBeInTheDocument();
        });
        expect(
            screen.getByText(`${platformName} Channels`),
        ).toBeInTheDocument();
        // The webhook URL is a secret, so it gets no table column.
        expect(screen.queryByText('Webhook URL')).not.toBeInTheDocument();
    });

    it('creates a channel with the platform channel type', async () => {
        mockApiPost.mockResolvedValue({ id: 5 });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<Component />);

        await waitFor(() => {
            expect(
                screen.getByText(`No ${platformName} channels configured.`),
            ).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Add Channel/i }));
        const dialog = screen.getByRole('dialog');

        fireEvent.change(within(dialog).getByLabelText('Name *'), {
            target: { value: `${platformName} alerts` },
        });
        fireEvent.change(within(dialog).getByLabelText('Webhook URL *'), {
            target: { value: webhookUrl },
        });

        await user.click(
            within(dialog).getByRole('button', { name: /Create/i }),
        );

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/notification-channels',
                expect.objectContaining({
                    channel_type: channelType,
                    name: `${platformName} alerts`,
                    webhook_url: webhookUrl,
                }),
            );
        });
    });
});
