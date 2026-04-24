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
import { Box, Typography, Chip } from '@mui/material';
import {
    Warning as WarningIcon,
    CheckCircleOutline as AckIcon,
    Edit as EditIcon,
    Psychology as AIIcon,
    PlayArrow as RunIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles, getSeverityChipSx } from '../helpPanelStyles';

export interface AlertsPageProps {
    aiEnabled: boolean;
}

/**
 * Alerts Page - Alert management help
 */
const AlertsPage: React.FC<AlertsPageProps> = ({ aiEnabled }) => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Alert Management
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Alerts notify you when database metrics exceed configured thresholds
            or when system issues are detected. You can view, acknowledge, and
            manage alerts from the Status Panel.
        </Typography>

        <SectionTitle icon={WarningIcon}>Alert Types</SectionTitle>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Connection Alerts"
                description="Triggered when connection counts exceed limits, including max connections, active connections, or idle connections."
            />
            <FeatureItem
                title="Performance Alerts"
                description="Triggered by high CPU usage, memory usage, or low disk space."
            />
            <FeatureItem
                title="Database Alerts"
                description="Triggered by deadlocks, high rollback rates, low cache hit ratios, or replication lag."
            />
            <FeatureItem
                title="Query Alerts"
                description="Triggered by long-running queries or blocked queries."
            />
        </Box>

        <SectionTitle>Alert Severity</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Alerts are classified by severity:
        </Typography>
        <Box sx={styles.severityChipsRow}>
            <Chip
                label="Critical"
                size="small"
                sx={getSeverityChipSx('error')}
            />
            <Chip
                label="Warning"
                size="small"
                sx={getSeverityChipSx('warning')}
            />
            <Chip
                label="Info"
                size="small"
                sx={getSeverityChipSx('info')}
            />
        </Box>

        <SectionTitle icon={AckIcon}>Acknowledging Alerts</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the checkmark icon on an alert to acknowledge it. You can:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Add a Reason"
                description="Explain why the alert is being acknowledged (e.g., 'Investigating', 'Known issue', 'Scheduled maintenance')."
            />
            <FeatureItem
                title="Mark as False Positive"
                description="Flag alerts that were triggered incorrectly to help improve alert accuracy over time."
            />
        </Box>
        <Typography sx={styles.bodyText}>
            Acknowledged alerts are moved to a separate collapsed section below
            active alerts. Use the undo icon on an acknowledged alert to restore
            it to active status.
        </Typography>

        <SectionTitle>Threshold Information</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            For threshold-based alerts, you will see the current value and the
            threshold that was exceeded (e.g., "108 exceeds threshold of 100").
            Alert thresholds follow a hierarchical override system:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Server Override"
                description="Thresholds set on a specific server take highest priority."
            />
            <FeatureItem
                title="Cluster Override"
                description="Thresholds set on a cluster apply to all servers in that cluster."
            />
            <FeatureItem
                title="Group Override"
                description="Thresholds set on a group apply to all clusters in that group."
            />
            <FeatureItem
                title="Global Default"
                description="The default threshold applies when no override exists at any level."
            />
        </Box>

        <SectionTitle icon={EditIcon}>Editing Overrides</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the edit (pencil) icon on an alert to open the override
            editor. The dialog allows you to create or update a threshold
            override for that alert rule.
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Scope Selection"
                description="Choose the scope for the override: Server, Cluster, or Group. Scopes above an existing override are disabled since editing them would have no effect."
            />
            <FeatureItem
                title="Override Fields"
                description="Configure the enabled state, comparison operator, threshold value, and severity level for the override."
            />
            <FeatureItem
                title="Saving"
                description="Saving creates a new override or updates an existing one at the selected scope level."
            />
        </Box>

        {aiEnabled && (
            <>
                <SectionTitle icon={AIIcon}>AI Alert Analysis</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    Each alert has an &quot;Analyze with AI&quot; button (brain icon)
                    that triggers AI-powered analysis of the alert.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="Automated Analysis"
                        description="The AI examines historical patterns, baselines, and alert context to generate a detailed report."
                    />
                    <FeatureItem
                        title="Report Contents"
                        description="Reports include a Summary, Analysis, Remediation Steps, and Threshold Tuning recommendations."
                    />
                    <FeatureItem
                        title="Cached Reports"
                        description="A green brain icon indicates a cached analysis is available. Click it to view the report instantly without waiting for regeneration."
                    />
                    <FeatureItem
                        title="Download"
                        description="Reports can be downloaded as markdown files for sharing or archiving."
                    />
                </Box>

                <SectionTitle icon={RunIcon}>Code Block Actions</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    Code blocks in analysis reports include action buttons in the
                    top-right corner. All code blocks have a copy-to-clipboard
                    button, and SQL code blocks also include a Run button (play
                    icon) to execute queries.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="Execute Queries"
                        description="Click the Run button to execute the SQL query against the monitored database server. Results appear inline below the code block in a table format."
                    />
                    <FeatureItem
                        title="Read-Only Queries"
                        description="Read-only queries such as SELECT and SHOW execute immediately when the Run button is clicked."
                    />
                    <FeatureItem
                        title="Write Statements"
                        description="Write statements such as ALTER SYSTEM show a confirmation dialog before executing, to prevent accidental changes."
                    />
                    <FeatureItem
                        title="SQL Detection"
                        description="The Run button only appears on code blocks identified as SQL. Configuration snippets and shell commands do not show the Run button."
                    />
                </Box>
            </>
        )}

        <HelpTip>
            Acknowledged alerts remain visible but are separated from active
            alerts. The alert count in the navigator only includes active
            (non-acknowledged) alerts.
        </HelpTip>
    </Box>
);

export default AlertsPage;
