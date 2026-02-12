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
import HealthOverviewSection from './HealthOverviewSection';
import KpiTilesSection from './KpiTilesSection';
import ClusterCardsSection from './ClusterCardsSection';
import HotSpotsSection from './HotSpotsSection';
import EventTimeline from '../../EventTimeline';

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
            <Box sx={HEADER_SX}>
                <Typography sx={TITLE_SX}>
                    Estate Dashboard
                </Typography>
                <TimeRangeSelector />
            </Box>

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

            <CollapsibleSection title="Event Timeline" defaultExpanded>
                <EventTimeline selection={selection} />
            </CollapsibleSection>
        </Box>
    );
};

export default EstateDashboard;
