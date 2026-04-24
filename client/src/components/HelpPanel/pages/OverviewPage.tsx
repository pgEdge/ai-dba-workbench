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
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

export interface OverviewPageProps {
    aiEnabled: boolean;
}

/**
 * Overview Page - Introduction to the workbench
 */
const OverviewPage: React.FC<OverviewPageProps> = ({ aiEnabled }) => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Welcome to AI DBA Workbench
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The AI DBA Workbench provides AI-powered tools for PostgreSQL database
            administration, monitoring, and optimization. This interface allows you to
            manage database servers organized in clusters and groups, monitor their
            health, and respond to alerts.
        </Typography>

        <SectionTitle>Key Features</SectionTitle>
        <FeatureItem
            title="Cluster Organization"
            description="Organize your database servers into logical clusters and groups for easier management."
        />
        <FeatureItem
            title="Real-time Monitoring"
            description="Monitor server status, connection health, and performance metrics in real-time."
        />
        <FeatureItem
            title="Alert Management"
            description="View and manage alerts for threshold violations and system issues. Acknowledge alerts with notes and track false positives."
        />
        <FeatureItem
            title="Replication Support"
            description="Support for binary replication (primary/standby), Spock multi-master replication, and logical replication."
        />
        {aiEnabled ? (
            <FeatureItem
                title="AI-Powered Overview"
                description="The status panel includes an AI-generated summary that provides context-aware insights for your current selection."
            />
        ) : (
            <FeatureItem
                title="AI Features (Disabled)"
                description="AI-powered features such as the AI Overview, Ask Ellie chat assistant, and AI analysis are currently disabled. To enable these features, configure an LLM provider with valid API credentials in the server configuration file."
            />
        )}

        <SectionTitle>Getting Started</SectionTitle>
        <Typography sx={styles.bodyText}>
            Use the <strong>Cluster Navigator</strong> on the left to browse your
            server hierarchy. Click on a server to view its details and alerts in
            the main panel. You can also view aggregated information for clusters
            or the entire estate.
        </Typography>

        <HelpTip>
            Click the estate header at the top of the navigator to see a summary of
            all servers across all clusters.
        </HelpTip>
    </Box>
);

export default OverviewPage;
