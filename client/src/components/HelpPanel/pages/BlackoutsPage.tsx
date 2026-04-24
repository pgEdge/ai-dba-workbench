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
    PauseCircle as BlackoutIcon,
    Schedule as ScheduleIcon,
    Timer as TimerIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Blackouts Page - Blackout management help
 */
const BlackoutsPage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Blackout Management
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Blackouts suppress alert notifications during planned maintenance
            windows. Create blackouts to prevent false alerts while performing
            database upgrades, schema changes, or infrastructure work.
        </Typography>

        <SectionTitle icon={BlackoutIcon}>Scopes</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Blackouts operate at four hierarchical levels. A blackout at any
            level suppresses alerts for everything beneath it:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Estate"
                description="Suppresses alerts for all servers across all groups and clusters. Use for organization-wide maintenance."
            />
            <FeatureItem
                title="Group"
                description="Suppresses alerts for all clusters and servers within a specific group."
            />
            <FeatureItem
                title="Cluster"
                description="Suppresses alerts for all servers within a specific cluster."
            />
            <FeatureItem
                title="Server"
                description="Suppresses alerts for only the specified individual server."
            />
        </Box>

        <SectionTitle icon={TimerIcon}>One-Time Blackouts</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Create a one-time blackout for a specific maintenance window. Two
            timing modes are available:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Start Now"
                description="Begin the blackout immediately with a preset duration (30 minutes, 1 hour, 2 hours, 4 hours, 8 hours) or a custom duration."
            />
            <FeatureItem
                title="Schedule Future"
                description="Pick specific start and end times for a planned maintenance window."
            />
            <FeatureItem
                title="Stop Early"
                description="Active blackouts can be stopped before their scheduled end time using the Stop button. Normal alert evaluation resumes immediately."
            />
        </Box>

        <SectionTitle icon={ScheduleIcon}>Recurring Schedules</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Set up recurring blackout schedules for regular maintenance windows.
            The system automatically creates blackouts when a schedule's cron
            expression matches:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Cron Presets"
                description="Choose from common patterns: Daily, Weekdays, Weekends, Weekly, or Monthly. Select the time of day for the maintenance window."
            />
            <FeatureItem
                title="Custom Cron"
                description="Enter a standard five-field cron expression for advanced scheduling (minute, hour, day of month, month, day of week)."
            />
            <FeatureItem
                title="Duration"
                description="Set how long each blackout lasts. Choose from presets (30 minutes, 1 hour, 2 hours, 4 hours) or enter a custom duration."
            />
            <FeatureItem
                title="Timezone"
                description="Specify the timezone for cron evaluation. Defaults to UTC."
            />
        </Box>

        <SectionTitle>Navigator Indicators</SectionTitle>
        <Typography sx={styles.bodyText}>
            The Cluster Navigator displays an amber pause icon on servers,
            clusters, and groups that have an active blackout. The icon appears
            at full opacity for direct blackouts and at reduced opacity for
            inherited blackouts.
        </Typography>

        <SectionTitle>Alert Suppression</SectionTitle>
        <Typography sx={styles.bodyText}>
            When a blackout is active, the alerter checks the scope hierarchy
            before firing any alert. If an active blackout exists at the server,
            cluster, group, or estate level, the alert is suppressed and no
            notifications are sent. Normal evaluation resumes when the blackout
            ends.
        </Typography>

        <HelpTip>
            Access blackout management from the pause icon in the header toolbar
            or from the blackout panel that appears in the Status Panel when
            blackouts are active.
        </HelpTip>
    </Box>
);

export default BlackoutsPage;
