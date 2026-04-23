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
 * Pure utility functions for topology panel logic.
 */

/**
 * Unassigned connection available for adding to a cluster.
 */
export interface UnassignedConnection {
    id: number;
    name: string;
    host: string;
    port: number;
}

/**
 * Props for the TopologyPanel component.
 */
export interface TopologyPanelProps {
    clusterId: number;
    clusterName: string;
    replicationType: string | null;
    autoClusterKey?: string | null;
    onMembershipChange?: () => void;
}

/**
 * Role option for dropdown selection.
 */
export interface RoleOption {
    value: string;
    label: string;
}

/**
 * Replication role options based on cluster replication type.
 */
export function getRolesForType(
    replicationType: string | null | undefined,
): RoleOption[] {
    switch (replicationType) {
        case 'binary':
            return [
                { value: 'binary_primary', label: 'Primary' },
                { value: 'binary_standby', label: 'Standby' },
            ];
        case 'spock':
            return [
                { value: 'spock_node', label: 'Node' },
                { value: 'binary_standby', label: 'Standby' },
            ];
        case 'logical':
            return [
                { value: 'logical_publisher', label: 'Publisher' },
                { value: 'logical_subscriber', label: 'Subscriber' },
            ];
        case 'other':
            return [
                { value: 'primary', label: 'Primary' },
                { value: 'replica', label: 'Replica' },
                { value: 'node', label: 'Node' },
            ];
        default:
            return [];
    }
}

/**
 * Maps a replication type to the corresponding relationship type.
 */
export function getRelationshipTypeForReplication(
    replicationType: string | null,
): string {
    switch (replicationType) {
        case 'binary':
            return 'streams_from';
        case 'logical':
            return 'subscribes_to';
        case 'spock':
            return 'replicates_with';
        default:
            return 'replicates_with';
    }
}

/**
 * Returns a human-readable label for a relationship type.
 */
export function getRelationshipLabel(relationshipType: string): string {
    switch (relationshipType) {
        case 'streams_from':
            return 'Streams from';
        case 'subscribes_to':
            return 'Subscribes to';
        case 'replicates_with':
            return 'Replicates with';
        default:
            return relationshipType;
    }
}

/**
 * Derives the effective replication type from explicit value or
 * auto_cluster_key prefix.
 */
export function deriveReplicationType(
    replicationType: string | null,
    autoClusterKey?: string | null,
): string | null {
    if (replicationType) {
        return replicationType;
    }
    if (autoClusterKey) {
        if (
            autoClusterKey.startsWith('sysid:') ||
            autoClusterKey.startsWith('binary:')
        ) {
            return 'binary';
        }
        if (autoClusterKey.startsWith('spock:')) {
            return 'spock';
        }
        if (autoClusterKey.startsWith('logical:')) {
            return 'logical';
        }
    }
    return null;
}
