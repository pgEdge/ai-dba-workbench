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
    SmartToyOutlined as ChatBotIcon,
    History as HistoryIcon,
    AutoAwesome as AutoAwesomeIcon,
} from '@mui/icons-material';
import { SectionTitle, HelpTip, FeatureItem } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Ask Ellie Page - AI chat assistant help
 */
const AskElliePage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Ask Ellie
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Ellie is your AI-powered database assistant. Ask questions about
            your PostgreSQL databases, get performance advice, troubleshoot
            issues, or explore pgEdge product documentation.
        </Typography>

        <SectionTitle icon={ChatBotIcon}>Getting Started</SectionTitle>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Opening the Chat"
                description="Click the chat button in the bottom-right corner to open the Ask Ellie panel. The panel appears alongside your current view."
            />
            <FeatureItem
                title="Asking Questions"
                description="Type your question in the input field and press Enter to send. Ellie can answer questions about your monitored databases, run read-only queries, analyze query plans, and search the pgEdge knowledge base."
            />
            <FeatureItem
                title="Tool Execution"
                description="Ellie has access to monitoring tools that run automatically as needed. A status indicator shows which tools are active during a response."
            />
            <FeatureItem
                title="Code Blocks"
                description="Code blocks in responses include a copy-to-clipboard button in the top-right corner for easy copying of code snippets, SQL queries, and configuration examples."
            />
        </Box>

        <SectionTitle icon={HistoryIcon}>Conversation History</SectionTitle>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Viewing History"
                description="Click the history button in the chat header to view previous conversations. The history overlay shows all saved conversations sorted by most recent."
            />
            <FeatureItem
                title="Managing Conversations"
                description="Right-click the menu icon on any conversation to rename or delete it. Use the Clear All button at the bottom to remove all conversations."
            />
            <FeatureItem
                title="New Conversation"
                description="Click the plus button in the chat header to start a fresh conversation. Previous conversations are saved automatically."
            />
            <FeatureItem
                title="Download Conversation"
                description="Click the download button in the chat header to save the current conversation as a markdown file. The file includes all messages with timestamps."
            />
        </Box>

        <SectionTitle icon={AutoAwesomeIcon}>Available Tools</SectionTitle>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Database Queries"
                description="Ellie can run read-only SQL queries against your monitored databases and return formatted results."
            />
            <FeatureItem
                title="Metrics Analysis"
                description="Query historical monitoring metrics with time-based aggregation to identify trends and anomalies."
            />
            <FeatureItem
                title="Schema Information"
                description="Explore database schemas, tables, and columns across your monitored connections."
            />
            <FeatureItem
                title="Knowledge Base"
                description="Search pgEdge documentation for information about PostgreSQL, Spock replication, and pgEdge products."
            />
            <FeatureItem
                title="Alert History"
                description="Review historical alerts and alert rules for any monitored connection."
            />
        </Box>

        <HelpTip>
            Use Shift+Enter to add a new line without sending. Press the up
            arrow at the start of the input to recall previous messages.
        </HelpTip>
    </Box>
);

export default AskElliePage;
