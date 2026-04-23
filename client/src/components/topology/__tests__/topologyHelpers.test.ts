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
import {
    getRolesForType,
    getRelationshipTypeForReplication,
    getRelationshipLabel,
    deriveReplicationType,
} from '../topologyHelpers';

describe('topologyHelpers', () => {
    describe('getRolesForType', () => {
        it('returns binary roles for binary replication type', () => {
            const roles = getRolesForType('binary');
            expect(roles).toEqual([
                { value: 'binary_primary', label: 'Primary' },
                { value: 'binary_standby', label: 'Standby' },
            ]);
        });

        it('returns spock roles for spock replication type', () => {
            const roles = getRolesForType('spock');
            expect(roles).toEqual([
                { value: 'spock_node', label: 'Node' },
                { value: 'binary_standby', label: 'Standby' },
            ]);
        });

        it('returns logical roles for logical replication type', () => {
            const roles = getRolesForType('logical');
            expect(roles).toEqual([
                { value: 'logical_publisher', label: 'Publisher' },
                { value: 'logical_subscriber', label: 'Subscriber' },
            ]);
        });

        it('returns other roles for other replication type', () => {
            const roles = getRolesForType('other');
            expect(roles).toEqual([
                { value: 'primary', label: 'Primary' },
                { value: 'replica', label: 'Replica' },
                { value: 'node', label: 'Node' },
            ]);
        });

        it('returns empty array for null replication type', () => {
            const roles = getRolesForType(null);
            expect(roles).toEqual([]);
        });

        it('returns empty array for undefined replication type', () => {
            const roles = getRolesForType(undefined);
            expect(roles).toEqual([]);
        });

        it('returns empty array for unknown replication type', () => {
            const roles = getRolesForType('unknown');
            expect(roles).toEqual([]);
        });
    });

    describe('getRelationshipTypeForReplication', () => {
        it('returns streams_from for binary replication type', () => {
            expect(getRelationshipTypeForReplication('binary')).toBe(
                'streams_from',
            );
        });

        it('returns subscribes_to for logical replication type', () => {
            expect(getRelationshipTypeForReplication('logical')).toBe(
                'subscribes_to',
            );
        });

        it('returns replicates_with for spock replication type', () => {
            expect(getRelationshipTypeForReplication('spock')).toBe(
                'replicates_with',
            );
        });

        it('returns replicates_with for null replication type', () => {
            expect(getRelationshipTypeForReplication(null)).toBe(
                'replicates_with',
            );
        });

        it('returns replicates_with for unknown replication type', () => {
            expect(getRelationshipTypeForReplication('unknown')).toBe(
                'replicates_with',
            );
        });

        it('returns replicates_with for other replication type', () => {
            expect(getRelationshipTypeForReplication('other')).toBe(
                'replicates_with',
            );
        });
    });

    describe('getRelationshipLabel', () => {
        it('returns "Streams from" for streams_from relationship type', () => {
            expect(getRelationshipLabel('streams_from')).toBe('Streams from');
        });

        it('returns "Subscribes to" for subscribes_to relationship type', () => {
            expect(getRelationshipLabel('subscribes_to')).toBe('Subscribes to');
        });

        it('returns "Replicates with" for replicates_with relationship type', () => {
            expect(getRelationshipLabel('replicates_with')).toBe(
                'Replicates with',
            );
        });

        it('returns the input value for unknown relationship type', () => {
            expect(getRelationshipLabel('custom_relationship')).toBe(
                'custom_relationship',
            );
        });

        it('returns empty string for empty relationship type', () => {
            expect(getRelationshipLabel('')).toBe('');
        });
    });

    describe('deriveReplicationType', () => {
        it('returns explicit replication type when provided', () => {
            expect(deriveReplicationType('binary', null)).toBe('binary');
            expect(deriveReplicationType('spock', null)).toBe('spock');
            expect(deriveReplicationType('logical', null)).toBe('logical');
            expect(deriveReplicationType('other', null)).toBe('other');
        });

        it('returns explicit replication type even with autoClusterKey', () => {
            expect(deriveReplicationType('binary', 'spock:cluster1')).toBe(
                'binary',
            );
        });

        it('derives binary from sysid: prefix', () => {
            expect(deriveReplicationType(null, 'sysid:123456')).toBe('binary');
        });

        it('derives binary from binary: prefix', () => {
            expect(deriveReplicationType(null, 'binary:cluster1')).toBe(
                'binary',
            );
        });

        it('derives spock from spock: prefix', () => {
            expect(deriveReplicationType(null, 'spock:cluster1')).toBe('spock');
        });

        it('derives logical from logical: prefix', () => {
            expect(deriveReplicationType(null, 'logical:cluster1')).toBe(
                'logical',
            );
        });

        it('returns null for unrecognized autoClusterKey prefix', () => {
            expect(deriveReplicationType(null, 'unknown:cluster1')).toBe(null);
        });

        it('returns null when both replicationType and autoClusterKey are null', () => {
            expect(deriveReplicationType(null, null)).toBe(null);
        });

        it('returns null when both replicationType and autoClusterKey are undefined', () => {
            expect(deriveReplicationType(null, undefined)).toBe(null);
        });

        it('returns null when replicationType is null and autoClusterKey is empty', () => {
            expect(deriveReplicationType(null, '')).toBe(null);
        });
    });
});
