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
    MenuItem,
    Button,
    Alert,
    Box,
    Typography,
    CircularProgress,
    Chip,
} from '@mui/material';
import {
    Settings as SettingsIcon,
} from '@mui/icons-material';
import { apiGet } from '../../utils/apiClient';
import {
    ClusterSummary,
    ConnectionClusterInfo,
    NewClusterFormData,
    ClusterFieldsValue,
} from './ServerDialog.types';
import { sectionLabelSx } from './ServerDialog.styles';
import { getSelectFieldSx } from '../shared/formStyles';

/**
 * Sentinel option appended to the cluster autocomplete list.
 */
const CREATE_NEW_SENTINEL = '__create_new__';

interface ClusterFieldsProps {
    mode: 'create' | 'edit';
    serverId?: number;
    value?: ClusterFieldsValue;
    onChange?: (value: ClusterFieldsValue) => void;
    onOpenClusterConfig?: (clusterId: number, clusterName: string) => void;
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
 * Returns a human-readable label for a server role value.
 */
function getRoleLabel(role: string | null): string {
    if (!role) {
        return 'Not assigned';
    }
    const allRoles: Record<string, string> = {
        binary_primary: 'Primary',
        binary_standby: 'Standby',
        spock_node: 'Node',
        logical_publisher: 'Publisher',
        logical_subscriber: 'Subscriber',
        primary: 'Primary',
        replica: 'Replica',
        node: 'Node',
    };
    return allRoles[role] ?? role;
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
    onOpenClusterConfig,
}) => {
    const isEditMode = mode === 'edit';
    const selectFieldSx = getSelectFieldSx(isEditMode ? 'background.default' : 'background.paper');

    // Cluster list for the autocomplete (create mode)
    const [clusters, setClusters] = useState<ClusterSummary[]>([]);
    const [loading, setLoading] = useState(false);

    // Track whether data has been fetched to avoid re-fetching on
    // remount when switching tabs in edit mode.
    const hasFetched = useRef(false);

    // Edit-mode local state (read-only info)
    const [clusterInfo, setClusterInfo] =
        useState<ConnectionClusterInfo | null>(null);
    const [errorMessage, setErrorMessage] = useState<string | null>(null);

    // Create-mode local state
    const [selectedClusterId, setSelectedClusterId] = useState<
        number | null
    >(null);
    const [selectedRole, setSelectedRole] = useState<string | null>(null);
    const [creatingNew, setCreatingNew] = useState(false);
    const [newCluster] = useState<NewClusterFormData>({
        name: '',
        replication_type: '',
    });
    const [saving] = useState(false);
    const [saveSuccess, setSaveSuccess] = useState(false);

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
                setClusters(resp.clusters ?? []);
                setSelectedClusterId(resp.info.cluster_id);
                setSelectedRole(resp.info.role);
                setCreatingNew(false);
            } else {
                const resp =
                    await apiGet<ClusterSummary[]>('/api/v1/clusters/list');
                setClusters(resp ?? []);
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

    // Currently selected option (create mode only)
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

    // Handle autocomplete change (create mode)
    const handleClusterChange = (
        _: React.SyntheticEvent,
        option: ClusterOption | null,
    ) => {
        setSaveSuccess(false);
        setErrorMessage(null);

        if (!option) {
            onChange?.({
                clusterId: null,
                role: null,
                membershipSource: 'auto',
            });
            return;
        }

        if (option.id === CREATE_NEW_SENTINEL) {
            onChange?.({
                clusterId: null,
                role: null,
                membershipSource: 'manual',
                newCluster: { name: '', replication_type: '' },
            });
            return;
        }

        onChange?.({
            clusterId: option.id as number,
            role: null,
            membershipSource: 'manual',
        });
    };

    // Handle role change (create mode)
    const handleRoleChange = (role: string) => {
        setSaveSuccess(false);
        onChange?.({
            clusterId: value?.clusterId ?? null,
            role,
            membershipSource: 'manual',
            newCluster: value?.newCluster,
        });
    };

    // Handle new cluster fields (create new inline, create mode only)
    const handleNewClusterFieldChange = (
        field: keyof NewClusterFormData,
        fieldValue: string,
    ) => {
        setSaveSuccess(false);
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
            membershipSource: 'manual',
            newCluster: updated,
        });
    };

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
                <CircularProgress size={28} />
            </Box>
        );
    }

    // ---------------------------------------------------------------
    // Edit mode: read-only cluster information with link to config
    // ---------------------------------------------------------------
    if (isEditMode) {
        const hasCluster = clusterInfo && clusterInfo.cluster_id !== null;
        const membershipLabel =
            clusterInfo?.membership_source === 'auto'
                ? 'Auto-detected'
                : 'Manual';
        const roleLabel = getRoleLabel(clusterInfo?.role ?? null);

        return (
            <Box>
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Cluster
                </Typography>

                {errorMessage && (
                    <Alert
                        severity="error"
                        sx={{ mb: 2, borderRadius: 1 }}
                        onClose={() => setErrorMessage(null)}
                    >
                        {errorMessage}
                    </Alert>
                )}

                {hasCluster ? (
                    <Box>
                        <Box
                            sx={{
                                display: 'grid',
                                gridTemplateColumns: '140px 1fr',
                                gap: 1.5,
                                mb: 2,
                            }}
                        >
                            <Typography
                                variant="body2"
                                sx={{ color: 'text.secondary' }}
                            >
                                Cluster
                            </Typography>
                            <Typography variant="body2">
                                {clusterInfo.cluster_name}
                            </Typography>

                            <Typography
                                variant="body2"
                                sx={{ color: 'text.secondary' }}
                            >
                                Replication type
                            </Typography>
                            <Typography variant="body2">
                                {getReplicationTypeLabel(
                                    effectiveReplicationType,
                                )}
                            </Typography>

                            <Typography
                                variant="body2"
                                sx={{ color: 'text.secondary' }}
                            >
                                Role
                            </Typography>
                            <Typography variant="body2">
                                {roleLabel}
                            </Typography>

                            <Typography
                                variant="body2"
                                sx={{ color: 'text.secondary' }}
                            >
                                Membership
                            </Typography>
                            <Box
                                sx={{
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 1,
                                }}
                            >
                                <Typography variant="body2">
                                    {membershipLabel}
                                </Typography>
                                <Chip
                                    label={membershipLabel}
                                    size="small"
                                    variant="outlined"
                                    sx={{
                                        height: 20,
                                        fontSize: '0.7rem',
                                    }}
                                />
                            </Box>
                        </Box>

                        <Button
                            variant="outlined"
                            startIcon={<SettingsIcon />}
                            onClick={() => {
                                if (
                                    onOpenClusterConfig &&
                                    clusterInfo.cluster_id !== null
                                ) {
                                    onOpenClusterConfig(
                                        clusterInfo.cluster_id,
                                        clusterInfo.cluster_name ?? '',
                                    );
                                }
                            }}
                            sx={{ textTransform: 'none' }}
                        >
                            Configure Cluster
                        </Button>
                    </Box>
                ) : (
                    <Box>
                        <Typography
                            variant="body2"
                            sx={{ color: 'text.secondary', mb: 1 }}
                        >
                            This server is not assigned to a cluster.
                        </Typography>
                        <Typography
                            variant="caption"
                            sx={{ color: 'text.disabled' }}
                        >
                            Use the cluster configuration dialog to add
                            this server to a cluster.
                        </Typography>
                    </Box>
                )}
            </Box>
        );
    }

    // ---------------------------------------------------------------
    // Create mode: full cluster assignment UI
    // ---------------------------------------------------------------

    // Determine whether to show "Create new" inline fields
    const showNewClusterFields = isCreatingNewInCreate;

    // Determine new cluster data for display
    const displayNewCluster =
        value?.newCluster ?? { name: '', replication_type: '' };

    return (
        <Box>
            <Typography variant="subtitle2" sx={sectionLabelSx}>
                Cluster
            </Typography>

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
                        InputLabelProps={{
                            ...params.InputLabelProps,
                            shrink: true,
                        }}
                        sx={selectFieldSx}
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
                    InputLabelProps={{ shrink: true }}
                    sx={{ ...selectFieldSx, mt: 1 }}
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
                        InputLabelProps={{ shrink: true }}
                        sx={selectFieldSx}
                    />

                    <TextField
                        select
                        fullWidth
                        label="Replication Type"
                        value={displayNewCluster.replication_type}
                        onChange={(e) =>
                            handleNewClusterFieldChange(
                                'replication_type',
                                e.target.value,
                            )
                        }
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                        sx={{ mt: 1, ...selectFieldSx }}
                    >
                        {REPLICATION_TYPES.map((rt) => (
                            <MenuItem key={rt.value} value={rt.value}>
                                {rt.label}
                            </MenuItem>
                        ))}
                    </TextField>
                </Box>
            )}

            {/* Role dropdown - shown when a cluster is selected or being
                created, and replication type is known */}
            {roleOptions.length > 0 && (selectedOption || showNewClusterFields) && (
                <TextField
                    select
                    fullWidth
                    label="Role"
                    value={currentRole ?? ''}
                    onChange={(e) => handleRoleChange(e.target.value)}
                    disabled={saving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={{ mt: 1, ...selectFieldSx }}
                >
                    {roleOptions.map((ro) => (
                        <MenuItem key={ro.value} value={ro.value}>
                            {ro.label}
                        </MenuItem>
                    ))}
                </TextField>
            )}
        </Box>
    );
};

export default ClusterFields;
