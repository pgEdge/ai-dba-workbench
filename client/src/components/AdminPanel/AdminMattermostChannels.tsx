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
    webhookUrlLabel: 'Webhook URL',
};

const AdminMattermostChannels: React.FC = () => (
    <AdminMessagingChannels config={MATTERMOST_CONFIG} />
);

export default AdminMattermostChannels;
