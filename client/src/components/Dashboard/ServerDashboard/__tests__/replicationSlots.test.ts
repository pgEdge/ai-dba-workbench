/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import type {
    ClusterGroup,
    ClusterServer,
} from '../../../../contexts/ClusterDataContext';
import {
    describeWalStatus,
    getReplicationContext,
    normaliseSlotRows,
    summariseSlots,
    type ReplicationSlotRow,
} from '../replicationSlots';

/** Build a slot row with sensible defaults. */
const makeSlot = (
    overrides: Partial<ReplicationSlotRow> = {},
): ReplicationSlotRow => ({
    slot_name: 'standby_1',
    slot_type: 'physical',
    active: true,
    wal_status: 'reserved',
    safe_wal_size: 1024,
    retained_bytes: 2048,
    ...overrides,
});

/** Build a server with the given relationships. */
const makeServer = (
    id: number,
    name: string,
    relationships: ClusterServer['relationships'] = [],
    children?: ClusterServer[],
): ClusterServer => ({ id, name, relationships, children });

/** Wrap servers in a single group and cluster. */
const makeClusterData = (servers: ClusterServer[]): ClusterGroup[] => [
    {
        id: 'group-1',
        name: 'Group',
        clusters: [{ id: 'cluster-1', name: 'Cluster', servers }],
    },
];

const rel = (
    targetId: number,
    targetName: string,
    type: string,
) => ({
    target_server_id: targetId,
    target_server_name: targetName,
    relationship_type: type,
    is_auto_detected: true,
});

describe('describeWalStatus', () => {
    it.each([
        ['reserved', 'Reserved', 'good'],
        ['extended', 'Extended', 'warning'],
        ['unreserved', 'Unreserved', 'critical'],
        ['lost', 'Lost', 'critical'],
    ])('maps %s to %s (%s)', (status, label, health) => {
        const result = describeWalStatus(status);
        expect(result.label).toBe(label);
        expect(result.health).toBe(health);
        expect(result.description).not.toBe('');
    });

    it('reports a missing status as not reported', () => {
        const result = describeWalStatus(null);
        expect(result.label).toBe('Not reported');
        expect(result.health).toBe('unknown');
        expect(result.description).toContain('PostgreSQL 13');
    });

    it('shows an unrecognised status as received', () => {
        const result = describeWalStatus('mystery');
        expect(result.label).toBe('mystery');
        expect(result.health).toBe('unknown');
        expect(result.description).toContain("'mystery'");
    });
});

describe('normaliseSlotRows', () => {
    it('returns an empty list for missing rows', () => {
        expect(normaliseSlotRows(undefined)).toEqual([]);
        expect(normaliseSlotRows(null)).toEqual([]);
    });

    it('converts well-formed rows', () => {
        expect(normaliseSlotRows([{
            slot_name: 'sub_1',
            slot_type: 'logical',
            active: false,
            wal_status: 'extended',
            safe_wal_size: 512,
            retained_bytes: 4096,
            spill_txns: 3,
        }])).toEqual([{
            slot_name: 'sub_1',
            slot_type: 'logical',
            active: false,
            wal_status: 'extended',
            safe_wal_size: 512,
            retained_bytes: 4096,
        }]);
    });

    it('parses numeric strings and rejects non-finite values', () => {
        const [row] = normaliseSlotRows([{
            slot_name: 's',
            retained_bytes: '123456789',
            safe_wal_size: 'not a number',
        }]);
        expect(row.retained_bytes).toBe(123456789);
        expect(row.safe_wal_size).toBeNull();

        const [inf] = normaliseSlotRows([{
            slot_name: 's',
            retained_bytes: Number.POSITIVE_INFINITY,
            safe_wal_size: '  ',
        }]);
        expect(inf.retained_bytes).toBeNull();
        expect(inf.safe_wal_size).toBeNull();
    });

    it('treats wrongly typed fields as absent', () => {
        const [row] = normaliseSlotRows([{
            slot_name: 's',
            slot_type: 7,
            active: 'yes',
            wal_status: '',
            retained_bytes: { value: 1 },
        }]);
        expect(row).toEqual({
            slot_name: 's',
            slot_type: null,
            active: null,
            wal_status: null,
            safe_wal_size: null,
            retained_bytes: null,
        });
    });

    it('drops rows without a slot name', () => {
        expect(normaliseSlotRows([
            { slot_name: '' },
            { slot_name: null },
            { slot_type: 'physical' },
            { slot_name: 'kept' },
        ]).map((r) => r.slot_name)).toEqual(['kept']);
    });
});

describe('summariseSlots', () => {
    it('counts active, inactive and at-risk slots', () => {
        expect(summariseSlots([
            makeSlot(),
            makeSlot({ active: false, wal_status: 'extended' }),
            makeSlot({ active: false, wal_status: 'lost' }),
            makeSlot({ active: null, wal_status: null }),
            makeSlot({ wal_status: 'unreserved' }),
        ])).toEqual({ total: 5, active: 2, inactive: 2, atRisk: 3 });
    });

    it('returns zeros for no slots', () => {
        expect(summariseSlots([])).toEqual({
            total: 0, active: 0, inactive: 0, atRisk: 0,
        });
    });
});

describe('getReplicationContext', () => {
    it('returns empty lists without cluster data', () => {
        expect(getReplicationContext(null, 1)).toEqual({
            upstream: [], downstream: [],
        });
        expect(getReplicationContext(undefined, 1)).toEqual({
            upstream: [], downstream: [],
        });
    });

    it('tolerates groups with null clusters', () => {
        const data: ClusterGroup[] = [
            { id: 'g', name: 'Empty', clusters: null },
        ];
        expect(getReplicationContext(data, 1)).toEqual({
            upstream: [], downstream: [],
        });
    });

    it('lists the servers this one replicates from', () => {
        const data = makeClusterData([
            makeServer(1, 'primary'),
            makeServer(2, 'standby', [rel(1, 'primary', 'streams_from')]),
        ]);
        expect(getReplicationContext(data, 2)).toEqual({
            upstream: [
                { serverId: 1, serverName: 'primary', label: 'Streams from' },
            ],
            downstream: [],
        });
    });

    it('lists standbys and subscribers, including nested servers', () => {
        const data = makeClusterData([
            makeServer(1, 'primary', [], [
                makeServer(2, 'standby', [rel(1, 'primary', 'streams_from')]),
            ]),
            makeServer(3, 'subscriber', [rel(1, 'primary', 'subscribes_to')]),
            makeServer(4, 'peer', [rel(1, 'primary', 'replicates_with')]),
        ]);
        expect(getReplicationContext(data, 1)).toEqual({
            upstream: [],
            downstream: [
                { serverId: 2, serverName: 'standby', label: 'Standby' },
                { serverId: 3, serverName: 'subscriber', label: 'Subscriber' },
            ],
        });
    });

    it('deduplicates repeated relationships', () => {
        const data = makeClusterData([
            makeServer(1, 'node1', [
                rel(2, 'node2', 'replicates_with'),
                rel(2, 'node2', 'replicates_with'),
            ]),
            makeServer(2, 'node2', [rel(1, 'node1', 'replicates_with')]),
        ]);
        expect(getReplicationContext(data, 1)).toEqual({
            upstream: [
                { serverId: 2, serverName: 'node2', label: 'Replicates with' },
            ],
            downstream: [],
        });
    });

    it('handles servers without relationships', () => {
        const data = makeClusterData([{ id: 1, name: 'alone' }]);
        expect(getReplicationContext(data, 1)).toEqual({
            upstream: [], downstream: [],
        });
    });
});
