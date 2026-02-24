/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
    Autocomplete,
    TextField,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    Button,
    Alert,
    Box,
    Typography,
    CircularProgress,
    Chip,
    Divider,
    IconButton,
    List,
    ListItem,
    ListItemText,
    ListItemSecondaryAction,
} from '@mui/material';
import { Delete as DeleteIcon, Add as AddIcon } from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { apiGet, apiPut, apiPost, apiDelete } from '../../utils/apiClient';
import {
    ClusterSummary,
    ConnectionClusterInfo,
    NewClusterFormData,
    ClusterFieldsValue,
    NodeRelationship,
    RelationshipInput,
    ClusterServerInfo,
} from './ServerDialog.types';
import {
    textFieldSx,
    sectionLabelSx,
    cancelButtonSx,
    getSaveButtonSx,
} from './ServerDialog.styles';
import TopologyDiagram from '../Dashboard/ClusterDashboard/topology/TopologyDiagram';
import { ClusterServer } from '../../contexts/ClusterDataContext';

/**
 * Sentinel option appended to the cluster autocomplete list.
 */
const CREATE_NEW_SENTINEL = '__create_new__';

interface ClusterFieldsProps {
    mode: 'create' | 'edit';
    serverId?: number;
    value?: ClusterFieldsValue;
    onChange?: (value: ClusterFieldsValue) => void;
}

/**
 * Replication type options displayed in the select dropdown.
 */
const REPLICATION_TYPES = [
    { value: 'binary', label: 'Binary (Physical)' },
    { value: 'spock', label: 'Spock' },
    { value: 'logical', label: 'Logical' },
    { value: 'other', label: 'Other' },
] as const;

/**
 * Returns the available role options based on the replication type.
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
 * Derives the replication type from a cluster's auto_cluster_key.
 * Auto-detected clusters use key prefixes: "sysid:" for binary
 * replication and "spock:" for Spock replication.
 */
function deriveReplicationType(cluster: ClusterSummary): string | null {
    if (cluster.replication_type) {
        return cluster.replication_type;
    }
    if (cluster.auto_cluster_key) {
        if (
            cluster.auto_cluster_key.startsWith('sysid:') ||
            cluster.auto_cluster_key.startsWith('binary:')
        ) {
            return 'binary';
        }
        if (cluster.auto_cluster_key.startsWith('spock:')) {
            return 'spock';
        }
        if (cluster.auto_cluster_key.startsWith('logical:')) {
            return 'logical';
        }
    }
    return null;
}

/**
 * Returns a human-readable label for a replication type value.
 */
function getReplicationTypeLabel(repType: string | null): string {
    if (!repType) {
        return 'Unknown';
    }
    const found = REPLICATION_TYPES.find((rt) => rt.value === repType);
    return found ? found.label : repType;
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
 * Returns an inverted label for an incoming relationship where this
 * server is the target. The label describes how this server relates
 * to the source node that owns the relationship.
 */
function getInverseRelationshipLabel(relationshipType: string): string {
    switch (relationshipType) {
        case 'streams_from':
            return 'Streams to';
        case 'subscribes_to':
            return 'Publishes to';
        default:
            return relationshipType;
    }
}

/**
 * Autocomplete option type that includes both real clusters and the
 * "Create new cluster..." sentinel entry.
 */
interface ClusterOption {
    id: number | typeof CREATE_NEW_SENTINEL;
    name: string;
    replication_type: string | null;
}

/**
 * ClusterFields renders cluster assignment controls for the server
 * dialog.  In edit mode it manages its own state and communicates
 * directly with the API.  In create mode the parent manages the
 * state through value/onChange props.
 */
const ClusterFields: React.FC<ClusterFieldsProps> = ({
    mode,
    serverId,
    value,
    onChange,
}) => {
    const theme = useTheme();
    const isEditMode = mode === 'edit';

    // Cluster list for the autocomplete
    const [clusters, setClusters] = useState<ClusterSummary[]>([]);
    const [loading, setLoading] = useState(false);

    // Track whether data has been fetched to avoid re-fetching on
    // remount when switching tabs in edit mode.
    const hasFetched = useRef(false);

    // Edit-mode local state
    const [clusterInfo, setClusterInfo] =
        useState<ConnectionClusterInfo | null>(null);
    const [selectedClusterId, setSelectedClusterId] = useState<
        number | null
    >(null);
    const [selectedRole, setSelectedRole] = useState<string | null>(null);
    const [creatingNew, setCreatingNew] = useState(false);
    const [newCluster, setNewCluster] = useState<NewClusterFormData>({
        name: '',
        replication_type: '',
    });
    const [saving, setSaving] = useState(false);
    const [saveSuccess, setSaveSuccess] = useState(false);
    const [errorMessage, setErrorMessage] = useState<string | null>(null);

    // Relationship state (edit mode only)
    const [relationships, setRelationships] = useState<NodeRelationship[]>(
        [],
    );
    const [clusterServers, setClusterServers] = useState<
        ClusterServerInfo[]
    >([]);
    const [relationshipsLoading, setRelationshipsLoading] = useState(false);
    const [selectedTargetId, setSelectedTargetId] = useState<number | ''>('');
    const [selectedRelType, setSelectedRelType] = useState<string>('');
    const [relationshipError, setRelationshipError] = useState<
        string | null
    >(null);

    // Topology diagram state (edit mode only)
    const [topologyServers, setTopologyServers] = useState<
        ClusterServer[]
    >([]);

    /**
     * Resolves the effective replication type for role option display.
     * For existing clusters, derives from auto_cluster_key when the
     * explicit replication_type field is null.
     */
    const getEffectiveReplicationType = useCallback((): string | null => {
        if (creatingNew) {
            return newCluster.replication_type || null;
        }

        const currentId = isEditMode ? selectedClusterId : value?.clusterId;
        if (currentId != null) {
            const cluster = clusters.find((c) => c.id === currentId);
            if (cluster) {
                return deriveReplicationType(cluster);
            }
        }

        if (isEditMode && clusterInfo) {
            if (clusterInfo.replication_type) {
                return clusterInfo.replication_type;
            }
            if (clusterInfo.auto_cluster_key) {
                if (
                    clusterInfo.auto_cluster_key.startsWith('sysid:') ||
                    clusterInfo.auto_cluster_key.startsWith('binary:')
                ) {
                    return 'binary';
                }
                if (clusterInfo.auto_cluster_key.startsWith('spock:')) {
                    return 'spock';
                }
                if (clusterInfo.auto_cluster_key.startsWith('logical:')) {
                    return 'logical';
                }
            }
            return null;
        }

        if (!isEditMode && value?.newCluster) {
            return value.newCluster.replication_type || null;
        }

        return null;
    }, [
        creatingNew,
        newCluster.replication_type,
        isEditMode,
        selectedClusterId,
        clusters,
        clusterInfo,
        value?.clusterId,
        value?.newCluster,
    ]);

    const effectiveReplicationType = getEffectiveReplicationType();
    const roleOptions = getRolesForType(effectiveReplicationType);

    // Fetch data on mount (once only)
    const fetchData = useCallback(async () => {
        setLoading(true);
        setErrorMessage(null);
        try {
            if (isEditMode && serverId) {
                const resp = await apiGet<{
                    info: ConnectionClusterInfo;
                    clusters: ClusterSummary[];
                }>(`/api/v1/connections/${serverId}/cluster`);
                setClusterInfo(resp.info);
                setClusters(resp.clusters);
                setSelectedClusterId(resp.info.cluster_id);
                setSelectedRole(resp.info.role);
                setCreatingNew(false);
            } else {
                const resp =
                    await apiGet<ClusterSummary[]>('/api/v1/clusters/list');
                setClusters(resp);
            }
            hasFetched.current = true;
        } catch (err) {
            setErrorMessage(
                err instanceof Error
                    ? err.message
                    : 'Failed to load cluster data',
            );
        } finally {
            setLoading(false);
        }
    }, [isEditMode, serverId]);

    useEffect(() => {
        if (!hasFetched.current) {
            fetchData();
        }
    }, [fetchData]);

    /**
     * Fetches relationships and cluster servers for the relationships
     * section. Only runs in edit mode when a cluster and role are set.
     */
    const fetchRelationships = useCallback(async () => {
        if (!isEditMode || !selectedClusterId || !serverId) {
            return;
        }

        setRelationshipsLoading(true);
        setRelationshipError(null);
        try {
            const [rels, servers] = await Promise.all([
                apiGet<NodeRelationship[]>(
                    `/api/v1/clusters/${selectedClusterId}/relationships`,
                ),
                apiGet<ClusterServerInfo[]>(
                    `/api/v1/clusters/${selectedClusterId}/servers`,
                ),
            ]);
            setRelationships(rels);
            setClusterServers(servers);
        } catch (err) {
            setRelationshipError(
                err instanceof Error
                    ? err.message
                    : 'Failed to load relationships',
            );
        } finally {
            setRelationshipsLoading(false);
        }
    }, [isEditMode, selectedClusterId, serverId]);

    // Fetch relationships when cluster and role are set
    useEffect(() => {
        if (isEditMode && selectedClusterId && selectedRole && serverId) {
            fetchRelationships();
        }
    }, [
        isEditMode,
        selectedClusterId,
        selectedRole,
        serverId,
        fetchRelationships,
    ]);

    /**
     * Fetches the full topology and extracts servers for the
     * selected cluster to display the topology diagram.
     */
    const fetchTopology = useCallback(async () => {
        if (!isEditMode || !selectedClusterId) {
            setTopologyServers([]);
            return;
        }

        try {
            interface TopologyServerInfo {
                id: number;
                name: string;
                description?: string;
                host?: string;
                port?: number;
                status: string;
                role?: string | null;
                primary_role?: string;
                version?: string | null;
                connection_error?: string;
                children?: TopologyServerInfo[];
                relationships?: Array<{
                    target_server_id: number;
                    target_server_name: string;
                    relationship_type: string;
                    is_auto_detected: boolean;
                }>;
            }
            interface TopologyCluster {
                id: string;
                name: string;
                servers: TopologyServerInfo[];
            }
            interface TopologyGroup {
                id: string;
                name: string;
                clusters: TopologyCluster[];
            }

            const groups = await apiGet<TopologyGroup[]>(
                '/api/v1/clusters',
            );

            // Find the cluster containing this server. Topology
            // cluster IDs use varying prefixed formats that do
            // not reliably match the numeric DB cluster ID, so
            // search by server membership instead.
            let matchedServers: ClusterServer[] = [];

            const containsServer = (
                servers: TopologyServerInfo[],
            ): boolean => {
                for (const s of servers) {
                    if (s.id === serverId) return true;
                    if (
                        s.children &&
                        containsServer(s.children)
                    )
                        return true;
                }
                return false;
            };

            for (const group of groups) {
                for (const cluster of group.clusters) {
                    if (containsServer(cluster.servers)) {
                        matchedServers =
                            cluster.servers as ClusterServer[];
                        break;
                    }
                }
                if (matchedServers.length > 0) {
                    break;
                }
            }

            setTopologyServers(matchedServers);
        } catch {
            // Topology is a non-critical enhancement; silently
            // degrade when the fetch fails.
            setTopologyServers([]);
        }
    }, [isEditMode, selectedClusterId]);

    // Fetch topology when cluster selection changes
    useEffect(() => {
        fetchTopology();
    }, [fetchTopology]);

    // Relationships for this server (where it is the source)
    const myRelationships = relationships.filter(
        (r) => r.source_connection_id === serverId,
    );

    // Incoming relationships (where this server is the target),
    // excluding bidirectional replicates_with which are already
    // shown as outgoing from the other node's perspective.
    const incomingRelationships = relationships.filter(
        (r) =>
            r.target_connection_id === serverId &&
            r.relationship_type !== 'replicates_with',
    );

    // Relationship type inferred from the cluster replication type
    const inferredRelType =
        getRelationshipTypeForReplication(effectiveReplicationType);

    // Sync selected relationship type with the inferred default
    useEffect(() => {
        setSelectedRelType(inferredRelType);
    }, [inferredRelType]);

    // Available targets: cluster members excluding this server and
    // servers that already have a relationship of the selected type
    const existingTargetIds = new Set(
        myRelationships
            .filter((r) => r.relationship_type === selectedRelType)
            .map((r) => r.target_connection_id),
    );
    const availableTargets = clusterServers.filter(
        (s) => s.id !== serverId && !existingTargetIds.has(s.id),
    );

    /**
     * Adds a new relationship for this server.
     */
    const handleAddRelationship = async () => {
        if (
            !selectedClusterId ||
            !serverId ||
            selectedTargetId === ''
        ) {
            return;
        }

        setRelationshipError(null);
        try {
            // Build the list of manual relationships plus the new
            // one. Auto-detected rows are preserved server-side
            // and must not be re-sent as manual inserts.
            const existing: RelationshipInput[] = myRelationships
                .filter((r) => !r.is_auto_detected)
                .map((r) => ({
                    target_connection_id: r.target_connection_id,
                    relationship_type: r.relationship_type,
                }));
            existing.push({
                target_connection_id: selectedTargetId as number,
                relationship_type: selectedRelType,
            });

            await apiPut(
                `/api/v1/clusters/${selectedClusterId}/connections/${serverId}/relationships`,
                { relationships: existing },
            );
            setSelectedTargetId('');
            setSelectedRelType(inferredRelType);
            await fetchRelationships();
            await fetchTopology();
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
        if (!selectedClusterId) {
            return;
        }

        setRelationshipError(null);
        try {
            await apiDelete(
                `/api/v1/clusters/${selectedClusterId}/relationships/${relationshipId}`,
            );
            await fetchRelationships();
            await fetchTopology();
        } catch (err) {
            setRelationshipError(
                err instanceof Error
                    ? err.message
                    : 'Failed to remove relationship',
            );
        }
    };

    // Build autocomplete options with derived replication types
    const autocompleteOptions: ClusterOption[] = [
        ...clusters.map((c) => ({
            id: c.id as number | typeof CREATE_NEW_SENTINEL,
            name: c.name,
            replication_type: deriveReplicationType(c),
        })),
        {
            id: CREATE_NEW_SENTINEL,
            name: 'Create new cluster...',
            replication_type: null,
        },
    ];

    // Currently selected option
    const currentId = isEditMode ? selectedClusterId : value?.clusterId;
    const isCreatingNewInCreate =
        !isEditMode && value?.newCluster !== undefined;
    const selectedOption = creatingNew || isCreatingNewInCreate
        ? autocompleteOptions.find((o) => o.id === CREATE_NEW_SENTINEL) ??
          null
        : autocompleteOptions.find((o) => o.id === currentId) ?? null;

    // Current role
    const currentRole = isEditMode ? selectedRole : value?.role;

    // Whether an existing cluster is selected (not creating new)
    const existingClusterSelected =
        selectedOption !== null &&
        selectedOption.id !== CREATE_NEW_SENTINEL &&
        !creatingNew &&
        !isCreatingNewInCreate;

    // Whether to show the relationships section
    const showRelationships =
        isEditMode &&
        existingClusterSelected &&
        selectedClusterId !== null &&
        selectedRole !== null &&
        selectedRole !== '';

    // Handle autocomplete change
    const handleClusterChange = (
        _: React.SyntheticEvent,
        option: ClusterOption | null,
    ) => {
        setSaveSuccess(false);
        setErrorMessage(null);

        if (!option) {
            // Cleared
            if (isEditMode) {
                setSelectedClusterId(null);
                setSelectedRole(null);
                setCreatingNew(false);
            } else {
                onChange?.({
                    clusterId: null,
                    role: null,
                    clusterOverride: false,
                });
            }
            return;
        }

        if (option.id === CREATE_NEW_SENTINEL) {
            if (isEditMode) {
                setSelectedClusterId(null);
                setSelectedRole(null);
                setCreatingNew(true);
                setNewCluster({ name: '', replication_type: '' });
            } else {
                onChange?.({
                    clusterId: null,
                    role: null,
                    clusterOverride: true,
                    newCluster: { name: '', replication_type: '' },
                });
            }
            return;
        }

        // Selected an existing cluster
        if (isEditMode) {
            setSelectedClusterId(option.id as number);
            setSelectedRole(null);
            setCreatingNew(false);
        } else {
            onChange?.({
                clusterId: option.id as number,
                role: null,
                clusterOverride: true,
            });
        }
    };

    // Handle role change
    const handleRoleChange = (role: string) => {
        setSaveSuccess(false);
        if (isEditMode) {
            setSelectedRole(role);
        } else {
            onChange?.({
                clusterId: value?.clusterId ?? null,
                role,
                clusterOverride: true,
                newCluster: value?.newCluster,
            });
        }
    };

    // Handle new cluster fields (create new inline)
    const handleNewClusterFieldChange = (
        field: keyof NewClusterFormData,
        fieldValue: string,
    ) => {
        setSaveSuccess(false);
        if (isEditMode) {
            const updated = { ...newCluster, [field]: fieldValue };
            setNewCluster(updated);
            // Reset role when replication type changes
            if (field === 'replication_type') {
                setSelectedRole(null);
            }
        } else {
            const updated = {
                ...(value?.newCluster ?? {
                    name: '',
                    replication_type: '',
                }),
                [field]: fieldValue,
            };
            onChange?.({
                clusterId: null,
                role: field === 'replication_type' ? null : value?.role ?? null,
                clusterOverride: true,
                newCluster: updated,
            });
        }
    };

    // Edit mode: save handler
    const handleSave = async () => {
        if (!serverId) {
            return;
        }

        setSaving(true);
        setSaveSuccess(false);
        setErrorMessage(null);

        try {
            let clusterId = selectedClusterId;

            // If creating a new cluster, create it first
            if (creatingNew) {
                if (!newCluster.name.trim()) {
                    setErrorMessage('Cluster name is required.');
                    setSaving(false);
                    return;
                }
                if (!newCluster.replication_type) {
                    setErrorMessage('Replication type is required.');
                    setSaving(false);
                    return;
                }

                const created = await apiPost<{ id: number; name: string }>(
                    '/api/v1/clusters',
                    {
                        name: newCluster.name.trim(),
                        replication_type: newCluster.replication_type,
                    },
                );
                clusterId = created.id;
            }

            // Assign the connection to the cluster
            await apiPut(`/api/v1/connections/${serverId}/cluster`, {
                cluster_id: clusterId,
                role: selectedRole,
                cluster_override: true,
            });

            setSaveSuccess(true);
            // Force re-fetch after save to get updated data
            hasFetched.current = false;
            await fetchData();
        } catch (err) {
            setErrorMessage(
                err instanceof Error
                    ? err.message
                    : 'Failed to save cluster assignment',
            );
        } finally {
            setSaving(false);
        }
    };

    // Edit mode: clear override
    const handleClearOverride = async () => {
        if (!serverId) {
            return;
        }

        setSaving(true);
        setSaveSuccess(false);
        setErrorMessage(null);

        try {
            await apiPut(`/api/v1/connections/${serverId}/cluster`, {
                cluster_id: null,
                role: null,
                cluster_override: false,
            });

            setSaveSuccess(true);
            // Force re-fetch after clearing override
            hasFetched.current = false;
            await fetchData();
        } catch (err) {
            setErrorMessage(
                err instanceof Error
                    ? err.message
                    : 'Failed to clear override',
            );
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
                <CircularProgress size={28} />
            </Box>
        );
    }

    // Determine whether to show "Create new" inline fields
    const showNewClusterFields = creatingNew || isCreatingNewInCreate;

    // Determine new cluster data for display
    const displayNewCluster = isEditMode
        ? newCluster
        : value?.newCluster ?? { name: '', replication_type: '' };

    return (
        <Box>
            <Typography variant="subtitle2" sx={sectionLabelSx}>
                Cluster
            </Typography>

            {/* Info banner: auto-detected cluster in edit mode */}
            {isEditMode &&
                clusterInfo &&
                !clusterInfo.cluster_override &&
                clusterInfo.cluster_id !== null && (
                <Alert severity="info" sx={{ mb: 2, fontSize: '0.875rem' }}>
                    Cluster membership was auto-detected. Setting a manual
                    override prevents auto-detection from removing this
                    server from the cluster.
                </Alert>
            )}

            {errorMessage && (
                <Alert
                    severity="error"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => setErrorMessage(null)}
                >
                    {errorMessage}
                </Alert>
            )}

            {saveSuccess && (
                <Alert
                    severity="success"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => setSaveSuccess(false)}
                >
                    Cluster assignment saved successfully.
                </Alert>
            )}

            {/* Topology diagram (edit mode with an assigned cluster) */}
            {isEditMode &&
                selectedClusterId !== null &&
                topologyServers.length > 0 && (
                <Box
                    sx={{
                        mb: 2,
                        border: '1px solid',
                        borderColor: 'divider',
                        borderRadius: 1.5,
                        p: 1,
                        bgcolor: 'background.paper',
                    }}
                >
                    <TopologyDiagram
                        servers={topologyServers}
                        highlightServerId={serverId}
                        maxWidth={650}
                    />
                </Box>
            )}

            {/* Cluster autocomplete */}
            <Autocomplete
                options={autocompleteOptions}
                getOptionLabel={(option) => option.name}
                value={selectedOption}
                onChange={handleClusterChange}
                isOptionEqualToValue={(option, val) => option.id === val.id}
                renderOption={(props, option) => (
                    <li {...props} key={String(option.id)}>
                        <Box>
                            <Typography
                                sx={{
                                    fontSize: '0.875rem',
                                    fontStyle:
                                        option.id === CREATE_NEW_SENTINEL
                                            ? 'italic'
                                            : 'normal',
                                }}
                            >
                                {option.name}
                            </Typography>
                            {option.replication_type && (
                                <Typography
                                    variant="caption"
                                    sx={{ color: 'text.secondary' }}
                                >
                                    {getReplicationTypeLabel(
                                        option.replication_type,
                                    )}
                                </Typography>
                            )}
                        </Box>
                    </li>
                )}
                renderInput={(params) => (
                    <TextField
                        {...params}
                        label="Cluster"
                        placeholder="Search clusters..."
                        margin="dense"
                        sx={textFieldSx}
                    />
                )}
                disabled={saving}
                sx={{ mt: 1 }}
            />

            {/* Read-only replication type for existing cluster */}
            {existingClusterSelected && effectiveReplicationType && (
                <TextField
                    fullWidth
                    label="Replication Type"
                    value={getReplicationTypeLabel(effectiveReplicationType)}
                    margin="dense"
                    sx={{ ...textFieldSx, mt: 1 }}
                    slotProps={{
                        input: {
                            readOnly: true,
                        },
                    }}
                />
            )}

            {/* New cluster inline fields */}
            {showNewClusterFields && (
                <Box sx={{ mt: 1 }}>
                    <TextField
                        fullWidth
                        label="Cluster Name"
                        value={displayNewCluster.name}
                        onChange={(e) =>
                            handleNewClusterFieldChange(
                                'name',
                                e.target.value,
                            )
                        }
                        required
                        disabled={saving}
                        margin="dense"
                        sx={textFieldSx}
                    />

                    <FormControl
                        fullWidth
                        margin="dense"
                        disabled={saving}
                        sx={{ mt: 1 }}
                    >
                        <InputLabel
                            sx={{
                                '&.Mui-focused': {
                                    color: 'primary.main',
                                },
                            }}
                        >
                            Replication Type
                        </InputLabel>
                        <Select
                            value={displayNewCluster.replication_type}
                            onChange={(e) =>
                                handleNewClusterFieldChange(
                                    'replication_type',
                                    e.target.value,
                                )
                            }
                            label="Replication Type"
                            sx={{ borderRadius: 1 }}
                        >
                            {REPLICATION_TYPES.map((rt) => (
                                <MenuItem key={rt.value} value={rt.value}>
                                    {rt.label}
                                </MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                </Box>
            )}

            {/* Role dropdown - shown when a cluster is selected or being
                created, and replication type is known */}
            {roleOptions.length > 0 && (selectedOption || showNewClusterFields) && (
                <FormControl
                    fullWidth
                    margin="dense"
                    disabled={saving}
                    sx={{ mt: 1 }}
                >
                    <InputLabel
                        sx={{
                            '&.Mui-focused': { color: 'primary.main' },
                        }}
                    >
                        Role
                    </InputLabel>
                    <Select
                        value={currentRole ?? ''}
                        onChange={(e) => handleRoleChange(e.target.value)}
                        label="Role"
                        sx={{ borderRadius: 1 }}
                    >
                        {roleOptions.map((ro) => (
                            <MenuItem key={ro.value} value={ro.value}>
                                {ro.label}
                            </MenuItem>
                        ))}
                    </Select>
                </FormControl>
            )}

            {/* Relationships section (edit mode only) */}
            {showRelationships && (
                <Box sx={{ mt: 2 }}>
                    <Divider sx={{ mb: 2 }} />
                    <Typography variant="subtitle2" sx={sectionLabelSx}>
                        Relationships
                    </Typography>

                    {relationshipError && (
                        <Alert
                            severity="error"
                            sx={{ mb: 1, borderRadius: 1 }}
                            onClose={() => setRelationshipError(null)}
                        >
                            {relationshipError}
                        </Alert>
                    )}

                    {relationshipsLoading ? (
                        <Box
                            sx={{
                                display: 'flex',
                                justifyContent: 'center',
                                py: 2,
                            }}
                        >
                            <CircularProgress size={24} />
                        </Box>
                    ) : (
                        <>
                            {/* Incoming relationships (read-only) */}
                            {incomingRelationships.length > 0 && (
                                <List dense disablePadding>
                                    {incomingRelationships.map((rel) => (
                                        <ListItem
                                            key={`incoming-${rel.id}`}
                                            disableGutters
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
                                                            sx={{
                                                                color: 'text.secondary',
                                                            }}
                                                        >
                                                            {getInverseRelationshipLabel(
                                                                rel.relationship_type,
                                                            )}{' '}
                                                            <strong>
                                                                {
                                                                    rel.source_name
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
                                        </ListItem>
                                    ))}
                                </List>
                            )}

                            {/* Current outgoing relationships list */}
                            {myRelationships.length > 0 ? (
                                <List dense disablePadding>
                                    {myRelationships.map((rel) => (
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
                                                            {getRelationshipLabel(
                                                                rel.relationship_type,
                                                            )}{' '}
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
                                                    aria-label={`Remove relationship with ${rel.target_name}`}
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
                                incomingRelationships.length === 0 && (
                                    <Typography
                                        variant="body2"
                                        sx={{
                                            color: 'text.secondary',
                                            mb: 1,
                                        }}
                                    >
                                        No relationships defined.
                                    </Typography>
                                )
                            )}

                            {/* Add relationship controls */}
                            <Box
                                sx={{
                                    display: 'flex',
                                    gap: 1,
                                    alignItems: 'center',
                                    mt: 1,
                                }}
                            >
                                {availableTargets.length > 0 ? (
                                    <FormControl
                                        margin="dense"
                                        sx={{ flex: 2 }}
                                        data-testid="relationship-server-select"
                                    >
                                        <InputLabel
                                            sx={{
                                                '&.Mui-focused': {
                                                    color: 'primary.main',
                                                },
                                            }}
                                        >
                                            Select Server
                                        </InputLabel>
                                        <Select
                                            value={selectedTargetId}
                                            onChange={(e) =>
                                                setSelectedTargetId(
                                                    e.target.value as
                                                        | number
                                                        | '',
                                                )
                                            }
                                            label="Select Server"
                                            sx={{ borderRadius: 1 }}
                                        >
                                            {availableTargets.map((s) => (
                                                <MenuItem
                                                    key={s.id}
                                                    value={s.id}
                                                >
                                                    {s.name}
                                                </MenuItem>
                                            ))}
                                        </Select>
                                    </FormControl>
                                ) : (
                                    <Typography
                                        variant="body2"
                                        sx={{
                                            flex: 2,
                                            color: 'text.secondary',
                                            fontStyle: 'italic',
                                            py: 1,
                                        }}
                                    >
                                        All members already have this
                                        relationship type.
                                    </Typography>
                                )}
                                <FormControl
                                    margin="dense"
                                    sx={{ flex: 1 }}
                                >
                                    <InputLabel
                                        sx={{
                                            '&.Mui-focused': {
                                                color: 'primary.main',
                                            },
                                        }}
                                    >
                                        Type
                                    </InputLabel>
                                    <Select
                                        value={selectedRelType}
                                        onChange={(e) => {
                                            setSelectedRelType(
                                                e.target.value,
                                            );
                                            setSelectedTargetId('');
                                        }}
                                        label="Type"
                                        sx={{ borderRadius: 1 }}
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
                                    </Select>
                                </FormControl>
                                <Button
                                    variant="outlined"
                                    size="small"
                                    startIcon={<AddIcon />}
                                    onClick={handleAddRelationship}
                                    disabled={
                                        selectedTargetId === '' ||
                                        availableTargets.length === 0
                                    }
                                    sx={{
                                        textTransform: 'none',
                                        whiteSpace: 'nowrap',
                                    }}
                                >
                                    Add
                                </Button>
                            </Box>
                        </>
                    )}
                </Box>
            )}

            {/* Edit mode action buttons */}
            {isEditMode && (
                <Box>
                    <Divider sx={{ mt: 2, mb: 2 }} />
                    <Box
                        sx={{
                            display: 'flex',
                            gap: 1,
                            justifyContent: 'flex-end',
                        }}
                    >
                    {clusterInfo?.cluster_override && (
                        <Button
                            onClick={handleClearOverride}
                            disabled={saving}
                            sx={cancelButtonSx}
                        >
                            Clear Override
                        </Button>
                    )}
                    <Button
                        variant="contained"
                        onClick={handleSave}
                        disabled={saving}
                        sx={getSaveButtonSx(theme)}
                    >
                        {saving ? (
                            <CircularProgress
                                size={20}
                                sx={{ color: 'inherit' }}
                                aria-label="Saving"
                            />
                        ) : (
                            'Save'
                        )}
                    </Button>
                    </Box>
                </Box>
            )}
        </Box>
    );
};

export default ClusterFields;
