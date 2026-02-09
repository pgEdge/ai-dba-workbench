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
    Chip,
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
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Send as SendIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
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

const API_BASE_URL = '/api/v1';

interface EmailChannel {
    id: number;
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    smtp_host: string;
    smtp_port: number;
    smtp_username: string;
    use_tls: boolean;
    from_address: string;
    from_name: string;
    recipient_count: number;
}

interface EmailRecipient {
    id: number;
    email: string;
    display_name: string;
    enabled: boolean;
}

interface ChannelFormState {
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    smtp_host: string;
    smtp_port: string;
    smtp_username: string;
    smtp_password: string;
    use_tls: boolean;
    from_address: string;
    from_name: string;
}

const DEFAULT_FORM_STATE: ChannelFormState = {
    name: '',
    description: '',
    enabled: true,
    is_estate_default: false,
    smtp_host: '',
    smtp_port: '587',
    smtp_username: '',
    smtp_password: '',
    use_tls: true,
    from_address: '',
    from_name: '',
};

const AdminEmailChannels: React.FC = () => {
    const theme = useTheme();

    const [channels, setChannels] = useState<EmailChannel[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Create/Edit dialog state
    const [dialogOpen, setDialogOpen] = useState(false);
    const [dialogTab, setDialogTab] = useState(0);
    const [editingChannel, setEditingChannel] = useState<EmailChannel | null>(null);
    const [form, setForm] = useState<ChannelFormState>(DEFAULT_FORM_STATE);
    const [saving, setSaving] = useState(false);
    const [dialogError, setDialogError] = useState<string | null>(null);

    // Recipients state (shown inside the edit dialog)
    const [recipients, setRecipients] = useState<EmailRecipient[]>([]);
    const [recipientsLoading, setRecipientsLoading] = useState(false);
    const [newRecipientEmail, setNewRecipientEmail] = useState('');
    const [newRecipientName, setNewRecipientName] = useState('');
    const [recipientSaving, setRecipientSaving] = useState(false);

    // Pending recipients for create mode (before channel exists)
    const [pendingRecipients, setPendingRecipients] = useState<Array<{ email: string; name: string }>>([]);

    // Test channel state
    const [testingChannelId, setTestingChannelId] = useState<number | null>(null);

    // Delete channel confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteChannel, setDeleteChannel] = useState<EmailChannel | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // --- Data fetching ---

    const fetchChannels = useCallback(async () => {
        try {
            setLoading(true);
            const response = await fetch(`${API_BASE_URL}/notification-channels`, {
                credentials: 'include',
            });
            if (!response.ok) {throw new Error('Failed to fetch notification channels');}
            const data = await response.json();
            const allChannels = data.notification_channels || data || [];
            const emailChannels: EmailChannel[] = allChannels
                .filter((ch: Record<string, unknown>) => ch.channel_type === 'email')
                .map((ch: Record<string, unknown>) => ({
                    id: ch.id as number,
                    name: ch.name as string,
                    description: (ch.description as string) || '',
                    enabled: ch.enabled as boolean,
                    is_estate_default: ch.is_estate_default as boolean,
                    smtp_host: (ch.smtp_host as string) || '',
                    smtp_port: (ch.smtp_port as number) || 587,
                    smtp_username: (ch.smtp_username as string) || '',
                    use_tls: ch.smtp_use_tls as boolean,
                    from_address: (ch.from_address as string) || '',
                    from_name: (ch.from_name as string) || '',
                    recipient_count: Array.isArray(ch.recipients)
                        ? (ch.recipients as unknown[]).length
                        : 0,
                }));
            setChannels(emailChannels);
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

    const fetchRecipients = useCallback(async (channelId: number) => {
        try {
            setRecipientsLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${channelId}/recipients`,
                { credentials: 'include' }
            );
            if (!response.ok) {throw new Error('Failed to fetch recipients');}
            const data = await response.json();
            const raw = data.recipients || data || [];
            const mapped: EmailRecipient[] = raw.map(
                (r: Record<string, unknown>) => ({
                    id: r.id as number,
                    email: (r.email_address as string) || '',
                    display_name: (r.display_name as string) || '',
                    enabled: r.enabled as boolean,
                })
            );
            setRecipients(mapped);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setDialogError(err.message);
            } else {
                setDialogError('Failed to load recipients');
            }
        } finally {
            setRecipientsLoading(false);
        }
    }, []);

    // --- Create / Edit dialog ---

    const handleOpenCreate = () => {
        setEditingChannel(null);
        setForm(DEFAULT_FORM_STATE);
        setRecipients([]);
        setPendingRecipients([]);
        setDialogError(null);
        setDialogTab(0);
        setNewRecipientEmail('');
        setNewRecipientName('');
        setDialogOpen(true);
    };

    const handleOpenEdit = (e: React.MouseEvent, channel: EmailChannel) => {
        e.stopPropagation();
        setEditingChannel(channel);
        setForm({
            name: channel.name,
            description: channel.description,
            enabled: channel.enabled,
            is_estate_default: channel.is_estate_default,
            smtp_host: channel.smtp_host,
            smtp_port: String(channel.smtp_port),
            smtp_username: channel.smtp_username,
            smtp_password: '',
            use_tls: channel.use_tls,
            from_address: channel.from_address,
            from_name: channel.from_name,
        });
        setDialogError(null);
        setDialogTab(0);
        setNewRecipientEmail('');
        setNewRecipientName('');
        setDialogOpen(true);
        fetchRecipients(channel.id);
    };

    const handleCloseDialog = () => {
        if (saving || recipientSaving) {return;}
        setDialogOpen(false);
        setEditingChannel(null);
        setRecipients([]);
        setPendingRecipients([]);
    };

    const handleFormChange = (field: keyof ChannelFormState, value: string | boolean) => {
        setForm((prev) => ({ ...prev, [field]: value }));
    };

    const handleSaveChannel = async () => {
        if (!form.name.trim()) {
            setDialogError('Name is required.');
            return;
        }
        if (!form.smtp_host.trim()) {
            setDialogError('SMTP host is required.');
            return;
        }
        if (!form.from_address.trim()) {
            setDialogError('From address is required.');
            return;
        }
        const portNum = parseInt(form.smtp_port, 10);
        if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
            setDialogError('SMTP port must be a valid port number (1-65535).');
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
                if (form.smtp_host.trim() !== editingChannel.smtp_host) {
                    body.smtp_host = form.smtp_host.trim();
                }
                if (portNum !== editingChannel.smtp_port) {
                    body.smtp_port = portNum;
                }
                if (form.smtp_username.trim() !== editingChannel.smtp_username) {
                    body.smtp_username = form.smtp_username.trim();
                }
                if (form.smtp_password) {
                    body.smtp_password = form.smtp_password;
                }
                if (form.use_tls !== editingChannel.use_tls) {
                    body.smtp_use_tls = form.use_tls;
                }
                if (form.from_address.trim() !== editingChannel.from_address) {
                    body.from_address = form.from_address.trim();
                }
                if (form.from_name.trim() !== editingChannel.from_name) {
                    body.from_name = form.from_name.trim();
                }

                const response = await fetch(
                    `${API_BASE_URL}/notification-channels/${editingChannel.id}`,
                    {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        credentials: 'include',
                        body: JSON.stringify(body),
                    }
                );
                if (!response.ok) {
                    const data = await response.json();
                    throw new Error(data.error || 'Failed to update channel');
                }
                setSuccess(`Channel "${form.name.trim()}" updated successfully.`);
            } else {
                // Create
                const body = {
                    channel_type: 'email',
                    name: form.name.trim(),
                    description: form.description.trim(),
                    enabled: form.enabled,
                    is_estate_default: form.is_estate_default,
                    smtp_host: form.smtp_host.trim(),
                    smtp_port: portNum,
                    smtp_username: form.smtp_username.trim(),
                    smtp_password: form.smtp_password,
                    smtp_use_tls: form.use_tls,
                    from_address: form.from_address.trim(),
                    from_name: form.from_name.trim(),
                };
                const response = await fetch(
                    `${API_BASE_URL}/notification-channels`,
                    {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        credentials: 'include',
                        body: JSON.stringify(body),
                    }
                );
                if (!response.ok) {
                    const data = await response.json();
                    throw new Error(data.error || 'Failed to create channel');
                }

                // Add any pending recipients to the newly created channel
                if (pendingRecipients.length > 0) {
                    const createData = await response.json();
                    const newChannelId = createData.id || createData.channel?.id;
                    if (newChannelId) {
                        for (const pending of pendingRecipients) {
                            await fetch(
                                `${API_BASE_URL}/notification-channels/${newChannelId}/recipients`,
                                {
                                    method: 'POST',
                                    headers: { 'Content-Type': 'application/json' },
                                    credentials: 'include',
                                    body: JSON.stringify({
                                        email_address: pending.email,
                                        display_name: pending.name,
                                        enabled: true,
                                    }),
                                }
                            );
                        }
                    }
                    setPendingRecipients([]);
                }

                setSuccess(`Channel "${form.name.trim()}" created successfully.`);
            }

            setDialogOpen(false);
            setEditingChannel(null);
            setRecipients([]);
            setPendingRecipients([]);
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

    const handleOpenDelete = (e: React.MouseEvent, channel: EmailChannel) => {
        e.stopPropagation();
        setDeleteChannel(channel);
        setDeleteOpen(true);
    };

    const handleDeleteChannel = async () => {
        if (!deleteChannel) {return;}
        try {
            setDeleteLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${deleteChannel.id}`,
                { method: 'DELETE', credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to delete channel');
            }
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

    const handleToggleEnabled = async (channel: EmailChannel) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${channel.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ enabled: !channel.enabled }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update channel');
            }
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

    const handleTestChannel = async (e: React.MouseEvent, channel: EmailChannel) => {
        e.stopPropagation();
        try {
            setTestingChannelId(channel.id);
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${channel.id}/test`,
                {
                    method: 'POST',
                    credentials: 'include',
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to send test email');
            }
            setSuccess(`Test email sent successfully for "${channel.name}".`);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('Failed to send test email');
            }
        } finally {
            setTestingChannelId(null);
        }
    };

    // --- Recipients management ---

    const handleAddRecipient = async () => {
        if (!newRecipientEmail.trim()) {return;}

        // In create mode, add to pending recipients locally
        if (!editingChannel) {
            setPendingRecipients((prev) => [
                ...prev,
                { email: newRecipientEmail.trim(), name: newRecipientName.trim() },
            ]);
            setNewRecipientEmail('');
            setNewRecipientName('');
            return;
        }

        // In edit mode, persist via API
        try {
            setRecipientSaving(true);
            setDialogError(null);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${editingChannel.id}/recipients`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        email_address: newRecipientEmail.trim(),
                        display_name: newRecipientName.trim(),
                        enabled: true,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to add recipient');
            }
            setNewRecipientEmail('');
            setNewRecipientName('');
            fetchRecipients(editingChannel.id);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setDialogError(err.message);
            } else {
                setDialogError('Failed to add recipient');
            }
        } finally {
            setRecipientSaving(false);
        }
    };

    const handleToggleRecipientEnabled = async (recipient: EmailRecipient) => {
        if (!editingChannel) {return;}
        try {
            setRecipientSaving(true);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${editingChannel.id}/recipients/${recipient.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ enabled: !recipient.enabled }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update recipient');
            }
            fetchRecipients(editingChannel.id);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setDialogError(err.message);
            } else {
                setDialogError('Failed to update recipient');
            }
        } finally {
            setRecipientSaving(false);
        }
    };

    const handleDeleteRecipient = async (recipient: EmailRecipient) => {
        if (!editingChannel) {return;}
        try {
            setRecipientSaving(true);
            const response = await fetch(
                `${API_BASE_URL}/notification-channels/${editingChannel.id}/recipients/${recipient.id}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) {
                throw new Error('Failed to delete recipient');
            }
            fetchRecipients(editingChannel.id);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setDialogError(err.message);
            } else {
                setDialogError('Failed to delete recipient');
            }
        } finally {
            setRecipientSaving(false);
        }
    };

    // --- Helpers ---

    const truncateDescription = (desc: string): string => {
        if (!desc) {return '';}
        const firstLine = desc.split('\n')[0];
        if (firstLine.length <= 60) {return firstLine;}
        return firstLine.substring(0, 60) + '...';
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
                    Email Channels
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
                            <TableCell sx={tableHeaderCellSx}>Recipients</TableCell>
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
                                        <Chip
                                            label={channel.recipient_count}
                                            size="small"
                                            variant="outlined"
                                        />
                                    </TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={channel.enabled}
                                            size="small"
                                            onChange={() => handleToggleEnabled(channel)}
                                        />
                                    </TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={channel.is_estate_default}
                                            size="small"
                                            disabled
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <Tooltip title="Send test email">
                                            <span>
                                                <IconButton
                                                    size="small"
                                                    onClick={(e) => handleTestChannel(e, channel)}
                                                    aria-label="send test email"
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
                                <TableCell colSpan={6} align="center" sx={emptyRowSx}>
                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                        No email channels configured.
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
                    {isEditing ? `Edit Channel: ${editingChannel.name}` : 'Create Email Channel'}
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
                        <Tab label="Recipients" />
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
                            label="SMTP Host"
                            value={form.smtp_host}
                            onChange={(e) => handleFormChange('smtp_host', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            required
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            fullWidth
                            label="SMTP Port"
                            type="number"
                            value={form.smtp_port}
                            onChange={(e) => handleFormChange('smtp_port', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            inputProps={{ min: 1, max: 65535 }}
                            InputLabelProps={{ shrink: true }}
                            sx={(sxTheme) => ({
                                '& input[type=number]': {
                                    colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                                },
                            })}
                        />
                        <TextField
                            fullWidth
                            label="SMTP Username"
                            value={form.smtp_username}
                            onChange={(e) => handleFormChange('smtp_username', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            fullWidth
                            label="SMTP Password"
                            type="password"
                            value={form.smtp_password}
                            onChange={(e) => handleFormChange('smtp_password', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            placeholder={isEditing ? '(unchanged)' : ''}
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            fullWidth
                            label="From Address"
                            value={form.from_address}
                            onChange={(e) => handleFormChange('from_address', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            required
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            fullWidth
                            label="From Name"
                            value={form.from_name}
                            onChange={(e) => handleFormChange('from_name', e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                        />
                        <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                            <FormControlLabel
                                sx={{ ml: 0, gap: 1 }}
                                control={
                                    <Switch
                                        checked={form.use_tls}
                                        onChange={(e) => handleFormChange('use_tls', e.target.checked)}
                                        disabled={saving}
                                    />
                                }
                                label="Use TLS"
                            />
                            <FormControlLabel
                                sx={{ ml: 0, gap: 1 }}
                                control={
                                    <Switch
                                        checked={form.enabled}
                                        onChange={(e) => handleFormChange('enabled', e.target.checked)}
                                        disabled={saving}
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

                    {/* Tab 1: Recipients */}
                    <Box sx={{ display: dialogTab === 1 ? 'block' : 'none' }}>
                        {isEditing && recipientsLoading ? (
                            <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                                <CircularProgress size={24} />
                            </Box>
                        ) : (
                            <TableContainer
                                component={Paper}
                                elevation={0}
                                sx={tableContainerSx}
                            >
                                <Table size="small">
                                    <TableHead>
                                        <TableRow>
                                            <TableCell sx={tableHeaderCellSx}>Email</TableCell>
                                            <TableCell sx={tableHeaderCellSx}>Display Name</TableCell>
                                            {isEditing && (
                                                <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                                            )}
                                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {isEditing && recipients.map((recipient) => (
                                            <TableRow key={recipient.id}>
                                                <TableCell>{recipient.email}</TableCell>
                                                <TableCell>{recipient.display_name || '-'}</TableCell>
                                                <TableCell>
                                                    <Switch
                                                        checked={recipient.enabled}
                                                        size="small"
                                                        onChange={() => handleToggleRecipientEnabled(recipient)}
                                                        disabled={recipientSaving}
                                                    />
                                                </TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => handleDeleteRecipient(recipient)}
                                                        aria-label="delete recipient"
                                                        sx={deleteIconSx}
                                                        disabled={recipientSaving}
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                        {!isEditing && pendingRecipients.map((pending, index) => (
                                            <TableRow key={`pending-${index}`}>
                                                <TableCell>{pending.email}</TableCell>
                                                <TableCell>{pending.name || '-'}</TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={() => setPendingRecipients(
                                                            (prev) => prev.filter((_, i) => i !== index)
                                                        )}
                                                        aria-label="remove pending recipient"
                                                        sx={deleteIconSx}
                                                    >
                                                        <DeleteIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                        {((isEditing && recipients.length === 0) ||
                                          (!isEditing && pendingRecipients.length === 0)) && (
                                            <TableRow>
                                                <TableCell
                                                    colSpan={isEditing ? 4 : 3}
                                                    align="center"
                                                    sx={emptyRowSx}
                                                >
                                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                        No recipients configured.
                                                    </Typography>
                                                </TableCell>
                                            </TableRow>
                                        )}
                                        {/* Add recipient row */}
                                        <TableRow>
                                            <TableCell>
                                                <TextField
                                                    size="small"
                                                    placeholder="Email address"
                                                    value={newRecipientEmail}
                                                    onChange={(e) => setNewRecipientEmail(e.target.value)}
                                                    disabled={recipientSaving}
                                                    fullWidth
                                                    variant="standard"
                                                />
                                            </TableCell>
                                            <TableCell>
                                                <TextField
                                                    size="small"
                                                    placeholder="Display name"
                                                    value={newRecipientName}
                                                    onChange={(e) => setNewRecipientName(e.target.value)}
                                                    disabled={recipientSaving}
                                                    fullWidth
                                                    variant="standard"
                                                />
                                            </TableCell>
                                            <TableCell
                                                colSpan={isEditing ? 2 : 1}
                                                align="right"
                                            >
                                                <Button
                                                    size="small"
                                                    variant="contained"
                                                    startIcon={recipientSaving
                                                        ? <CircularProgress size={14} color="inherit" />
                                                        : <AddIcon />
                                                    }
                                                    onClick={handleAddRecipient}
                                                    disabled={recipientSaving || !newRecipientEmail.trim()}
                                                    sx={containedButtonSx}
                                                >
                                                    Add
                                                </Button>
                                            </TableCell>
                                        </TableRow>
                                    </TableBody>
                                </Table>
                            </TableContainer>
                        )}
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={handleCloseDialog} disabled={saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveChannel}
                        variant="contained"
                        disabled={saving || !form.name.trim() || !form.smtp_host.trim() || !form.from_address.trim()}
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
                title="Delete Email Channel"
                message="Are you sure you want to delete the email channel"
                itemName={deleteChannel?.name ? `"${deleteChannel.name}"?` : '?'}
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminEmailChannels;
