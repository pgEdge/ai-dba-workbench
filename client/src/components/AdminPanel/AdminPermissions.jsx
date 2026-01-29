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
} from '@mui/material';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';

const API_BASE_URL = '/api/v1';
const ACCENT_COLOR = '#15AABF';
const ACCENT_HOVER = '#0C8599';

const PERMISSION_TYPES = [
    { value: 'manage_users', label: 'Manage Users' },
    { value: 'manage_groups', label: 'Manage Groups' },
    { value: 'manage_privileges', label: 'Manage Privileges' },
    { value: 'manage_connections', label: 'Manage Connections' },
    { value: 'manage_token_scopes', label: 'Manage Token Scopes' },
];

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

const AdminPermissions = ({ mode }) => {
    const isDark = mode === 'dark';
    const [groups, setGroups] = useState([]);
    const [selectedGroupId, setSelectedGroupId] = useState('');
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [permissions, setPermissions] = useState([]);
    const [permsLoading, setPermsLoading] = useState(false);

    // Grant dialog
    const [grantOpen, setGrantOpen] = useState(false);
    const [selectedPermission, setSelectedPermission] = useState('');
    const [grantLoading, setGrantLoading] = useState(false);
    const [grantError, setGrantError] = useState(null);

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

    const fetchPermissions = useCallback(async (groupId) => {
        if (!groupId) return;
        try {
            setPermsLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${groupId}/permissions`,
                { credentials: 'include' }
            );
            if (!response.ok) throw new Error('Failed to fetch permissions');
            const data = await response.json();
            setPermissions(data.permissions || []);
        } catch (err) {
            setError(err.message);
        } finally {
            setPermsLoading(false);
        }
    }, []);

    useEffect(() => {
        if (selectedGroupId) {
            fetchPermissions(selectedGroupId);
        } else {
            setPermissions([]);
        }
    }, [selectedGroupId, fetchPermissions]);

    const handleGrant = async () => {
        if (!selectedPermission || !selectedGroupId) return;
        try {
            setGrantLoading(true);
            setGrantError(null);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ permission: selectedPermission }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to grant permission');
            }
            setGrantOpen(false);
            fetchPermissions(selectedGroupId);
        } catch (err) {
            setGrantError(err.message);
        } finally {
            setGrantLoading(false);
        }
    };

    const handleRevoke = async (permission) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${selectedGroupId}/permissions`,
                {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        permission: permission.permission || permission.name || permission,
                    }),
                }
            );
            if (!response.ok) throw new Error('Failed to revoke permission');
            fetchPermissions(selectedGroupId);
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
                Admin Permissions
            </Typography>

            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}

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
                <Box>
                    <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                        <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }}>
                            Granted Permissions
                        </Typography>
                        <Button
                            size="small"
                            startIcon={<AddIcon />}
                            onClick={() => {
                                setSelectedPermission('');
                                setGrantError(null);
                                setGrantOpen(true);
                            }}
                            sx={{ textTransform: 'none', color: ACCENT_COLOR }}
                        >
                            Grant Permission
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
                                    <TableCell sx={{ fontWeight: 600 }}>Permission</TableCell>
                                    <TableCell sx={{ fontWeight: 600 }} align="right">Actions</TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {permsLoading ? (
                                    <TableRow>
                                        <TableCell colSpan={2} align="center" sx={{ py: 3 }}>
                                            <CircularProgress size={24} sx={{ color: ACCENT_COLOR }} />
                                        </TableCell>
                                    </TableRow>
                                ) : permissions.length > 0 ? (
                                    permissions.map((p, i) => {
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
                                                        onClick={() => handleRevoke(p)}
                                                        sx={{ color: '#EF4444' }}
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
                                        <TableCell colSpan={2} align="center" sx={{ py: 2 }}>
                                            <Typography color="text.secondary" sx={{ fontSize: '0.875rem' }}>
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

            {/* Grant Permission Dialog */}
            <Dialog open={grantOpen} onClose={() => !grantLoading && setGrantOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={{ fontWeight: 600 }}>Grant Admin Permission</DialogTitle>
                <DialogContent>
                    {grantError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{grantError}</Alert>
                    )}
                    <FormControl fullWidth margin="dense" sx={textFieldSx}>
                        <InputLabel sx={{ '&.Mui-focused': { color: ACCENT_COLOR } }}>
                            Permission
                        </InputLabel>
                        <Select
                            value={selectedPermission}
                            label="Permission"
                            onChange={(e) => setSelectedPermission(e.target.value)}
                            disabled={grantLoading}
                        >
                            {PERMISSION_TYPES.map((pt) => (
                                <MenuItem key={pt.value} value={pt.value}>{pt.label}</MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions sx={{ px: 3, pb: 2 }}>
                    <Button onClick={() => setGrantOpen(false)} disabled={grantLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleGrant}
                        variant="contained"
                        disabled={grantLoading || !selectedPermission}
                        sx={{ bgcolor: ACCENT_COLOR, '&:hover': { bgcolor: ACCENT_HOVER } }}
                    >
                        {grantLoading ? <CircularProgress size={20} color="inherit" /> : 'Grant'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminPermissions;
