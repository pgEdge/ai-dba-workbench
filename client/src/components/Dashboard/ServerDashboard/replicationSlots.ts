/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Types and pure helpers for the replication slot health panel. They
 * turn the collector's pg_replication_slots snapshot into display rows
 * and the cluster topology into the server's replication context, so
 * the section component holds no data-shaping logic of its own.
 */

import type {
    ClusterGroup,
    ClusterServer,
} from '../../../contexts/ClusterDataContext';
import { collectServers } from '../../../utils/clusterHelpers';
import { getRelationshipLabel } from '../../topology/topologyHelpers';

/** One replication slot from the latest collected snapshot. */
export interface ReplicationSlotRow {
    slot_name: string;
    slot_type: string | null;
    active: boolean | null;
    wal_status: string | null;
    safe_wal_size: number | null;
    retained_bytes: number | null;
}

/** The latest-snapshot response for the pg_replication_slots probe. */
export interface ReplicationSlotsResponse {
    rows?: Record<string, unknown>[] | null;
    total_count?: number;
}

/** Health of a slot's WAL reservation, as drawn on its chip. */
export type SlotHealth = 'good' | 'warning' | 'critical' | 'unknown';

/** Display label and health for a pg_replication_slots.wal_status. */
export interface WalStatusDisplay {
    label: string;
    health: SlotHealth;
    description: string;
}

/**
 * The wal_status values PostgreSQL reports (13 and later), with what
 * each means for the slot. 'extended' is held beyond max_wal_size but
 * is still safe, whilst 'unreserved' will be lost at the next
 * checkpoint unless the consumer catches up, and 'lost' cannot be used
 * again.
 */
const WAL_STATUS_DISPLAY: Record<string, WalStatusDisplay> = {
    reserved: {
        label: 'Reserved',
        health: 'good',
        description: 'Required WAL is within max_wal_size',
    },
    extended: {
        label: 'Extended',
        health: 'warning',
        description:
            'Required WAL exceeds max_wal_size but is still retained',
    },
    unreserved: {
        label: 'Unreserved',
        health: 'critical',
        description:
            'Required WAL will be removed at the next checkpoint',
    },
    lost: {
        label: 'Lost',
        health: 'critical',
        description:
            'Required WAL has been removed; the slot can no longer be used',
    },
};

/**
 * Describe a slot's wal_status. A null status means the server predates
 * PostgreSQL 13, which does not report one; an unrecognised value is
 * shown as received rather than hidden.
 */
export const describeWalStatus = (
    walStatus: string | null,
): WalStatusDisplay => {
    if (!walStatus) {
        return {
            label: 'Not reported',
            health: 'unknown',
            description: 'WAL status requires PostgreSQL 13 or later',
        };
    }
    return WAL_STATUS_DISPLAY[walStatus] ?? {
        label: walStatus,
        health: 'unknown',
        description: `Unrecognised WAL status '${walStatus}'`,
    };
};

/**
 * Read a numeric field that may arrive as a JSON number or, from a
 * server that predates NUMERIC normalisation, as a numeric string.
 * Anything else, including a non-finite value, is treated as absent.
 */
const toNumber = (value: unknown): number | null => {
    if (typeof value === 'number') {
        return Number.isFinite(value) ? value : null;
    }
    if (typeof value === 'string' && value.trim() !== '') {
        const parsed = Number(value);
        return Number.isFinite(parsed) ? parsed : null;
    }
    return null;
};

/** Read a text field, treating anything but a non-empty string as absent. */
const toText = (value: unknown): string | null =>
    typeof value === 'string' && value !== '' ? value : null;

/**
 * Convert the raw latest-snapshot rows into slot rows, dropping any row
 * without a slot name, since there is nothing to identify it by.
 */
export const normaliseSlotRows = (
    rows: Record<string, unknown>[] | null | undefined,
): ReplicationSlotRow[] => {
    if (!Array.isArray(rows)) { return []; }
    const result: ReplicationSlotRow[] = [];
    for (const row of rows) {
        const slotName = toText(row.slot_name);
        if (!slotName) { continue; }
        result.push({
            slot_name: slotName,
            slot_type: toText(row.slot_type),
            active: typeof row.active === 'boolean' ? row.active : null,
            wal_status: toText(row.wal_status),
            safe_wal_size: toNumber(row.safe_wal_size),
            retained_bytes: toNumber(row.retained_bytes),
        });
    }
    return result;
};

/** Counts shown in the panel summary line. */
export interface SlotSummary {
    total: number;
    active: number;
    inactive: number;
    atRisk: number;
}

/**
 * Summarise the slots: how many are active or inactive, and how many
 * have a WAL status that needs attention (anything other than good or
 * unknown).
 */
export const summariseSlots = (slots: ReplicationSlotRow[]): SlotSummary => {
    let active = 0;
    let inactive = 0;
    let atRisk = 0;
    for (const slot of slots) {
        if (slot.active === true) { active++; }
        if (slot.active === false) { inactive++; }
        const health = describeWalStatus(slot.wal_status).health;
        if (health === 'warning' || health === 'critical') { atRisk++; }
    }
    return { total: slots.length, active, inactive, atRisk };
};

/** One edge of the server's replication context. */
export interface ReplicationPeer {
    serverId: number;
    serverName: string;
    label: string;
}

/** Where a server replicates from, and which servers replicate from it. */
export interface ReplicationContext {
    upstream: ReplicationPeer[];
    downstream: ReplicationPeer[];
}

/** Label for a server that consumes this server's changes. */
const getDownstreamLabel = (relationshipType: string): string =>
    relationshipType === 'streams_from' ? 'Standby' : 'Subscriber';

/**
 * Relationship types whose source consumes the target's changes. A
 * replicates_with edge is symmetric and already appears in the
 * server's own upstream list, so it is not repeated as downstream.
 */
const DOWNSTREAM_TYPES = new Set(['streams_from', 'subscribes_to']);

/**
 * Resolve a server's replication context from the cluster topology,
 * whose relationships are detected by the collector (or set by an
 * administrator). Upstream lists the servers this one replicates from;
 * downstream lists the standbys and subscribers that replicate from
 * it, which are the usual consumers of its slots.
 */
export const getReplicationContext = (
    clusterData: ClusterGroup[] | null | undefined,
    serverId: number,
): ReplicationContext => {
    const servers: ClusterServer[] = [];
    for (const group of clusterData ?? []) {
        for (const cluster of group.clusters ?? []) {
            servers.push(...collectServers(cluster.servers ?? []));
        }
    }

    const upstream: ReplicationPeer[] = [];
    const downstream: ReplicationPeer[] = [];
    const seen = new Set<string>();
    const add = (
        list: ReplicationPeer[],
        direction: string,
        peer: ReplicationPeer,
    ): void => {
        const key = `${direction}:${peer.serverId}:${peer.label}`;
        if (seen.has(key)) { return; }
        seen.add(key);
        list.push(peer);
    };

    for (const server of servers) {
        for (const rel of server.relationships ?? []) {
            if (server.id === serverId) {
                add(upstream, 'up', {
                    serverId: rel.target_server_id,
                    serverName: rel.target_server_name,
                    label: getRelationshipLabel(rel.relationship_type),
                });
            } else if (rel.target_server_id === serverId
                && DOWNSTREAM_TYPES.has(rel.relationship_type)) {
                add(downstream, 'down', {
                    serverId: server.id,
                    serverName: server.name,
                    label: getDownstreamLabel(rel.relationship_type),
                });
            }
        }
    }

    return { upstream, downstream };
};
