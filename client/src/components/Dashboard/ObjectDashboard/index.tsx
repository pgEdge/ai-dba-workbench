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
import { ObjectType } from '../types';
import { useDashboard } from '../../../contexts/DashboardContext';
import TableDetail from './TableDetail';
import IndexDetail from './IndexDetail';
import QueryDetail from './QueryDetail';

interface ObjectDashboardProps {
    connectionId: number;
    databaseName: string;
    objectType: ObjectType;
    schemaName?: string;
    objectName: string;
}

/** Header container with object type label */
const HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    mb: 2,
};

/** Object type badge */
const TYPE_BADGE_SX = {
    fontSize: '0.6875rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
    px: 1,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: 'primary.main',
    color: 'primary.contrastText',
};

/** Database context label */
const CONTEXT_SX = {
    fontSize: '0.8125rem',
    color: 'text.secondary',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

/**
 * ObjectDashboard routes to the appropriate detail view based on
 * the object type (table, index, or query). It provides the header
 * with object type badge and database context, then delegates to
 * the specific detail component.
 */
const ObjectDashboard: React.FC<ObjectDashboardProps> = ({
    connectionId,
    databaseName,
    objectType,
    schemaName,
    objectName,
}) => {
    const { currentOverlay } = useDashboard();

    const typeLabel = useMemo(() => {
        return objectType.charAt(0).toUpperCase()
            + objectType.slice(1);
    }, [objectType]);

    const contextLabel = useMemo(() => {
        const parts: string[] = [];
        if (databaseName) {
            parts.push(databaseName);
        }
        const name = currentOverlay?.connectionName;
        parts.push(name || `Connection ${connectionId}`);
        return parts.join(' | ');
    }, [databaseName, connectionId, currentOverlay?.connectionName]);

    if (!objectName) {
        return (
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: 'center', py: 4 }}
            >
                No object selected
            </Typography>
        );
    }

    return (
        <Box>
            <Box sx={HEADER_SX}>
                <Box sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1,
                }}>
                    <Box
                        component="span"
                        sx={TYPE_BADGE_SX}
                    >
                        {typeLabel}
                    </Box>
                    <Typography sx={CONTEXT_SX}>
                        {contextLabel}
                    </Typography>
                </Box>
            </Box>

            {objectType === 'table' && (
                <TableDetail
                    connectionId={connectionId}
                    databaseName={databaseName}
                    schemaName={schemaName}
                    objectName={objectName}
                />
            )}

            {objectType === 'index' && (
                <IndexDetail
                    connectionId={connectionId}
                    databaseName={databaseName}
                    schemaName={schemaName}
                    objectName={objectName}
                />
            )}

            {objectType === 'query' && (
                <QueryDetail
                    connectionId={connectionId}
                    databaseName={databaseName}
                    objectName={objectName}
                />
            )}
        </Box>
    );
};

export default ObjectDashboard;
