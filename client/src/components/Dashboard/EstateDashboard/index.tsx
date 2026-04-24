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
import HealthOverviewSection from './HealthOverviewSection';
import KpiTilesSection from './KpiTilesSection';
import ClusterCardsSection from './ClusterCardsSection';
import { extractEstateServerIds } from '../../../utils/clusterHelpers';
import type { EstateSelection } from '../../../types/selection';

/**
 * EstateDashboard displays fleet-wide health and provides a
 * fleet health assessment at a glance. Shows health overview
 * rings, KPI tiles, and cluster cards for the entire estate.
 */
const EstateDashboard: React.FC<{ selection: EstateSelection }> = ({ selection }) => {
    const serverIds = useMemo(() => extractEstateServerIds(selection), [selection]);

    return (
        <Box>
            <CollapsibleSection title="Health Overview" defaultExpanded>
                <HealthOverviewSection selection={selection} />
            </CollapsibleSection>

            <CollapsibleSection title="Key Performance Indicators" defaultExpanded>
                <KpiTilesSection
                    selection={selection}
                    serverIds={serverIds}
                />
            </CollapsibleSection>

            <CollapsibleSection title="Clusters" defaultExpanded>
                <ClusterCardsSection selection={selection} />
            </CollapsibleSection>
        </Box>
    );
};

export default EstateDashboard;
