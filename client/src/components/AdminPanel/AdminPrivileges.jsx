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
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';

const API_BASE_URL = '/api/v1';
const ACCENT_COLOR = '#15AABF';
const ACCENT_HOVER = '#0C8599';

const textFieldSx = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: ACCENT_COLOR,
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: ACCENT_COLOR,
    },
};

const AdminPrivileges = ({ mode }) => {
    const isDark = mode === 'dark';
    const [groups, setGroups] = useState([]);
    const [selectedGroupId, setSelectedGroupId] = useState('');
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // MCP privileges
    const [mcpPrivileges, setMcpPrivileges] = useState([]);
    const [mcpLoading, setMcpLoading] = useState(false);
    const [grantMcpOpen, setGrantMcpOpen] = useState(false);
    const [availableMcpPrivileges, setAvailableMcpPrivileges] = useState([]);
    const [selectedMcpPrivilege, setSelectedMcpPrivilege] = useState(null);
    const [grantMcpLoading, setGrantMcpLoading] = useState(false);
    const [grantMcpError, setGrantMcpError] = useState(null);

    // Connection privileges
    const [connPrivileges, setConnPrivileges] = useState([]);
    const [connLoading, setConnLoading] = useState(false);
    const [grantConnOpen, setGrantConnOpen] = useState(false);
    const [availableConnections, setAvailableConnections] = useState([]);
    const [selectedConnectionId, setSelectedConnectionId] = useState('');
    const [selectedAccessLevel, setSelectedAccessLevel] = useState('read');
    const [grantConnLoading, setGrantConnLoading] = useState(false);
    const [grantConnError, setGrantConnError] = useState(null);

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

    // Fetch privileges when group changes
    const fetchPrivileges = useCallback(async (groupId) => {
        if (!groupId) return;
        try {
            setMcpLoading(true);
            setConnLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${groupId}/privileges`,
                { credentials: 'include' }
            );
            if (!response.ok) throw new Error('Failed to fetch privileges');
            const data = await response.json();
            setMcpPrivileges(data.mcp_privileges || []);
            setConnPrivileges(data.connection_privileges || []);
        } catch (err) {
            setError(err.message);
        } finally {
            setMcpLoading(false);
            setConnLoading(false);
        }
    }, []);

    useEffect(() => {
        if (selectedGroupId) {
            fetchPrivileges(selectedGroupId);
        } else {
            setMcpPrivileges([]);
            setConnPrivileges([]);
        }
    }, [selectedGroupId, fetchPrivileges]);

    // Grant MCP privilege
    const handleOpenGrantMcp = async () => {
        setGrantMcpOpen(true);
        setGrantMcpError(null);
        setSelectedMcpPrivilege(null);
        try {
            const response = await fetch(`${API_BASE_URL}/rbac/mcp-privileges`, {
                credentials: 'include',
            });
            if (response.ok) {
                const data = await response.json();
                setAvailableMcpPrivileges(data.privileges || []);
            }
        } catch (err) {
            setGrantMcpError('Failed to load available privileges');
        }
    };

    const handleGrantMcp = async () => {
        if (!selectedMcpPrivilege || !selectedGroupId) return;
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
                        privilege: selectedMcpPrivilege.name || selectedMcpPrivilege,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to grant privilege');
            }
            setGrantMcpOpen(false);
            fetchPrivileges(selectedGroupId);
        } catch (err) {
            setGrantMcpError(err.message);
        } finally {
            setGrantMcpLoading(false);
        }
    };

    const handleRevokeMcp = async (privilege) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/mcp`,
                {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        privilege: privilege.name || privilege.privilege || privilege,
                    }),
                }
            );
            if (!response.ok) throw new Error('Failed to revoke privilege');
            fetchPrivileges(selectedGroupId);
        } catch (err) {
            setError(err.message);
        }
    };

    // Grant connection privilege
    const handleOpenGrantConn = async () => {
        setGrantConnOpen(true);
        setGrantConnError(null);
        setSelectedConnectionId('');
        setSelectedAccessLevel('read');
        try {
            const response = await fetch(`${API_BASE_URL}/connections`, {
                credentials: 'include',
            });
            if (response.ok) {
                const data = await response.json();
                setAvailableConnections(data.connections || data || []);
            }
        } catch (err) {
            setGrantConnError('Failed to load available connections');
        }
    };

    const handleGrantConn = async () => {
        if (!selectedConnectionId || !selectedGroupId) return;
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
                throw new Error(data.error || 'Failed to grant privilege');
            }
            setGrantConnOpen(false);
            fetchPrivileges(selectedGroupId);
        } catch (err) {
            setGrantConnError(err.message);
        } finally {
            setGrantConnLoading(false);
        }
    };

    const handleRevokeConn = async (privilege) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/privileges/connections`,
                {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        connection_id: privilege.connection_id,
                    }),
                }
            );
            if (!response.ok) throw new Error('Failed to revoke privilege');
            fetchPrivileges(selectedGroupId);
        } catch (err) {
            setError(err.message);
        }
    };

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
                <CircularProgress sx={{ color: ACCENT_COLOR }} />
            </Box>
        );
    }

    return (
        <Box>
            <Typography variant="h6" sx={{ fontWeight: 600, mb: 2, color: 'text.primary' }}>
                Privileges
            </Typography>

            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}

            {/* Group Selector */}
            <FormControl fullWidth sx={{ mb: 3, ...textFieldSx }}>
                <InputLabel sx={{ '&.Mui-focused': { color: ACCENT_COLOR } }}>
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
                    {/* MCP Privileges */}
                    <Box sx={{ mb: 4 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                            <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }}>
                                MCP Privileges
                            </Typography>
                            <Button
                                size="small"
                                startIcon={<AddIcon />}
                                onClick={handleOpenGrantMcp}
                                sx={{ textTransform: 'none', color: ACCENT_COLOR }}
                            >
                                Grant
                            </Button>
                        </Box>
                        <TableContainer
                            component={Paper}
                            elevation={0}
                            sx={{
                                border: '1px solid',
                                borderColor: isDark ? '#334155' : '#E5E7EB',
                                borderRadius: 1,
                            }}
                        >
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell sx={{ fontWeight: 600 }}>Privilege</TableCell>
                                        <TableCell sx={{ fontWeight: 600 }} align="right">Actions</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {mcpLoading ? (
                                        <TableRow>
                                            <TableCell colSpan={2} align="center" sx={{ py: 3 }}>
                                                <CircularProgress size={24} sx={{ color: ACCENT_COLOR }} />
                                            </TableCell>
                                        </TableRow>
                                    ) : mcpPrivileges.length > 0 ? (
                                        mcpPrivileges.map((p, i) => (
                                            <TableRow key={i}>
                                                <TableCell>{p.name || p.privilege || p}</TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => handleRevokeMcp(p)}
                                                        sx={{ color: '#EF4444' }}
                                                        aria-label="revoke privilege"
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))
                                    ) : (
                                        <TableRow>
                                            <TableCell colSpan={2} align="center" sx={{ py: 2 }}>
                                                <Typography color="text.secondary" sx={{ fontSize: '0.875rem' }}>
                                                    No MCP privileges granted.
                                                </Typography>
                                            </TableCell>
                                        </TableRow>
                                    )}
                                </TableBody>
                            </Table>
                        </TableContainer>
                    </Box>

                    {/* Connection Privileges */}
                    <Box>
                        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                            <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }}>
                                Connection Privileges
                            </Typography>
                            <Button
                                size="small"
                                startIcon={<AddIcon />}
                                onClick={handleOpenGrantConn}
                                sx={{ textTransform: 'none', color: ACCENT_COLOR }}
                            >
                                Grant
                            </Button>
                        </Box>
                        <TableContainer
                            component={Paper}
                            elevation={0}
                            sx={{
                                border: '1px solid',
                                borderColor: isDark ? '#334155' : '#E5E7EB',
                                borderRadius: 1,
                            }}
                        >
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell sx={{ fontWeight: 600 }}>Connection</TableCell>
                                        <TableCell sx={{ fontWeight: 600 }}>Access Level</TableCell>
                                        <TableCell sx={{ fontWeight: 600 }} align="right">Actions</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {connLoading ? (
                                        <TableRow>
                                            <TableCell colSpan={3} align="center" sx={{ py: 3 }}>
                                                <CircularProgress size={24} sx={{ color: ACCENT_COLOR }} />
                                            </TableCell>
                                        </TableRow>
                                    ) : connPrivileges.length > 0 ? (
                                        connPrivileges.map((p, i) => (
                                            <TableRow key={i}>
                                                <TableCell>{p.connection_name || p.connection_id}</TableCell>
                                                <TableCell>{p.access_level || 'read'}</TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => handleRevokeConn(p)}
                                                        sx={{ color: '#EF4444' }}
                                                        aria-label="revoke privilege"
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))
                                    ) : (
                                        <TableRow>
                                            <TableCell colSpan={3} align="center" sx={{ py: 2 }}>
                                                <Typography color="text.secondary" sx={{ fontSize: '0.875rem' }}>
                                                    No connection privileges granted.
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

            {/* Grant MCP Privilege Dialog */}
            <Dialog open={grantMcpOpen} onClose={() => !grantMcpLoading && setGrantMcpOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={{ fontWeight: 600 }}>Grant MCP Privilege</DialogTitle>
                <DialogContent>
                    {grantMcpError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantMcpError}</Alert>
                    )}
                    <Autocomplete
                        options={availableMcpPrivileges}
                        getOptionLabel={(option) => option.name || option}
                        value={selectedMcpPrivilege}
                        onChange={(e, value) => setSelectedMcpPrivilege(value)}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Privilege"
                                margin="dense"
                                sx={textFieldSx}
                            />
                        )}
                        disabled={grantMcpLoading}
                        sx={{ mt: 1 }}
                    />
                </DialogContent>
                <DialogActions sx={{ px: 3, pb: 2 }}>
                    <Button onClick={() => setGrantMcpOpen(false)} disabled={grantMcpLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrantMcp}
                        variant="contained"
                        disabled={grantMcpLoading || !selectedMcpPrivilege}
                        sx={{ bgcolor: ACCENT_COLOR, '&:hover': { bgcolor: ACCENT_HOVER } }}
                    >
                        {grantMcpLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Grant Connection Privilege Dialog */}
            <Dialog open={grantConnOpen} onClose={() => !grantConnLoading && setGrantConnOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={{ fontWeight: 600 }}>Grant Connection Privilege</DialogTitle>
                <DialogContent>
                    {grantConnError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantConnError}</Alert>
                    )}
                    <FormControl fullWidth margin="dense" sx={textFieldSx}>
                        <InputLabel sx={{ '&.Mui-focused': { color: ACCENT_COLOR } }}>
                            Connection
                        </InputLabel>
                        <Select
                            value={selectedConnectionId}
                            label="Connection"
                            onChange={(e) => setSelectedConnectionId(e.target.value)}
                            disabled={grantConnLoading}
                        >
                            {availableConnections.map((c) => (
                                <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                    <FormControl fullWidth margin="dense" sx={textFieldSx}>
                        <InputLabel sx={{ '&.Mui-focused': { color: ACCENT_COLOR } }}>
                            Access Level
                        </InputLabel>
                        <Select
                            value={selectedAccessLevel}
                            label="Access Level"
                            onChange={(e) => setSelectedAccessLevel(e.target.value)}
                            disabled={grantConnLoading}
                        >
                            <MenuItem value="read">Read</MenuItem>
                            <MenuItem value="write">Write</MenuItem>
                            <MenuItem value="admin">Admin</MenuItem>
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions sx={{ px: 3, pb: 2 }}>
                    <Button onClick={() => setGrantConnOpen(false)} disabled={grantConnLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrantConn}
                        variant="contained"
                        disabled={grantConnLoading || !selectedConnectionId}
                        sx={{ bgcolor: ACCENT_COLOR, '&:hover': { bgcolor: ACCENT_HOVER } }}
                    >
                        {grantConnLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminPrivileges;
