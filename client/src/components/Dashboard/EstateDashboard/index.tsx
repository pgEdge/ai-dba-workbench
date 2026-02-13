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
import HealthOverviewSection from './HealthOverviewSection';
import KpiTilesSection from './KpiTilesSection';
import ClusterCardsSection from './ClusterCardsSection';
import HotSpotsSection from './HotSpotsSection';

/**
 * Extract all server IDs from an estate selection by traversing
 * groups, clusters, and nested server children.
 */
const extractAllServerIds = (selection: Record<string, unknown>): number[] => {
    const ids: number[] = [];
    const groups = selection.groups as Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const collectServers = (servers: Array<Record<string, unknown>> | undefined): void => {
                servers?.forEach(s => {
                    if (typeof s.id === 'number') {
                        ids.push(s.id);
                    }
                    if (s.children) {
                        collectServers(s.children as Array<Record<string, unknown>>);
                    }
                });
            };
            collectServers(cluster.servers as Array<Record<string, unknown>> | undefined);
        });
    });

    return ids;
};

/**
 * EstateDashboard displays fleet-wide health and provides a
 * fleet health assessment at a glance. Shows health overview
 * rings, KPI tiles, cluster cards, hot spots, and an event
 * timeline for the entire estate.
 */
const EstateDashboard: React.FC<BaseDashboardProps> = ({ selection }) => {
    const serverIds = useMemo(() => extractAllServerIds(selection), [selection]);

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

            <CollapsibleSection title="Hot Spots" defaultExpanded>
                <HotSpotsSection
                    selection={selection}
                    serverIds={serverIds}
                />
            </CollapsibleSection>
        </Box>
    );
};

export default EstateDashboard;
