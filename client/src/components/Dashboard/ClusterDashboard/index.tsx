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
import { BaseDashboardProps } from '../types';
import TimeRangeSelector from '../TimeRangeSelector';
import CollapsibleSection from '../CollapsibleSection';
import TopologySection from './TopologySection';
import ReplicationLagSection from './ReplicationLagSection';
import ComparativeChartsSection from './ComparativeChartsSection';
import AlertSummarySection from './AlertSummarySection';

const HEADER_SX = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    mb: 2,
};

const TITLE_SX = {
    fontWeight: 600,
    fontSize: '1.25rem',
    color: 'text.primary',
};

const SUBTITLE_SX = {
    fontSize: '0.875rem',
    color: 'text.secondary',
    fontWeight: 400,
    ml: 1,
};

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
    const clusterName = (selection.name as string) || 'Cluster';
    const serverIds = useMemo(() => extractServerIds(selection), [selection]);

    return (
        <Box>
            <Box sx={HEADER_SX}>
                <Box sx={{ display: 'flex', alignItems: 'baseline' }}>
                    <Typography sx={TITLE_SX}>
                        Cluster Dashboard
                    </Typography>
                    <Typography sx={SUBTITLE_SX}>
                        {clusterName}
                    </Typography>
                </Box>
                <TimeRangeSelector />
            </Box>

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
