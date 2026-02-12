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
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import SystemResourcesSection from './SystemResourcesSection';
import PostgresOverviewSection from './PostgresOverviewSection';
import WalReplicationSection from './WalReplicationSection';
import DatabaseSummariesSection from './DatabaseSummariesSection';
import TopQueriesSection from './TopQueriesSection';

interface ServerDashboardProps {
    selection: Record<string, unknown>;
}

/**
 * ServerDashboard provides comprehensive server health and
 * performance information organized in collapsible sections.
 * It displays system resources, PostgreSQL metrics, WAL and
 * replication status, database summaries, and top queries.
 */
const ServerDashboard: React.FC<ServerDashboardProps> = ({
    selection,
}) => {
    const connectionId = selection.id as number;
    const connectionName = selection.name as string | undefined;

    if (!connectionId && connectionId !== 0) {
        return (
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: 'center', py: 4 }}
            >
                No server selected
            </Typography>
        );
    }

    return (
        <Box>
            <SystemResourcesSection connectionId={connectionId} />
            <PostgresOverviewSection connectionId={connectionId} />
            <WalReplicationSection connectionId={connectionId} />
            <DatabaseSummariesSection connectionId={connectionId} connectionName={connectionName} />
            <TopQueriesSection connectionId={connectionId} connectionName={connectionName} />
        </Box>
    );
};

export default ServerDashboard;
