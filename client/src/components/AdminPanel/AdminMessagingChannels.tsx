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
import { useCallback, useState } from 'react';
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
import { useCrudPanel, extractErrorMessage } from './_shared';

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

/**
 * Messaging channel as returned by the API.
 *
 * The server redacts `webhook_url` (issue #187); clients only see whether
 * one is configured via `webhook_url_set`.
 */
interface MessagingChannel {
    id: number;
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    webhook_url_set: boolean;
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

    // Fetch and filter to this channel type. The endpoint returns every
    // channel type; we narrow by `channel_type` and normalise the raw
    // record into our typed shape.
    const fetchChannels = useCallback(async (): Promise<MessagingChannel[]> => {
        const data = await apiGet<Record<string, unknown>>('/api/v1/notification-channels');
        const allChannels = (data.notification_channels || data || []) as Array<Record<string, unknown>>;
        return allChannels
            .filter((ch) => ch.channel_type === channelType)
            .map((ch) => ({
                id: ch.id as number,
                name: ch.name as string,
                description: (ch.description as string) || '',
                enabled: ch.enabled as boolean,
                is_estate_default: ch.is_estate_default as boolean,
                webhook_url_set: Boolean(ch.webhook_url_set),
            }));
    }, [channelType]);

    const crud = useCrudPanel<MessagingChannel>({
        fetchItems: fetchChannels,
        deps: [channelType],
    });

    // Per-form fields for the create/edit dialog. Kept here because the
    // shape is channel-specific (name + description + webhook URL +
    // toggle pair).
    const [form, setForm] = useState<ChannelFormState>(DEFAULT_FORM_STATE);

    // Test-notification button uses its own per-row spinner; tracked
    // separately from the shared `saving` flag since it is a non-CRUD
    // action that does not refresh the list.
    const [testingChannelId, setTestingChannelId] = useState<number | null>(null);

    // --- Create / Edit dialog ---

    const handleOpenCreate = () => {
        setForm(DEFAULT_FORM_STATE);
        crud.openCreate();
    };

    const handleOpenEdit = (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        // The webhook URL is a redacted secret on the server; never
        // pre-populate it. An empty value at save time means "leave the
        // existing URL unchanged".
        setForm({
            name: channel.name,
            description: channel.description,
            webhook_url: '',
            enabled: channel.enabled,
            is_estate_default: channel.is_estate_default,
        });
        crud.openEdit(channel);
    };

    const handleFormChange = (field: keyof ChannelFormState, value: string | boolean) => {
        setForm((prev) => ({ ...prev, [field]: value }));
    };

    const handleSaveChannel = async () => {
        const editingChannel = crud.editingItem;
        if (!form.name.trim()) {
            crud.setDialogError('Name is required.');
            return;
        }
        // On create, the URL is required. On edit, an empty URL means
        // "preserve the existing one", which is allowed only when the
        // server already has one configured.
        const trimmedUrl = form.webhook_url.trim();
        if (!editingChannel && !trimmedUrl) {
            crud.setDialogError('Webhook URL is required.');
            return;
        }
        if (editingChannel && !trimmedUrl && !editingChannel.webhook_url_set) {
            crud.setDialogError('Webhook URL is required.');
            return;
        }

        const successName = form.name.trim();
        let request: () => Promise<unknown>;
        if (editingChannel) {
            // Update — send only changed fields. For the webhook URL,
            // omit it entirely when the user left the form blank; the
            // server preserves the stored value when the field is
            // absent from the request body.
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
            if (trimmedUrl) {
                body.webhook_url = trimmedUrl;
            }
            request = () => apiPut(`/api/v1/notification-channels/${editingChannel.id}`, body);
        } else {
            request = () =>
                apiPost('/api/v1/notification-channels', {
                    channel_type: channelType,
                    name: form.name.trim(),
                    description: form.description.trim(),
                    webhook_url: trimmedUrl,
                    enabled: form.enabled,
                    is_estate_default: form.is_estate_default,
                });
        }

        const result = await crud.runMutation(request, {
            successMessage: editingChannel
                ? `Channel "${successName}" updated successfully.`
                : `Channel "${successName}" created successfully.`,
        });
        if (result !== undefined) {
            crud.closeDialog();
        }
    };

    // --- Delete channel ---

    const handleOpenDelete = (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        crud.openDelete(channel);
    };

    const handleDeleteChannel = async () => {
        const target = crud.deleteItem;
        if (!target) { return; }
        const result = await crud.runMutation(
            () => apiDelete(`/api/v1/notification-channels/${target.id}`),
            {
                errorTarget: 'page',
                successMessage: `Channel "${target.name}" deleted successfully.`,
            },
        );
        if (result !== undefined) {
            crud.closeDelete();
        }
    };

    // --- Inline toggle enabled on main table ---

    const handleToggleEnabled = async (channel: MessagingChannel) => {
        await crud.runMutation(
            () => apiPut(`/api/v1/notification-channels/${channel.id}`, { enabled: !channel.enabled }),
            { errorTarget: 'page' },
        );
    };

    // --- Test channel ---

    const handleTestChannel = async (e: React.MouseEvent, channel: MessagingChannel) => {
        e.stopPropagation();
        setTestingChannelId(channel.id);
        try {
            crud.setError(null);
            await apiPost(`/api/v1/notification-channels/${channel.id}/test`);
            crud.setSuccess(`Test notification sent successfully for "${channel.name}".`);
        } catch (err: unknown) {
            crud.setError(extractErrorMessage(err, 'Failed to send test notification'));
        } finally {
            setTestingChannelId(null);
        }
    };

    // --- Render ---

    if (crud.loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading channels" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const editingChannel = crud.editingItem;
    const isEditing = editingChannel !== null;
    // When editing a channel that already has a URL configured, allow
    // saving without re-typing the URL. The empty value will be omitted
    // from the PUT body so the server preserves the stored secret.
    const webhookUrlOptional = isEditing && editingChannel.webhook_url_set;
    const webhookUrlPlaceholder = webhookUrlOptional
        ? 'Leave blank to keep existing URL'
        : '';
    const submitDisabled =
        crud.saving
        || !form.name.trim()
        || (!form.webhook_url.trim() && !webhookUrlOptional);

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

            {crud.error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => { crud.setError(null); }}>
                    {crud.error}
                </Alert>
            )}
            {crud.success && (
                <Alert severity="success" sx={{ mb: 2, borderRadius: 1 }} onClose={() => { crud.setSuccess(null); }}>
                    {crud.success}
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
                        {crud.items.length > 0 ? (
                            crud.items.map((channel) => (
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
                                                onClick={(e) => { handleOpenEdit(e, channel); }}
                                                aria-label="edit channel"
                                            >
                                                <EditIcon fontSize="small" />
                                            </IconButton>
                                        </Tooltip>
                                        <Tooltip title="Delete channel">
                                            <IconButton
                                                size="small"
                                                onClick={(e) => { handleOpenDelete(e, channel); }}
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
                open={crud.dialogOpen}
                onClose={() => { crud.closeDialog(); }}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    {isEditing ? `Edit channel: ${editingChannel.name}` : `Create ${platformName} channel`}
                </DialogTitle>
                <DialogContent>
                    {crud.dialogError && (
                        <Alert severity="error" sx={{ mb: 2, mt: 1, borderRadius: 1 }}>
                            {crud.dialogError}
                        </Alert>
                    )}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={form.name}
                        onChange={(e) => { handleFormChange('name', e.target.value); }}
                        disabled={crud.saving}
                        margin="dense"
                        required
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Description"
                        value={form.description}
                        onChange={(e) => { handleFormChange('description', e.target.value); }}
                        disabled={crud.saving}
                        margin="dense"
                        multiline
                        rows={2}
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label={webhookUrlLabel}
                        value={form.webhook_url}
                        onChange={(e) => { handleFormChange('webhook_url', e.target.value); }}
                        disabled={crud.saving}
                        margin="dense"
                        required={!webhookUrlOptional}
                        placeholder={webhookUrlPlaceholder}
                        helperText={
                            webhookUrlOptional
                                ? 'A webhook URL is configured. Leave this blank to keep it unchanged.'
                                : undefined
                        }
                        InputLabelProps={{ shrink: true }}
                    />
                    <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <FormControlLabel
                            sx={{ ml: 0, gap: 1 }}
                            control={
                                <Switch
                                    checked={form.enabled}
                                    onChange={(e) => { handleFormChange('enabled', e.target.checked); }}
                                    disabled={crud.saving}
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
                                    onChange={(e) => { handleFormChange('is_estate_default', e.target.checked); }}
                                    disabled={crud.saving}
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
                    <Button onClick={() => { crud.closeDialog(); }} disabled={crud.saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveChannel}
                        variant="contained"
                        disabled={submitDisabled}
                        sx={containedButtonSx}
                    >
                        {crud.saving ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : (isEditing ? 'Save' : 'Create')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={crud.deleteOpen}
                onClose={() => { crud.closeDelete(); }}
                onConfirm={handleDeleteChannel}
                title={`Delete ${platformName} Channel`}
                message={`Are you sure you want to delete the ${platformName} channel`}
                itemName={crud.deleteItem?.name ? `"${crud.deleteItem.name}"?` : '?'}
                loading={crud.deleteLoading}
            />
        </Box>
    );
};

export default AdminMessagingChannels;
