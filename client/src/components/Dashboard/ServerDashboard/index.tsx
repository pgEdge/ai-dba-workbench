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
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import SystemResourcesSection from './SystemResourcesSection';
import PostgresOverviewSection from './PostgresOverviewSection';
import WalReplicationSection from './WalReplicationSection';
import DatabaseSummariesSection from './DatabaseSummariesSection';
import TopQueriesSection from './TopQueriesSection';
import type { ServerSelection } from '../../../types/selection';

/**
 * ServerDashboard provides comprehensive server health and
 * performance information organized in collapsible sections.
 * It displays system resources, PostgreSQL metrics, WAL and
 * replication status, database summaries, and top queries.
 */
const ServerDashboard: React.FC<{ selection: ServerSelection }> = ({
    selection,
}) => {
    const connectionId = selection.id;
    const connectionName = selection.name;

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
            <SystemResourcesSection connectionId={connectionId} connectionName={connectionName} />
            <PostgresOverviewSection connectionId={connectionId} connectionName={connectionName} />
            <WalReplicationSection connectionId={connectionId} connectionName={connectionName} />
            <DatabaseSummariesSection connectionId={connectionId} connectionName={connectionName} />
            <TopQueriesSection connectionId={connectionId} connectionName={connectionName} />
        </Box>
    );
};

export default ServerDashboard;
