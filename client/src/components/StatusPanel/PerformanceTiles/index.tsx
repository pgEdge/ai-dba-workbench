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
import { Box } from '@mui/material';
import { usePerformanceSummary } from './usePerformanceSummary';
import { PerformanceTilesProps } from './types';
import { TILE_GRID_SX } from './styles';
import DatabaseAgeTile from './DatabaseAgeTile';
import CacheHitTile from './CacheHitTile';
import TransactionTile from './TransactionTile';
import CheckpointTile from './CheckpointTile';

/**
 * PerformanceTiles displays a row of four performance summary
 * tiles: XID Age, Cache Hit Ratio, Transactions, and Checkpoints.
 */
const PerformanceTiles: React.FC<PerformanceTilesProps> = ({ selection }) => {
    const { data, loading } = usePerformanceSummary(selection);

    const connections = data?.connections ?? [];
    const isMultiServer = selection.type !== 'server';

    return (
        <Box sx={TILE_GRID_SX}>
            <DatabaseAgeTile
                connections={connections}
                loading={loading}
                isMultiServer={isMultiServer}
            />
            <CacheHitTile
                connections={connections}
                loading={loading}
                isMultiServer={isMultiServer}
            />
            <TransactionTile
                connections={connections}
                loading={loading}
            />
            <CheckpointTile
                connections={connections}
                loading={loading}
            />
        </Box>
    );
};

export default PerformanceTiles;
