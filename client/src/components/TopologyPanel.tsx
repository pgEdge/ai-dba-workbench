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
import { useState, useEffect, useCallback, useMemo } from 'react';
import {
    Box,
    Typography,
    CircularProgress,
    Alert,
} from '@mui/material';
import { apiGet, apiPost, apiPut, apiDelete } from '../utils/apiClient';
import TopologyDiagram from './Dashboard/ClusterDashboard/topology/TopologyDiagram';
import type { ClusterServer } from '../contexts/ClusterDataContext';
import type {
    NodeRelationship,
    RelationshipInput,
    ClusterServerInfo,
} from './ServerDialog/ServerDialog.types';
import {
    getRolesForType,
    getRelationshipTypeForReplication,
    deriveReplicationType,
    ServerManagementSection,
    RelationshipSection,
    RemoveServerDialog,
} from './topology';
import type { UnassignedConnection, TopologyPanelProps } from './topology';

/**
 * TopologyPanel provides a complete cluster topology management
 * interface.  It displays the topology diagram, controls for
 * adding and removing servers, and relationship management.
 */
const TopologyPanel: React.FC<TopologyPanelProps> = ({
    clusterId,
    clusterName,
    replicationType,
    autoClusterKey,
    onMembershipChange,
}) => {
    // Servers and relationships state
    const [clusterServers, setClusterServers] = useState<ClusterServerInfo[]>(
        [],
    );
    const [relationships, setRelationships] = useState<NodeRelationship[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(null);

    // Add server state
    const [unassignedConnections, setUnassignedConnections] = useState<
        UnassignedConnection[]
    >([]);
    const [selectedConnection, setSelectedConnection] =
        useState<UnassignedConnection | null>(null);
    const [selectedRole, setSelectedRole] = useState<string>('');
    const [addingServer, setAddingServer] = useState(false);

    // Remove confirmation state
    const [removeTarget, setRemoveTarget] =
        useState<ClusterServerInfo | null>(null);
    const [removingServer, setRemovingServer] = useState(false);

    // Relationship add state
    const [selectedSourceId, setSelectedSourceId] = useState<number | ''>('');
    const [selectedTargetId, setSelectedTargetId] = useState<number | ''>('');
    const [selectedRelType, setSelectedRelType] = useState<string>('');
    const [relationshipError, setRelationshipError] = useState<string | null>(
        null,
    );

    const effectiveReplicationType = deriveReplicationType(
        replicationType,
        autoClusterKey,
    );
    const roleOptions = getRolesForType(effectiveReplicationType);
    const inferredRelType = getRelationshipTypeForReplication(
        effectiveReplicationType,
    );

    // Sync selected relationship type with the inferred default
    useEffect(() => {
        setSelectedRelType(inferredRelType);
    }, [inferredRelType]);

    /**
     * Fetches cluster servers and relationships.
     */
    const fetchData = useCallback(async () => {
        setError(null);
        try {
            const [servers, rels] = await Promise.all([
                apiGet<ClusterServerInfo[]>(
                    `/api/v1/clusters/${clusterId}/servers`,
                ),
                apiGet<NodeRelationship[]>(
                    `/api/v1/clusters/${clusterId}/relationships`,
                ),
            ]);
            setClusterServers(servers ?? []);
            setRelationships(rels ?? []);
        } catch (err) {
            setError(
                err instanceof Error
                    ? err.message
                    : 'Failed to load cluster topology',
            );
        } finally {
            setLoading(false);
        }
    }, [clusterId]);

    /**
     * Fetches connections available for assignment to this cluster.
     * A connection is available if it is unassigned (cluster_id is
     * null) or belongs to an auto-detected cluster other than the
     * current one. Connections manually assigned to another cluster
     * are excluded because they were intentionally placed.
     */
    const fetchUnassigned = useCallback(async () => {
        try {
            interface ConnectionListItem {
                id: number;
                name: string;
                host: string;
                port: number;
                cluster_id?: number | null;
                membership_source?: string;
            }
            const all = await apiGet<ConnectionListItem[]>(
                '/api/v1/connections',
            );
            const unassigned = (all ?? []).filter(
                (c) =>
                    c.cluster_id == null ||
                    (c.membership_source === 'auto' &&
                        c.cluster_id !== clusterId),
            );
            setUnassignedConnections(
                unassigned.map((c) => ({
                    id: c.id,
                    name: c.name,
                    host: c.host,
                    port: c.port,
                })),
            );
        } catch (err) {
            setUnassignedConnections([]);
            setError(
                err instanceof Error
                    ? err.message
                    : 'Failed to load available servers',
            );
        }
    }, [clusterId]);

    useEffect(() => {
        fetchData();
        fetchUnassigned();
    }, [fetchData, fetchUnassigned]);

    /**
     * Builds ClusterServer objects for the topology diagram from
     * the fetched servers and relationships data.
     */
    const topologyServers: ClusterServer[] = useMemo(() => {
        if (clusterServers.length === 0) {
            return [];
        }

        const relsBySource = new Map<number, NodeRelationship[]>();
        for (const rel of relationships) {
            const list = relsBySource.get(rel.source_connection_id) ?? [];
            list.push(rel);
            relsBySource.set(rel.source_connection_id, list);
        }

        return clusterServers.map((cs) => {
            const serverRels = relsBySource.get(cs.id) ?? [];
            const relEntries = serverRels.map((r) => ({
                target_server_id: r.target_connection_id,
                target_server_name: r.target_name,
                relationship_type: r.relationship_type,
                is_auto_detected: r.is_auto_detected,
            }));

            return {
                id: cs.id,
                name: cs.name,
                host: cs.host,
                port: cs.port,
                status: cs.status,
                role: cs.role ?? null,
                relationships: relEntries.length > 0 ? relEntries : undefined,
            } as ClusterServer;
        });
    }, [clusterServers, relationships]);

    /**
     * Handles adding a server to the cluster.
     */
    const handleAddServer = async () => {
        if (!selectedConnection) {
            return;
        }

        setAddingServer(true);
        setError(null);
        setSuccessMessage(null);
        try {
            await apiPost(`/api/v1/clusters/${clusterId}/servers`, {
                connection_id: selectedConnection.id,
                role: selectedRole || undefined,
            });
            setSuccessMessage(
                `${selectedConnection.name} added to ${clusterName}.`,
            );
            setSelectedConnection(null);
            setSelectedRole('');
            await fetchData();
            await fetchUnassigned();
            onMembershipChange?.();
        } catch (err) {
            setError(
                err instanceof Error
                    ? err.message
                    : 'Failed to add server to cluster',
            );
        } finally {
            setAddingServer(false);
        }
    };

    /**
     * Handles removing a server from the cluster.
     */
    const handleRemoveServer = async () => {
        if (!removeTarget) {
            return;
        }

        setRemovingServer(true);
        setError(null);
        setSuccessMessage(null);
        try {
            await apiDelete(
                `/api/v1/clusters/${clusterId}/servers/${removeTarget.id}`,
            );
            setSuccessMessage(
                `${removeTarget.name} removed from ${clusterName}.`,
            );
            setRemoveTarget(null);
            await fetchData();
            await fetchUnassigned();
            onMembershipChange?.();
        } catch (err) {
            setError(
                err instanceof Error
                    ? err.message
                    : 'Failed to remove server from cluster',
            );
        } finally {
            setRemovingServer(false);
        }
    };

    // Existing relationship source-target pairs for filtering
    const existingRelPairs = useMemo(
        () =>
            new Set(
                relationships.map(
                    (r) =>
                        `${r.source_connection_id}-${r.target_connection_id}-${r.relationship_type}`,
                ),
            ),
        [relationships],
    );

    // Available targets for the selected source and relationship type
    const availableTargets = clusterServers.filter(
        (s) =>
            s.id !== selectedSourceId &&
            selectedSourceId !== '' &&
            !existingRelPairs.has(
                `${selectedSourceId}-${s.id}-${selectedRelType}`,
            ),
    );

    // Check if all relationships of the current type are fully meshed
    // (i.e., every possible source has no available targets)
    const allRelationshipsExist = useMemo(() => {
        if (clusterServers.length < 2) {
            return false;
        }
        // For each server as a potential source, check if it has any available targets
        for (const source of clusterServers) {
            const hasAvailableTarget = clusterServers.some(
                (target) =>
                    target.id !== source.id &&
                    !existingRelPairs.has(
                        `${source.id}-${target.id}-${selectedRelType}`,
                    ),
            );
            if (hasAvailableTarget) {
                // Found a source with available targets, so not fully meshed
                return false;
            }
        }
        // All sources have no available targets
        return true;
    }, [clusterServers, existingRelPairs, selectedRelType]);

    /**
     * Adds a new relationship between two cluster servers.
     */
    const handleAddRelationship = async () => {
        if (
            selectedSourceId === '' ||
            selectedTargetId === '' ||
            !selectedRelType
        ) {
            return;
        }

        setRelationshipError(null);
        try {
            // Fetch existing manual relationships for the source
            // server and append the new one.
            const sourceRels = relationships
                .filter(
                    (r) =>
                        r.source_connection_id === selectedSourceId &&
                        !r.is_auto_detected,
                )
                .map((r) => ({
                    target_connection_id: r.target_connection_id,
                    relationship_type: r.relationship_type,
                }));

            const newRel: RelationshipInput = {
                target_connection_id: selectedTargetId,
                relationship_type: selectedRelType,
            };
            sourceRels.push(newRel);

            await apiPut(
                `/api/v1/clusters/${clusterId}/connections/${selectedSourceId}/relationships`,
                { relationships: sourceRels },
            );
            setSelectedSourceId('');
            setSelectedTargetId('');
            setSelectedRelType(inferredRelType);
            await fetchData();
        } catch (err) {
            setRelationshipError(
                err instanceof Error
                    ? err.message
                    : 'Failed to add relationship',
            );
        }
    };

    /**
     * Removes a single relationship by its ID.
     */
    const handleDeleteRelationship = async (relationshipId: number) => {
        setRelationshipError(null);
        try {
            await apiDelete(
                `/api/v1/clusters/${clusterId}/relationships/${relationshipId}`,
            );
            await fetchData();
        } catch (err) {
            setRelationshipError(
                err instanceof Error
                    ? err.message
                    : 'Failed to remove relationship',
            );
        }
    };

    /**
     * Handles source selection change, resetting target selection.
     */
    const handleSourceChange = (sourceId: number | '') => {
        setSelectedSourceId(sourceId);
        setSelectedTargetId('');
    };

    /**
     * Handles relationship type change, resetting target selection.
     */
    const handleRelTypeChange = (relType: string) => {
        setSelectedRelType(relType);
        setSelectedTargetId('');
    };

    if (loading) {
        return (
            <Box
                sx={{
                    display: 'flex',
                    justifyContent: 'center',
                    py: 6,
                }}
            >
                <CircularProgress size={28} />
            </Box>
        );
    }

    return (
        <Box sx={{ maxWidth: 900, mx: 'auto' }}>
            {error && (
                <Alert
                    severity="error"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => setError(null)}
                >
                    {error}
                </Alert>
            )}

            {successMessage && (
                <Alert
                    severity="success"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => setSuccessMessage(null)}
                >
                    {successMessage}
                </Alert>
            )}

            {/* Topology diagram */}
            {topologyServers.length > 0 && (
                <Box
                    sx={{
                        mb: 3,
                        border: '1px solid',
                        borderColor: 'divider',
                        borderRadius: 1.5,
                        p: 2,
                        bgcolor: 'background.paper',
                    }}
                >
                    <TopologyDiagram servers={topologyServers} />
                </Box>
            )}

            {topologyServers.length === 0 && (
                <Box
                    sx={{
                        mb: 3,
                        p: 4,
                        border: '1px dashed',
                        borderColor: 'divider',
                        borderRadius: 1.5,
                        textAlign: 'center',
                    }}
                >
                    <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                        No servers in this cluster. Add a server below to begin
                        building the topology.
                    </Typography>
                </Box>
            )}

            {/* Add server section */}
            <ServerManagementSection
                unassignedConnections={unassignedConnections}
                selectedConnection={selectedConnection}
                selectedRole={selectedRole}
                roleOptions={roleOptions}
                addingServer={addingServer}
                clusterServers={clusterServers}
                onConnectionChange={setSelectedConnection}
                onRoleChange={setSelectedRole}
                onAddServer={handleAddServer}
                onRemoveTarget={setRemoveTarget}
            />

            {/* Relationships section */}
            {clusterServers.length >= 2 && (
                <RelationshipSection
                    relationships={relationships}
                    clusterServers={clusterServers}
                    selectedSourceId={selectedSourceId}
                    selectedTargetId={selectedTargetId}
                    selectedRelType={selectedRelType}
                    relationshipError={relationshipError}
                    onSourceChange={handleSourceChange}
                    onTargetChange={setSelectedTargetId}
                    onRelTypeChange={handleRelTypeChange}
                    onAddRelationship={handleAddRelationship}
                    onDeleteRelationship={handleDeleteRelationship}
                    onClearError={() => setRelationshipError(null)}
                    availableTargets={availableTargets}
                    allRelationshipsExist={allRelationshipsExist}
                />
            )}

            {/* Remove confirmation dialog */}
            <RemoveServerDialog
                server={removeTarget}
                clusterName={clusterName}
                removing={removingServer}
                onConfirm={handleRemoveServer}
                onCancel={() => setRemoveTarget(null)}
            />
        </Box>
    );
};

export default TopologyPanel;
