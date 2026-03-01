/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
    Box,
    Typography,
    Button,
    CircularProgress,
    Alert,
    Autocomplete,
    TextField,
    MenuItem,
    IconButton,
    Divider,
    List,
    ListItem,
    ListItemText,
    ListItemSecondaryAction,
    Chip,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogContentText,
    DialogActions,
} from '@mui/material';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import { apiGet, apiPost, apiPut, apiDelete } from '../utils/apiClient';
import TopologyDiagram from './Dashboard/ClusterDashboard/topology/TopologyDiagram';
import { ClusterServer } from '../contexts/ClusterDataContext';
import type {
    NodeRelationship,
    RelationshipInput,
    ClusterServerInfo,
} from './ServerDialog/ServerDialog.types';
import { SELECT_FIELD_DEFAULT_BG_SX } from './shared/formStyles';

/**
 * Replication role options based on cluster replication type.
 */
function getRolesForType(
    replicationType: string | null | undefined,
): { value: string; label: string }[] {
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
function getRelationshipTypeForReplication(
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
function getRelationshipLabel(relationshipType: string): string {
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
 * Unassigned connection available for adding to a cluster.
 */
interface UnassignedConnection {
    id: number;
    name: string;
    host: string;
    port: number;
}

interface TopologyPanelProps {
    clusterId: number;
    clusterName: string;
    replicationType: string | null;
    autoClusterKey?: string | null;
    onMembershipChange?: () => void;
}

/**
 * Derives the effective replication type from explicit value or
 * auto_cluster_key prefix.
 */
function deriveReplicationType(
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
    const [clusterServers, setClusterServers] = useState<
        ClusterServerInfo[]
    >([]);
    const [relationships, setRelationships] = useState<NodeRelationship[]>(
        [],
    );
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(
        null,
    );

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
    const [relationshipError, setRelationshipError] = useState<
        string | null
    >(null);

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
     * Fetches connections not assigned to any cluster.
     * The connections API returns cluster_id on each connection;
     * unassigned connections have cluster_id set to null.
     */
    const fetchUnassigned = useCallback(async () => {
        try {
            interface ConnectionListItem {
                id: number;
                name: string;
                host: string;
                port: number;
                cluster_id?: number | null;
            }
            const all = await apiGet<ConnectionListItem[]>(
                '/api/v1/connections',
            );
            const unassigned = (all ?? []).filter(
                (c) => c.cluster_id == null,
            );
            setUnassignedConnections(
                unassigned.map((c) => ({
                    id: c.id,
                    name: c.name,
                    host: c.host,
                    port: c.port,
                })),
            );
        } catch {
            setUnassignedConnections([]);
        }
    }, []);

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
                relationships:
                    relEntries.length > 0 ? relEntries : undefined,
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
                target_connection_id: selectedTargetId as number,
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
                    <TopologyDiagram
                        servers={topologyServers}
                    />
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
                    <Typography
                        variant="body2"
                        sx={{ color: 'text.secondary' }}
                    >
                        No servers in this cluster. Add a server below
                        to begin building the topology.
                    </Typography>
                </Box>
            )}

            {/* Add server section */}
            <Box sx={{ mb: 3 }}>
                <Typography
                    variant="subtitle2"
                    sx={{
                        color: 'text.secondary',
                        textTransform: 'uppercase',
                        fontSize: '0.875rem',
                        letterSpacing: '0.05em',
                        mb: 1.5,
                    }}
                >
                    Add Server
                </Typography>
                <Box
                    sx={{
                        p: 2,
                        border: '1px solid',
                        borderColor: 'divider',
                        borderRadius: 1.5,
                        bgcolor: 'background.paper',
                    }}
                >
                    <Box
                        sx={{
                            display: 'flex',
                            gap: 1.5,
                            alignItems: 'center',
                        }}
                    >
                        <Autocomplete
                            options={unassignedConnections}
                            getOptionLabel={(option) =>
                                `${option.name} (${option.host}:${option.port})`
                            }
                            value={selectedConnection}
                            onChange={(_, val) =>
                                setSelectedConnection(val)
                            }
                            renderInput={(params) => (
                                <TextField
                                    {...params}
                                    label="Server"
                                    margin="dense"
                                    placeholder="Search unassigned servers..."
                                    InputLabelProps={{
                                        ...params.InputLabelProps,
                                        shrink: true,
                                    }}
                                    sx={SELECT_FIELD_DEFAULT_BG_SX}
                                />
                            )}
                            sx={{ flex: 2 }}
                            disabled={addingServer}
                            isOptionEqualToValue={(a, b) =>
                                a.id === b.id
                            }
                        />
                        {roleOptions.length > 0 && (
                            <TextField
                                select
                                margin="dense"
                                sx={{ flex: 1, minWidth: 160, ...SELECT_FIELD_DEFAULT_BG_SX }}
                                disabled={addingServer}
                                label="Role"
                                value={selectedRole}
                                onChange={(e) =>
                                    setSelectedRole(e.target.value)
                                }
                                InputLabelProps={{ shrink: true }}
                            >
                                {roleOptions.map((r) => (
                                    <MenuItem
                                        key={r.value}
                                        value={r.value}
                                    >
                                        {r.label}
                                    </MenuItem>
                                ))}
                            </TextField>
                        )}
                        <Button
                            variant="outlined"
                            startIcon={<AddIcon />}
                            onClick={handleAddServer}
                            disabled={
                                !selectedConnection || addingServer
                            }
                            sx={{
                                textTransform: 'none',
                                whiteSpace: 'nowrap',
                                height: 40,
                            }}
                        >
                            {addingServer ? (
                                <CircularProgress
                                    size={18}
                                    sx={{ color: 'inherit' }}
                                />
                            ) : (
                                'Add'
                            )}
                        </Button>
                    </Box>

                    {/* Current servers list with remove buttons */}
                    {clusterServers.length > 0 && (
                        <Box sx={{ mt: 2 }}>
                            <Divider sx={{ mb: 1 }} />
                            <List dense disablePadding>
                                {clusterServers.map((server) => (
                                    <ListItem
                                        key={server.id}
                                        disableGutters
                                        sx={{ pr: 6 }}
                                    >
                                        <ListItemText
                                            primary={
                                                <Box
                                                    sx={{
                                                        display: 'flex',
                                                        alignItems:
                                                            'center',
                                                        gap: 1,
                                                    }}
                                                >
                                                    <Typography
                                                        variant="body2"
                                                        sx={{
                                                            fontWeight: 500,
                                                        }}
                                                    >
                                                        {server.name}
                                                    </Typography>
                                                    {server.role && (
                                                        <Chip
                                                            label={
                                                                server.role
                                                                    .replace(
                                                                        /_/g,
                                                                        ' ',
                                                                    )
                                                                    .replace(
                                                                        /\b\w/g,
                                                                        (
                                                                            c,
                                                                        ) =>
                                                                            c.toUpperCase(),
                                                                    )
                                                            }
                                                            size="small"
                                                            variant="outlined"
                                                            sx={{
                                                                height: 20,
                                                                fontSize:
                                                                    '0.7rem',
                                                            }}
                                                        />
                                                    )}
                                                </Box>
                                            }
                                            secondary={`${server.host}:${server.port}`}
                                        />
                                        <ListItemSecondaryAction>
                                            <IconButton
                                                edge="end"
                                                size="small"
                                                onClick={() =>
                                                    setRemoveTarget(
                                                        server,
                                                    )
                                                }
                                                aria-label={`Remove ${server.name} from cluster`}
                                                sx={{
                                                    color: 'text.disabled',
                                                    '&:hover': {
                                                        color: 'error.main',
                                                    },
                                                }}
                                            >
                                                <DeleteIcon
                                                    fontSize="small"
                                                />
                                            </IconButton>
                                        </ListItemSecondaryAction>
                                    </ListItem>
                                ))}
                            </List>
                        </Box>
                    )}
                </Box>
            </Box>

            {/* Relationships section */}
            {clusterServers.length >= 2 && (
                <Box>
                    <Typography
                        variant="subtitle2"
                        sx={{
                            color: 'text.secondary',
                            textTransform: 'uppercase',
                            fontSize: '0.875rem',
                            letterSpacing: '0.05em',
                            mb: 1.5,
                        }}
                    >
                        Relationships
                    </Typography>
                    <Box
                        sx={{
                            p: 2,
                            border: '1px solid',
                            borderColor: 'divider',
                            borderRadius: 1.5,
                            bgcolor: 'background.paper',
                        }}
                    >
                        {relationshipError && (
                            <Alert
                                severity="error"
                                sx={{ mb: 1.5, borderRadius: 1 }}
                                onClose={() =>
                                    setRelationshipError(null)
                                }
                            >
                                {relationshipError}
                            </Alert>
                        )}

                        {/* Existing relationships list */}
                        {relationships.length > 0 ? (
                            <List dense disablePadding>
                                {relationships.map((rel) => (
                                    <ListItem
                                        key={rel.id}
                                        disableGutters
                                        sx={{ pr: 6 }}
                                    >
                                        <ListItemText
                                            primary={
                                                <Box
                                                    sx={{
                                                        display: 'flex',
                                                        alignItems:
                                                            'center',
                                                        gap: 1,
                                                    }}
                                                >
                                                    <Typography
                                                        variant="body2"
                                                        component="span"
                                                    >
                                                        <strong>
                                                            {
                                                                rel.source_name
                                                            }
                                                        </strong>
                                                        {' '}
                                                        {getRelationshipLabel(
                                                            rel.relationship_type,
                                                        ).toLowerCase()}
                                                        {' '}
                                                        <strong>
                                                            {
                                                                rel.target_name
                                                            }
                                                        </strong>
                                                    </Typography>
                                                    {rel.is_auto_detected && (
                                                        <Chip
                                                            label="Auto"
                                                            size="small"
                                                            variant="outlined"
                                                            sx={{
                                                                height: 20,
                                                                fontSize:
                                                                    '0.7rem',
                                                            }}
                                                        />
                                                    )}
                                                </Box>
                                            }
                                        />
                                        <ListItemSecondaryAction>
                                            <IconButton
                                                edge="end"
                                                size="small"
                                                onClick={() =>
                                                    handleDeleteRelationship(
                                                        rel.id,
                                                    )
                                                }
                                                aria-label={`Remove relationship between ${rel.source_name} and ${rel.target_name}`}
                                                sx={{
                                                    color: 'text.disabled',
                                                    '&:hover': {
                                                        color: 'error.main',
                                                    },
                                                }}
                                            >
                                                <DeleteIcon
                                                    fontSize="small"
                                                />
                                            </IconButton>
                                        </ListItemSecondaryAction>
                                    </ListItem>
                                ))}
                            </List>
                        ) : (
                            <Typography
                                variant="body2"
                                sx={{
                                    color: 'text.secondary',
                                    mb: 1.5,
                                }}
                            >
                                No relationships defined.
                            </Typography>
                        )}

                        {/* Add relationship controls */}
                        <Divider sx={{ my: 1.5 }} />
                        {allRelationshipsExist ? (
                            <Typography
                                variant="body2"
                                sx={{
                                    fontStyle: 'italic',
                                    color: 'text.secondary',
                                }}
                            >
                                All members already have this relationship
                                type.
                            </Typography>
                        ) : (
                            <Box
                                sx={{
                                    display: 'flex',
                                    gap: 1,
                                    alignItems: 'center',
                                }}
                            >
                                <TextField
                                    select
                                    margin="dense"
                                    sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                                    label="Source"
                                    value={selectedSourceId}
                                    onChange={(e) => {
                                        const val = e.target.value;
                                        setSelectedSourceId(
                                            val === ''
                                                ? ''
                                                : Number(val),
                                        );
                                        setSelectedTargetId('');
                                    }}
                                    InputLabelProps={{ shrink: true }}
                                >
                                    {clusterServers.map((s) => (
                                        <MenuItem
                                            key={s.id}
                                            value={s.id}
                                        >
                                            {s.name}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <TextField
                                    select
                                    margin="dense"
                                    sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                                    label="Target"
                                    value={selectedTargetId}
                                    onChange={(e) => {
                                        const val = e.target.value;
                                        setSelectedTargetId(
                                            val === ''
                                                ? ''
                                                : Number(val),
                                        );
                                    }}
                                    disabled={
                                        selectedSourceId === '' ||
                                        availableTargets.length === 0
                                    }
                                    InputLabelProps={{ shrink: true }}
                                >
                                    {availableTargets.map((s) => (
                                        <MenuItem
                                            key={s.id}
                                            value={s.id}
                                        >
                                            {s.name}
                                        </MenuItem>
                                    ))}
                                </TextField>
                                <TextField
                                    select
                                    margin="dense"
                                    sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                                    label="Type"
                                    value={selectedRelType}
                                    onChange={(e) => {
                                        setSelectedRelType(
                                            e.target.value,
                                        );
                                        setSelectedTargetId('');
                                    }}
                                    InputLabelProps={{ shrink: true }}
                                >
                                    <MenuItem value="streams_from">
                                        Streams from (physical)
                                    </MenuItem>
                                    <MenuItem value="subscribes_to">
                                        Subscribes to (logical)
                                    </MenuItem>
                                    <MenuItem value="replicates_with">
                                        Replicates with (Spock)
                                    </MenuItem>
                                </TextField>
                                <Button
                                    variant="outlined"
                                    startIcon={<AddIcon />}
                                    onClick={handleAddRelationship}
                                    disabled={
                                        selectedSourceId === '' ||
                                        selectedTargetId === '' ||
                                        !selectedRelType
                                    }
                                    sx={{
                                        textTransform: 'none',
                                        whiteSpace: 'nowrap',
                                        height: 40,
                                    }}
                                >
                                    Add
                                </Button>
                            </Box>
                        )}
                    </Box>
                </Box>
            )}

            {/* Remove confirmation dialog */}
            <Dialog
                open={removeTarget !== null}
                onClose={() =>
                    !removingServer && setRemoveTarget(null)
                }
            >
                <DialogTitle>Remove server from cluster</DialogTitle>
                <DialogContent>
                    <DialogContentText>
                        Remove <strong>{removeTarget?.name}</strong>{' '}
                        from <strong>{clusterName}</strong>? The
                        server will become standalone. All
                        relationships involving this server within the
                        cluster will be deleted.
                    </DialogContentText>
                </DialogContent>
                <DialogActions>
                    <Button
                        onClick={() => setRemoveTarget(null)}
                        disabled={removingServer}
                    >
                        Cancel
                    </Button>
                    <Button
                        onClick={handleRemoveServer}
                        variant="contained"
                        color="error"
                        disabled={removingServer}
                    >
                        {removingServer ? (
                            <CircularProgress
                                size={18}
                                sx={{ color: 'inherit' }}
                            />
                        ) : (
                            'Remove'
                        )}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default TopologyPanel;
