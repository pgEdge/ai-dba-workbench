/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
];

const mcpTypeLabel = (itemType) => {
    switch (itemType) {
        case 'tool': return 'Tool';
        case 'resource': return 'Resource';
        case 'prompt': return 'Prompt';
        default: return 'API';
    }
};

const formatMcpName = (permission) => {
    const name = permission.identifier || permission.privilege || permission;
    const type = permission.item_type;
    if (type) return `${mcpTypeLabel(type)}: ${name}`;
    return name;
};

interface AdminPermissionsProps {
    mode: string;
}

const AdminPermissions: React.FC<AdminPermissionsProps> = ({ mode }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const isSuperuser = !!user?.isSuperuser;

    const [groups, setGroups] = useState([]);
    const [selectedGroupId, setSelectedGroupId] = useState('');
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // MCP permissions
    const [mcpPermissions, setMcpPermissions] = useState([]);
    const [mcpLoading, setMcpLoading] = useState(false);
    const [grantMcpOpen, setGrantMcpOpen] = useState(false);
    const [availableMcpPermissions, setAvailableMcpPermissions] = useState([]);
    const [selectedMcpPermission, setSelectedMcpPermission] = useState(null);
    const [grantMcpLoading, setGrantMcpLoading] = useState(false);
    const [grantMcpError, setGrantMcpError] = useState(null);

    // Connection permissions
    const [connPermissions, setConnPermissions] = useState([]);
    const [connLoading, setConnLoading] = useState(false);
    const [grantConnOpen, setGrantConnOpen] = useState(false);
    const [availableConnections, setAvailableConnections] = useState([]);
    const [selectedConnectionId, setSelectedConnectionId] = useState('');
    const [selectedAccessLevel, setSelectedAccessLevel] = useState('read');
    const [grantConnLoading, setGrantConnLoading] = useState(false);
    const [grantConnError, setGrantConnError] = useState(null);
    const [connections, setConnections] = useState([]);

    // Admin permissions
    const [adminPermissions, setAdminPermissions] = useState([]);
    const [adminPermsLoading, setAdminPermsLoading] = useState(false);
    const [grantAdminOpen, setGrantAdminOpen] = useState(false);
    const [selectedAdminPermission, setSelectedAdminPermission] = useState('');
    const [grantAdminLoading, setGrantAdminLoading] = useState(false);
    const [grantAdminError, setGrantAdminError] = useState(null);

    // Fetch groups on mount
    useEffect(() => {
        const fetchGroups = async () => {
            try {
                const response = await fetch(`${API_BASE_URL}/rbac/groups`, {
                    credentials: 'include',
                });
                if (!response.ok) throw new Error('Failed to fetch groups');
                const data = await response.json();
                setGroups(data.groups || []);
            } catch (err) {
                setError(err.message);
            } finally {
                setLoading(false);
            }
        };
        fetchGroups();
    }, []);

    // Fetch MCP and connection permissions when group changes
    const fetchPermissions = useCallback(async (groupId) => {
        if (!groupId) return;
        try {
            setMcpLoading(true);
            setConnLoading(true);
            const [groupResponse, connResponse] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/groups/${groupId}`, {
                    credentials: 'include',
                }),
                fetch(`${API_BASE_URL}/connections`, {
                    credentials: 'include',
                }),
            ]);
            if (!groupResponse.ok) throw new Error('Failed to fetch permissions');
            const data = await groupResponse.json();
            setMcpPermissions(data.mcp_privileges || []);
            setConnPermissions(data.connection_privileges || []);
            if (connResponse.ok) {
                const connData = await connResponse.json();
                setConnections(connData.connections || connData || []);
            }
        } catch (err) {
            setError(err.message);
        } finally {
            setMcpLoading(false);
            setConnLoading(false);
        }
    }, []);

    // Fetch admin permissions when group changes
    const fetchAdminPermissions = useCallback(async (groupId) => {
        if (!groupId || !isSuperuser) return;
        try {
            setAdminPermsLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${groupId}/permissions`,
                { credentials: 'include' }
            );
            if (!response.ok) throw new Error('Failed to fetch admin permissions');
            const data = await response.json();
            setAdminPermissions(data.permissions || []);
        } catch (err) {
            setError(err.message);
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
            const response = await fetch(`${API_BASE_URL}/rbac/privileges/mcp`, {
                credentials: 'include',
            });
            if (response.ok) {
                const data = await response.json();
                setAvailableMcpPermissions(data || []);
            }
        } catch (err) {
            setGrantMcpError('Failed to load available permissions');
        }
    };

    const handleGrantMcp = async () => {
        if (!selectedMcpPermission || !selectedGroupId) return;
        try {
            setGrantMcpLoading(true);
            setGrantMcpError(null);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/mcp`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        privilege: selectedMcpPermission.identifier || selectedMcpPermission,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to grant permission');
            }
            setGrantMcpOpen(false);
            fetchPermissions(selectedGroupId);
        } catch (err) {
            setGrantMcpError(err.message);
        } finally {
            setGrantMcpLoading(false);
        }
    };

    const handleRevokeMcp = async (permission) => {
        try {
            const identifier = permission.identifier || permission.privilege || permission;
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/mcp?name=${encodeURIComponent(identifier)}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) throw new Error('Failed to revoke permission');
            fetchPermissions(selectedGroupId);
        } catch (err) {
            setError(err.message);
        }
    };

    const getConnectionName = (id) => {
        if (id === 0) return 'All Connections';
        const conn = connections.find((c) => c.id === id);
        return conn ? conn.name : id;
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
        if (selectedConnectionId === '' || !selectedGroupId) return;
        try {
            setGrantConnLoading(true);
            setGrantConnError(null);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/connections`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        connection_id: parseInt(selectedConnectionId, 10),
                        access_level: selectedAccessLevel,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to grant permission');
            }
            setGrantConnOpen(false);
            fetchPermissions(selectedGroupId);
        } catch (err) {
            setGrantConnError(err.message);
        } finally {
            setGrantConnLoading(false);
        }
    };

    const handleRevokeConn = async (permission) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/connections/${permission.connection_id}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) throw new Error('Failed to revoke permission');
            fetchPermissions(selectedGroupId);
        } catch (err) {
            setError(err.message);
        }
    };

    // Grant admin permission
    const handleGrantAdmin = async () => {
        if (!selectedAdminPermission || !selectedGroupId) return;
        try {
            setGrantAdminLoading(true);
            setGrantAdminError(null);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ permission: selectedAdminPermission }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to grant permission');
            }
            setGrantAdminOpen(false);
            fetchAdminPermissions(selectedGroupId);
        } catch (err) {
            setGrantAdminError(err.message);
        } finally {
            setGrantAdminLoading(false);
        }
    };

    const handleRevokeAdmin = async (permission) => {
        try {
            const permValue = permission.permission || permission.name || permission;
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions/${encodeURIComponent(permValue)}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) throw new Error('Failed to revoke permission');
            fetchAdminPermissions(selectedGroupId);
        } catch (err) {
            setError(err.message);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress />
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
                            <Button
                                size="small"
                                startIcon={<AddIcon />}
                                onClick={handleOpenGrantConn}
                                sx={textButtonSx}
                            >
                                Grant
                            </Button>
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
                                                <CircularProgress size={24} />
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
                                                    <CircularProgress size={24} />
                                                </TableCell>
                                            </TableRow>
                                        ) : adminPermissions.length > 0 ? (
                                            adminPermissions.map((p, i) => {
                                                const permValue = p.permission || p.name || p;
                                                const permLabel = PERMISSION_TYPES.find(
                                                    (pt) => pt.value === permValue
                                                )?.label || permValue;
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
                            <Button
                                size="small"
                                startIcon={<AddIcon />}
                                onClick={handleOpenGrantMcp}
                                sx={textButtonSx}
                            >
                                Grant
                            </Button>
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
                                                <CircularProgress size={24} />
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
                <DialogTitle sx={dialogTitleSx}>Grant MCP Permission</DialogTitle>
                <DialogContent>
                    {grantMcpError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantMcpError}</Alert>
                    )}
                    <Autocomplete
                        options={availableMcpPermissions}
                        getOptionLabel={(option) => typeof option === 'string' ? option : formatMcpName(option)}
                        value={selectedMcpPermission}
                        onChange={(e, value) => setSelectedMcpPermission(value)}
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
                        {grantMcpLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Grant Connection Permission Dialog */}
            <Dialog open={grantConnOpen} onClose={() => !grantConnLoading && setGrantConnOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Grant Connection Permission</DialogTitle>
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
                            <MenuItem value={0}>All Connections</MenuItem>
                            {availableConnections.map((c) => (
                                <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                            ))}
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
                        {grantConnLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Grant Admin Permission Dialog */}
            <Dialog open={grantAdminOpen} onClose={() => !grantAdminLoading && setGrantAdminOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Grant Admin Permission</DialogTitle>
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
                            {PERMISSION_TYPES.map((pt) => (
                                <MenuItem key={pt.value} value={pt.value}>{pt.label}</MenuItem>
                            ))}
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
                        {grantAdminLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminPermissions;
