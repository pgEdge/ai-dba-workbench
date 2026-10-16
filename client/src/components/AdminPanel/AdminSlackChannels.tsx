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

const SLACK_CONFIG: MessagingChannelConfig = {
    channelType: 'slack',
    platformName: 'Slack',
    fields: [
        {
            key: 'webhook_url',
            label: 'Webhook URL',
            setFlag: 'webhook_url_set',
            secret: true,
            required: true,
        },
    ],
};

const AdminSlackChannels: React.FC = () => (
    <AdminMessagingChannels config={SLACK_CONFIG} />
);

export default AdminSlackChannels;
