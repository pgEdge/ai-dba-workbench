/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useEffect, useCallback } from 'react';
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
    Switch,
    TextField,
    Tooltip,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    FormControlLabel,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Send as SendIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import { truncateDescription } from '../../utils/textHelpers';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
} from './styles';

/**
 * Configuration that varies between messaging platforms (Slack,
 * Mattermost, etc.).
 */
export interface MessagingChannelConfig {
    /** API channel_type value, e.g. 'slack' or 'mattermost'. */
    channelType: string;
    /** Human-readable platform name shown in headings and messages. */
    platformName: string;
    /** Label for the webhook URL form field. */
    webhookUrlLabel: string;
}

interface MessagingChannel {
    id: number;
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    webhook_url: string;
}

interface ChannelFormState {
    name: string;
    description: string;
    webhook_url: string;
    enabled: boolean;
    is_estate_default: boolean;
}

const DEFAULT_FORM_STATE: ChannelFormState = {
    name: '',
    description: '',
    webhook_url: '',
    enabled: true,
    is_estate_default: false,
};

interface AdminMessagingChannelsProps {
    config: MessagingChannelConfig;
}

const AdminMessagingChannels: React.FC<AdminMessagingChannelsProps> = ({ config }) => {
    const theme = useTheme();
    const { channelType, platformName, webhookUrlLabel } = config;

    const [channels, setChannels] = useState<MessagingChannel[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Create/Edit dialog state
    const [dialogOpen, setDialogOpen] = useState(false);
    const [editingChannel, setEditingChannel] = useState<MessagingChannel | null>(null);
    const [form, setForm] = useState<ChannelFormState>(DEFAULT_FORM_STATE);
    const [saving, setSaving] = useState(false);
    const [dialogError, setDialogError] = useState<string | null>(null);

    // Test channel state
    const [testingChannelId, setTestingChannelId] = useState<number | null>(null);

    // Delete channel confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteChannel, setDeleteChannel] = useState<MessagingChannel | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // --- Data fetching ---

    const fetchChannels = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<Record<string, unknown>>('/api/v1/notification-channels');
            const allChannels = (data.notification_channels || data || []) as Array<Record<string, unknown>>;
            const filtered: MessagingChannel[] = allChannels
                .filter((ch: Record<string, unknown>) => ch.channel_type === channelType)
                .map((ch: Record<string, unknown>) => ({
                    id: ch.id as number,
                    name: ch.name as string,
                    description: (ch.description as string) || '',
                    enabled: ch.enabled as boolean,
                    is_estate_default: ch.is_estate_default as boolean,
                    webhook_url: (ch.webhook_url as string) || '',
                }));
            setChannels(filtered);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setLoading(false);
        }
    }, [channelType]);

    useEffect(() => {
        fetchChannels();
    }, [fetchChannels]);

    // --- Create / Edit dialog ---

    const handleOpenCreate = () => {
        setEditingChannel(null);
        setForm(DEFAULT_FORM_STATE);
        setDialogError(null);
        setDialogOpen(true);
    };

    const handleOpenEdit = (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        setEditingChannel(channel);
        setForm({
            name: channel.name,
            description: channel.description,
            webhook_url: channel.webhook_url,
            enabled: channel.enabled,
            is_estate_default: channel.is_estate_default,
        });
        setDialogError(null);
        setDialogOpen(true);
    };

    const handleCloseDialog = () => {
        if (saving) {return;}
        setDialogOpen(false);
        setEditingChannel(null);
    };

    const handleFormChange = (field: keyof ChannelFormState, value: string | boolean) => {
        setForm((prev) => ({ ...prev, [field]: value }));
    };

    const handleSaveChannel = async () => {
        if (!form.name.trim()) {
            setDialogError('Name is required.');
            return;
        }
        if (!form.webhook_url.trim()) {
            setDialogError('Webhook URL is required.');
            return;
        }

        try {
            setSaving(true);
            setDialogError(null);

            if (editingChannel) {
                // Update - send only changed fields
                const body: Record<string, unknown> = {};
                if (form.name.trim() !== editingChannel.name) {
                    body.name = form.name.trim();
                }
                if (form.description.trim() !== editingChannel.description) {
                    body.description = form.description.trim();
                }
                if (form.enabled !== editingChannel.enabled) {
                    body.enabled = form.enabled;
                }
                if (form.is_estate_default !== editingChannel.is_estate_default) {
                    body.is_estate_default = form.is_estate_default;
                }
                if (form.webhook_url.trim() !== editingChannel.webhook_url) {
                    body.webhook_url = form.webhook_url.trim();
                }

                await apiPut(`/api/v1/notification-channels/${editingChannel.id}`, body);
                setSuccess(`Channel "${form.name.trim()}" updated successfully.`);
            } else {
                // Create
                const body = {
                    channel_type: channelType,
                    name: form.name.trim(),
                    description: form.description.trim(),
                    webhook_url: form.webhook_url.trim(),
                    enabled: form.enabled,
                    is_estate_default: form.is_estate_default,
                };
                await apiPost('/api/v1/notification-channels', body);
                setSuccess(`Channel "${form.name.trim()}" created successfully.`);
            }

            setDialogOpen(false);
            setEditingChannel(null);
            fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setDialogError(err.message);
            } else {
                setDialogError('An unexpected error occurred');
            }
        } finally {
            setSaving(false);
        }
    };

    // --- Delete channel ---

    const handleOpenDelete = (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        setDeleteChannel(channel);
        setDeleteOpen(true);
    };

    const handleDeleteChannel = async () => {
        if (!deleteChannel) {return;}
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/notification-channels/${deleteChannel.id}`);
            setDeleteOpen(false);
            setDeleteChannel(null);
            setSuccess(`Channel "${deleteChannel.name}" deleted successfully.`);
            fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setDeleteLoading(false);
        }
    };

    // --- Inline toggle enabled on main table ---

    const handleToggleEnabled = async (channel: MessagingChannel) => {
        try {
            await apiPut(`/api/v1/notification-channels/${channel.id}`, { enabled: !channel.enabled });
            fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        }
    };

    // --- Test channel ---

    const handleTestChannel = async (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        try {
            setTestingChannelId(channel.id);
            setError(null);
            await apiPost(`/api/v1/notification-channels/${channel.id}/test`);
            setSuccess(`Test notification sent successfully for "${channel.name}".`);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('Failed to send test notification');
            }
        } finally {
            setTestingChannelId(null);
        }
    };

    // --- Render ---

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading channels" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const isEditing = editingChannel !== null;

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    {platformName} Channels
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={handleOpenCreate}
                    sx={containedButtonSx}
                >
                    Add Channel
                </Button>
            </Box>

            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}
            {success && (
                <Alert severity="success" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setSuccess(null)}>
                    {success}
                </Alert>
            )}

            <TableContainer
                component={Paper}
                elevation={0}
                sx={tableContainerSx}
            >
                <Table size="small">
                    <TableHead>
                        <TableRow>
                            <TableCell sx={tableHeaderCellSx}>Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Description</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Estate Default</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {channels.length > 0 ? (
                            channels.map((channel) => (
                                <TableRow key={channel.id} hover>
                                    <TableCell>
                                        {channel.name}
                                    </TableCell>
                                    <TableCell>
                                        {truncateDescription(channel.description)}
                                    </TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={channel.enabled}
                                            size="small"
                                            onChange={() => handleToggleEnabled(channel)}
                                            inputProps={{ 'aria-label': 'Toggle channel enabled' }}
                                        />
                                    </TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={channel.is_estate_default}
                                            size="small"
                                            disabled
                                            inputProps={{ 'aria-label': 'Toggle estate default' }}
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <Tooltip title="Send test notification">
                                            <span>
                                                <IconButton
                                                    size="small"
                                                    onClick={(e) => handleTestChannel(e, channel)}
                                                    aria-label="send test notification"
                                                    disabled={testingChannelId === channel.id}
                                                >
                                                    {testingChannelId === channel.id ? (
                                                        <CircularProgress size={18} aria-label="Sending test" />
                                                    ) : (
                                                        <SendIcon fontSize="small" />
                                                    )}
                                                </IconButton>
                                            </span>
                                        </Tooltip>
                                        <Tooltip title="Edit channel">
                                            <IconButton
                                                size="small"
                                                onClick={(e) => handleOpenEdit(e, channel)}
                                                aria-label="edit channel"
                                            >
                                                <EditIcon fontSize="small" />
                                            </IconButton>
                                        </Tooltip>
                                        <Tooltip title="Delete channel">
                                            <IconButton
                                                size="small"
                                                onClick={(e) => handleOpenDelete(e, channel)}
                                                aria-label="delete channel"
                                                sx={deleteIconSx}
                                            >
                                                <DeleteIcon fontSize="small" />
                                            </IconButton>
                                        </Tooltip>
                                    </TableCell>
                                </TableRow>
                            ))
                        ) : (
                            <TableRow>
                                <TableCell colSpan={5} align="center" sx={emptyRowSx}>
                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                        No {platformName} channels configured.
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* Create / Edit Channel Dialog */}
            <Dialog
                open={dialogOpen}
                onClose={handleCloseDialog}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    {isEditing ? `Edit channel: ${editingChannel.name}` : `Create ${platformName} channel`}
                </DialogTitle>
                <DialogContent>
                    {dialogError && (
                        <Alert severity="error" sx={{ mb: 2, mt: 1, borderRadius: 1 }}>
                            {dialogError}
                        </Alert>
                    )}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={form.name}
                        onChange={(e) => handleFormChange('name', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        required
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Description"
                        value={form.description}
                        onChange={(e) => handleFormChange('description', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label={webhookUrlLabel}
                        value={form.webhook_url}
                        onChange={(e) => handleFormChange('webhook_url', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        required
                        InputLabelProps={{ shrink: true }}
                    />
                    <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <FormControlLabel
                            sx={{ ml: 0, gap: 1 }}
                            control={
                                <Switch
                                    checked={form.enabled}
                                    onChange={(e) => handleFormChange('enabled', e.target.checked)}
                                    disabled={saving}
                                    inputProps={{ 'aria-label': 'Toggle channel enabled' }}
                                />
                            }
                            label="Enabled"
                        />
                        <FormControlLabel
                            sx={{ ml: 0, gap: 1 }}
                            control={
                                <Switch
                                    checked={form.is_estate_default}
                                    onChange={(e) => handleFormChange('is_estate_default', e.target.checked)}
                                    disabled={saving}
                                    inputProps={{ 'aria-label': 'Toggle estate default' }}
                                />
                            }
                            label={
                                <Box>
                                    <Typography variant="body1">Estate Default</Typography>
                                    <Typography variant="caption" color="text.secondary">
                                        When enabled, this channel is active for all servers by default
                                    </Typography>
                                </Box>
                            }
                        />
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={handleCloseDialog} disabled={saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveChannel}
                        variant="contained"
                        disabled={saving || !form.name.trim() || !form.webhook_url.trim()}
                        sx={containedButtonSx}
                    >
                        {saving ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : (isEditing ? 'Save' : 'Create')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => { setDeleteOpen(false); setDeleteChannel(null); }}
                onConfirm={handleDeleteChannel}
                title={`Delete ${platformName} Channel`}
                message={`Are you sure you want to delete the ${platformName} channel`}
                itemName={deleteChannel?.name ? `"${deleteChannel.name}"?` : '?'}
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminMessagingChannels;
