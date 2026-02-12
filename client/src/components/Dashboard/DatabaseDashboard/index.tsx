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
import PerformanceSection from './PerformanceSection';
import TableLeaderboardSection from './TableLeaderboardSection';
import IndexLeaderboardSection from './IndexLeaderboardSection';
import VacuumStatusSection from './VacuumStatusSection';

interface DatabaseDashboardProps {
    connectionId: number;
    databaseName: string;
}

/** Header container with database name and time range selector */
const HEADER_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    mb: 2,
};

/** Database info display */
const DB_INFO_SX = {
    display: 'flex',
    alignItems: 'baseline',
    gap: 1,
};

/** Database name */
const DB_NAME_SX = {
    fontWeight: 600,
    fontSize: '1.1rem',
};

/** Connection metadata */
const DB_META_SX = {
    fontSize: '0.8125rem',
    color: 'text.secondary',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

/**
 * DatabaseDashboard provides database-specific performance and health
 * information organized in collapsible sections. It displays a
 * performance overview, table and index leaderboards, and vacuum
 * status for the selected database.
 */
const DatabaseDashboard: React.FC<DatabaseDashboardProps> = ({
    connectionId,
    databaseName,
}) => {
    const connectionLabel = useMemo(() => {
        return `Connection ${connectionId}`;
    }, [connectionId]);

    if (!databaseName) {
        return (
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: 'center', py: 4 }}
            >
                No database selected
            </Typography>
        );
    }

    return (
        <Box>
            <Box sx={HEADER_SX}>
                <Box sx={DB_INFO_SX}>
                    <Typography sx={DB_NAME_SX}>
                        {databaseName}
                    </Typography>
                    <Typography sx={DB_META_SX}>
                        {connectionLabel}
                    </Typography>
                </Box>
                <TimeRangeSelector />
            </Box>

            <PerformanceSection
                connectionId={connectionId}
                databaseName={databaseName}
            />
            <TableLeaderboardSection
                connectionId={connectionId}
                databaseName={databaseName}
            />
            <IndexLeaderboardSection
                connectionId={connectionId}
                databaseName={databaseName}
            />
            <VacuumStatusSection
                connectionId={connectionId}
                databaseName={databaseName}
            />
        </Box>
    );
};

export default DatabaseDashboard;
