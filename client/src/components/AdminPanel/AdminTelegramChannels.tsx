/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import AdminMessagingChannels, { type MessagingChannelConfig } from './AdminMessagingChannels';

/**
 * Telegram needs two values rather than one webhook URL: the Bot API
 * identifies the bot by a secret token and the destination by a
 * separate, non-secret chat ID.
 */
const TELEGRAM_CONFIG: MessagingChannelConfig = {
    channelType: 'telegram',
    platformName: 'Telegram',
    fields: [
        {
            key: 'telegram_bot_token',
            label: 'Bot Token',
            setFlag: 'telegram_bot_token_set',
            secret: true,
            required: true,
            helperText: 'The token issued by @BotFather when you created the bot.',
            showInTable: true,
        },
        {
            key: 'telegram_chat_id',
            label: 'Chat ID',
            valueKey: 'telegram_chat_id',
            required: true,
            helperText: 'Numeric chat ID, or @channelusername for a public channel.',
            placeholder: '-1001234567890',
            showInTable: true,
        },
    ],
};

const AdminTelegramChannels: React.FC = () => (
    <AdminMessagingChannels config={TELEGRAM_CONFIG} />
);

export default AdminTelegramChannels;
