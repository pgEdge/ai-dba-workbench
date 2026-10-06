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
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { Hub as HubIcon } from '@mui/icons-material';
import { useClusterData } from '../../../contexts/useClusterData';
import { useReplicationSlots } from '../../../hooks/useReplicationSlots';
import { formatBytes } from '../../../utils/formatters';
import { METRIC_LABEL_SX, MONO_CAPTION_SX } from '../../../theme/tokens';
import CollapsibleSection from '../CollapsibleSection';
import type { ServerSectionProps } from './types';
import {
    describeWalStatus,
    getReplicationContext,
    summariseSlots,
    type ReplicationPeer,
    type ReplicationSlotRow,
    type SlotHealth,
} from './replicationSlots';

/** Grid template shared by the header row and the data rows. */
const GRID_TEMPLATE = '2fr repeat(5, minmax(0, 1fr))';

/** Scroll container for the slot table. */
const TABLE_CONTAINER_SX = {
    overflowX: 'auto' as const,
    mb: 1,
};

/** Header row of the slot table. */
const TABLE_HEADER_SX = {
    display: 'grid',
    gridTemplateColumns: GRID_TEMPLATE,
    gap: 1,
    px: 1.5,
    py: 1,
    minWidth: 640,
    borderBottom: '2px solid',
    borderColor: 'divider',
};

/** Data row of the slot table. */
const TABLE_ROW_SX = {
    display: 'grid',
    gridTemplateColumns: GRID_TEMPLATE,
    gap: 1,
    px: 1.5,
    py: 1,
    minWidth: 640,
    alignItems: 'center',
    borderBottom: '1px solid',
    borderColor: 'divider',
    '&:last-child': {
        borderBottom: 'none',
    },
};

/** Header cell typography. */
const HEADER_CELL_SX = {
    ...METRIC_LABEL_SX,
    fontWeight: 700,
};

/** Right-aligned header cell typography for the size columns. */
const NUMERIC_HEADER_CELL_SX = {
    ...HEADER_CELL_SX,
    textAlign: 'right' as const,
};

/** Slot name cell typography. */
const NAME_CELL_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.primary',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
};

/** Plain text cell typography. */
const TEXT_CELL_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.primary',
};

/** Size cell typography. */
const NUMERIC_CELL_SX = {
    ...MONO_CAPTION_SX,
    fontWeight: 500,
    color: 'text.primary',
    textAlign: 'right' as const,
};

/** Centred block used for the loading, error, and empty states. */
const MESSAGE_SX = { textAlign: 'center' as const, py: 3 };

/** Chip colour for each slot health. */
const HEALTH_CHIP_COLOR: Record<
    SlotHealth, 'success' | 'warning' | 'error' | 'default'
> = {
    good: 'success',
    warning: 'warning',
    critical: 'error',
    unknown: 'default',
};

/** A status chip, outlined when it carries no health meaning. */
const StatusChip: React.FC<{
    label: string;
    health: SlotHealth;
    description: string;
}> = ({ label, health, description }) => (
    <Box>
        <Tooltip title={description}>
            <Chip
                label={label}
                size="small"
                color={HEALTH_CHIP_COLOR[health]}
                variant={health === 'unknown' ? 'outlined' : 'filled'}
                sx={{ fontSize: '0.875rem' }}
            />
        </Tooltip>
    </Box>
);

/**
 * Describe whether a slot has a connected consumer. An inactive slot
 * still holds back WAL removal, which is why it is flagged.
 */
const describeActivity = (
    active: boolean | null,
): { label: string; health: SlotHealth; description: string } => {
    if (active === true) {
        return {
            label: 'Active',
            health: 'good',
            description: 'A consumer is connected to this slot',
        };
    }
    if (active === false) {
        return {
            label: 'Inactive',
            health: 'warning',
            description:
                'No consumer is connected; the slot still retains WAL',
        };
    }
    return {
        label: 'Unknown',
        health: 'unknown',
        description: 'Activity was not reported',
    };
};

/**
 * Format a slot's safe WAL size. PostgreSQL reports it as null when
 * max_slot_wal_keep_size is -1, so a null alongside a reported WAL
 * status means the slot may retain WAL without limit.
 */
const formatSafeWalSize = (slot: ReplicationSlotRow): string => {
    if (slot.safe_wal_size !== null) {
        return formatBytes(slot.safe_wal_size);
    }
    return slot.wal_status ? 'Unlimited' : '--';
};

/** Format a slot type for display. */
const formatSlotType = (slotType: string | null): string => {
    if (slotType === 'physical') { return 'Physical'; }
    if (slotType === 'logical') { return 'Logical'; }
    return slotType ?? '--';
};

/** One labelled line of the replication context. */
const PeerLine: React.FC<{
    heading: string;
    peers: ReplicationPeer[];
}> = ({ heading, peers }) => (
    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'baseline' }}>
        <Typography component="span" sx={METRIC_LABEL_SX}>
            {heading}
        </Typography>
        {peers.map((peer, index) => (
            <Typography
                key={`${peer.serverId}-${peer.label}`}
                component="span"
                variant="body2"
            >
                {`${peer.label} `}
                <bdi>{peer.serverName}</bdi>
                {index < peers.length - 1 ? ';' : ''}
            </Typography>
        ))}
    </Box>
);

/** Build the summary sentence shown above the table. */
const describeSummary = (
    summary: ReturnType<typeof summariseSlots>,
): string => {
    const noun = summary.total === 1 ? 'slot' : 'slots';
    const parts = [
        `${summary.total} ${noun}`,
        `${summary.active} active`,
        `${summary.inactive} inactive`,
    ];
    if (summary.atRisk > 0) {
        parts.push(`${summary.atRisk} with WAL at risk`);
    }
    return parts.join(', ');
};

/** The slot table: a header row followed by one row per slot. */
const SlotTable: React.FC<{ slots: ReplicationSlotRow[] }> = ({ slots }) => (
    <Box sx={TABLE_CONTAINER_SX} role="table" aria-label="Replication slots">
        <Box sx={TABLE_HEADER_SX} role="row">
            <Typography sx={HEADER_CELL_SX} role="columnheader">Slot</Typography>
            <Typography sx={HEADER_CELL_SX} role="columnheader">Type</Typography>
            <Typography sx={HEADER_CELL_SX} role="columnheader">Status</Typography>
            <Typography sx={HEADER_CELL_SX} role="columnheader">
                WAL status
            </Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX} role="columnheader">
                Retained WAL
            </Typography>
            <Typography sx={NUMERIC_HEADER_CELL_SX} role="columnheader">
                Safe WAL size
            </Typography>
        </Box>

        {slots.map((slot) => {
            const activity = describeActivity(slot.active);
            const walStatus = describeWalStatus(slot.wal_status);
            return (
                <Box
                    key={`${slot.slot_name}-${slot.slot_type ?? ''}`}
                    sx={TABLE_ROW_SX}
                    role="row"
                >
                    <Typography
                        sx={NAME_CELL_SX}
                        title={slot.slot_name}
                        role="cell"
                    >
                        {slot.slot_name}
                    </Typography>
                    <Typography sx={TEXT_CELL_SX} role="cell">
                        {formatSlotType(slot.slot_type)}
                    </Typography>
                    <Box role="cell">
                        <StatusChip {...activity} />
                    </Box>
                    <Box role="cell">
                        <StatusChip {...walStatus} />
                    </Box>
                    <Typography sx={NUMERIC_CELL_SX} role="cell">
                        {formatBytes(slot.retained_bytes)}
                    </Typography>
                    <Typography sx={NUMERIC_CELL_SX} role="cell">
                        {formatSafeWalSize(slot)}
                    </Typography>
                </Box>
            );
        })}
    </Box>
);

/**
 * Replication Slots section lists the server's replication slots from
 * the collector's latest pg_replication_slots snapshot, with each
 * slot's type, whether a consumer is connected, its WAL status and
 * the WAL it retains. Above the table it shows the server's place in
 * the replication topology the collector detected: the servers it
 * replicates from, and the standbys and subscribers that replicate
 * from it, which are the usual consumers of its slots.
 */
const ReplicationSlotsSection: React.FC<ServerSectionProps> = ({
    connectionId,
}) => {
    const { clusterData } = useClusterData();
    const { slots, totalCount, loading, error } =
        useReplicationSlots(connectionId);

    const context = useMemo(
        () => getReplicationContext(clusterData, connectionId),
        [clusterData, connectionId],
    );
    const summary = useMemo(() => summariseSlots(slots), [slots]);

    const showTable = !loading && !error && slots.length > 0;
    const showEmpty = !loading && !error && slots.length === 0;
    const hasContext = context.upstream.length > 0
        || context.downstream.length > 0;

    return (
        <CollapsibleSection
            title="Replication Slots"
            icon={<HubIcon sx={{ fontSize: 16 }} />}
            defaultExpanded
            storageKey="dashboard-section-replication-slots-expanded"
        >
            {hasContext && (
                <Box
                    sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, mb: 1.5 }}
                    aria-label="Replication context"
                    role="group"
                >
                    {context.upstream.length > 0 && (
                        <PeerLine heading="Upstream" peers={context.upstream} />
                    )}
                    {context.downstream.length > 0 && (
                        <PeerLine
                            heading="Downstream"
                            peers={context.downstream}
                        />
                    )}
                </Box>
            )}

            {loading && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                    <CircularProgress
                        size={24}
                        aria-label="Loading replication slots"
                    />
                </Box>
            )}

            {error && (
                <Typography variant="body2" color="error" sx={MESSAGE_SX}>
                    {error}
                </Typography>
            )}

            {showEmpty && (
                <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={MESSAGE_SX}
                >
                    No replication slots were collected from this server
                    in the last hour.
                </Typography>
            )}

            {showTable && (
                <>
                    <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ mb: 1 }}
                    >
                        {describeSummary(summary)}
                    </Typography>
                    <SlotTable slots={slots} />
                    {totalCount > slots.length && (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ textAlign: 'right' }}
                        >
                            {`Showing the first ${slots.length} of ${totalCount} slots by name`}
                        </Typography>
                    )}
                </>
            )}
        </CollapsibleSection>
    );
};

export default ReplicationSlotsSection;
