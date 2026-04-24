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
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    CircularProgress,
    Alert,
    Collapse,
    List,
    ListItem,
    ListItemText,
    ListItemSecondaryAction,
    MenuItem,
    RadioGroup,
    FormControlLabel,
    Radio,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    PersonRemove as RemoveIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import EffectivePermissionsPanel from './EffectivePermissionsPanel';
import { useAuth } from '../../contexts/useAuth';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import { SELECT_FIELD_SX } from '../shared/formStyles';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    subsectionLabelSx,
    getContainedButtonSx,
    getTextButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
    getRadioSx,
} from './styles';


interface RbacGroup {
    id: number;
    name: string;
    description?: string;
    member_count?: number;
}

interface GroupDetail {
    user_members?: string[];
    group_members?: string[];
    [key: string]: unknown;
}

interface EffectivePermsData {
    connection_privileges?: unknown[];
    admin_permissions?: unknown[];
    mcp_privileges?: unknown[];
}

interface RbacUser {
    id: number;
    username: string;
}

const AdminGroups: React.FC = () => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const { user } = useAuth();
    const isSuperuser = !!user?.isSuperuser;
    const [groups, setGroups] = useState<RbacGroup[]>([]);
    const [connections, setConnections] = useState<Array<{ id: number; name: string }>>([]);
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedGroup, setExpandedGroup] = useState<number | null>(null);
    const [groupDetail, setGroupDetail] = useState<GroupDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState<boolean>(false);
    const [effectivePerms, setEffectivePerms] = useState<EffectivePermsData | null>(null);
    const [effectivePermsLoading, setEffectivePermsLoading] = useState<boolean>(false);

    // Create group dialog
    const [createOpen, setCreateOpen] = useState<boolean>(false);
    const [createName, setCreateName] = useState<string>('');
    const [createDesc, setCreateDesc] = useState<string>('');
    const [createLoading, setCreateLoading] = useState<boolean>(false);
    const [createError, setCreateError] = useState<string | null>(null);

    // Edit group dialog
    const [editOpen, setEditOpen] = useState<boolean>(false);
    const [editGroup, setEditGroup] = useState<RbacGroup | null>(null);
    const [editName, setEditName] = useState<string>('');
    const [editDesc, setEditDesc] = useState<string>('');
    const [editLoading, setEditLoading] = useState<boolean>(false);
    const [editError, setEditError] = useState<string | null>(null);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState<boolean>(false);
    const [deleteGroup, setDeleteGroup] = useState<RbacGroup | null>(null);
    const [deleteLoading, setDeleteLoading] = useState<boolean>(false);

    // Add member dialog
    const [addMemberOpen, setAddMemberOpen] = useState<boolean>(false);
    const [memberType, setMemberType] = useState<string>('user');
    const [selectedMemberId, setSelectedMemberId] = useState<string>('');
    const [addMemberLoading, setAddMemberLoading] = useState<boolean>(false);
    const [addMemberError, setAddMemberError] = useState<string | null>(null);
    const [availableUsers, setAvailableUsers] = useState<RbacUser[]>([]);
    const [availableGroups, setAvailableGroups] = useState<RbacGroup[]>([]);

    const fetchGroups = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const [groupsData, connResult] = await Promise.all([
                apiGet<{ groups: RbacGroup[] }>('/api/v1/rbac/groups'),
                apiGet<{ connections?: Array<{ id: number; name: string }> }>('/api/v1/connections').catch(() => null),
            ]);
            setGroups(groupsData.groups || []);
            if (connResult) {
                setConnections(connResult.connections || []);
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchGroups();
    }, [fetchGroups]);

    const fetchGroupDetail = useCallback(async (groupId: number) => {
        try {
            setDetailLoading(true);
            setEffectivePermsLoading(true);
            const [detailData, effectiveResult] = await Promise.all([
                apiGet<GroupDetail>(`/api/v1/rbac/groups/${groupId}`),
                apiGet<EffectivePermsData>(`/api/v1/rbac/groups/${groupId}/effective-privileges`).catch(() => null),
            ]);
            setGroupDetail(detailData);
            setEffectivePerms(effectiveResult);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setDetailLoading(false);
            setEffectivePermsLoading(false);
        }
    }, []);

    const handleRowClick = (group: RbacGroup) => {
        if (expandedGroup === group.id) {
            setExpandedGroup(null);
            setGroupDetail(null);
            setEffectivePerms(null);
        } else {
            setExpandedGroup(group.id);
            fetchGroupDetail(group.id);
        }
    };

    // Create group
    const handleCreateGroup = async () => {
        if (!createName.trim()) {return;}
        try {
            setCreateLoading(true);
            setCreateError(null);
            await apiPost('/api/v1/rbac/groups', {
                name: createName.trim(),
                description: createDesc.trim(),
            });
            setCreateOpen(false);
            setCreateName('');
            setCreateDesc('');
            fetchGroups();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setCreateError(message);
        } finally {
            setCreateLoading(false);
        }
    };

    // Edit group
    const handleOpenEdit = (e: React.MouseEvent, group: RbacGroup) => {
        e.stopPropagation();
        setEditGroup(group);
        setEditName(group.name);
        setEditDesc(group.description || '');
        setEditError(null);
        setEditOpen(true);
    };

    const handleEditGroup = async () => {
        if (!editName.trim() || !editGroup) {return;}
        try {
            setEditLoading(true);
            setEditError(null);
            await apiPut(`/api/v1/rbac/groups/${editGroup.id}`, {
                name: editName.trim(),
                description: editDesc.trim(),
            });
            setEditOpen(false);
            fetchGroups();
            if (expandedGroup === editGroup.id) {
                fetchGroupDetail(editGroup.id);
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setEditError(message);
        } finally {
            setEditLoading(false);
        }
    };

    // Delete group
    const handleOpenDelete = (e: React.MouseEvent, group: RbacGroup) => {
        e.stopPropagation();
        setDeleteGroup(group);
        setDeleteOpen(true);
    };

    const handleDeleteGroup = async () => {
        if (!deleteGroup) {return;}
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/rbac/groups/${deleteGroup.id}`);
            setDeleteOpen(false);
            setDeleteGroup(null);
            if (expandedGroup === deleteGroup.id) {
                setExpandedGroup(null);
                setGroupDetail(null);
            }
            fetchGroups();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setDeleteLoading(false);
        }
    };

    // Add member
    const handleOpenAddMember = async () => {
        setAddMemberOpen(true);
        setAddMemberError(null);
        setSelectedMemberId('');
        setMemberType('user');
        try {
            const [usersData, groupsData] = await Promise.all([
                apiGet<{ users: RbacUser[] }>('/api/v1/rbac/users').catch(() => null),
                apiGet<{ groups: RbacGroup[] }>('/api/v1/rbac/groups').catch(() => null),
            ]);
            if (usersData) {
                setAvailableUsers(usersData.users || []);
            }
            if (groupsData) {
                // Exclude the current group from the list
                setAvailableGroups(
                    (groupsData.groups || []).filter((g: RbacGroup) => g.id !== expandedGroup)
                );
            }
        } catch {
            setAddMemberError('Failed to load available members');
        }
    };

    const handleAddMember = async () => {
        if (!selectedMemberId || !expandedGroup) {return;}
        try {
            setAddMemberLoading(true);
            setAddMemberError(null);
            try {
                await apiPost(
                    `/api/v1/rbac/groups/${expandedGroup}/members`,
                    memberType === 'user'
                        ? { user_id: parseInt(selectedMemberId, 10) }
                        : { group_id: parseInt(selectedMemberId, 10) }
                );
            } catch (apiErr: unknown) {
                const errorMsg = apiErr instanceof Error ? apiErr.message : String(apiErr);
                throw new Error(
                    errorMsg.includes('UNIQUE constraint')
                        ? 'This member is already in the group.'
                        : errorMsg
                );
            }
            setAddMemberOpen(false);
            fetchGroupDetail(expandedGroup);
            fetchGroups();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setAddMemberError(message);
        } finally {
            setAddMemberLoading(false);
        }
    };

    const handleRemoveMember = async (memberId: number, mType: string) => {
        if (!expandedGroup) {return;}
        try {
            await apiDelete(`/api/v1/rbac/groups/${expandedGroup}/members/${mType}/${memberId}`);
            fetchGroupDetail(expandedGroup);
            fetchGroups();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    const handleRemoveMemberByName = async (name: string, mType: string) => {
        if (!expandedGroup) {return;}
        try {
            let memberId: number | undefined;
            if (mType === 'user') {
                try {
                    const data = await apiGet<{ users?: { id: number; username: string }[] }>('/api/v1/rbac/users');
                    const foundUser = (data.users || []).find(u => u.username === name);
                    if (foundUser) {memberId = foundUser.id;}
                } catch { /* ignore */ }
            } else {
                const found = groups.find(g => g.name === name);
                if (found) {memberId = found.id;}
            }
            if (!memberId) {
                setError(`Could not find ${mType} "${name}"`);
                return;
            }
            await handleRemoveMember(memberId, mType);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading groups" />
            </Box>
        );
    }

    if (error) {
        return <Alert severity="error" sx={{ borderRadius: 1 }}>{error}</Alert>;
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const textButtonSx = getTextButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const radioSx = getRadioSx(theme);
    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Groups
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={() => {
                        setCreateError(null);
                        setCreateName('');
                        setCreateDesc('');
                        setCreateOpen(true);
                    }}
                    sx={containedButtonSx}
                >
                    Create Group
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
                            <TableCell sx={tableHeaderCellSx}>Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Description</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="center">Members</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {groups.map((group) => (
                            <React.Fragment key={group.id}>
                                <TableRow
                                    hover
                                    onClick={() => handleRowClick(group)}
                                    sx={{ cursor: 'pointer' }}
                                >
                                    <TableCell sx={{ px: 1 }}>
                                        {expandedGroup === group.id
                                            ? <ExpandLessIcon sx={{ color: 'text.secondary' }} />
                                            : <ExpandMoreIcon sx={{ color: 'text.secondary' }} />
                                        }
                                    </TableCell>
                                    <TableCell>{group.name}</TableCell>
                                    <TableCell>{group.description || '-'}</TableCell>
                                    <TableCell align="center">{group.member_count ?? 0}</TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={(e) => handleOpenEdit(e, group)}
                                            aria-label="edit group"
                                        >
                                            <EditIcon fontSize="small" />
                                        </IconButton>
                                        <IconButton
                                            size="small"
                                            onClick={(e) => handleOpenDelete(e, group)}
                                            aria-label="delete group"
                                            sx={deleteIconSx}
                                        >
                                            <DeleteIcon fontSize="small" />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                                <TableRow>
                                    <TableCell colSpan={5} sx={{ py: 0, borderBottom: expandedGroup === group.id ? undefined : 'none' }}>
                                        <Collapse in={expandedGroup === group.id} timeout="auto" unmountOnExit>
                                            <Box sx={{ py: 2, px: 2 }}>
                                                {detailLoading ? (
                                                    <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                                                        <CircularProgress size={24} aria-label="Loading group details" />
                                                    </Box>
                                                ) : groupDetail ? (
                                                    <Box>
                                                        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                                                            <Typography
                                                                variant="subtitle2"
                                                                sx={{ ...subsectionLabelSx, flex: 1 }}
                                                            >
                                                                Members
                                                            </Typography>
                                                            <Button
                                                                size="small"
                                                                startIcon={<AddIcon />}
                                                                onClick={handleOpenAddMember}
                                                                sx={textButtonSx}
                                                            >
                                                                Add Member
                                                            </Button>
                                                        </Box>
                                                        {((groupDetail.user_members?.length > 0) || (groupDetail.group_members?.length > 0)) ? (
                                                            <List dense disablePadding>
                                                                {(groupDetail.user_members || []).map((username, i) => (
                                                                    <ListItem key={`user-${i}`} disablePadding sx={{ py: 0.5 }}>
                                                                        <ListItemText
                                                                            primary={username}
                                                                            secondary="user"
                                                                            primaryTypographyProps={{ fontSize: '1rem' }}
                                                                            secondaryTypographyProps={{ fontSize: '0.875rem' }}
                                                                        />
                                                                        <ListItemSecondaryAction>
                                                                            <IconButton
                                                                                edge="end"
                                                                                size="small"
                                                                                onClick={() => handleRemoveMemberByName(username, 'user')}
                                                                                aria-label="remove member"
                                                                                sx={deleteIconSx}
                                                                            >
                                                                                <RemoveIcon fontSize="small" />
                                                                            </IconButton>
                                                                        </ListItemSecondaryAction>
                                                                    </ListItem>
                                                                ))}
                                                                {(groupDetail.group_members || []).map((groupName, i) => (
                                                                    <ListItem key={`group-${i}`} disablePadding sx={{ py: 0.5 }}>
                                                                        <ListItemText
                                                                            primary={groupName}
                                                                            secondary="group"
                                                                            primaryTypographyProps={{ fontSize: '1rem' }}
                                                                            secondaryTypographyProps={{ fontSize: '0.875rem' }}
                                                                        />
                                                                        <ListItemSecondaryAction>
                                                                            <IconButton
                                                                                edge="end"
                                                                                size="small"
                                                                                onClick={() => handleRemoveMemberByName(groupName, 'group')}
                                                                                aria-label="remove member"
                                                                                sx={deleteIconSx}
                                                                            >
                                                                                <RemoveIcon fontSize="small" />
                                                                            </IconButton>
                                                                        </ListItemSecondaryAction>
                                                                    </ListItem>
                                                                ))}
                                                            </List>
                                                        ) : (
                                                            <Typography color="text.secondary" sx={{ fontSize: '1rem', py: 1 }}>
                                                                No members in this group.
                                                            </Typography>
                                                        )}
                                                        {effectivePermsLoading ? (
                                                            <Box sx={{ display: 'flex', justifyContent: 'center', py: 2, mt: 3 }}>
                                                                <CircularProgress size={24} aria-label="Loading permissions" />
                                                            </Box>
                                                        ) : effectivePerms ? (
                                                            <Box sx={{ mt: 3 }}>
                                                                <Typography
                                                                    variant="subtitle2"
                                                                    sx={{ ...subsectionLabelSx, mb: 1 }}
                                                                >
                                                                    Effective Permissions
                                                                </Typography>
                                                                <EffectivePermissionsPanel
                                                                    connectionPrivileges={effectivePerms.connection_privileges}
                                                                    adminPermissions={effectivePerms.admin_permissions}
                                                                    mcpPrivileges={effectivePerms.mcp_privileges}
                                                                    isSuperuser={isSuperuser}
                                                                    isDark={isDark}
                                                                    connections={connections}
                                                                />
                                                            </Box>
                                                        ) : null}
                                                    </Box>
                                                ) : null}
                                            </Box>
                                        </Collapse>
                                    </TableCell>
                                </TableRow>
                            </React.Fragment>
                        ))}
                        {groups.length === 0 && (
                            <TableRow>
                                <TableCell colSpan={5} align="center" sx={{ py: 4 }}>
                                    <Typography color="text.secondary">No groups found.</Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* Create Group Dialog */}
            <Dialog open={createOpen} onClose={() => !createLoading && setCreateOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Create group</DialogTitle>
                <DialogContent>
                    {createError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{createError}</Alert>
                    )}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={createName}
                        onChange={(e) => setCreateName(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        required
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                    <TextField
                        fullWidth
                        label="Description"
                        value={createDesc}
                        onChange={(e) => setCreateDesc(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setCreateOpen(false)} disabled={createLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleCreateGroup}
                        variant="contained"
                        disabled={createLoading || !createName.trim()}
                        sx={containedButtonSx}
                    >
                        {createLoading ? <CircularProgress size={20} color="inherit" aria-label="Creating" /> : 'Create'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Edit Group Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Edit group</DialogTitle>
                <DialogContent>
                    {editError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{editError}</Alert>
                    )}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        required
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                    <TextField
                        fullWidth
                        label="Description"
                        value={editDesc}
                        onChange={(e) => setEditDesc(e.target.value)}
                        disabled={editLoading}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setEditOpen(false)} disabled={editLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleEditGroup}
                        variant="contained"
                        disabled={editLoading || !editName.trim()}
                        sx={containedButtonSx}
                    >
                        {editLoading ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => { setDeleteOpen(false); setDeleteGroup(null); }}
                onConfirm={handleDeleteGroup}
                title="Delete Group"
                message="Are you sure you want to delete the group"
                itemName={deleteGroup?.name ? `"${deleteGroup.name}"?` : '?'}
                loading={deleteLoading}
            />

            {/* Add Member Dialog */}
            <Dialog open={addMemberOpen} onClose={() => !addMemberLoading && setAddMemberOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Add member</DialogTitle>
                <DialogContent>
                    {addMemberError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{addMemberError}</Alert>
                    )}
                    <RadioGroup
                        row
                        value={memberType}
                        onChange={(e) => { setMemberType(e.target.value); setSelectedMemberId(''); }}
                        sx={{ mb: 1 }}
                    >
                        <FormControlLabel value="user" control={<Radio sx={radioSx} />} label="User" />
                        <FormControlLabel value="group" control={<Radio sx={radioSx} />} label="Group" />
                    </RadioGroup>
                    <TextField
                        select
                        fullWidth
                        label={memberType === 'user' ? 'Select User' : 'Select Group'}
                        value={selectedMemberId}
                        onChange={(e) => setSelectedMemberId(e.target.value)}
                        disabled={addMemberLoading}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    >
                        {memberType === 'user'
                            ? availableUsers.map((u) => (
                                <MenuItem key={u.id} value={u.id}>{u.username}</MenuItem>
                            ))
                            : availableGroups.map((g) => (
                                <MenuItem key={g.id} value={g.id}>{g.name}</MenuItem>
                            ))
                        }
                    </TextField>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setAddMemberOpen(false)} disabled={addMemberLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleAddMember}
                        variant="contained"
                        disabled={addMemberLoading || !selectedMemberId}
                        sx={containedButtonSx}
                    >
                        {addMemberLoading ? <CircularProgress size={20} color="inherit" aria-label="Adding member" /> : 'Add'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminGroups;
