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

const MATTERMOST_CONFIG: MessagingChannelConfig = {
    channelType: 'mattermost',
    platformName: 'Mattermost',
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

const AdminMattermostChannels: React.FC = () => (
    <AdminMessagingChannels config={MATTERMOST_CONFIG} />
);

export default AdminMattermostChannels;
