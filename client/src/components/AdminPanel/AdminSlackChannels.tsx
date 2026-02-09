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
import AdminMessagingChannels, { MessagingChannelConfig } from './AdminMessagingChannels';

const SLACK_CONFIG: MessagingChannelConfig = {
    channelType: 'slack',
    platformName: 'Slack',
    webhookUrlLabel: 'Webhook URL',
};

const AdminSlackChannels: React.FC = () => (
    <AdminMessagingChannels config={SLACK_CONFIG} />
);

export default AdminSlackChannels;
