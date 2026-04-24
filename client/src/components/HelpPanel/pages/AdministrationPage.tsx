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
import { Box, Typography } from '@mui/material';
import {
    Settings as SettingsIcon,
    NotificationsActive as AlertsIcon,
    Notifications as NotificationsIcon,
    Psychology as AIIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Administration Page - Admin panel help
 */
const AdministrationPage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Administration
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The Administration panel provides tools for managing security,
            monitoring configuration, notification channels, and AI assistant
            settings. Access the
            admin panel from the settings icon in the header. Available
            sections depend on your assigned permissions.
        </Typography>

        <SectionTitle icon={SettingsIcon}>Security</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Manage users, groups, and access control for the workbench:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Users"
                description="Create and manage user accounts, set passwords, and assign superuser status."
            />
            <FeatureItem
                title="Groups"
                description="Organize users into groups for role-based access control."
            />
            <FeatureItem
                title="Permissions"
                description="Grant admin permissions, MCP privileges, and connection access to groups."
            />
            <FeatureItem
                title="Tokens"
                description="Manage API tokens with scoped access for service accounts and integrations."
            />
        </Box>

        <SectionTitle icon={AlertsIcon}>Monitoring</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Configure default settings for data collection and alerting:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Probe Defaults"
                description="Configure data collection frequency, retention periods, and enabled state for each monitoring probe."
            />
            <FeatureItem
                title="Alert Defaults"
                description="Set default thresholds, operators, severity levels, and enabled state for each alert rule."
            />
            <FeatureItem
                title="Hierarchical Overrides"
                description="Customize alert thresholds, probe settings, and notification channels at group, cluster, or server level. Settings at lower levels override higher levels (Server > Cluster > Group > Defaults)."
            />
        </Box>

        <SectionTitle icon={NotificationsIcon}>Notification Channels</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Configure how alert notifications are delivered. Four channel
            types are available:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Email Channels"
                description="Send alerts via SMTP to configured recipients. Supports TLS, authentication, and per-channel recipient management."
            />
            <FeatureItem
                title="Slack Channels"
                description="Send alerts to Slack channels via incoming webhook URLs."
            />
            <FeatureItem
                title="Mattermost Channels"
                description="Send alerts to Mattermost channels via incoming webhook URLs."
            />
            <FeatureItem
                title="Webhook Channels"
                description="Send alerts to arbitrary HTTP endpoints with configurable methods, headers, authentication, and JSON payload templates."
            />
            <FeatureItem
                title="Estate Defaults"
                description="Channels marked as estate defaults are active for all servers. Override the default at group, cluster, or server level to enable or disable specific channels."
            />
        </Box>

        <SectionTitle icon={AIIcon}>AI</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Manage AI assistant features. This section appears when an
            LLM provider is configured.
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Memories"
                description="View and manage persistent memories that Ellie uses across conversations. Memories store facts, preferences, instructions, and context. Toggle the pinned switch to control whether a memory is automatically included in every conversation. Delete memories that are no longer needed."
            />
            <FeatureItem
                title="Scope"
                description="User-scoped memories are private to the user who created them. System-scoped memories are visible to all users. Deleting system-scoped memories requires the Store System Memories admin permission."
            />
            <FeatureItem
                title="Pinned Memories"
                description="Pinned memories are automatically appended to every conversation with Ellie. Use pinned memories for critical preferences and instructions that should always inform responses."
            />
        </Box>

        <SectionTitle>Webhook Templates</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Webhook channels support customizable JSON payload templates
            using Go template syntax. Three templates can be configured for
            each channel: Alert Fire (when an alert triggers), Alert Clear
            (when an alert resolves), and Reminder (for periodic reminders
            of active alerts). Leave templates blank to use sensible defaults.
        </Typography>
        <Typography sx={styles.bodyText}>
            Templates have access to alert context variables including
            AlertTitle, AlertDescription, Severity, ServerName, ServerHost,
            DatabaseName, MetricName, MetricValue, ThresholdValue, and more.
            Use conditional blocks like {'{{if .MetricName}}...{{end}}'} for
            optional fields.
        </Typography>

        <SectionTitle icon={SettingsIcon}>Configuration Overrides</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Alert thresholds, probe settings, and notification channels
            can be customized at each level of the server hierarchy.
            Override settings are managed through tabs in the server,
            cluster, and group edit dialogs:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Alert Overrides"
                description="Customize alert rule thresholds, operators, severity levels, and enabled state for a specific group, cluster, or server."
            />
            <FeatureItem
                title="Probe Overrides"
                description="Customize data collection frequency, retention period, and enabled state for probes at a specific group, cluster, or server."
            />
            <FeatureItem
                title="Channel Overrides"
                description="Enable or disable notification channels for a specific group, cluster, or server. Channels inherit their estate default unless overridden."
            />
        </Box>
        <Typography sx={styles.bodyTextMb2}>
            The override precedence from highest to lowest priority is:
            Server, Cluster, Group, then global defaults. Items without
            an override at a given level appear dimmed to show they
            inherit from a higher level.
        </Typography>

        <HelpTip>
            Use the test button on any channel to send a test notification
            and verify your configuration.
        </HelpTip>
    </Box>
);

export default AdministrationPage;
