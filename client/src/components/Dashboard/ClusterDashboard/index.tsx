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
import { BaseDashboardProps } from '../types';
import CollapsibleSection from '../CollapsibleSection';
import ReplicationLagSection from './ReplicationLagSection';
import ComparativeChartsSection from './ComparativeChartsSection';


/**
 * Extract all server IDs from a cluster selection, including
 * nested children.
 */
const extractServerIds = (selection: Record<string, unknown>): number[] => {
    const ids = new Set<number>();
    const servers = selection.servers as Array<Record<string, unknown>> | undefined;

    const collectIds = (serverList: Array<Record<string, unknown>> | undefined): void => {
        serverList?.forEach(s => {
            if (typeof s.id === 'number') {
                ids.add(s.id);
            }
            if (s.children) {
                collectIds(s.children as Array<Record<string, unknown>>);
            }
        });
    };

    collectIds(servers);
    return Array.from(ids);
};

/**
 * ClusterDashboard focuses on replication health and shows
 * comparative metrics across cluster members. Displays topology,
 * replication lag, and comparative charts for all servers in
 * the cluster.
 */
const ClusterDashboard: React.FC<BaseDashboardProps> = ({ selection }) => {
    const serverIds = useMemo(() => extractServerIds(selection), [selection]);

    return (
        <Box>
            <CollapsibleSection title="Replication Lag" defaultExpanded>
                <ReplicationLagSection
                    selection={selection}
                    serverIds={serverIds}
                />
            </CollapsibleSection>

            <CollapsibleSection title="Comparative Metrics" defaultExpanded>
                <ComparativeChartsSection serverIds={serverIds} />
            </CollapsibleSection>
        </Box>
    );
};

export default ClusterDashboard;
