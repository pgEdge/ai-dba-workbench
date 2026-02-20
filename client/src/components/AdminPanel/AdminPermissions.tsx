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
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Paper,
    Button,
    IconButton,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Autocomplete,
    TextField,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import { apiGet, apiPost, apiDelete } from '../../utils/apiClient';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    sectionHeaderSx,
    sectionTitleSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getTextButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
    getFocusedLabelSx,
} from './styles';

const API_BASE_URL = '/api/v1';

const PERMISSION_TYPES = [
    { value: 'manage_blackouts', label: 'Manage Blackouts' },
    { value: 'manage_connections', label: 'Manage Connections' },
    { value: 'manage_groups', label: 'Manage Groups' },
    { value: 'manage_permissions', label: 'Manage Permissions' },
    { value: 'manage_token_scopes', label: 'Manage Token Scopes' },
    { value: 'manage_users', label: 'Manage Users' },
    { value: 'manage_probes', label: 'Manage Probes' },
    { value: 'manage_alert_rules', label: 'Manage Alert Rules' },
    { value: 'manage_notification_channels', label: 'Manage Notification Channels' },
];

interface RbacGroup {
    id: number;
    name: string;
}

interface McpPermission {
    identifier?: string;
    privilege?: string;
    item_type?: string;
    _isAll?: boolean;
}

interface ConnPermission {
    connection_id: number;
    access_level?: string;
}

interface Connection {
    id: number;
    name: string;
}

const mcpTypeLabel = (itemType: string | undefined): string => {
    switch (itemType) {
        case 'tool': return 'Tool';
        case 'resource': return 'Resource';
        case 'prompt': return 'Prompt';
        default: return 'API';
    }
};

const formatMcpName = (permission: McpPermission | string): string => {
    if (typeof permission === 'string') {
        if (permission === '*') {return 'All MCP Privileges';}
        return permission;
    }
    const name = permission.identifier || permission.privilege || '';
    if (name === '*') {return 'All MCP Privileges';}
    const type = permission.item_type;
    if (type) {return `${mcpTypeLabel(type)}: ${name}`;}
    return name;
};

const AdminPermissions: React.FC = () => {
    const theme = useTheme();
    const { user } = useAuth();
    const isSuperuser = !!user?.isSuperuser;

    const [groups, setGroups] = useState<RbacGroup[]>([]);
    const [selectedGroupId, setSelectedGroupId] = useState<string>('');
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);

    // MCP permissions
    const [mcpPermissions, setMcpPermissions] = useState<McpPermission[]>([]);
    const [mcpLoading, setMcpLoading] = useState<boolean>(false);
    const [grantMcpOpen, setGrantMcpOpen] = useState<boolean>(false);
    const [availableMcpPermissions, setAvailableMcpPermissions] = useState<McpPermission[]>([]);
    const [selectedMcpPermission, setSelectedMcpPermission] = useState<McpPermission | null>(null);
    const [grantMcpLoading, setGrantMcpLoading] = useState<boolean>(false);
    const [grantMcpError, setGrantMcpError] = useState<string | null>(null);

    // Connection permissions
    const [connPermissions, setConnPermissions] = useState<ConnPermission[]>([]);
    const [connLoading, setConnLoading] = useState<boolean>(false);
    const [grantConnOpen, setGrantConnOpen] = useState<boolean>(false);
    const [availableConnections, setAvailableConnections] = useState<Connection[]>([]);
    const [selectedConnectionId, setSelectedConnectionId] = useState<string>('');
    const [selectedAccessLevel, setSelectedAccessLevel] = useState<string>('read');
    const [grantConnLoading, setGrantConnLoading] = useState<boolean>(false);
    const [grantConnError, setGrantConnError] = useState<string | null>(null);
    const [connections, setConnections] = useState<Connection[]>([]);

    // Admin permissions
    const [adminPermissions, setAdminPermissions] = useState<string[]>([]);
    const [adminPermsLoading, setAdminPermsLoading] = useState<boolean>(false);
    const [grantAdminOpen, setGrantAdminOpen] = useState<boolean>(false);
    const [selectedAdminPermission, setSelectedAdminPermission] = useState<string>('');
    const [grantAdminLoading, setGrantAdminLoading] = useState<boolean>(false);
    const [grantAdminError, setGrantAdminError] = useState<string | null>(null);

    // Fetch groups on mount
    useEffect(() => {
        const fetchGroups = async () => {
            try {
                const data = await apiGet<{ groups?: RbacGroup[] }>(`${API_BASE_URL}/rbac/groups`);
                setGroups(data.groups || []);
            } catch (err: unknown) {
                const message = err instanceof Error ? err.message : String(err);
                setError(message);
            } finally {
                setLoading(false);
            }
        };
        fetchGroups();
    }, []);

    // Fetch MCP and connection permissions when group changes
    const fetchPermissions = useCallback(async (groupId: string) => {
        if (!groupId) {return;}
        try {
            setMcpLoading(true);
            setConnLoading(true);
            const [groupData, connData] = await Promise.all([
                apiGet<{ mcp_privileges?: McpPermission[]; connection_privileges?: ConnPermission[] }>(
                    `${API_BASE_URL}/rbac/groups/${groupId}`
                ),
                apiGet<{ connections?: Connection[] } | Connection[]>(
                    `${API_BASE_URL}/connections`
                ).catch(() => null),
            ]);
            setMcpPermissions(groupData.mcp_privileges || []);
            setConnPermissions(groupData.connection_privileges || []);
            if (connData) {
                if (Array.isArray(connData)) {
                    setConnections(connData);
                } else {
                    setConnections(connData.connections || []);
                }
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setMcpLoading(false);
            setConnLoading(false);
        }
    }, []);

    // Fetch admin permissions when group changes
    const fetchAdminPermissions = useCallback(async (groupId: string) => {
        if (!groupId || !isSuperuser) {return;}
        try {
            setAdminPermsLoading(true);
            const data = await apiGet<{ permissions?: string[] }>(
                `${API_BASE_URL}/rbac/groups/${groupId}/permissions`
            );
            setAdminPermissions(data.permissions || []);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setAdminPermsLoading(false);
        }
    }, [isSuperuser]);

    useEffect(() => {
        if (selectedGroupId) {
            fetchPermissions(selectedGroupId);
            fetchAdminPermissions(selectedGroupId);
        } else {
            setMcpPermissions([]);
            setConnPermissions([]);
            setAdminPermissions([]);
        }
    }, [selectedGroupId, fetchPermissions, fetchAdminPermissions]);

    // Grant MCP permission
    const handleOpenGrantMcp = async () => {
        setGrantMcpOpen(true);
        setGrantMcpError(null);
        setSelectedMcpPermission(null);
        try {
            const data = await apiGet<McpPermission[]>(`${API_BASE_URL}/rbac/privileges/mcp`);
            const assignedIdentifiers = new Set(
                mcpPermissions.map((p: McpPermission) => p.identifier || p.privilege || '')
            );
            const hasWildcard = assignedIdentifiers.has('*');
            if (!hasWildcard) {
                const filtered = (data || []).filter(
                    (p: McpPermission) => !assignedIdentifiers.has(p.identifier || '')
                );
                const allOption: McpPermission = { identifier: '*', item_type: undefined, _isAll: true };
                setAvailableMcpPermissions([allOption, ...filtered]);
            } else {
                setAvailableMcpPermissions([]);
            }
        } catch {
            setGrantMcpError('Failed to load available permissions');
        }
    };

    const handleGrantMcp = async () => {
        if (!selectedMcpPermission || !selectedGroupId) {return;}
        try {
            setGrantMcpLoading(true);
            setGrantMcpError(null);
            await apiPost(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/mcp`,
                {
                    privilege: selectedMcpPermission.identifier || selectedMcpPermission,
                },
            );
            setGrantMcpOpen(false);
            fetchPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setGrantMcpError(message);
        } finally {
            setGrantMcpLoading(false);
        }
    };

    const handleRevokeMcp = async (permission: McpPermission) => {
        try {
            const identifier = permission.identifier || permission.privilege || '';
            await apiDelete(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/mcp?name=${encodeURIComponent(identifier)}`
            );
            fetchPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    const getConnectionName = (id: number): string => {
        if (id === 0) {return 'All Connections';}
        const conn = connections.find((c: Connection) => c.id === id);
        return conn ? conn.name : String(id);
    };

    // Grant connection permission
    const handleOpenGrantConn = () => {
        setGrantConnOpen(true);
        setGrantConnError(null);
        setSelectedConnectionId('');
        setSelectedAccessLevel('read');
        setAvailableConnections(connections);
    };

    const handleGrantConn = async () => {
        if (selectedConnectionId === '' || !selectedGroupId) {return;}
        try {
            setGrantConnLoading(true);
            setGrantConnError(null);
            await apiPost(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/connections`,
                {
                    connection_id: parseInt(selectedConnectionId, 10),
                    access_level: selectedAccessLevel,
                },
            );
            setGrantConnOpen(false);
            fetchPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setGrantConnError(message);
        } finally {
            setGrantConnLoading(false);
        }
    };

    const handleRevokeConn = async (permission: ConnPermission) => {
        try {
            await apiDelete(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/connections/${permission.connection_id}`
            );
            fetchPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    // Grant admin permission
    const handleGrantAdmin = async () => {
        if (!selectedAdminPermission || !selectedGroupId) {return;}
        try {
            setGrantAdminLoading(true);
            setGrantAdminError(null);
            await apiPost(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions`,
                { permission: selectedAdminPermission },
            );
            setGrantAdminOpen(false);
            fetchAdminPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setGrantAdminError(message);
        } finally {
            setGrantAdminLoading(false);
        }
    };

    const handleRevokeAdmin = async (permission: string) => {
        try {
            await apiDelete(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions/${encodeURIComponent(permission)}`
            );
            fetchAdminPermissions(selectedGroupId);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading permissions" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const textButtonSx = getTextButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const focusedLabelSx = getFocusedLabelSx(theme);

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Permissions
            </Typography>

            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}

            {/* Group Selector */}
            <FormControl fullWidth sx={{ mb: 3 }}>
                <InputLabel sx={focusedLabelSx}>
                    Select Group
                </InputLabel>
                <Select
                    value={selectedGroupId}
                    label="Select Group"
                    onChange={(e) => setSelectedGroupId(e.target.value)}
                >
                    {groups.map((g) => (
                        <MenuItem key={g.id} value={g.id}>{g.name}</MenuItem>
                    ))}
                </Select>
            </FormControl>

            {selectedGroupId && (
                <>
                    {/* Connection Permissions */}
                    <Box sx={{ mb: 4 }}>
                        <Box sx={sectionHeaderSx}>
                            <Typography variant="subtitle1" sx={sectionTitleSx}>
                                Connection Permissions
                            </Typography>
                            {!connPermissions.some(p => p.connection_id === 0) && (
                                <Button
                                    size="small"
                                    startIcon={<AddIcon />}
                                    onClick={handleOpenGrantConn}
                                    sx={textButtonSx}
                                >
                                    Grant
                                </Button>
                            )}
                        </Box>
                        <TableContainer
                            component={Paper}
                            elevation={0}
                            sx={tableContainerSx}
                        >
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell sx={tableHeaderCellSx}>Connection</TableCell>
                                        <TableCell sx={tableHeaderCellSx}>Access Level</TableCell>
                                        <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {connLoading ? (
                                        <TableRow>
                                            <TableCell colSpan={3} align="center" sx={{ py: 3 }}>
                                                <CircularProgress size={24} aria-label="Loading connections" />
                                            </TableCell>
                                        </TableRow>
                                    ) : connPermissions.length > 0 ? (
                                        connPermissions.map((p, i) => (
                                            <TableRow key={i}>
                                                <TableCell>{getConnectionName(p.connection_id)}</TableCell>
                                                <TableCell>{p.access_level || 'read'}</TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => handleRevokeConn(p)}
                                                        sx={deleteIconSx}
                                                        aria-label="revoke permission"
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))
                                    ) : (
                                        <TableRow>
                                            <TableCell colSpan={3} align="center" sx={emptyRowSx}>
                                                <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                    No connection permissions granted.
                                                </Typography>
                                            </TableCell>
                                        </TableRow>
                                    )}
                                </TableBody>
                            </Table>
                        </TableContainer>
                    </Box>

                    {/* Admin Permissions - superuser only */}
                    {isSuperuser && (
                        <Box sx={{ mb: 4 }}>
                            <Box sx={sectionHeaderSx}>
                                <Typography variant="subtitle1" sx={sectionTitleSx}>
                                    Admin Permissions
                                </Typography>
                                {!adminPermissions.includes('*') && (
                                    <Button
                                        size="small"
                                        startIcon={<AddIcon />}
                                        onClick={() => {
                                            setSelectedAdminPermission('');
                                            setGrantAdminError(null);
                                            setGrantAdminOpen(true);
                                        }}
                                        sx={textButtonSx}
                                    >
                                        Grant Permission
                                    </Button>
                                )}
                            </Box>
                            <TableContainer
                                component={Paper}
                                elevation={0}
                                sx={tableContainerSx}
                            >
                                <Table size="small">
                                    <TableHead>
                                        <TableRow>
                                            <TableCell sx={tableHeaderCellSx}>Permission</TableCell>
                                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {adminPermsLoading ? (
                                            <TableRow>
                                                <TableCell colSpan={2} align="center" sx={{ py: 3 }}>
                                                    <CircularProgress size={24} aria-label="Loading admin permissions" />
                                                </TableCell>
                                            </TableRow>
                                        ) : adminPermissions.length > 0 ? (
                                            adminPermissions.map((p, i) => {
                                                const permLabel = p === '*'
                                                    ? 'All Admin Permissions'
                                                    : PERMISSION_TYPES.find(
                                                        (pt) => pt.value === p
                                                    )?.label || p;
                                                return (
                                                    <TableRow key={i}>
                                                        <TableCell>{permLabel}</TableCell>
                                                        <TableCell align="right">
                                                            <IconButton
                                                                size="small"
                                                                onClick={() => handleRevokeAdmin(p)}
                                                                sx={deleteIconSx}
                                                                aria-label="revoke permission"
                                                            >
                                                                <DeleteIcon fontSize="small" />
                                                            </IconButton>
                                                        </TableCell>
                                                    </TableRow>
                                                );
                                            })
                                        ) : (
                                            <TableRow>
                                                <TableCell colSpan={2} align="center" sx={emptyRowSx}>
                                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                        No admin permissions granted.
                                                    </Typography>
                                                </TableCell>
                                            </TableRow>
                                        )}
                                    </TableBody>
                                </Table>
                            </TableContainer>
                        </Box>
                    )}

                    {/* MCP Permissions */}
                    <Box>
                        <Box sx={sectionHeaderSx}>
                            <Typography variant="subtitle1" sx={sectionTitleSx}>
                                MCP Permissions
                            </Typography>
                            {!mcpPermissions.some(p => (p.identifier || p.privilege || '') === '*') && (
                                <Button
                                    size="small"
                                    startIcon={<AddIcon />}
                                    onClick={handleOpenGrantMcp}
                                    sx={textButtonSx}
                                >
                                    Grant
                                </Button>
                            )}
                        </Box>
                        <TableContainer
                            component={Paper}
                            elevation={0}
                            sx={tableContainerSx}
                        >
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell sx={tableHeaderCellSx}>Permission</TableCell>
                                        <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {mcpLoading ? (
                                        <TableRow>
                                            <TableCell colSpan={2} align="center" sx={{ py: 3 }}>
                                                <CircularProgress size={24} aria-label="Loading MCP permissions" />
                                            </TableCell>
                                        </TableRow>
                                    ) : mcpPermissions.length > 0 ? (
                                        mcpPermissions.map((p, i) => (
                                            <TableRow key={i}>
                                                <TableCell>{formatMcpName(p)}</TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => handleRevokeMcp(p)}
                                                        sx={deleteIconSx}
                                                        aria-label="revoke permission"
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))
                                    ) : (
                                        <TableRow>
                                            <TableCell colSpan={2} align="center" sx={emptyRowSx}>
                                                <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                    No MCP permissions granted.
                                                </Typography>
                                            </TableCell>
                                        </TableRow>
                                    )}
                                </TableBody>
                            </Table>
                        </TableContainer>
                    </Box>
                </>
            )}

            {/* Grant MCP Permission Dialog */}
            <Dialog open={grantMcpOpen} onClose={() => !grantMcpLoading && setGrantMcpOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Grant MCP permission</DialogTitle>
                <DialogContent>
                    {grantMcpError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantMcpError}</Alert>
                    )}
                    <Autocomplete
                        options={availableMcpPermissions}
                        getOptionLabel={(option: McpPermission | string) => typeof option === 'string' ? option : formatMcpName(option)}
                        value={selectedMcpPermission}
                        onChange={(_e: React.SyntheticEvent, value: McpPermission | null) => setSelectedMcpPermission(value)}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Permission"
                                margin="dense"
                            />
                        )}
                        disabled={grantMcpLoading}
                        sx={{ mt: 1 }}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setGrantMcpOpen(false)} disabled={grantMcpLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrantMcp}
                        variant="contained"
                        disabled={grantMcpLoading || !selectedMcpPermission}
                        sx={containedButtonSx}
                    >
                        {grantMcpLoading ? <CircularProgress size={20} color="inherit" aria-label="Granting" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Grant Connection Permission Dialog */}
            <Dialog open={grantConnOpen} onClose={() => !grantConnLoading && setGrantConnOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Grant connection permission</DialogTitle>
                <DialogContent>
                    {grantConnError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantConnError}</Alert>
                    )}
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>
                            Connection
                        </InputLabel>
                        <Select
                            value={selectedConnectionId}
                            label="Connection"
                            onChange={(e) => setSelectedConnectionId(e.target.value)}
                            disabled={grantConnLoading}
                        >
                            {!connPermissions.some(p => p.connection_id === 0) && (
                                [
                                    <MenuItem key="all" value={0}>All Connections</MenuItem>,
                                    ...availableConnections
                                        .filter(c => !connPermissions.some(p => p.connection_id === c.id))
                                        .map((c) => (
                                            <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                                        ))
                                ]
                            )}
                        </Select>
                    </FormControl>
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>
                            Access Level
                        </InputLabel>
                        <Select
                            value={selectedAccessLevel}
                            label="Access Level"
                            onChange={(e) => setSelectedAccessLevel(e.target.value)}
                            disabled={grantConnLoading}
                        >
                            <MenuItem value="read">Read</MenuItem>
                            <MenuItem value="read_write">Read/Write</MenuItem>
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setGrantConnOpen(false)} disabled={grantConnLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrantConn}
                        variant="contained"
                        disabled={grantConnLoading || selectedConnectionId === ''}
                        sx={containedButtonSx}
                    >
                        {grantConnLoading ? <CircularProgress size={20} color="inherit" aria-label="Granting" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Grant Admin Permission Dialog */}
            <Dialog open={grantAdminOpen} onClose={() => !grantAdminLoading && setGrantAdminOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Grant admin permission</DialogTitle>
                <DialogContent>
                    {grantAdminError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantAdminError}</Alert>
                    )}
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>
                            Permission
                        </InputLabel>
                        <Select
                            value={selectedAdminPermission}
                            label="Permission"
                            onChange={(e) => setSelectedAdminPermission(e.target.value)}
                            disabled={grantAdminLoading}
                        >
                            {!adminPermissions.includes('*') && (
                                [
                                    <MenuItem key="*" value="*">All Admin Permissions</MenuItem>,
                                    ...PERMISSION_TYPES
                                        .filter(pt => !adminPermissions.includes(pt.value))
                                        .map((pt) => (
                                            <MenuItem key={pt.value} value={pt.value}>{pt.label}</MenuItem>
                                        ))
                                ]
                            )}
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setGrantAdminOpen(false)} disabled={grantAdminLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrantAdmin}
                        variant="contained"
                        disabled={grantAdminLoading || !selectedAdminPermission}
                        sx={containedButtonSx}
                    >
                        {grantAdminLoading ? <CircularProgress size={20} color="inherit" aria-label="Granting" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminPermissions;
