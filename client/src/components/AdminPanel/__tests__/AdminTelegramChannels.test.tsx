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

import AdminTelegramChannels from '../AdminTelegramChannels';

/**
 * A plausible @BotFather token. The server never returns one — the
 * list response only carries `telegram_bot_token_set` — so this value
 * exists in the tests purely to type into the form and to assert that
 * it never reaches the rendered document.
 */
const BOT_TOKEN = '123456789:AAHfakeTokenValueForTests';

const telegramChannels = [
    {
        id: 11,
        channel_type: 'telegram',
        name: 'Ops Telegram',
        description: 'Ops alerts',
        enabled: true,
        is_estate_default: false,
        telegram_bot_token_set: true,
        telegram_chat_id: '-1001234567890',
    },
    {
        id: 12,
        channel_type: 'telegram',
        name: 'Public Channel',
        description: 'Public announcements',
        enabled: false,
        is_estate_default: true,
        telegram_bot_token_set: false,
        telegram_chat_id: '@pgedge_alerts',
    },
];

/** Opens the create dialog and returns it. */
const openCreateDialog = async (): Promise<HTMLElement> => {
    const user = userEvent.setup({ delay: null });
    await user.click(screen.getByRole('button', { name: /Add Channel/i }));
    await waitFor(() => {
        expect(screen.getByText('Create Telegram channel')).toBeInTheDocument();
    });
    return screen.getByRole('dialog');
};

/** Opens the edit dialog for the first listed channel. */
const openEditDialog = async (): Promise<HTMLElement> => {
    const user = userEvent.setup({ delay: null });
    const editButtons = screen.getAllByRole('button', {
        name: /edit channel/i,
    });
    await user.click(editButtons[0]);
    await waitFor(() => {
        expect(
            screen.getByText('Edit channel: Ops Telegram'),
        ).toBeInTheDocument();
    });
    return screen.getByRole('dialog');
};

describe('AdminTelegramChannels', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiGet.mockResolvedValue({
            notification_channels: telegramChannels,
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Listing', () => {
        it('lists Telegram channels with the heading and empty state text', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Telegram channels configured.'),
                ).toBeInTheDocument();
            });
            expect(screen.getByText('Telegram Channels')).toBeInTheDocument();
        });

        it('shows the bot-token indicator and the literal chat ID per row', async () => {
            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            // Column headers come from the field descriptors.
            expect(screen.getByText('Bot Token')).toBeInTheDocument();
            expect(screen.getByText('Chat ID')).toBeInTheDocument();

            const configuredRow = screen.getByText('Ops Telegram').closest('tr');
            expect(configuredRow).not.toBeNull();
            expect(
                within(configuredRow as HTMLElement).getByText('Configured'),
            ).toBeInTheDocument();
            // The chat ID is not a secret, so it is shown verbatim.
            expect(
                within(configuredRow as HTMLElement).getByText('-1001234567890'),
            ).toBeInTheDocument();

            const unconfiguredRow = screen
                .getByText('Public Channel')
                .closest('tr');
            expect(
                within(unconfiguredRow as HTMLElement).getByText('Not configured'),
            ).toBeInTheDocument();
            expect(
                within(unconfiguredRow as HTMLElement).getByText('@pgedge_alerts'),
            ).toBeInTheDocument();
        });

        it('filters out channels of other types', async () => {
            mockApiGet.mockResolvedValue({
                notification_channels: [
                    ...telegramChannels,
                    {
                        id: 3,
                        channel_type: 'slack',
                        name: 'A Slack Channel',
                        description: '',
                        enabled: true,
                        is_estate_default: false,
                        webhook_url_set: true,
                    },
                ],
            });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });
            expect(
                screen.queryByText('A Slack Channel'),
            ).not.toBeInTheDocument();
        });
    });

    describe('Create dialog', () => {
        it('renders both credential fields with their helper text', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Telegram channels configured.'),
                ).toBeInTheDocument();
            });

            const dialog = await openCreateDialog();

            const tokenInput = within(dialog).getByLabelText('Bot Token *');
            const chatInput = within(dialog).getByLabelText('Chat ID *');
            expect(tokenInput).toBeInTheDocument();
            expect(chatInput).toBeInTheDocument();
            // The token is a secret, so it is masked on entry.
            expect(tokenInput).toHaveAttribute('type', 'password');
            expect(chatInput).toHaveAttribute('type', 'text');
            expect(
                within(dialog).getByText(
                    'The token issued by @BotFather when you created the bot.',
                ),
            ).toBeInTheDocument();
            expect(
                within(dialog).getByText(
                    'Numeric chat ID, or @channelusername for a public channel.',
                ),
            ).toBeInTheDocument();
        });

        it('sends both the bot token and the chat ID on create', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 42 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Telegram channels configured.'),
                ).toBeInTheDocument();
            });

            const dialog = await openCreateDialog();
            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'New Telegram' },
            });
            fireEvent.change(within(dialog).getByLabelText('Bot Token *'), {
                target: { value: BOT_TOKEN },
            });
            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '-1009876543210' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Create/i }),
            );

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels',
                    expect.objectContaining({
                        channel_type: 'telegram',
                        name: 'New Telegram',
                        telegram_bot_token: BOT_TOKEN,
                        telegram_chat_id: '-1009876543210',
                    }),
                );
            });
        });

        it('requires the bot token on create', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Telegram channels configured.'),
                ).toBeInTheDocument();
            });

            const dialog = await openCreateDialog();
            const createButton = within(dialog).getByRole('button', {
                name: /Create/i,
            });

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'Missing token' },
            });
            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '-100123' },
            });
            expect(createButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Bot Token *'), {
                target: { value: BOT_TOKEN },
            });
            expect(createButton).not.toBeDisabled();
        });

        it('requires the chat ID on create', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No Telegram channels configured.'),
                ).toBeInTheDocument();
            });

            const dialog = await openCreateDialog();
            const createButton = within(dialog).getByRole('button', {
                name: /Create/i,
            });

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'Missing chat' },
            });
            fireEvent.change(within(dialog).getByLabelText('Bot Token *'), {
                target: { value: BOT_TOKEN },
            });
            expect(createButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '@somewhere' },
            });
            expect(createButton).not.toBeDisabled();
        });
    });

    describe('Edit dialog', () => {
        it('leaves the bot token blank and pre-fills the chat ID', async () => {
            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const dialog = await openEditDialog();

            const tokenInput = within(dialog).getByLabelText('Bot Token');
            expect(tokenInput).toHaveValue('');
            expect(tokenInput).toHaveAttribute('type', 'password');
            expect(within(dialog).getByLabelText('Chat ID *')).toHaveValue(
                '-1001234567890',
            );
            expect(
                within(dialog).getByText(
                    'A Bot Token is configured. Leave this blank to keep it unchanged.',
                ),
            ).toBeInTheDocument();
        });

        it('omits the bot token from the PUT body but sends the chat ID', async () => {
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const dialog = await openEditDialog();
            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '@moved_channel' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [url, body] = mockApiPut.mock.calls[0];
            expect(url).toBe('/api/v1/notification-channels/11');
            // Sending an empty token would clear the stored secret.
            expect(body).not.toHaveProperty('telegram_bot_token');
            expect(body).toHaveProperty('telegram_chat_id', '@moved_channel');
        });

        it('sends a rotated bot token when one is typed', async () => {
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const dialog = await openEditDialog();
            fireEvent.change(within(dialog).getByLabelText('Bot Token'), {
                target: { value: '987654321:AAHrotated' },
            });

            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/11',
                    expect.objectContaining({
                        telegram_bot_token: '987654321:AAHrotated',
                        telegram_chat_id: '-1001234567890',
                    }),
                );
            });
        });

        it('keeps the chat ID mandatory on edit', async () => {
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const dialog = await openEditDialog();
            const saveButton = within(dialog).getByRole('button', {
                name: /Save/i,
            });
            // A configured token may stay blank...
            expect(saveButton).not.toBeDisabled();

            // ...but the chat ID is not a secret, so it must be present.
            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '  ' },
            });
            expect(saveButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Chat ID *'), {
                target: { value: '-100123' },
            });
            expect(saveButton).not.toBeDisabled();

            await user.click(saveButton);
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
        });

        it('requires the bot token on edit when none is configured', async () => {
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Public Channel')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit channel/i,
            });
            // Second row is the channel with no token configured.
            await user.click(editButtons[1]);

            await waitFor(() => {
                expect(
                    screen.getByText('Edit channel: Public Channel'),
                ).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');
            const saveButton = within(dialog).getByRole('button', {
                name: /Save/i,
            });
            expect(saveButton).toBeDisabled();
            expect(
                within(dialog).queryByText(/is configured. Leave this blank/i),
            ).not.toBeInTheDocument();

            fireEvent.change(within(dialog).getByLabelText('Bot Token *'), {
                target: { value: BOT_TOKEN },
            });
            expect(saveButton).not.toBeDisabled();
        });
    });

    describe('Secret handling', () => {
        it('never renders a bot token into the document', async () => {
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            // Nothing in the table exposes a token: the API only
            // reports whether one is set.
            expect(document.body.textContent).not.toContain(BOT_TOKEN);
            expect(document.body.textContent).not.toContain('123456789:');

            const dialog = await openEditDialog();
            expect(within(dialog).getByLabelText('Bot Token')).toHaveValue('');
            expect(document.body.textContent).not.toContain(BOT_TOKEN);

            await user.click(
                within(dialog).getByRole('button', { name: /Cancel/i }),
            );
        });
    });

    describe('Test notification', () => {
        it('sends a test notification for the selected channel', async () => {
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/11/test',
                );
            });
            await waitFor(() => {
                expect(
                    screen.getByText(
                        /Test notification sent successfully for "Ops Telegram"/i,
                    ),
                ).toBeInTheDocument();
            });
        });

        it('surfaces a failed test notification', async () => {
            mockApiPost.mockRejectedValue(new Error('Bot token rejected'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', {
                name: /send test notification/i,
            });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Bot token rejected'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Delete channel', () => {
        it('deletes a Telegram channel', async () => {
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminTelegramChannels />);

            await waitFor(() => {
                expect(screen.getByText('Ops Telegram')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete channel/i,
            });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('Delete Telegram Channel'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/11',
                );
            });
        });
    });
});
