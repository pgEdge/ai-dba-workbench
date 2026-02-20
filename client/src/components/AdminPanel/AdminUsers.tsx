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
import { ThemeMode } from '../../types/theme';
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
    Collapse,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    FormControlLabel,
    Switch,
    Chip,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import EffectivePermissionsPanel from './EffectivePermissionsPanel';
import { useAuth } from '../../contexts/AuthContext';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    subsectionLabelSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
} from './styles';

interface AdminUsersProps {
    mode: ThemeMode;
}

interface RbacUser {
    id: number;
    username: string;
    display_name?: string;
    email?: string;
    annotation?: string;
    is_service_account?: boolean;
    is_superuser?: boolean;
    enabled?: boolean;
}

interface UserPermissions {
    connection_privileges?: unknown[];
    admin_permissions?: unknown[];
    mcp_privileges?: unknown[];
    groups?: unknown[];
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
interface CreateUserBody { [key: string]: any }
// eslint-disable-next-line @typescript-eslint/no-explicit-any
interface EditUserBody { [key: string]: any }

const AdminUsers: React.FC<AdminUsersProps> = ({ mode }) => {
    const theme = useTheme();
    const { user: currentUser } = useAuth();
    const [users, setUsers] = useState<RbacUser[]>([]);
    const [connections, setConnections] = useState<Array<{ id: number; name: string }>>([]);
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedUser, setExpandedUser] = useState<number | null>(null);
    const [permissions, setPermissions] = useState<UserPermissions | null>(null);
    const [permissionsLoading, setPermissionsLoading] = useState<boolean>(false);
    const [permissionsError, setPermissionsError] = useState<string | null>(null);

    // Create user dialog
    const [createOpen, setCreateOpen] = useState<boolean>(false);
    const [createUsername, setCreateUsername] = useState<string>('');
    const [createPassword, setCreatePassword] = useState<string>('');
    const [createDisplayName, setCreateDisplayName] = useState<string>('');
    const [createEmail, setCreateEmail] = useState<string>('');
    const [createAnnotation, setCreateAnnotation] = useState<string>('');
    const [createServiceAccount, setCreateServiceAccount] = useState<boolean>(false);
    const [createEnabled, setCreateEnabled] = useState<boolean>(true);
    const [createSuperuser, setCreateSuperuser] = useState<boolean>(false);
    const [createLoading, setCreateLoading] = useState<boolean>(false);
    const [createError, setCreateError] = useState<string | null>(null);

    // Edit user dialog
    const [editOpen, setEditOpen] = useState<boolean>(false);
    const [editUser, setEditUser] = useState<RbacUser | null>(null);
    const [editPassword, setEditPassword] = useState<string>('');
    const [editDisplayName, setEditDisplayName] = useState<string>('');
    const [editEmail, setEditEmail] = useState<string>('');
    const [editAnnotation, setEditAnnotation] = useState<string>('');
    const [editEnabled, setEditEnabled] = useState<boolean>(true);
    const [editSuperuser, setEditSuperuser] = useState<boolean>(false);
    const [editLoading, setEditLoading] = useState<boolean>(false);
    const [editError, setEditError] = useState<string | null>(null);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState<boolean>(false);
    const [deleteUser, setDeleteUser] = useState<RbacUser | null>(null);
    const [deleteLoading, setDeleteLoading] = useState<boolean>(false);

    const fetchUsers = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const [usersData, connResult] = await Promise.all([
                apiGet<any>('/api/v1/rbac/users'),
                apiGet<any>('/api/v1/connections').catch(() => null),
            ]);
            setUsers(usersData.users || []);
            if (connResult) {
                setConnections(connResult.connections || connResult || []);
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchUsers();
    }, [fetchUsers]);

    const handleRowClick = async (rowUser: RbacUser) => {
        if (expandedUser === rowUser.id) {
            setExpandedUser(null);
            setPermissions(null);
            return;
        }
        setExpandedUser(rowUser.id);
        setPermissions(null);
        setPermissionsError(null);
        setPermissionsLoading(true);
        try {
            const data = await apiGet<UserPermissions>(`/api/v1/rbac/users/${rowUser.id}/privileges`);
            setPermissions(data);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setPermissionsError(message);
        } finally {
            setPermissionsLoading(false);
        }
    };

    // Create user
    const handleCreateUser = async () => {
        if (!createUsername.trim() || (!createServiceAccount && !createPassword)) {return;}
        try {
            setCreateLoading(true);
            setCreateError(null);
            const body: CreateUserBody = {
                username: createUsername.trim(),
            };
            if (createServiceAccount) {
                body.is_service_account = true;
            } else {
                body.password = createPassword;
            }
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
            await apiPost('/api/v1/rbac/users', body);
            setCreateOpen(false);
            setCreateUsername('');
            setCreatePassword('');
            setCreateDisplayName('');
            setCreateEmail('');
            setCreateAnnotation('');
            setCreateServiceAccount(false);
            setCreateEnabled(true);
            setCreateSuperuser(false);
            fetchUsers();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setCreateError(message);
        } finally {
            setCreateLoading(false);
        }
    };

    // Edit user
    const handleOpenEdit = (e: React.MouseEvent, rowUser: RbacUser) => {
        e.stopPropagation();
        setEditUser(rowUser);
        setEditPassword('');
        setEditDisplayName(rowUser.display_name || '');
        setEditEmail(rowUser.email || '');
        setEditAnnotation(rowUser.annotation || '');
        setEditEnabled(rowUser.enabled !== false);
        setEditSuperuser(rowUser.is_superuser || false);
        setEditError(null);
        setEditOpen(true);
    };

    const handleEditUser = async () => {
        if (!editUser) {return;}
        try {
            setEditLoading(true);
            setEditError(null);
            const body: EditUserBody = {};
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
            await apiPut(`/api/v1/rbac/users/${editUser.id}`, body);
            setEditOpen(false);
            fetchUsers();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setEditError(message);
        } finally {
            setEditLoading(false);
        }
    };

    // Delete user
    const handleOpenDelete = (e: React.MouseEvent, rowUser: RbacUser) => {
        e.stopPropagation();
        setDeleteUser(rowUser);
        setDeleteOpen(true);
    };

    const handleDeleteUser = async () => {
        if (!deleteUser) {return;}
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/rbac/users/${deleteUser.id}`);
            setDeleteOpen(false);
            setDeleteUser(null);
            fetchUsers();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setDeleteLoading(false);
        }
    };

    // Inline toggle handlers
    const handleToggleSuperuser = async (e: React.MouseEvent, rowUser: RbacUser) => {
        e.stopPropagation();
        try {
            await apiPut(`/api/v1/rbac/users/${rowUser.id}`, {
                is_superuser: !rowUser.is_superuser,
            });
            fetchUsers();
            if (expandedUser === rowUser.id) {
                const data = await apiGet<UserPermissions>(`/api/v1/rbac/users/${rowUser.id}/privileges`);
                setPermissions(data);
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    const handleToggleEnabled = async (e: React.MouseEvent, rowUser: RbacUser) => {
        e.stopPropagation();
        try {
            await apiPut(`/api/v1/rbac/users/${rowUser.id}`, {
                enabled: !(rowUser.enabled !== false),
            });
            fetchUsers();
            if (expandedUser === rowUser.id) {
                const data = await apiGet<UserPermissions>(`/api/v1/rbac/users/${rowUser.id}/privileges`);
                setPermissions(data);
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading users" />
            </Box>
        );
    }

    if (error) {
        return <Alert severity="error" sx={{ borderRadius: 1 }}>{error}</Alert>;
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
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
                        setCreateServiceAccount(false);
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
                            <TableCell sx={{ ...tableHeaderCellSx, width: 40 }} />
                            <TableCell sx={tableHeaderCellSx}>Username</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Type</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Display Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Email</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Notes</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="center">Superuser</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="center">Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {users.map((rowUser) => (
                            <React.Fragment key={rowUser.id}>
                                <TableRow
                                    hover
                                    onClick={() => handleRowClick(rowUser)}
                                    sx={{ cursor: 'pointer' }}
                                >
                                    <TableCell sx={{ px: 1 }}>
                                        {expandedUser === rowUser.id
                                            ? <ExpandLessIcon sx={{ color: 'text.secondary' }} />
                                            : <ExpandMoreIcon sx={{ color: 'text.secondary' }} />
                                        }
                                    </TableCell>
                                    <TableCell>{rowUser.username}</TableCell>
                                    <TableCell>
                                        {rowUser.is_service_account ? (
                                            <Chip
                                                label="Service Account"
                                                size="small"
                                                sx={{
                                                    bgcolor: alpha(theme.palette.info.main, 0.15),
                                                    color: theme.palette.info.main,
                                                    fontSize: '0.875rem',
                                                }}
                                            />
                                        ) : (
                                            <Typography variant="body2">User</Typography>
                                        )}
                                    </TableCell>
                                    <TableCell>{rowUser.display_name || '-'}</TableCell>
                                    <TableCell>{rowUser.email || '-'}</TableCell>
                                    <TableCell>{rowUser.annotation || '-'}</TableCell>
                                    <TableCell align="center">
                                        <Switch
                                            checked={rowUser.is_superuser || false}
                                            size="small"
                                            onClick={(e) => handleToggleSuperuser(e, rowUser)}
                                            disabled={
                                                rowUser.is_service_account === true ||
                                                rowUser.username === currentUser?.username
                                            }
                                            inputProps={{ 'aria-label': 'Toggle superuser' }}
                                        />
                                    </TableCell>
                                    <TableCell align="center">
                                        <Switch
                                            checked={rowUser.enabled !== false}
                                            size="small"
                                            onClick={(e) => handleToggleEnabled(e, rowUser)}
                                            disabled={
                                                rowUser.is_service_account === true ||
                                                rowUser.username === currentUser?.username
                                            }
                                            inputProps={{ 'aria-label': 'Toggle enabled' }}
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={(e) => handleOpenEdit(e, rowUser)}
                                            aria-label="edit user"
                                        >
                                            <EditIcon fontSize="small" />
                                        </IconButton>
                                        <IconButton
                                            size="small"
                                            onClick={(e) => handleOpenDelete(e, rowUser)}
                                            aria-label="delete user"
                                            sx={deleteIconSx}
                                        >
                                            <DeleteIcon fontSize="small" />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                                <TableRow>
                                    <TableCell colSpan={9} sx={{ py: 0, borderBottom: expandedUser === rowUser.id ? undefined : 'none' }}>
                                        <Collapse in={expandedUser === rowUser.id} timeout="auto" unmountOnExit>
                                            <Box sx={{ py: 2, px: 2 }}>
                                                {permissionsLoading ? (
                                                    <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                                                        <CircularProgress size={24} aria-label="Loading permissions" />
                                                    </Box>
                                                ) : permissionsError ? (
                                                    <Alert severity="error" sx={{ borderRadius: 1 }}>
                                                        {permissionsError}
                                                    </Alert>
                                                ) : permissions ? (
                                                    <Box>
                                                        <Typography
                                                            variant="subtitle2"
                                                            sx={{ ...subsectionLabelSx, mb: 1 }}
                                                        >
                                                            Effective Permissions
                                                        </Typography>
                                                        <EffectivePermissionsPanel
                                                            connectionPrivileges={permissions.connection_privileges}
                                                            adminPermissions={permissions.admin_permissions}
                                                            mcpPrivileges={permissions.mcp_privileges}
                                                            isSuperuser={rowUser.is_superuser || false}
                                                            isDark={isDark}
                                                            groups={permissions.groups}
                                                            connections={connections}
                                                        />
                                                    </Box>
                                                ) : null}
                                            </Box>
                                        </Collapse>
                                    </TableCell>
                                </TableRow>
                            </React.Fragment>
                        ))}
                        {users.length === 0 && (
                            <TableRow>
                                <TableCell colSpan={9} align="center" sx={{ py: 4 }}>
                                    <Typography color="text.secondary">No users found.</Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* Create User Dialog */}
            <Dialog open={createOpen} onClose={() => !createLoading && setCreateOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Create user</DialogTitle>
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
                    {!createServiceAccount && (
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
                    )}
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
                    <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={createServiceAccount}
                                    onChange={(e) => setCreateServiceAccount(e.target.checked)}
                                    disabled={createLoading}
                                />
                            }
                            label="Service Account"
                            sx={{ ml: 0, gap: 1 }}
                        />
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={createEnabled}
                                    onChange={(e) => setCreateEnabled(e.target.checked)}
                                    disabled={createLoading}
                                />
                            }
                            label="Enabled"
                            sx={{ ml: 0, gap: 1 }}
                        />
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={createSuperuser}
                                    onChange={(e) => setCreateSuperuser(e.target.checked)}
                                    disabled={createLoading}
                                />
                            }
                            label="Superuser"
                            sx={{ ml: 0, gap: 1 }}
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
                        disabled={createLoading || !createUsername.trim() || (!createServiceAccount && !createPassword)}
                        sx={containedButtonSx}
                    >
                        {createLoading ? <CircularProgress size={20} color="inherit" aria-label="Creating" /> : 'Create'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Edit User Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Edit user: {editUser?.username}</DialogTitle>
                <DialogContent>
                    {editError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{editError}</Alert>
                    )}
                    {!editUser?.is_service_account && (
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
                    )}
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
                    <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={editEnabled}
                                    onChange={(e) => setEditEnabled(e.target.checked)}
                                    disabled={editLoading}
                                />
                            }
                            label="Enabled"
                            sx={{ ml: 0, gap: 1 }}
                        />
                        <FormControlLabel
                            control={
                                <Switch
                                    checked={editSuperuser}
                                    onChange={(e) => setEditSuperuser(e.target.checked)}
                                    disabled={editLoading}
                                />
                            }
                            label="Superuser"
                            sx={{ ml: 0, gap: 1 }}
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
                        {editLoading ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : 'Save'}
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
