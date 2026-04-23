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
import { Box, Typography } from '@mui/material';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Folder as GroupIcon,
    Star as PrimaryIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Server Management Page - Adding/editing servers help
 */
const ServerManagementPage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Server Management
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The AI DBA Workbench allows you to add, edit, and organize database
            server connections within your cluster hierarchy.
        </Typography>

        <SectionTitle icon={AddIcon}>Adding Servers</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the + button in the navigator header or right-click on a
            cluster to add a new server. You'll need to provide:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Connection Name"
                description="A friendly name to identify this server in the navigator."
            />
            <FeatureItem
                title="Host & Port"
                description="The hostname or IP address and port number of the PostgreSQL server."
            />
            <FeatureItem
                title="Database & User"
                description="The database name and username for the connection."
            />
            <FeatureItem
                title="Password"
                description="The password for authentication. Stored securely in the datastore."
            />
            <FeatureItem
                title="Cluster Assignment"
                description="Select which cluster this server belongs to."
            />
        </Box>

        <SectionTitle icon={EditIcon}>Editing Servers</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the gear icon that appears when hovering over a server name
            in the navigator to edit its configuration. The edit dialog provides
            four tabs:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Connection"
                description="Modify host, port, database, credentials, SSL settings, and monitoring options."
            />
            <FeatureItem
                title="Alert Overrides"
                description="Customize alert thresholds for this specific server."
            />
            <FeatureItem
                title="Probe Configuration"
                description="Customize data collection settings for this specific server."
            />
            <FeatureItem
                title="Notification Channels"
                description="Enable or disable notification channels for this specific server."
            />
        </Box>

        <SectionTitle icon={DeleteIcon}>Deleting Servers</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click the trash icon to remove a server connection. You'll be asked
            to confirm before the server is deleted. This removes the connection
            from the workbench but does not affect the actual database server.
        </Typography>

        <SectionTitle icon={GroupIcon}>Managing Groups</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Groups can be created from the + menu in the navigator header. Edit
            a group by clicking the gear icon on the group header. The group edit
            dialog provides four tabs:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Details"
                description="Modify the group name, description, and sharing options."
            />
            <FeatureItem
                title="Alert Overrides"
                description="Set default alert thresholds for all servers in this group."
            />
            <FeatureItem
                title="Probe Configuration"
                description="Set default probe settings for all servers in this group."
            />
            <FeatureItem
                title="Notification Channels"
                description="Enable or disable notification channels for all servers in this group."
            />
        </Box>

        <SectionTitle icon={PrimaryIcon}>Server Roles</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Server roles are automatically detected based on the PostgreSQL
            configuration:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Primary"
                description="The main read-write server in a binary replication setup."
            />
            <FeatureItem
                title="Standby"
                description="A read-only replica receiving changes from a primary."
            />
            <FeatureItem
                title="Cascading"
                description="A standby that also replicates to other standbys."
            />
            <FeatureItem
                title="Spock Node"
                description="A node in a Spock multi-master replication cluster."
            />
            <FeatureItem
                title="Standalone"
                description="A server not participating in replication."
            />
            <FeatureItem
                title="Publisher/Subscriber"
                description="Servers using logical replication."
            />
        </Box>

        <HelpTip>
            Drag servers between clusters to reorganize your infrastructure
            without editing each server individually.
        </HelpTip>
    </Box>
);

export default ServerManagementPage;
