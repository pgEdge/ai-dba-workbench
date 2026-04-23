/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Chip } from '@mui/material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import {
    useChannelCRUD,
    ChannelTable,
    ChannelDialogShell,
    ChannelColumnDef,
} from './channels';
import {
    EmailChannel,
    EmailRecipient,
    EmailFormState,
    DEFAULT_EMAIL_FORM,
    EmailSettingsTab,
    EmailRecipientsTab,
} from './email';

/** Map raw API response to EmailChannel type. */
const mapEmailChannel = (ch: Record<string, unknown>): EmailChannel => ({
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
});

const AdminEmailChannels: React.FC = () => {
    const crud = useChannelCRUD<EmailChannel>('email', mapEmailChannel);

    // Form state
    const [form, setForm] = useState<EmailFormState>(DEFAULT_EMAIL_FORM);

    // Recipients state (for edit mode)
    const [recipients, setRecipients] = useState<EmailRecipient[]>([]);
    const [recipientsLoading, setRecipientsLoading] = useState(false);
    const [recipientSaving, setRecipientSaving] = useState(false);

    // Pending recipients (for create mode)
    const [pendingRecipients, setPendingRecipients] = useState<
        Array<{ email: string; name: string }>
    >([]);

    // Ref to track the currently-editing channel ID for staleness checks.
    // This avoids race conditions when the user quickly switches channels.
    const editingChannelIdRef = useRef<number | null>(null);

    // Keep ref in sync with crud.editingChannel
    useEffect(() => {
        editingChannelIdRef.current = crud.editingChannel?.id ?? null;
    }, [crud.editingChannel]);

    // --- Fetch recipients ---

    const fetchRecipients = useCallback(async (channelId: number) => {
        try {
            setRecipientsLoading(true);
            const data = await apiGet<Record<string, unknown>>(
                `/api/v1/notification-channels/${channelId}/recipients`
            );
            // Guard against stale responses: only update if still editing same channel
            if (editingChannelIdRef.current !== channelId) { return; }
            const raw = (data.recipients || data || []) as Record<string, unknown>[];
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
            // Only show error if still editing the same channel
            if (editingChannelIdRef.current !== channelId) { return; }
            if (err instanceof Error) {
                crud.setDialogError(err.message);
            } else {
                crud.setDialogError('Failed to load recipients');
            }
        } finally {
            // Only clear loading if still editing the same channel
            if (editingChannelIdRef.current === channelId) {
                setRecipientsLoading(false);
            }
        }
    }, [crud]);

    // --- Form change handler ---

    const handleFormChange = (field: keyof EmailFormState, value: string | boolean) => {
        setForm((prev) => ({ ...prev, [field]: value }));
    };

    // --- Dialog open handlers ---

    const handleOpenCreate = () => {
        editingChannelIdRef.current = null;
        setRecipientsLoading(false);
        crud.openCreate();
        setForm(DEFAULT_EMAIL_FORM);
        setRecipients([]);
        setPendingRecipients([]);
    };

    const handleOpenEdit = (e: React.MouseEvent, channel: EmailChannel) => {
        editingChannelIdRef.current = channel.id;
        crud.openEdit(e, channel);
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
        fetchRecipients(channel.id);
    };

    const handleCloseDialog = () => {
        if (crud.saving || recipientSaving) { return; }
        editingChannelIdRef.current = null;
        setRecipientsLoading(false);
        crud.closeDialog();
        setRecipients([]);
        setPendingRecipients([]);
    };

    // --- Save handler ---

    const handleSaveChannel = async () => {
        if (!form.name.trim()) {
            crud.setDialogError('Name is required.');
            return;
        }
        if (!form.smtp_host.trim()) {
            crud.setDialogError('SMTP host is required.');
            return;
        }
        if (!form.from_address.trim()) {
            crud.setDialogError('From address is required.');
            return;
        }
        const portNum = parseInt(form.smtp_port, 10);
        if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
            crud.setDialogError('SMTP port must be a valid port number (1-65535).');
            return;
        }

        try {
            crud.setSaving(true);
            crud.setDialogError(null);

            if (crud.editingChannel) {
                // Update - send only changed fields
                const body: Record<string, unknown> = {};
                if (form.name.trim() !== crud.editingChannel.name) {
                    body.name = form.name.trim();
                }
                if (form.description.trim() !== crud.editingChannel.description) {
                    body.description = form.description.trim();
                }
                if (form.enabled !== crud.editingChannel.enabled) {
                    body.enabled = form.enabled;
                }
                if (form.is_estate_default !== crud.editingChannel.is_estate_default) {
                    body.is_estate_default = form.is_estate_default;
                }
                if (form.smtp_host.trim() !== crud.editingChannel.smtp_host) {
                    body.smtp_host = form.smtp_host.trim();
                }
                if (portNum !== crud.editingChannel.smtp_port) {
                    body.smtp_port = portNum;
                }
                if (form.smtp_username.trim() !== crud.editingChannel.smtp_username) {
                    body.smtp_username = form.smtp_username.trim();
                }
                if (form.smtp_password) {
                    body.smtp_password = form.smtp_password;
                }
                if (form.use_tls !== crud.editingChannel.use_tls) {
                    body.smtp_use_tls = form.use_tls;
                }
                if (form.from_address.trim() !== crud.editingChannel.from_address) {
                    body.from_address = form.from_address.trim();
                }
                if (form.from_name.trim() !== crud.editingChannel.from_name) {
                    body.from_name = form.from_name.trim();
                }

                await apiPut(`/api/v1/notification-channels/${crud.editingChannel.id}`, body);
                crud.setSuccess(`Channel "${form.name.trim()}" updated successfully.`);
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
                const createData = await apiPost<Record<string, unknown>>(
                    '/api/v1/notification-channels',
                    body
                );

                // Capture pending recipients before closing dialog
                const recipientsToAdd = [...pendingRecipients];
                const channelName = form.name.trim();

                // Close dialog immediately to prevent duplicate creation attempts
                crud.closeDialog();
                setRecipients([]);
                setPendingRecipients([]);
                crud.setSaving(false);
                crud.setSuccess(`Channel "${channelName}" created successfully.`);
                crud.fetchChannels();

                // Add pending recipients as best-effort follow-up
                if (recipientsToAdd.length > 0) {
                    const newChannelId = createData.id ||
                        (createData.channel as Record<string, unknown>)?.id;
                    if (newChannelId) {
                        const failedRecipients: string[] = [];
                        for (const pending of recipientsToAdd) {
                            try {
                                await apiPost(
                                    `/api/v1/notification-channels/${newChannelId}/recipients`,
                                    {
                                        email_address: pending.email,
                                        display_name: pending.name,
                                        enabled: true,
                                    }
                                );
                            } catch {
                                failedRecipients.push(pending.email);
                            }
                        }
                        // Update success message if some recipients failed
                        if (failedRecipients.length > 0) {
                            crud.setSuccess(
                                `Channel "${channelName}" created successfully, ` +
                                `but failed to add recipients: ${failedRecipients.join(', ')}.`
                            );
                        }
                        // Refresh to show updated recipient counts
                        crud.fetchChannels();
                    }
                }
                return; // Early return since we already handled cleanup
            }

            crud.closeDialog();
            setRecipients([]);
            setPendingRecipients([]);
            crud.fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                crud.setDialogError(err.message);
            } else {
                crud.setDialogError('An unexpected error occurred');
            }
        } finally {
            crud.setSaving(false);
        }
    };

    // --- Recipient handlers ---

    const handleAddRecipient = async (email: string, name: string) => {
        // In create mode, add to pending recipients locally
        if (!crud.editingChannel) {
            setPendingRecipients((prev) => [...prev, { email, name }]);
            return;
        }

        // In edit mode, persist via API
        const channelId = crud.editingChannel.id;
        try {
            setRecipientSaving(true);
            crud.setDialogError(null);
            await apiPost(
                `/api/v1/notification-channels/${channelId}/recipients`,
                {
                    email_address: email,
                    display_name: name,
                    enabled: true,
                }
            );
            // Guard against stale responses
            if (editingChannelIdRef.current === channelId) {
                fetchRecipients(channelId);
            }
        } catch (err: unknown) {
            // Only show error if still editing the same channel
            if (editingChannelIdRef.current === channelId) {
                if (err instanceof Error) {
                    crud.setDialogError(err.message);
                } else {
                    crud.setDialogError('Failed to add recipient');
                }
            }
        } finally {
            if (editingChannelIdRef.current === channelId) {
                setRecipientSaving(false);
            }
        }
    };

    const handleToggleRecipientEnabled = async (recipient: EmailRecipient) => {
        if (!crud.editingChannel) { return; }
        const channelId = crud.editingChannel.id;
        try {
            setRecipientSaving(true);
            await apiPut(
                `/api/v1/notification-channels/${channelId}/recipients/${recipient.id}`,
                { enabled: !recipient.enabled }
            );
            // Guard against stale responses
            if (editingChannelIdRef.current === channelId) {
                fetchRecipients(channelId);
            }
        } catch (err: unknown) {
            // Only show error if still editing the same channel
            if (editingChannelIdRef.current === channelId) {
                if (err instanceof Error) {
                    crud.setDialogError(err.message);
                } else {
                    crud.setDialogError('Failed to update recipient');
                }
            }
        } finally {
            if (editingChannelIdRef.current === channelId) {
                setRecipientSaving(false);
            }
        }
    };

    const handleDeleteRecipient = async (recipient: EmailRecipient) => {
        if (!crud.editingChannel) { return; }
        const channelId = crud.editingChannel.id;
        try {
            setRecipientSaving(true);
            await apiDelete(
                `/api/v1/notification-channels/${channelId}/recipients/${recipient.id}`
            );
            // Guard against stale responses
            if (editingChannelIdRef.current === channelId) {
                fetchRecipients(channelId);
            }
        } catch (err: unknown) {
            // Only show error if still editing the same channel
            if (editingChannelIdRef.current === channelId) {
                if (err instanceof Error) {
                    crud.setDialogError(err.message);
                } else {
                    crud.setDialogError('Failed to delete recipient');
                }
            }
        } finally {
            if (editingChannelIdRef.current === channelId) {
                setRecipientSaving(false);
            }
        }
    };

    // --- Extra column for recipient count ---

    const recipientCountColumn: ChannelColumnDef<EmailChannel> = {
        label: 'Recipients',
        render: (channel) => (
            <Chip
                label={channel.recipient_count}
                size="small"
                variant="outlined"
            />
        ),
    };

    return (
        <>
            <ChannelTable
                channels={crud.channels}
                loading={crud.loading}
                extraColumns={[recipientCountColumn]}
                testingChannelId={crud.testingChannelId}
                onEdit={handleOpenEdit}
                onDelete={crud.openDelete}
                onToggleEnabled={crud.toggleEnabled}
                onTest={crud.testChannel}
                onAdd={handleOpenCreate}
                emptyMessage="No email channels configured."
                testTooltip="Send test email"
                testAriaLabel="send test email"
                testingAriaLabel="Sending test email"
                title="Email channels"
                error={crud.error}
                success={crud.success}
                onClearError={() => crud.setError(null)}
                onClearSuccess={() => crud.setSuccess(null)}
            />
            <ChannelDialogShell
                open={crud.dialogOpen}
                onClose={handleCloseDialog}
                title={crud.editingChannel
                    ? `Edit channel: ${crud.editingChannel.name}`
                    : 'Create email channel'}
                tabs={['Settings', 'Recipients']}
                activeTab={crud.dialogTab}
                onTabChange={crud.setDialogTab}
                error={crud.dialogError}
                saving={crud.saving}
                onSave={handleSaveChannel}
                saveDisabled={
                    !form.name.trim() ||
                    !form.smtp_host.trim() ||
                    !form.from_address.trim()
                }
                saveLabel={crud.editingChannel ? 'Save' : 'Create'}
                maxWidth="sm"
            >
                <EmailSettingsTab
                    form={form}
                    onChange={handleFormChange}
                    saving={crud.saving}
                    isEditing={!!crud.editingChannel}
                    visible={crud.dialogTab === 0}
                />
                <EmailRecipientsTab
                    visible={crud.dialogTab === 1}
                    isEditing={!!crud.editingChannel}
                    recipients={recipients}
                    recipientsLoading={recipientsLoading}
                    recipientSaving={recipientSaving}
                    onToggleRecipientEnabled={handleToggleRecipientEnabled}
                    onDeleteRecipient={handleDeleteRecipient}
                    onAddRecipient={handleAddRecipient}
                    pendingRecipients={pendingRecipients}
                    onRemovePending={(index) =>
                        setPendingRecipients((prev) => prev.filter((_, i) => i !== index))
                    }
                />
            </ChannelDialogShell>
            <DeleteConfirmationDialog
                open={crud.deleteOpen}
                onClose={crud.closeDelete}
                onConfirm={crud.confirmDelete}
                title="Delete Email Channel"
                message="Are you sure you want to delete the email channel"
                itemName={crud.deleteChannel?.name ? `"${crud.deleteChannel.name}"?` : '?'}
                loading={crud.deleteLoading}
            />
        </>
    );
};

export default AdminEmailChannels;
