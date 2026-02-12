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
import TopologySection from './TopologySection';
import ReplicationLagSection from './ReplicationLagSection';
import ComparativeChartsSection from './ComparativeChartsSection';
import AlertSummarySection from './AlertSummarySection';

/**
 * Extract all server IDs from a cluster selection, including
 * nested children.
 */
const extractServerIds = (selection: Record<string, unknown>): number[] => {
    const ids: number[] = [];
    const servers = selection.servers as Array<Record<string, unknown>> | undefined;

    const collectIds = (serverList: Array<Record<string, unknown>> | undefined): void => {
        serverList?.forEach(s => {
            if (typeof s.id === 'number') {
                ids.push(s.id);
            }
            if (s.children) {
                collectIds(s.children as Array<Record<string, unknown>>);
            }
        });
    };

    collectIds(servers);
    return ids;
};

/**
 * ClusterDashboard focuses on replication health and shows
 * comparative metrics across cluster members. Displays topology,
 * replication lag, comparative charts, and an alert summary
 * for all servers in the cluster.
 */
const ClusterDashboard: React.FC<BaseDashboardProps> = ({ selection }) => {
    const serverIds = useMemo(() => extractServerIds(selection), [selection]);

    return (
        <Box>
            <CollapsibleSection title="Topology" defaultExpanded>
                <TopologySection selection={selection} />
            </CollapsibleSection>

            <CollapsibleSection title="Replication Lag" defaultExpanded>
                <ReplicationLagSection
                    selection={selection}
                    serverIds={serverIds}
                />
            </CollapsibleSection>

            <CollapsibleSection title="Comparative Metrics" defaultExpanded>
                <ComparativeChartsSection serverIds={serverIds} />
            </CollapsibleSection>

            <CollapsibleSection title="Alert Summary" defaultExpanded>
                <AlertSummarySection serverIds={serverIds} />
            </CollapsibleSection>
        </Box>
    );
};

export default ClusterDashboard;
