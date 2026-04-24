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
import { useMemo } from 'react';
import Box from '@mui/material/Box';
import CollapsibleSection from '../CollapsibleSection';
import ReplicationLagSection from './ReplicationLagSection';
import ComparativeChartsSection from './ComparativeChartsSection';
import { extractClusterServerIds } from '../../../utils/clusterHelpers';
import type { ClusterSelection } from '../../../types/selection';

/**
 * ClusterDashboard focuses on replication health and shows
 * comparative metrics across cluster members. Displays topology,
 * replication lag, and comparative charts for all servers in
 * the cluster.
 */
const ClusterDashboard: React.FC<{ selection: ClusterSelection }> = ({ selection }) => {
    const serverIds = useMemo(() => extractClusterServerIds(selection), [selection]);

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
