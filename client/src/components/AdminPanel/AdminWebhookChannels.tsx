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
    Tab,
    Tabs,
    MenuItem,
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

interface WebhookChannel {
    id: number;
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    endpoint_url: string;
    http_method: string;
    headers: Record<string, string>;
    auth_type: string;
    auth_credentials: string;
    template_alert_fire: string;
    template_alert_clear: string;
    template_reminder: string;
}

interface ChannelFormState {
    name: string;
    description: string;
    endpoint_url: string;
    http_method: string;
    headers: { key: string; value: string }[];
    auth_type: string;
    auth_credentials: string;
    enabled: boolean;
    is_estate_default: boolean;
    template_alert_fire: string;
    template_alert_clear: string;
    template_reminder: string;
}

const DEFAULT_ALERT_FIRE_TEMPLATE = `{
  "event": "alert_fire",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  {{- if .DatabaseName}}
  "database": "{{.DatabaseName}}",
  {{- end}}
  {{- if .MetricName}}
  "metric": {
    "name": "{{.MetricName}}"
    {{- if .MetricValue}}, "value": {{.MetricValue}}{{end}}
    {{- if .ThresholdValue}}, "threshold": {{.ThresholdValue}}{{end}}
    {{- if .Operator}}, "operator": "{{.Operator}}"{{end}}
  },
  {{- end}}
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
}`;

const DEFAULT_ALERT_CLEAR_TEMPLATE = `{
  "event": "alert_clear",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- if .ClearedAt}},
  "cleared_at": "{{.ClearedAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- end}},
  "duration": "{{.Duration}}"
}`;

const DEFAULT_REMINDER_TEMPLATE = `{
  "event": "reminder",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at": "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}",
  "reminder_count": {{.ReminderCount}}
}`;

const DEFAULT_FORM_STATE: ChannelFormState = {
    name: '',
    description: '',
    endpoint_url: '',
    http_method: 'POST',
    headers: [],
    auth_type: 'none',
    auth_credentials: '',
    enabled: true,
    is_estate_default: false,
    template_alert_fire: '',
    template_alert_clear: '',
    template_reminder: '',
};

const HTTP_METHODS = ['POST', 'GET', 'PUT', 'PATCH'];

const AUTH_TYPES = [
    { value: 'none', label: 'None' },
    { value: 'basic', label: 'Basic' },
    { value: 'bearer', label: 'Bearer Token' },
    { value: 'api_key', label: 'API Key' },
];

/**
 * Parse auth_credentials based on auth_type into individual fields.
 */
const parseAuthCredentials = (authType: string, credentials: string): Record<string, string> => {
    switch (authType) {
        case 'basic': {
            const separatorIndex = credentials.indexOf(':');
            if (separatorIndex === -1) {return { username: credentials, password: '' };}
            return {
                username: credentials.substring(0, separatorIndex),
                password: credentials.substring(separatorIndex + 1),
            };
        }
        case 'bearer':
            return { token: credentials };
        case 'api_key': {
            const separatorIndex = credentials.indexOf(':');
            if (separatorIndex === -1) {return { headerName: credentials, apiKeyValue: '' };}
            return {
                headerName: credentials.substring(0, separatorIndex),
                apiKeyValue: credentials.substring(separatorIndex + 1),
            };
        }
        default:
            return {};
    }
};

/**
 * Build auth_credentials string from individual fields based on auth_type.
 */
const buildAuthCredentials = (authType: string, fields: Record<string, string>): string => {
    switch (authType) {
        case 'basic':
            return `${fields.username || ''}:${fields.password || ''}`;
        case 'bearer':
            return fields.token || '';
        case 'api_key':
            return `${fields.headerName || ''}:${fields.apiKeyValue || ''}`;
        default:
            return '';
    }
};

const AdminWebhookChannels: React.FC = () => {
    const theme = useTheme();

    const [channels, setChannels] = useState<WebhookChannel[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Create/Edit dialog state
    const [dialogOpen, setDialogOpen] = useState(false);
    const [dialogTab, setDialogTab] = useState(0);
    const [editingChannel, setEditingChannel] = useState<WebhookChannel | null>(null);
    const [form, setForm] = useState<ChannelFormState>(DEFAULT_FORM_STATE);
    const [authFields, setAuthFields] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);
    const [dialogError, setDialogError] = useState<string | null>(null);

    // Test channel state
    const [testingChannelId, setTestingChannelId] = useState<number | null>(null);

    // Delete channel confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteChannel, setDeleteChannel] = useState<WebhookChannel | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // --- Data fetching ---

    const fetchChannels = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<Record<string, unknown>>('/api/v1/notification-channels');
            const allChannels = (data.notification_channels || data || []) as Record<string, unknown>[];
            const webhookChannels: WebhookChannel[] = allChannels
                .filter((ch: Record<string, unknown>) => ch.channel_type === 'webhook')
                .map((ch: Record<string, unknown>) => ({
                    id: ch.id as number,
                    name: ch.name as string,
                    description: (ch.description as string) || '',
                    enabled: ch.enabled as boolean,
                    is_estate_default: ch.is_estate_default as boolean,
                    endpoint_url: (ch.endpoint_url as string) || '',
                    http_method: (ch.http_method as string) || 'POST',
                    headers: (ch.headers as Record<string, string>) || {},
                    auth_type: (ch.auth_type as string) || 'none',
                    auth_credentials: (ch.auth_credentials as string) || '',
                    template_alert_fire: (ch.template_alert_fire as string) || '',
                    template_alert_clear: (ch.template_alert_clear as string) || '',
                    template_reminder: (ch.template_reminder as string) || '',
                }));
            setChannels(webhookChannels);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchChannels();
    }, [fetchChannels]);

    // --- Helpers ---

    const headersObjectToArray = (headers: Record<string, string>): { key: string; value: string }[] => {
        const entries = Object.entries(headers);
        if (entries.length === 0) {return [];}
        return entries.map(([key, value]) => ({ key, value }));
    };

    const headersArrayToObject = (headers: { key: string; value: string }[]): Record<string, string> => {
        return headers.reduce<Record<string, string>>((acc, h) => {
            if (h.key.trim()) {acc[h.key.trim()] = h.value;}
            return acc;
        }, {});
    };

    const truncateDescription = (desc: string): string => {
        if (!desc) {return '';}
        const firstLine = desc.split('\n')[0];
        if (firstLine.length <= 60) {return firstLine;}
        return firstLine.substring(0, 60) + '...';
    };

    // --- Create / Edit dialog ---

    const handleOpenCreate = () => {
        setEditingChannel(null);
        setForm(DEFAULT_FORM_STATE);
        setAuthFields({});
        setDialogError(null);
        setDialogTab(0);
        setDialogOpen(true);
    };

    const handleOpenEdit = (e: React.MouseEvent, channel: WebhookChannel) => {
        e.stopPropagation();
        setEditingChannel(channel);
        const parsedAuth = parseAuthCredentials(channel.auth_type, channel.auth_credentials);
        setForm({
            name: channel.name,
            description: channel.description,
            endpoint_url: channel.endpoint_url,
            http_method: channel.http_method,
            headers: headersObjectToArray(channel.headers),
            auth_type: channel.auth_type || 'none',
            auth_credentials: channel.auth_credentials,
            enabled: channel.enabled,
            is_estate_default: channel.is_estate_default,
            template_alert_fire: channel.template_alert_fire || '',
            template_alert_clear: channel.template_alert_clear || '',
            template_reminder: channel.template_reminder || '',
        });
        setAuthFields(parsedAuth);
        setDialogError(null);
        setDialogTab(0);
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

    const handleAuthTypeChange = (newAuthType: string) => {
        setForm((prev) => ({ ...prev, auth_type: newAuthType }));
        setAuthFields({});
    };

    const handleAuthFieldChange = (field: string, value: string) => {
        setAuthFields((prev) => ({ ...prev, [field]: value }));
    };

    // --- Headers management ---

    const handleAddHeader = () => {
        setForm((prev) => ({
            ...prev,
            headers: [...prev.headers, { key: '', value: '' }],
        }));
    };

    const handleHeaderChange = (index: number, field: 'key' | 'value', value: string) => {
        setForm((prev) => {
            const updated = [...prev.headers];
            updated[index] = { ...updated[index], [field]: value };
            return { ...prev, headers: updated };
        });
    };

    const handleRemoveHeader = (index: number) => {
        setForm((prev) => ({
            ...prev,
            headers: prev.headers.filter((_, i) => i !== index),
        }));
    };

    // --- Save channel ---

    const handleSaveChannel = async () => {
        if (!form.name.trim()) {
            setDialogError('Name is required.');
            return;
        }
        if (!form.endpoint_url.trim()) {
            setDialogError('Endpoint URL is required.');
            return;
        }

        const headersObj = headersArrayToObject(form.headers);
        const authCredentials = buildAuthCredentials(form.auth_type, authFields);

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
                if (form.endpoint_url.trim() !== editingChannel.endpoint_url) {
                    body.endpoint_url = form.endpoint_url.trim();
                }
                if (form.http_method !== editingChannel.http_method) {
                    body.http_method = form.http_method;
                }
                if (JSON.stringify(headersObj) !== JSON.stringify(editingChannel.headers)) {
                    body.headers = headersObj;
                }
                if (form.auth_type !== editingChannel.auth_type) {
                    body.auth_type = form.auth_type;
                }
                if (authCredentials !== editingChannel.auth_credentials) {
                    body.auth_credentials = authCredentials;
                }
                if (form.template_alert_fire.trim() !== (editingChannel.template_alert_fire || '')) {
                    body.template_alert_fire = form.template_alert_fire.trim();
                }
                if (form.template_alert_clear.trim() !== (editingChannel.template_alert_clear || '')) {
                    body.template_alert_clear = form.template_alert_clear.trim();
                }
                if (form.template_reminder.trim() !== (editingChannel.template_reminder || '')) {
                    body.template_reminder = form.template_reminder.trim();
                }

                await apiPut(`/api/v1/notification-channels/${editingChannel.id}`, body);
                setSuccess(`Channel "${form.name.trim()}" updated successfully.`);
            } else {
                // Create
                const body = {
                    channel_type: 'webhook',
                    name: form.name.trim(),
                    description: form.description.trim(),
                    endpoint_url: form.endpoint_url.trim(),
                    http_method: form.http_method,
                    headers: headersObj,
                    auth_type: form.auth_type,
                    auth_credentials: authCredentials,
                    enabled: form.enabled,
                    is_estate_default: form.is_estate_default,
                    ...(form.template_alert_fire.trim() ? { template_alert_fire: form.template_alert_fire.trim() } : {}),
                    ...(form.template_alert_clear.trim() ? { template_alert_clear: form.template_alert_clear.trim() } : {}),
                    ...(form.template_reminder.trim() ? { template_reminder: form.template_reminder.trim() } : {}),
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

    const handleOpenDelete = (e: React.MouseEvent, channel: WebhookChannel) => {
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

    const handleToggleEnabled = async (channel: WebhookChannel) => {
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

    const handleTestChannel = async (e: React.MouseEvent, channel: WebhookChannel) => {
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
                <CircularProgress />
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
                    Webhook channels
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
                                                        <CircularProgress size={18} />
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
                                        No webhook channels configured.
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
                maxWidth="md"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    {isEditing ? `Edit channel: ${editingChannel.name}` : 'Create webhook channel'}
                </DialogTitle>
                <DialogContent>
                    {dialogError && (
                        <Alert severity="error" sx={{ mb: 2, mt: 1, borderRadius: 1 }}>
                            {dialogError}
                        </Alert>
                    )}
                    <Tabs
                        value={dialogTab}
                        onChange={(_e, newValue: number) => setDialogTab(newValue)}
                        sx={{ mb: 2, borderBottom: 1, borderColor: 'divider' }}
                    >
                        <Tab label="Settings" />
                        <Tab label="Headers" />
                        <Tab label="Authentication" />
                        <Tab label="Templates" />
                    </Tabs>

                    {/* Tab 0: Settings */}
                    <Box sx={{ display: dialogTab === 0 ? 'block' : 'none' }}>
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
                            label="Endpoint URL"
                            value={form.endpoint_url}
                            onChange={(e) => handleFormChange('endpoint_url', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            required
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            fullWidth
                            select
                            label="HTTP Method"
                            value={form.http_method}
                            onChange={(e) => handleFormChange('http_method', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                        >
                            {HTTP_METHODS.map((method) => (
                                <MenuItem key={method} value={method}>
                                    {method}
                                </MenuItem>
                            ))}
                        </TextField>
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
                    </Box>

                    {/* Tab 1: Headers */}
                    <Box sx={{ display: dialogTab === 1 ? 'block' : 'none' }}>
                        {form.headers.length > 0 ? (
                            form.headers.map((header, index) => (
                                <Box
                                    key={index}
                                    sx={{ display: 'flex', gap: 1, alignItems: 'center', mb: 1 }}
                                >
                                    <TextField
                                        label="Key"
                                        value={header.key}
                                        onChange={(e) => handleHeaderChange(index, 'key', e.target.value)}
                                        disabled={saving}
                                        size="small"
                                        sx={{ flex: 1 }}
                                        InputLabelProps={{ shrink: true }}
                                    />
                                    <TextField
                                        label="Value"
                                        value={header.value}
                                        onChange={(e) => handleHeaderChange(index, 'value', e.target.value)}
                                        disabled={saving}
                                        size="small"
                                        sx={{ flex: 1 }}
                                        InputLabelProps={{ shrink: true }}
                                    />
                                    <IconButton
                                        size="small"
                                        onClick={() => handleRemoveHeader(index)}
                                        aria-label="remove header"
                                        sx={deleteIconSx}
                                        disabled={saving}
                                    >
                                        <DeleteIcon fontSize="small" />
                                    </IconButton>
                                </Box>
                            ))
                        ) : (
                            <Typography
                                color="text.secondary"
                                sx={{ fontSize: '1rem', mb: 2, mt: 1 }}
                            >
                                No custom headers configured.
                            </Typography>
                        )}
                        <Button
                            size="small"
                            variant="contained"
                            startIcon={<AddIcon />}
                            onClick={handleAddHeader}
                            disabled={saving}
                            sx={containedButtonSx}
                        >
                            Add Header
                        </Button>
                    </Box>

                    {/* Tab 2: Authentication */}
                    <Box sx={{ display: dialogTab === 2 ? 'block' : 'none' }}>
                        <TextField
                            fullWidth
                            select
                            label="Auth Type"
                            value={form.auth_type}
                            onChange={(e) => handleAuthTypeChange(e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                        >
                            {AUTH_TYPES.map((type) => (
                                <MenuItem key={type.value} value={type.value}>
                                    {type.label}
                                </MenuItem>
                            ))}
                        </TextField>

                        {/* Basic auth fields */}
                        {form.auth_type === 'basic' && (
                            <>
                                <TextField
                                    fullWidth
                                    label="Username"
                                    value={authFields.username || ''}
                                    onChange={(e) => handleAuthFieldChange('username', e.target.value)}
                                    disabled={saving}
                                    margin="dense"
                                    InputLabelProps={{ shrink: true }}
                                />
                                <TextField
                                    fullWidth
                                    label="Password"
                                    type="password"
                                    value={authFields.password || ''}
                                    onChange={(e) => handleAuthFieldChange('password', e.target.value)}
                                    disabled={saving}
                                    margin="dense"
                                    InputLabelProps={{ shrink: true }}
                                />
                            </>
                        )}

                        {/* Bearer token field */}
                        {form.auth_type === 'bearer' && (
                            <TextField
                                fullWidth
                                label="Token"
                                value={authFields.token || ''}
                                onChange={(e) => handleAuthFieldChange('token', e.target.value)}
                                disabled={saving}
                                margin="dense"
                                InputLabelProps={{ shrink: true }}
                            />
                        )}

                        {/* API Key fields */}
                        {form.auth_type === 'api_key' && (
                            <>
                                <TextField
                                    fullWidth
                                    label="Header Name"
                                    value={authFields.headerName || ''}
                                    onChange={(e) => handleAuthFieldChange('headerName', e.target.value)}
                                    disabled={saving}
                                    margin="dense"
                                    InputLabelProps={{ shrink: true }}
                                />
                                <TextField
                                    fullWidth
                                    label="API Key Value"
                                    value={authFields.apiKeyValue || ''}
                                    onChange={(e) => handleAuthFieldChange('apiKeyValue', e.target.value)}
                                    disabled={saving}
                                    margin="dense"
                                    InputLabelProps={{ shrink: true }}
                                />
                            </>
                        )}
                    </Box>

                    {/* Tab 3: Templates */}
                    <Box sx={{ display: dialogTab === 3 ? 'block' : 'none' }}>
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ mb: 2, mt: 1 }}
                        >
                            Templates use{' '}
                            <a
                                href="https://pkg.go.dev/text/template"
                                target="_blank"
                                rel="noopener noreferrer"
                                style={{ color: 'inherit' }}
                            >
                                Go template syntax
                            </a>
                            . Leave blank to use the
                            defaults shown as placeholders. Available variables: AlertID,
                            AlertTitle, AlertDescription, Severity, SeverityColor,
                            SeverityEmoji, ServerName, ServerHost, ServerPort, DatabaseName,
                            MetricName, MetricValue, ThresholdValue, Operator, TriggeredAt,
                            ClearedAt, Duration, ReminderCount, Timestamp.
                        </Typography>
                        <TextField
                            fullWidth
                            label="Alert Fire Template"
                            value={form.template_alert_fire}
                            onChange={(e) => handleFormChange('template_alert_fire', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            multiline
                            rows={12}
                            placeholder={DEFAULT_ALERT_FIRE_TEMPLATE}
                            InputLabelProps={{ shrink: true }}
                            InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
                        />
                        <TextField
                            fullWidth
                            label="Alert Clear Template"
                            value={form.template_alert_clear}
                            onChange={(e) => handleFormChange('template_alert_clear', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            multiline
                            rows={10}
                            placeholder={DEFAULT_ALERT_CLEAR_TEMPLATE}
                            InputLabelProps={{ shrink: true }}
                            InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
                        />
                        <TextField
                            fullWidth
                            label="Alert Reminder Template"
                            value={form.template_reminder}
                            onChange={(e) => handleFormChange('template_reminder', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            multiline
                            rows={10}
                            placeholder={DEFAULT_REMINDER_TEMPLATE}
                            InputLabelProps={{ shrink: true }}
                            InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
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
                        disabled={saving || !form.name.trim() || !form.endpoint_url.trim()}
                        sx={containedButtonSx}
                    >
                        {saving ? <CircularProgress size={20} color="inherit" /> : (isEditing ? 'Save' : 'Create')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => { setDeleteOpen(false); setDeleteChannel(null); }}
                onConfirm={handleDeleteChannel}
                title="Delete Webhook Channel"
                message="Are you sure you want to delete the webhook channel"
                itemName={deleteChannel?.name ? `"${deleteChannel.name}"?` : '?'}
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminWebhookChannels;
