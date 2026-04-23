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
import { useTheme } from '@mui/material/styles';
import { Dns as DnsIcon } from '@mui/icons-material';
import { Section, KV } from '../components';
import { getSectionIconSx, getKvGridSx } from '../serverInfoStyles';
import type { PostgreSQLInfo } from '../serverInfoTypes';

export interface PostgreSQLSectionProps {
    postgresql: PostgreSQLInfo;
}

/**
 * PostgreSQL server information section.
 */
const PostgreSQLSection: React.FC<PostgreSQLSectionProps> = ({ postgresql: pg }) => {
    const theme = useTheme();

    return (
        <Section
            sectionId="postgresql"
            icon={<DnsIcon sx={getSectionIconSx(theme)} />}
            title="PostgreSQL"
        >
            <Box sx={getKvGridSx()}>
                {pg.version && (
                    <KV label="Version" value={pg.version} />
                )}
                {pg.cluster_name && (
                    <KV label="Cluster Name" value={pg.cluster_name} />
                )}
                {pg.data_directory && (
                    <KV label="Data Directory" value={pg.data_directory} span />
                )}
                {pg.max_connections != null && (
                    <KV label="Max Connections" value={String(pg.max_connections)} />
                )}
                {pg.max_wal_senders != null && (
                    <KV label="Max WAL Senders" value={String(pg.max_wal_senders)} />
                )}
                {pg.max_replication_slots != null && (
                    <KV label="Max Replication Slots" value={String(pg.max_replication_slots)} />
                )}
            </Box>
        </Section>
    );
};

export default PostgreSQLSection;
