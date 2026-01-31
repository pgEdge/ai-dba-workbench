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
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    FormControlLabel,
    Switch,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Close as CloseIcon,
    CheckCircle as CheckIcon,
    Cancel as CancelIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import EffectivePermissionsPanel from './EffectivePermissionsPanel';
import { useAuth } from '../../contexts/AuthContext';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getSuccessIconSx,
    getInactiveIconSx,
    getTableContainerSx,
} from './styles';

const API_BASE_URL = '/api/v1';

const AdminUsers = ({ mode }) => {
    const theme = useTheme();
    const { user: authUser } = useAuth();
    const [users, setUsers] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [selectedUser, setSelectedUser] = useState(null);
    const [permissions, setPermissions] = useState(null);
    const [permissionsLoading, setPermissionsLoading] = useState(false);
    const [permissionsError, setPermissionsError] = useState(null);

    // Create user dialog
    const [createOpen, setCreateOpen] = useState(false);
    const [createUsername, setCreateUsername] = useState('');
    const [createPassword, setCreatePassword] = useState('');
    const [createDisplayName, setCreateDisplayName] = useState('');
    const [createEmail, setCreateEmail] = useState('');
    const [createAnnotation, setCreateAnnotation] = useState('');
    const [createEnabled, setCreateEnabled] = useState(true);
    const [createSuperuser, setCreateSuperuser] = useState(false);
    const [createLoading, setCreateLoading] = useState(false);
    const [createError, setCreateError] = useState(null);

    // Edit user dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editUser, setEditUser] = useState(null);
    const [editPassword, setEditPassword] = useState('');
    const [editDisplayName, setEditDisplayName] = useState('');
    const [editEmail, setEditEmail] = useState('');
    const [editAnnotation, setEditAnnotation] = useState('');
    const [editEnabled, setEditEnabled] = useState(true);
    const [editSuperuser, setEditSuperuser] = useState(false);
    const [editLoading, setEditLoading] = useState(false);
    const [editError, setEditError] = useState(null);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteUser, setDeleteUser] = useState(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

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
        setPermissions(null);
        setPermissionsError(null);
        setPermissionsLoading(true);
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/users/${user.id}/privileges`,
                { credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to fetch user permissions');
            }
            const data = await response.json();
            setPermissions(data);
        } catch (err) {
            setPermissionsError(err.message);
        } finally {
            setPermissionsLoading(false);
        }
    };

    const handleClosePermissions = () => {
        setSelectedUser(null);
        setPermissions(null);
    };

    // Create user
    const handleCreateUser = async () => {
        if (!createUsername.trim() || !createPassword) return;
        try {
            setCreateLoading(true);
            setCreateError(null);
            const body = {
                username: createUsername.trim(),
                password: createPassword,
            };
            if (createDisplayName.trim()) {
                body.display_name = createDisplayName.trim();
            }
            if (createEmail.trim()) {
                body.email = createEmail.trim();
            }
            if (createAnnotation.trim()) {
                body.annotation = createAnnotation.trim();
            }
            if (!createEnabled) {
                body.enabled = false;
            }
            if (createSuperuser) {
                body.is_superuser = true;
            }
            const response = await fetch(`${API_BASE_URL}/rbac/users`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to create user');
            }
            setCreateOpen(false);
            setCreateUsername('');
            setCreatePassword('');
            setCreateDisplayName('');
            setCreateEmail('');
            setCreateAnnotation('');
            setCreateEnabled(true);
            setCreateSuperuser(false);
            fetchUsers();
        } catch (err) {
            setCreateError(err.message);
        } finally {
            setCreateLoading(false);
        }
    };

    // Edit user
    const handleOpenEdit = (e, user) => {
        e.stopPropagation();
        setEditUser(user);
        setEditPassword('');
        setEditDisplayName(user.display_name || '');
        setEditEmail(user.email || '');
        setEditAnnotation(user.annotation || '');
        setEditEnabled(user.enabled !== false);
        setEditSuperuser(user.is_superuser || false);
        setEditError(null);
        setEditOpen(true);
    };

    const handleEditUser = async () => {
        if (!editUser) return;
        try {
            setEditLoading(true);
            setEditError(null);
            const body = {};
            if (editPassword) {
                body.password = editPassword;
            }
            const currentDisplayName = editUser.display_name || '';
            if (editDisplayName.trim() !== currentDisplayName) {
                body.display_name = editDisplayName.trim();
            }
            const currentEmail = editUser.email || '';
            if (editEmail.trim() !== currentEmail) {
                body.email = editEmail.trim();
            }
            const currentAnnotation = editUser.annotation || '';
            if (editAnnotation.trim() !== currentAnnotation) {
                body.annotation = editAnnotation.trim();
            }
            const currentEnabled = editUser.enabled !== false;
            if (editEnabled !== currentEnabled) {
                body.enabled = editEnabled;
            }
            const currentSuperuser = editUser.is_superuser || false;
            if (editSuperuser !== currentSuperuser) {
                body.is_superuser = editSuperuser;
            }
            const response = await fetch(
                `${API_BASE_URL}/rbac/users/${editUser.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify(body),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update user');
            }
            setEditOpen(false);
            fetchUsers();
        } catch (err) {
            setEditError(err.message);
        } finally {
            setEditLoading(false);
        }
    };

    // Delete user
    const handleOpenDelete = (e, user) => {
        e.stopPropagation();
        setDeleteUser(user);
        setDeleteOpen(true);
    };

    const handleDeleteUser = async () => {
        if (!deleteUser) return;
        try {
            setDeleteLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/users/${deleteUser.id}`,
                { method: 'DELETE', credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to delete user');
            }
            setDeleteOpen(false);
            setDeleteUser(null);
            fetchUsers();
        } catch (err) {
            setError(err.message);
        } finally {
            setDeleteLoading(false);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress />
            </Box>
        );
    }

    if (error) {
        return <Alert severity="error" sx={{ borderRadius: 1 }}>{error}</Alert>;
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const successIconSx = getSuccessIconSx(theme);
    const inactiveIconSx = getInactiveIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const isDark = mode === 'dark';

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Users
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={() => {
                        setCreateError(null);
                        setCreateUsername('');
                        setCreatePassword('');
                        setCreateDisplayName('');
                        setCreateEmail('');
                        setCreateAnnotation('');
                        setCreateEnabled(true);
                        setCreateSuperuser(false);
                        setCreateOpen(true);
                    }}
                    sx={containedButtonSx}
                >
                    Create User
                </Button>
            </Box>

            <TableContainer
                component={Paper}
                elevation={0}
                sx={tableContainerSx}
            >
                <Table>
                    <TableHead>
                        <TableRow>
                            <TableCell sx={tableHeaderCellSx}>Username</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Display Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Email</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Notes</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="center">Superuser</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="center">Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
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
                                <TableCell>{user.annotation || '-'}</TableCell>
                                <TableCell align="center">
                                    {user.is_superuser ? (
                                        <CheckIcon sx={successIconSx} />
                                    ) : (
                                        <CancelIcon sx={inactiveIconSx} />
                                    )}
                                </TableCell>
                                <TableCell align="center">
                                    {user.enabled !== false ? (
                                        <CheckIcon sx={successIconSx} />
                                    ) : (
                                        <CancelIcon sx={inactiveIconSx} />
                                    )}
                                </TableCell>
                                <TableCell align="right">
                                    <IconButton
                                        size="small"
                                        onClick={(e) => handleOpenEdit(e, user)}
                                        aria-label="edit user"
                                    >
                                        <EditIcon fontSize="small" />
                                    </IconButton>
                                    <IconButton
                                        size="small"
                                        onClick={(e) => handleOpenDelete(e, user)}
                                        aria-label="delete user"
                                        sx={deleteIconSx}
                                    >
                                        <DeleteIcon fontSize="small" />
                                    </IconButton>
                                </TableCell>
                            </TableRow>
                        ))}
                        {users.length === 0 && (
                            <TableRow>
                                <TableCell colSpan={7} align="center" sx={{ py: 4 }}>
                                    <Typography color="text.secondary">No users found.</Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* User Permissions Dialog */}
            <Dialog
                open={!!selectedUser}
                onClose={handleClosePermissions}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle sx={{ ...dialogTitleSx, display: 'flex', alignItems: 'center' }}>
                    <Box sx={{ flex: 1 }}>
                        Permissions for {selectedUser?.username}
                    </Box>
                    <IconButton onClick={handleClosePermissions} size="small">
                        <CloseIcon />
                    </IconButton>
                </DialogTitle>
                <DialogContent>
                    {permissionsLoading && (
                        <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
                            <CircularProgress />
                        </Box>
                    )}
                    {permissionsError && (
                        <Alert severity="error" sx={{ borderRadius: 1 }}>
                            {permissionsError}
                        </Alert>
                    )}
                    {permissions && !permissionsLoading && (
                        <EffectivePermissionsPanel
                            connectionPrivileges={permissions.connection_privileges}
                            adminPermissions={permissions.admin_permissions}
                            mcpPrivileges={permissions.mcp_privileges}
                            isSuperuser={!!authUser?.isSuperuser}
                            isDark={isDark}
                            groups={permissions.groups}
                        />
                    )}
                </DialogContent>
            </Dialog>

            {/* Create User Dialog */}
            <Dialog open={createOpen} onClose={() => !createLoading && setCreateOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Create User</DialogTitle>
                <DialogContent>
                    {createError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{createError}</Alert>
                    )}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Username"
                        value={createUsername}
                        onChange={(e) => setCreateUsername(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        required
                    />
                    <TextField
                        fullWidth
                        label="Password"
                        type="password"
                        value={createPassword}
                        onChange={(e) => setCreatePassword(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        required
                    />
                    <TextField
                        fullWidth
                        label="Display Name"
                        value={createDisplayName}
                        onChange={(e) => setCreateDisplayName(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Email"
                        type="email"
                        value={createEmail}
                        onChange={(e) => setCreateEmail(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Notes"
                        value={createAnnotation}
                        onChange={(e) => setCreateAnnotation(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                    />
                    <Box sx={{ mt: 2 }}>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={createEnabled}
                                    onChange={(e) => setCreateEnabled(e.target.checked)}
                                    disabled={createLoading}
                                />
                            }
                            label="Enabled"
                        />
                    </Box>
                    <Box>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={createSuperuser}
                                    onChange={(e) => setCreateSuperuser(e.target.checked)}
                                    disabled={createLoading}
                                />
                            }
                            label="Superuser"
                        />
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setCreateOpen(false)} disabled={createLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleCreateUser}
                        variant="contained"
                        disabled={createLoading || !createUsername.trim() || !createPassword}
                        sx={containedButtonSx}
                    >
                        {createLoading ? <CircularProgress size={20} color="inherit" /> : 'Create'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Edit User Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Edit User: {editUser?.username}</DialogTitle>
                <DialogContent>
                    {editError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{editError}</Alert>
                    )}
                    <TextField
                        fullWidth
                        label="Password"
                        type="password"
                        value={editPassword}
                        onChange={(e) => setEditPassword(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        placeholder="Leave blank to keep current"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Display Name"
                        value={editDisplayName}
                        onChange={(e) => setEditDisplayName(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Email"
                        type="email"
                        value={editEmail}
                        onChange={(e) => setEditEmail(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Notes"
                        value={editAnnotation}
                        onChange={(e) => setEditAnnotation(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                    />
                    <Box sx={{ mt: 2 }}>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={editEnabled}
                                    onChange={(e) => setEditEnabled(e.target.checked)}
                                    disabled={editLoading}
                                />
                            }
                            label="Enabled"
                        />
                    </Box>
                    <Box>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={editSuperuser}
                                    onChange={(e) => setEditSuperuser(e.target.checked)}
                                    disabled={editLoading}
                                />
                            }
                            label="Superuser"
                        />
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setEditOpen(false)} disabled={editLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleEditUser}
                        variant="contained"
                        disabled={editLoading}
                        sx={containedButtonSx}
                    >
                        {editLoading ? <CircularProgress size={20} color="inherit" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => { setDeleteOpen(false); setDeleteUser(null); }}
                onConfirm={handleDeleteUser}
                title="Delete User"
                message="Are you sure you want to delete the user"
                itemName={deleteUser?.username ? `"${deleteUser.username}"?` : '?'}
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminUsers;
