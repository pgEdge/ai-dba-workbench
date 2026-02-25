/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback } from 'react';
import {
    Box,
    Typography,
    Button,
    CircularProgress,
    Alert,
    Chip,
    IconButton,
    Autocomplete,
    TextField,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogContentText,
    DialogActions,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
} from '@mui/material';
import {
    Add as AddIcon,
    PersonRemove as RemoveIcon,
} from '@mui/icons-material';
import { apiGet, apiPost, apiDelete } from '../utils/apiClient';
import type { ClusterMemberInfo } from './ServerDialog/ServerDialog.types';

/**
 * Replication role options used when adding a server to a cluster.
 */
const ROLE_OPTIONS = [
    { value: 'binary_primary', label: 'Primary (Binary)' },
    { value: 'binary_standby', label: 'Standby (Binary)' },
    { value: 'spock_node', label: 'Node (Spock)' },
    { value: 'logical_publisher', label: 'Publisher (Logical)' },
    { value: 'logical_subscriber', label: 'Subscriber (Logical)' },
] as const;

/**
 * Unassigned connection available for adding to a cluster.
 */
interface UnassignedConnection {
    id: number;
    name: string;
    host: string;
    port: number;
}

interface MembersPanelProps {
    clusterId: number;
    clusterName: string;
    onMembershipChange?: () => void;
}

/**
 * Returns a human-readable label for a server role value.
 */
function formatRole(role: string | undefined): string {
    if (!role) {
        return 'None';
    }
    const found = ROLE_OPTIONS.find((r) => r.value === role);
    if (found) {
        return found.label;
    }
    // Fall back to a cleaned-up version of the raw value
    return role
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (c) => c.toUpperCase());
}

/**
 * Returns a color for the status chip.
 */
function getStatusColor(
    status: string,
): 'success' | 'warning' | 'error' | 'default' {
    switch (status) {
        case 'online':
            return 'success';
        case 'warning':
            return 'warning';
        case 'offline':
            return 'error';
        default:
            return 'default';
    }
}

/**
 * MembersPanel displays the list of servers in a cluster and
 * provides controls for adding and removing members.
 */
const MembersPanel: React.FC<MembersPanelProps> = ({
    clusterId,
    clusterName,
    onMembershipChange,
}) => {
    const [members, setMembers] = useState<ClusterMemberInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [successMessage, setSuccessMessage] = useState<string | null>(
        null,
    );

    // Add server state
    const [showAddForm, setShowAddForm] = useState(false);
    const [unassignedConnections, setUnassignedConnections] = useState<
        UnassignedConnection[]
    >([]);
    const [selectedConnection, setSelectedConnection] =
        useState<UnassignedConnection | null>(null);
    const [selectedRole, setSelectedRole] = useState<string>('');
    const [addingServer, setAddingServer] = useState(false);

    // Remove confirmation state
    const [removeTarget, setRemoveTarget] =
        useState<ClusterMemberInfo | null>(null);
    const [removingServer, setRemovingServer] = useState(false);

    /**
     * Fetches the current list of cluster members.
     */
    const fetchMembers = useCallback(async () => {
        setError(null);
        try {
            const data = await apiGet<ClusterMemberInfo[]>(
                `/api/v1/clusters/${clusterId}/servers`,
            );
            setMembers(data ?? []);
        } catch (err) {
            setError(
                err instanceof Error
                    ? err.message
                    : 'Failed to load cluster members',
            );
        } finally {
            setLoading(false);
        }
    }, [clusterId]);

    /**
     * Fetches connections that are not assigned to any cluster.
     */
    const fetchUnassigned = useCallback(async () => {
        try {
            // The connections list endpoint returns all connections;
            // filter for those without a cluster assignment.
            interface ConnectionListItem {
                id: number;
                name: string;
                host: string;
                port: number;
                cluster_name?: string | null;
            }
            const all = await apiGet<ConnectionListItem[]>(
                '/api/v1/connections',
            );
            const unassigned = (all ?? []).filter(
                (c) => !c.cluster_name,
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
            // Non-critical; the add form will just show an empty list
            setUnassignedConnections([]);
        }
    }, []);

    useEffect(() => {
        fetchMembers();
    }, [fetchMembers]);

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
            setShowAddForm(false);
            await fetchMembers();
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
            await fetchMembers();
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

    /**
     * Opens the add server form and fetches unassigned connections.
     */
    const handleShowAddForm = () => {
        setShowAddForm(true);
        fetchUnassigned();
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
        <Box sx={{ maxWidth: 800, mx: 'auto' }}>
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

            {/* Header with add button */}
            <Box
                sx={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    mb: 2,
                }}
            >
                <Typography
                    variant="subtitle2"
                    sx={{
                        color: 'text.secondary',
                        textTransform: 'uppercase',
                        fontSize: '0.875rem',
                        letterSpacing: '0.05em',
                    }}
                >
                    Cluster Members
                </Typography>
                <Button
                    size="small"
                    startIcon={<AddIcon />}
                    onClick={handleShowAddForm}
                    sx={{ textTransform: 'none' }}
                >
                    Add server
                </Button>
            </Box>

            {/* Add server form */}
            {showAddForm && (
                <Box
                    sx={{
                        mb: 2,
                        p: 2,
                        border: '1px solid',
                        borderColor: 'divider',
                        borderRadius: 1.5,
                        bgcolor: 'background.paper',
                    }}
                >
                    <Typography
                        variant="body2"
                        sx={{ mb: 1.5, fontWeight: 600 }}
                    >
                        Add a server to this cluster
                    </Typography>
                    <Box
                        sx={{
                            display: 'flex',
                            gap: 1.5,
                            alignItems: 'flex-start',
                        }}
                    >
                        <Autocomplete
                            options={unassignedConnections}
                            getOptionLabel={(option) =>
                                `${option.name} (${option.host}:${option.port})`
                            }
                            value={selectedConnection}
                            onChange={(_, val) => setSelectedConnection(val)}
                            renderInput={(params) => (
                                <TextField
                                    {...params}
                                    label="Server"
                                    placeholder="Search unassigned servers..."
                                    size="small"
                                />
                            )}
                            sx={{ flex: 2 }}
                            disabled={addingServer}
                            isOptionEqualToValue={(a, b) => a.id === b.id}
                        />
                        <FormControl
                            size="small"
                            sx={{ flex: 1, minWidth: 160 }}
                            disabled={addingServer}
                        >
                            <InputLabel>Role</InputLabel>
                            <Select
                                value={selectedRole}
                                onChange={(e) =>
                                    setSelectedRole(e.target.value)
                                }
                                label="Role"
                            >
                                <MenuItem value="">
                                    <em>Auto-detect</em>
                                </MenuItem>
                                {ROLE_OPTIONS.map((r) => (
                                    <MenuItem key={r.value} value={r.value}>
                                        {r.label}
                                    </MenuItem>
                                ))}
                            </Select>
                        </FormControl>
                        <Button
                            variant="contained"
                            size="small"
                            onClick={handleAddServer}
                            disabled={!selectedConnection || addingServer}
                            sx={{
                                textTransform: 'none',
                                whiteSpace: 'nowrap',
                                mt: 0.5,
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
                        <Button
                            size="small"
                            onClick={() => {
                                setShowAddForm(false);
                                setSelectedConnection(null);
                                setSelectedRole('');
                            }}
                            disabled={addingServer}
                            sx={{
                                textTransform: 'none',
                                color: 'text.secondary',
                                mt: 0.5,
                            }}
                        >
                            Cancel
                        </Button>
                    </Box>
                </Box>
            )}

            {/* Members table */}
            {members.length === 0 ? (
                <Typography
                    variant="body2"
                    sx={{
                        color: 'text.secondary',
                        py: 4,
                        textAlign: 'center',
                    }}
                >
                    No servers in this cluster.
                </Typography>
            ) : (
                <TableContainer>
                    <Table size="small">
                        <TableHead>
                            <TableRow>
                                <TableCell>Server</TableCell>
                                <TableCell>Role</TableCell>
                                <TableCell>Status</TableCell>
                                <TableCell>Source</TableCell>
                                <TableCell align="right">Actions</TableCell>
                            </TableRow>
                        </TableHead>
                        <TableBody>
                            {members.map((member) => (
                                <TableRow key={member.id}>
                                    <TableCell>
                                        <Box>
                                            <Typography
                                                variant="body2"
                                                sx={{ fontWeight: 500 }}
                                            >
                                                {member.name}
                                            </Typography>
                                            <Typography
                                                variant="caption"
                                                sx={{
                                                    color: 'text.secondary',
                                                }}
                                            >
                                                {member.host}:{member.port}
                                            </Typography>
                                        </Box>
                                    </TableCell>
                                    <TableCell>
                                        <Typography variant="body2">
                                            {formatRole(member.role)}
                                        </Typography>
                                    </TableCell>
                                    <TableCell>
                                        <Chip
                                            label={member.status}
                                            size="small"
                                            color={getStatusColor(
                                                member.status,
                                            )}
                                            variant="outlined"
                                            sx={{
                                                height: 22,
                                                fontSize: '0.75rem',
                                                textTransform: 'capitalize',
                                            }}
                                        />
                                    </TableCell>
                                    <TableCell>
                                        <Chip
                                            label={
                                                member.membership_source ===
                                                'manual'
                                                    ? 'Manual'
                                                    : 'Auto'
                                            }
                                            size="small"
                                            variant="outlined"
                                            sx={{
                                                height: 22,
                                                fontSize: '0.75rem',
                                            }}
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={() =>
                                                setRemoveTarget(member)
                                            }
                                            aria-label={`Remove ${member.name} from cluster`}
                                            sx={{
                                                color: 'text.disabled',
                                                '&:hover': {
                                                    color: 'error.main',
                                                },
                                            }}
                                        >
                                            <RemoveIcon
                                                sx={{ fontSize: 18 }}
                                            />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </TableContainer>
            )}

            {/* Remove confirmation dialog */}
            <Dialog
                open={removeTarget !== null}
                onClose={() => !removingServer && setRemoveTarget(null)}
            >
                <DialogTitle>Remove server from cluster</DialogTitle>
                <DialogContent>
                    <DialogContentText>
                        Remove <strong>{removeTarget?.name}</strong> from{' '}
                        <strong>{clusterName}</strong>? The server will
                        become standalone. All relationships involving
                        this server within the cluster will be deleted.
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

export default MembersPanel;
