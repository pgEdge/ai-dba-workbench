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
    Dashboard as StatusIcon,
    Storage as ServerIcon,
    AccountTree as TopologyIcon,
    Timeline as TimelineIcon,
    Psychology as AIIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

export interface MonitoringPageProps {
    aiEnabled: boolean;
}

/**
 * Monitoring Dashboards Page - Dashboard hierarchy and features help
 */
const MonitoringPage: React.FC<MonitoringPageProps> = ({ aiEnabled }) => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Monitoring Dashboards
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The monitoring dashboards provide a hierarchical drill-down view
            of your database infrastructure. Navigate from a high-level estate
            overview down to individual database objects, with time-series
            charts and KPI tiles at every level.
        </Typography>

        <SectionTitle icon={StatusIcon}>Estate Dashboard</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            The top-level view showing the health of your entire estate:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Server Status"
                description="Donut charts showing the distribution of servers by health status across all clusters."
            />
            <FeatureItem
                title="KPI Tiles"
                description="Key metrics with sparklines showing trends over the selected time range."
            />
            <FeatureItem
                title="Cluster Cards"
                description="Summary cards for each cluster with status indicators. Click a card to drill down to the cluster dashboard."
            />
            <FeatureItem
                title="Hot Spots"
                description="Servers with concerning metrics such as high CPU, low disk space, or replication lag are highlighted for quick attention."
            />
        </Box>

        <SectionTitle icon={TopologyIcon}>Cluster Dashboard</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Detailed view of a single cluster and its members:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Topology Diagram"
                description="Interactive diagram showing server nodes and replication connections. Edge colors indicate replication type: physical/streaming, Spock bidirectional, or logical. Click a node to navigate to that server's dashboard."
            />
            <FeatureItem
                title="Replication Lag"
                description="KPI tiles and a time-series chart showing replication lag across cluster members."
            />
            <FeatureItem
                title="Comparative Metrics"
                description="Side-by-side charts comparing CPU, memory, connections, and other metrics across all servers in the cluster."
            />
        </Box>

        <SectionTitle icon={ServerIcon}>Server Dashboard</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Comprehensive metrics for an individual server:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="System Resources"
                description="CPU utilization, memory usage, disk space, load average, and network I/O with time-series charts."
            />
            <FeatureItem
                title="PostgreSQL Overview"
                description="Active connections, transactions per second, cache hit ratio, and temporary file usage."
            />
            <FeatureItem
                title="WAL & Replication"
                description="WAL generation rate and replication metrics for servers participating in replication."
            />
            <FeatureItem
                title="Database Summaries"
                description="Cards for each database on the server. Click a card to drill down to the database dashboard."
            />
            <FeatureItem
                title="Top Queries"
                description="Leaderboards showing the most resource-intensive queries by total time, calls, mean time, or rows returned; the Database column shows the source database for each query. The Hide monitoring queries toggle filters out workbench monitoring queries by default."
            />
        </Box>

        <SectionTitle icon={ServerIcon}>Database Dashboard</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Performance and health metrics for a single database:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Performance KPIs"
                description="Database size, cache hit ratio, total transactions, and dead tuple ratio with sparklines and AI analysis."
            />
            <FeatureItem
                title="Table & Index Leaderboards"
                description="Tables ranked by rows, sequential scans, dead tuples, or modifications; indexes ranked by reads, scans, or unused. Click an entry to open the object dashboard. AI analysis covers all metrics."
            />
            <FeatureItem
                title="Vacuum Status"
                description="Tables sorted by dead tuple ratio with color-coded timestamps indicating vacuum freshness. AI analysis provides vacuum recommendations."
            />
        </Box>

        <SectionTitle>Object Dashboard</SectionTitle>
        <Typography sx={styles.bodyText}>
            The deepest level shows detailed metrics for individual tables,
            indexes, or queries. Access an object dashboard by clicking an
            item in any leaderboard or top queries list.
        </Typography>

        <SectionTitle>Query Plan</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            The query detail view includes a Query Plan section that
            displays PostgreSQL EXPLAIN output in text and visual formats.
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Visual Tab"
                description="Renders a graphical flow diagram of the query plan. Leaf scan nodes appear on the left and the root node on the right, connected by bezier arrows."
            />
            <FeatureItem
                title="Text Tab"
                description="Displays the standard EXPLAIN output in a monospace format. The text plan provides a concise overview of the execution strategy."
            />
            <FeatureItem
                title="Cost Coloring"
                description="Each tile has a colored left border indicating its cost relative to the total plan cost. Red indicates nodes consuming over 80% of the total cost; orange indicates over 50%."
            />
            <FeatureItem
                title="Node Details"
                description="Click any tile in the visual diagram to open a popover with comprehensive details including cost range, estimated rows, width, output columns, conditions, and worker information."
            />
            <FeatureItem
                title="Parameterized Queries"
                description="Queries using parameter placeholders ($1, $2) use the GENERIC_PLAN option on PostgreSQL 16 and later. Older versions display an informational message."
            />
        </Box>

        <SectionTitle icon={TimelineIcon}>Key Features</SectionTitle>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Time Range Selector"
                description="Choose from 1 hour, 6 hours, 24 hours, 7 days, or 30 days. The selected range applies to all charts in the monitoring section."
            />
            <FeatureItem
                title="Drill-Down Navigation"
                description="Navigate the hierarchy from estate to cluster to server to database to object. Breadcrumbs at the top show your current position and allow quick navigation back up."
            />
            <FeatureItem
                title="Auto-Refresh"
                description="Charts and metrics refresh automatically to keep data current."
            />
            <FeatureItem
                title="Event Timeline"
                description="A timeline below the charts shows configuration changes, alerts, server restarts, and other notable events in the selected time range."
            />
        </Box>

        {aiEnabled && (
            <>
                <SectionTitle icon={AIIcon}>AI Query Analysis</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    The query detail view includes AI-powered analysis of
                    individual query performance from pg_stat_statements.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="AI Overview"
                        description="The query detail view displays an AI Overview panel below the query text. The panel provides a brief summary of the query's performance characteristics and health status."
                    />
                    <FeatureItem
                        title="Full Analysis"
                        description="Click the brain icon in the AI Overview panel to open a full-screen analysis dialog. The analysis uses an agentic AI loop that gathers schema information, metric baselines, and query plans before generating its report."
                    />
                    <FeatureItem
                        title="Available Tools"
                        description="The AI uses tools to query metrics, fetch metric baselines, query the database, inspect schema, validate SQL queries, and search the knowledgebase during analysis."
                    />
                    <FeatureItem
                        title="Cached Reports"
                        description="Analysis results are cached for 30 minutes. The AI Overview panel shows when the summary was last generated and includes a refresh button to regenerate it."
                    />
                    <FeatureItem
                        title="Code Block Actions"
                        description="SQL code blocks in analysis reports include a Run button to execute queries against the monitored database. Write statements require confirmation before execution."
                    />
                </Box>
            </>
        )}

        {aiEnabled && (
            <>
                <SectionTitle icon={AIIcon}>AI Chart Analysis</SectionTitle>
                <Typography sx={styles.bodyTextMb2}>
                    Every chart and KPI tile displays a brain icon that triggers
                    AI-powered analysis of the displayed data.
                </Typography>
                <Box sx={styles.indentedBlock}>
                    <FeatureItem
                        title="Data Analysis"
                        description="The AI examines the chart data, identifies trends and anomalies, and generates a report with summary, patterns, and recommendations."
                    />
                    <FeatureItem
                        title="Timeline Correlation"
                        description="The analysis includes timeline events such as configuration changes, alerts, and server restarts to identify correlations with metric changes."
                    />
                    <FeatureItem
                        title="Cached Reports"
                        description="An amber brain icon indicates a cached analysis is available. Click it to view the report instantly without waiting for regeneration."
                    />
                    <FeatureItem
                        title="Code Block Actions"
                        description="Code blocks in analysis reports include a copy-to-clipboard button. SQL code blocks also include a Run button to execute queries against the monitored server. Results appear inline below the code block."
                    />
                    <FeatureItem
                        title="Download"
                        description="Reports can be downloaded as markdown files for sharing or archiving."
                    />
                </Box>
            </>
        )}

        <HelpTip>
            Select a server, cluster, or the estate header in the Cluster
            Navigator to view the corresponding monitoring dashboard in the
            Status Panel.
        </HelpTip>
    </Box>
);

export default MonitoringPage;
