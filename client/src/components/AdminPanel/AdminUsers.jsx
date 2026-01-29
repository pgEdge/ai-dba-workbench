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
    Chip,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    IconButton,
    List,
    ListItem,
    ListItemText,
    alpha,
} from '@mui/material';
import {
    Close as CloseIcon,
    CheckCircle as CheckIcon,
    Cancel as CancelIcon,
} from '@mui/icons-material';

const API_BASE_URL = '/api/v1';

const AdminUsers = ({ mode }) => {
    const isDark = mode === 'dark';
    const [users, setUsers] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [selectedUser, setSelectedUser] = useState(null);
    const [privileges, setPrivileges] = useState(null);
    const [privilegesLoading, setPrivilegesLoading] = useState(false);
    const [privilegesError, setPrivilegesError] = useState(null);

    const fetchUsers = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const response = await fetch(`${API_BASE_URL}/rbac/users`, {
                credentials: 'include',
            });
            if (!response.ok) {
                throw new Error('Failed to fetch users');
            }
            const data = await response.json();
            setUsers(data.users || []);
        } catch (err) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchUsers();
    }, [fetchUsers]);

    const handleRowClick = async (user) => {
        setSelectedUser(user);
        setPrivileges(null);
        setPrivilegesError(null);
        setPrivilegesLoading(true);
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/users/${user.id}/privileges`,
                { credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to fetch user privileges');
            }
            const data = await response.json();
            setPrivileges(data);
        } catch (err) {
            setPrivilegesError(err.message);
        } finally {
            setPrivilegesLoading(false);
        }
    };

    const handleClosePrivileges = () => {
        setSelectedUser(null);
        setPrivileges(null);
    };

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
                <CircularProgress sx={{ color: '#15AABF' }} />
            </Box>
        );
    }

    if (error) {
        return <Alert severity="error" sx={{ borderRadius: 1 }}>{error}</Alert>;
    }

    return (
        <Box>
            <Typography variant="h6" sx={{ fontWeight: 600, mb: 2, color: 'text.primary' }}>
                Users
            </Typography>

            <TableContainer
                component={Paper}
                elevation={0}
                sx={{
                    border: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                    borderRadius: 1,
                }}
            >
                <Table>
                    <TableHead>
                        <TableRow>
                            <TableCell sx={{ fontWeight: 600 }}>Username</TableCell>
                            <TableCell sx={{ fontWeight: 600 }}>Display Name</TableCell>
                            <TableCell sx={{ fontWeight: 600 }}>Email</TableCell>
                            <TableCell sx={{ fontWeight: 600 }} align="center">Superuser</TableCell>
                            <TableCell sx={{ fontWeight: 600 }} align="center">Enabled</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {users.map((user) => (
                            <TableRow
                                key={user.id}
                                hover
                                onClick={() => handleRowClick(user)}
                                sx={{ cursor: 'pointer' }}
                            >
                                <TableCell>{user.username}</TableCell>
                                <TableCell>{user.display_name || '-'}</TableCell>
                                <TableCell>{user.email || '-'}</TableCell>
                                <TableCell align="center">
                                    {user.is_superuser ? (
                                        <CheckIcon sx={{ color: '#22C55E', fontSize: 20 }} />
                                    ) : (
                                        <CancelIcon sx={{ color: '#94A3B8', fontSize: 20 }} />
                                    )}
                                </TableCell>
                                <TableCell align="center">
                                    {user.is_enabled !== false ? (
                                        <Chip
                                            label="Enabled"
                                            size="small"
                                            sx={{
                                                bgcolor: alpha('#22C55E', 0.15),
                                                color: '#22C55E',
                                                fontWeight: 600,
                                                fontSize: '0.75rem',
                                            }}
                                        />
                                    ) : (
                                        <Chip
                                            label="Disabled"
                                            size="small"
                                            sx={{
                                                bgcolor: alpha('#EF4444', 0.15),
                                                color: '#EF4444',
                                                fontWeight: 600,
                                                fontSize: '0.75rem',
                                            }}
                                        />
                                    )}
                                </TableCell>
                            </TableRow>
                        ))}
                        {users.length === 0 && (
                            <TableRow>
                                <TableCell colSpan={5} align="center" sx={{ py: 4 }}>
                                    <Typography color="text.secondary">No users found.</Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* User Privileges Dialog */}
            <Dialog
                open={!!selectedUser}
                onClose={handleClosePrivileges}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle sx={{ fontWeight: 600, display: 'flex', alignItems: 'center' }}>
                    <Box sx={{ flex: 1 }}>
                        Privileges for {selectedUser?.username}
                    </Box>
                    <IconButton onClick={handleClosePrivileges} size="small">
                        <CloseIcon />
                    </IconButton>
                </DialogTitle>
                <DialogContent>
                    {privilegesLoading && (
                        <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
                            <CircularProgress sx={{ color: '#15AABF' }} />
                        </Box>
                    )}
                    {privilegesError && (
                        <Alert severity="error" sx={{ borderRadius: 1 }}>
                            {privilegesError}
                        </Alert>
                    )}
                    {privileges && !privilegesLoading && (
                        <Box>
                            {privileges.mcp_privileges?.length > 0 && (
                                <Box sx={{ mb: 2 }}>
                                    <Typography
                                        variant="subtitle2"
                                        sx={{ fontWeight: 600, mb: 1, color: 'text.secondary', textTransform: 'uppercase', fontSize: '0.75rem' }}
                                    >
                                        MCP Privileges
                                    </Typography>
                                    <List dense disablePadding>
                                        {privileges.mcp_privileges.map((p, i) => (
                                            <ListItem key={i} disablePadding sx={{ py: 0.25 }}>
                                                <ListItemText
                                                    primary={p.privilege || p.name || p}
                                                    primaryTypographyProps={{ fontSize: '0.875rem' }}
                                                />
                                            </ListItem>
                                        ))}
                                    </List>
                                </Box>
                            )}
                            {privileges.connection_privileges?.length > 0 && (
                                <Box sx={{ mb: 2 }}>
                                    <Typography
                                        variant="subtitle2"
                                        sx={{ fontWeight: 600, mb: 1, color: 'text.secondary', textTransform: 'uppercase', fontSize: '0.75rem' }}
                                    >
                                        Connection Privileges
                                    </Typography>
                                    <List dense disablePadding>
                                        {privileges.connection_privileges.map((p, i) => (
                                            <ListItem key={i} disablePadding sx={{ py: 0.25 }}>
                                                <ListItemText
                                                    primary={`${p.connection_name || p.connection_id} - ${p.access_level || 'read'}`}
                                                    primaryTypographyProps={{ fontSize: '0.875rem' }}
                                                />
                                            </ListItem>
                                        ))}
                                    </List>
                                </Box>
                            )}
                            {(!privileges.mcp_privileges?.length && !privileges.connection_privileges?.length) && (
                                <Typography color="text.secondary" sx={{ py: 2, textAlign: 'center' }}>
                                    No privileges assigned.
                                </Typography>
                            )}
                        </Box>
                    )}
                </DialogContent>
            </Dialog>
        </Box>
    );
};

export default AdminUsers;
