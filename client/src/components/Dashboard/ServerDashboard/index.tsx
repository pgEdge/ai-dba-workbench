/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import TimeRangeSelector from '../TimeRangeSelector';
import SystemResourcesSection from './SystemResourcesSection';
import PostgresOverviewSection from './PostgresOverviewSection';
import WalReplicationSection from './WalReplicationSection';
import DatabaseSummariesSection from './DatabaseSummariesSection';
import TopQueriesSection from './TopQueriesSection';

interface ServerDashboardProps {
    selection: Record<string, unknown>;
}

/** Header container with server name and time range selector */
const HEADER_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    mb: 2,
};

/** Server info display */
const SERVER_INFO_SX = {
    display: 'flex',
    alignItems: 'baseline',
    gap: 1,
};

/** Server name */
const SERVER_NAME_SX = {
    fontWeight: 600,
    fontSize: '1.1rem',
};

/** Server metadata (host, role, version) */
const SERVER_META_SX = {
    fontSize: '0.8125rem',
    color: 'text.secondary',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

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

    const serverLabel = useMemo(() => {
        const name = selection.name as string | undefined;
        const host = selection.host as string | undefined;
        const port = selection.port as number | undefined;
        const role = selection.role as string | undefined;
        const version = selection.version as string | undefined;

        const parts: string[] = [];
        if (host) {
            parts.push(port ? `${host}:${port}` : host);
        }
        if (role) {
            parts.push(role);
        }
        if (version) {
            parts.push(`PG ${version}`);
        }

        return {
            name: name || 'Server',
            meta: parts.join(' | '),
        };
    }, [
        selection.name,
        selection.host,
        selection.port,
        selection.role,
        selection.version,
    ]);

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
            <Box sx={HEADER_SX}>
                <Box sx={SERVER_INFO_SX}>
                    <Typography sx={SERVER_NAME_SX}>
                        {serverLabel.name}
                    </Typography>
                    {serverLabel.meta && (
                        <Typography sx={SERVER_META_SX}>
                            {serverLabel.meta}
                        </Typography>
                    )}
                </Box>
                <TimeRangeSelector />
            </Box>

            <SystemResourcesSection connectionId={connectionId} />
            <PostgresOverviewSection connectionId={connectionId} />
            <WalReplicationSection connectionId={connectionId} />
            <DatabaseSummariesSection connectionId={connectionId} />
            <TopQueriesSection connectionId={connectionId} />
        </Box>
    );
};

export default ServerDashboard;
