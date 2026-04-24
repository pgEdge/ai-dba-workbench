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
    Folder as GroupIcon,
    Dns as ClusterIcon,
    Storage as ServerIcon,
    Search as SearchIcon,
    DragIndicator as DragIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Navigator Page - Cluster Navigator help
 */
const NavigatorPage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Cluster Navigator
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The Cluster Navigator provides a hierarchical view of your database
            infrastructure, organized by groups, clusters, and individual servers.
        </Typography>

        <SectionTitle icon={GroupIcon}>Groups</SectionTitle>
        <Typography sx={styles.bodyText}>
            Groups are top-level containers for organizing related clusters. They
            might represent different environments (Production, Staging), regions,
            or business units.
        </Typography>

        <SectionTitle icon={ClusterIcon}>Clusters</SectionTitle>
        <Typography sx={styles.bodyText}>
            Clusters contain one or more database servers that work together. A
            cluster typically represents a replication set with a primary server
            and one or more standbys, or a Spock multi-master replication group.
        </Typography>

        <SectionTitle icon={ServerIcon}>Servers</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Individual PostgreSQL server connections. Each server displays its role
            and current status:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Roles"
                description="Primary, Standby, Cascading standby, Spock node, Standalone, Publisher, or Subscriber."
            />
            <FeatureItem
                title="Status Indicators"
                description="Green checkmark for healthy servers, yellow warning with alert count for servers with active alerts, red error for offline servers."
            />
        </Box>

        <SectionTitle icon={SearchIcon}>Search</SectionTitle>
        <Typography sx={styles.bodyText}>
            Use the search bar at the top of the navigator to quickly filter
            servers by name. The search filters in real-time as you type.
        </Typography>

        <SectionTitle icon={DragIcon}>Drag and Drop</SectionTitle>
        <Typography sx={styles.bodyText}>
            Reorganize your server hierarchy by dragging servers between clusters
            or clusters between groups. Drag a server onto a cluster to move it,
            or drag a cluster onto a group to reassign it.
        </Typography>

        <HelpTip>
            Click on a cluster name to view aggregated status and alerts for all
            servers in that cluster.
        </HelpTip>
    </Box>
);

export default NavigatorPage;
