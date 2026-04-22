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
    Storage as ServerIcon,
    Dns as ClusterIcon,
    AutoAwesome as AutoAwesomeIcon,
    Psychology as AIIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

export interface StatusPanelPageProps {
    aiEnabled: boolean;
}

/**
 * Status Panel Page - Status panel and server details help
 */
const StatusPanelPage: React.FC<StatusPanelPageProps> = ({ aiEnabled }) => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Status Panel
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The Status Panel displays detailed information about your current
            selection, whether it's a single server, a cluster, or the entire
            estate.
        </Typography>

        {aiEnabled && (
            <>
                <SectionTitle icon={AutoAwesomeIcon}>AI Overview</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    The AI Overview panel appears at the top of the status panel. It
                    provides an AI-generated summary of your current selection.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="Context-Aware Summaries"
                        description="The summary adapts to your selection. It covers the full estate, a specific cluster, or an individual server."
                    />
                    <FeatureItem
                        title="Auto-Refresh"
                        description="The summary refreshes automatically every 30 seconds and displays how recently it was updated."
                    />
                    <FeatureItem
                        title="Manual Refresh"
                        description="A refresh button next to the 'Updated N min ago' timestamp forces immediate regeneration of the overview."
                    />
                    <FeatureItem
                        title="Stale Indicator"
                        description="A stale indicator appears if the summary has not refreshed in over five minutes."
                    />
                    <FeatureItem
                        title="Collapse and Expand"
                        description="Click the toggle button to collapse or expand the panel. The collapse state persists across sessions."
                    />
                    <FeatureItem
                        title="Generation Status"
                        description="While generating a new summary, the panel displays a 'Generating estate overview...' message."
                    />
                </Box>

                <SectionTitle icon={AIIcon}>Server &amp; Cluster Analysis</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    The AI Overview panel displays a brain icon for servers and clusters
                    that opens a full AI-powered analysis dialog.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="Agentic Analysis"
                        description="The AI examines server metrics, alert history, configuration, and schema using an agentic tool loop to generate a comprehensive health report."
                    />
                    <FeatureItem
                        title="Cluster Analysis"
                        description="For clusters, the AI analyzes all member servers and compares metrics across the cluster to identify replication issues and performance disparities."
                    />
                    <FeatureItem
                        title="Progress Indicators"
                        description="The analysis dialog shows real-time progress as the AI gathers data using monitoring tools such as querying metrics, fetching baselines, and reviewing alert history."
                    />
                    <FeatureItem
                        title="Query Validation"
                        description="The AI validates all generated SQL queries before presenting them. Invalid queries are automatically corrected, ensuring that suggested SQL is syntactically correct."
                    />
                    <FeatureItem
                        title="Code Block Actions"
                        description="Code blocks in analysis reports include a copy-to-clipboard button. SQL code blocks also include a Run button to execute queries against the server. Results appear inline below the code block."
                    />
                    <FeatureItem
                        title="Cached Reports"
                        description="An amber brain icon in the AI Overview panel indicates a cached analysis is available. Click it to view the report instantly."
                    />
                    <FeatureItem
                        title="Download"
                        description="Reports can be downloaded as markdown files for sharing or archiving."
                    />
                </Box>
            </>
        )}

        <SectionTitle icon={ServerIcon}>Server View</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            When viewing a single server, you'll see:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Connection Details"
                description="Host, port, database name, and username for the connection."
            />
            <FeatureItem
                title="Server Information"
                description="PostgreSQL version, operating system, and server role."
            />
            <FeatureItem
                title="Server Information Dialog"
                description="Click the info button on the server properties bar to open a detailed dialog showing system hardware, PostgreSQL configuration, database listings with AI-generated descriptions, and key configuration settings grouped by category."
            />
            <FeatureItem
                title="Replication Status"
                description="For Spock servers, displays the Spock version and node name."
            />
            <FeatureItem
                title="Active Alerts"
                description="Any alerts currently active for this server."
            />
        </Box>

        <SectionTitle icon={ClusterIcon}>Cluster View</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            When viewing a cluster, you'll see metric cards summarizing the health
            of all servers in the cluster:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="OK Count"
                description="Number of servers with no active alerts."
            />
            <FeatureItem
                title="Warning Count"
                description="Number of servers with active alerts."
            />
            <FeatureItem
                title="Offline Count"
                description="Number of servers that are unreachable."
            />
        </Box>

        <SectionTitle>Estate View</SectionTitle>
        <Typography sx={styles.bodyText}>
            The estate view shows a summary of all servers across all clusters
            and groups, including total counts for clusters and groups.
        </Typography>

        <HelpTip>
            The status indicator in the header shows the overall health of your
            selection at a glance.
        </HelpTip>
    </Box>
);

export default StatusPanelPage;
