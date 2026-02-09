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
    FormControl,
    InputLabel,
    Select,
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
import { useAuth } from '../../contexts/AuthContext';
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
    getFocusedLabelSx,
} from './styles';

const API_BASE_URL = '/api/v1';

interface AdminGroupsProps {
    mode: string;
}

const AdminGroups: React.FC<AdminGroupsProps> = ({ mode }) => {
    const theme = useTheme();
    const isDark = mode === 'dark';
    const { user } = useAuth();
    const isSuperuser = !!user?.isSuperuser;
    const [groups, setGroups] = useState([]);
    const [connections, setConnections] = useState<Array<{ id: number; name: string }>>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [expandedGroup, setExpandedGroup] = useState(null);
    const [groupDetail, setGroupDetail] = useState(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [effectivePerms, setEffectivePerms] = useState(null);
    const [effectivePermsLoading, setEffectivePermsLoading] = useState(false);

    // Create group dialog
    const [createOpen, setCreateOpen] = useState(false);
    const [createName, setCreateName] = useState('');
    const [createDesc, setCreateDesc] = useState('');
    const [createLoading, setCreateLoading] = useState(false);
    const [createError, setCreateError] = useState(null);

    // Edit group dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editGroup, setEditGroup] = useState(null);
    const [editName, setEditName] = useState('');
    const [editDesc, setEditDesc] = useState('');
    const [editLoading, setEditLoading] = useState(false);
    const [editError, setEditError] = useState(null);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteGroup, setDeleteGroup] = useState(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // Add member dialog
    const [addMemberOpen, setAddMemberOpen] = useState(false);
    const [memberType, setMemberType] = useState('user');
    const [selectedMemberId, setSelectedMemberId] = useState('');
    const [addMemberLoading, setAddMemberLoading] = useState(false);
    const [addMemberError, setAddMemberError] = useState(null);
    const [availableUsers, setAvailableUsers] = useState([]);
    const [availableGroups, setAvailableGroups] = useState([]);

    const fetchGroups = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const [groupsRes, connRes] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/groups`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/connections`, { credentials: 'include' }),
            ]);
            if (!groupsRes.ok) {
                throw new Error('Failed to fetch groups');
            }
            const data = await groupsRes.json();
            setGroups(data.groups || []);
            if (connRes.ok) {
                const connData = await connRes.json();
                setConnections(connData.connections || connData || []);
            }
        } catch (err) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchGroups();
    }, [fetchGroups]);

    const fetchGroupDetail = useCallback(async (groupId) => {
        try {
            setDetailLoading(true);
            setEffectivePermsLoading(true);
            const [detailRes, effectiveRes] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/groups/${groupId}`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/rbac/groups/${groupId}/effective-privileges`, { credentials: 'include' }),
            ]);
            if (!detailRes.ok) {
                throw new Error('Failed to fetch group details');
            }
            const data = await detailRes.json();
            setGroupDetail(data);
            if (effectiveRes.ok) {
                const permsData = await effectiveRes.json();
                setEffectivePerms(permsData);
            } else {
                setEffectivePerms(null);
            }
        } catch (err) {
            setError(err.message);
        } finally {
            setDetailLoading(false);
            setEffectivePermsLoading(false);
        }
    }, []);

    const handleRowClick = (group) => {
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
            const response = await fetch(`${API_BASE_URL}/rbac/groups`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify({
                    name: createName.trim(),
                    description: createDesc.trim(),
                }),
            });
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to create group');
            }
            setCreateOpen(false);
            setCreateName('');
            setCreateDesc('');
            fetchGroups();
        } catch (err) {
            setCreateError(err.message);
        } finally {
            setCreateLoading(false);
        }
    };

    // Edit group
    const handleOpenEdit = (e, group) => {
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
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${editGroup.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        name: editName.trim(),
                        description: editDesc.trim(),
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update group');
            }
            setEditOpen(false);
            fetchGroups();
            if (expandedGroup === editGroup.id) {
                fetchGroupDetail(editGroup.id);
            }
        } catch (err) {
            setEditError(err.message);
        } finally {
            setEditLoading(false);
        }
    };

    // Delete group
    const handleOpenDelete = (e, group) => {
        e.stopPropagation();
        setDeleteGroup(group);
        setDeleteOpen(true);
    };

    const handleDeleteGroup = async () => {
        if (!deleteGroup) {return;}
        try {
            setDeleteLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${deleteGroup.id}`,
                { method: 'DELETE', credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to delete group');
            }
            setDeleteOpen(false);
            setDeleteGroup(null);
            if (expandedGroup === deleteGroup.id) {
                setExpandedGroup(null);
                setGroupDetail(null);
            }
            fetchGroups();
        } catch (err) {
            setError(err.message);
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
            const [usersRes, groupsRes] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/users`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/rbac/groups`, { credentials: 'include' }),
            ]);
            if (usersRes.ok) {
                const data = await usersRes.json();
                setAvailableUsers(data.users || []);
            }
            if (groupsRes.ok) {
                const data = await groupsRes.json();
                // Exclude the current group from the list
                setAvailableGroups(
                    (data.groups || []).filter((g) => g.id !== expandedGroup)
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
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${expandedGroup}/members`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify(
                        memberType === 'user'
                            ? { user_id: parseInt(selectedMemberId, 10) }
                            : { group_id: parseInt(selectedMemberId, 10) }
                    ),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                const errorMsg = data.error || 'Failed to add member';
                throw new Error(
                    errorMsg.includes('UNIQUE constraint')
                        ? 'This member is already in the group.'
                        : errorMsg
                );
            }
            setAddMemberOpen(false);
            fetchGroupDetail(expandedGroup);
            fetchGroups();
        } catch (err) {
            setAddMemberError(err.message);
        } finally {
            setAddMemberLoading(false);
        }
    };

    const handleRemoveMember = async (memberId, mType) => {
        if (!expandedGroup) {return;}
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/groups/${expandedGroup}/members/${mType}/${memberId}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) {
                throw new Error('Failed to remove member');
            }
            fetchGroupDetail(expandedGroup);
            fetchGroups();
        } catch (err) {
            setError(err.message);
        }
    };

    const handleRemoveMemberByName = async (name, mType) => {
        if (!expandedGroup) {return;}
        try {
            let memberId;
            if (mType === 'user') {
                const res = await fetch(`${API_BASE_URL}/rbac/users`, { credentials: 'include' });
                if (res.ok) {
                    const data = await res.json();
                    const user = (data.users || []).find(u => u.username === name);
                    if (user) {memberId = user.id;}
                }
            } else {
                const found = groups.find(g => g.name === name);
                if (found) {memberId = found.id;}
            }
            if (!memberId) {
                setError(`Could not find ${mType} "${name}"`);
                return;
            }
            await handleRemoveMember(memberId, mType);
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

    if (error) {
        return <Alert severity="error" sx={{ borderRadius: 1 }}>{error}</Alert>;
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const textButtonSx = getTextButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const radioSx = getRadioSx(theme);
    const focusedLabelSx = getFocusedLabelSx(theme);

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
                                                        <CircularProgress size={24} />
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
                                                                            primaryTypographyProps={{ fontSize: '0.875rem' }}
                                                                            secondaryTypographyProps={{ fontSize: '0.75rem' }}
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
                                                                            primaryTypographyProps={{ fontSize: '0.875rem' }}
                                                                            secondaryTypographyProps={{ fontSize: '0.75rem' }}
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
                                                            <Typography color="text.secondary" sx={{ fontSize: '0.875rem', py: 1 }}>
                                                                No members in this group.
                                                            </Typography>
                                                        )}
                                                        {effectivePermsLoading ? (
                                                            <Box sx={{ display: 'flex', justifyContent: 'center', py: 2, mt: 3 }}>
                                                                <CircularProgress size={24} />
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
                <DialogTitle sx={dialogTitleSx}>Create Group</DialogTitle>
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
                        {createLoading ? <CircularProgress size={20} color="inherit" /> : 'Create'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Edit Group Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Edit Group</DialogTitle>
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
                        {editLoading ? <CircularProgress size={20} color="inherit" /> : 'Save'}
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
                <DialogTitle sx={dialogTitleSx}>Add Member</DialogTitle>
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
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>
                            {memberType === 'user' ? 'Select User' : 'Select Group'}
                        </InputLabel>
                        <Select
                            value={selectedMemberId}
                            label={memberType === 'user' ? 'Select User' : 'Select Group'}
                            onChange={(e) => setSelectedMemberId(e.target.value)}
                            disabled={addMemberLoading}
                        >
                            {memberType === 'user'
                                ? availableUsers.map((u) => (
                                    <MenuItem key={u.id} value={u.id}>{u.username}</MenuItem>
                                ))
                                : availableGroups.map((g) => (
                                    <MenuItem key={g.id} value={g.id}>{g.name}</MenuItem>
                                ))
                            }
                        </Select>
                    </FormControl>
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
                        {addMemberLoading ? <CircularProgress size={20} color="inherit" /> : 'Add'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminGroups;
