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
import { useState, useCallback } from 'react';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { apiPost, apiPut } from '../../utils/apiClient';
import { useChannelCRUD, ChannelTable, ChannelDialogShell } from './channels';
import {
    type WebhookChannel,
    type WebhookFormState,
    DEFAULT_WEBHOOK_FORM,
    parseAuthCredentials,
    buildAuthCredentials,
    headersObjectToArray,
    headersArrayToObject,
    WebhookSettingsTab,
    WebhookHeadersTab,
    WebhookAuthTab,
    WebhookTemplatesTab,
} from './webhook';

/**
 * Map raw API data to a typed WebhookChannel object.
 */
const mapWebhookChannel = (ch: Record<string, unknown>): WebhookChannel => ({
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
});

const DIALOG_TABS = ['Settings', 'Headers', 'Authentication', 'Templates'];

const AdminWebhookChannels: React.FC = () => {
    const crud = useChannelCRUD<WebhookChannel>('webhook', mapWebhookChannel);

    // Local form state
    const [form, setForm] = useState<WebhookFormState>(DEFAULT_WEBHOOK_FORM);
    const [authFields, setAuthFields] = useState<Record<string, string>>({});

    // --- Form management ---

    const handleFormChange = useCallback(
        (field: keyof WebhookFormState, value: string | boolean) => {
            setForm((prev) => ({ ...prev, [field]: value }));
        },
        [],
    );

    // --- Dialog open handlers ---

    const handleOpenCreate = useCallback(() => {
        setForm(DEFAULT_WEBHOOK_FORM);
        setAuthFields({});
        crud.openCreate();
    }, [crud]);

    const handleOpenEdit = useCallback(
        (e: React.MouseEvent, channel: WebhookChannel) => {
            const parsedAuth = parseAuthCredentials(
                channel.auth_type,
                channel.auth_credentials,
            );
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
            crud.openEdit(e, channel);
        },
        [crud],
    );

    const handleCloseDialog = useCallback(() => {
        crud.closeDialog();
    }, [crud]);

    // --- Header management ---

    const handleAddHeader = useCallback(() => {
        setForm((prev) => ({
            ...prev,
            headers: [
                ...prev.headers,
                { id: crypto.randomUUID(), key: '', value: '' },
            ],
        }));
    }, []);

    const handleHeaderChange = useCallback(
        (id: string, field: 'key' | 'value', value: string) => {
            setForm((prev) => ({
                ...prev,
                headers: prev.headers.map((h) =>
                    h.id === id ? { ...h, [field]: value } : h,
                ),
            }));
        },
        [],
    );

    const handleRemoveHeader = useCallback((id: string) => {
        setForm((prev) => ({
            ...prev,
            headers: prev.headers.filter((h) => h.id !== id),
        }));
    }, []);

    // --- Auth management ---

    const handleAuthTypeChange = useCallback((newAuthType: string) => {
        setForm((prev) => ({ ...prev, auth_type: newAuthType }));
        setAuthFields({});
    }, []);

    const handleAuthFieldChange = useCallback((field: string, value: string) => {
        setAuthFields((prev) => ({ ...prev, [field]: value }));
    }, []);

    // --- Save channel ---

    const handleSaveChannel = useCallback(async () => {
        if (!form.name.trim()) {
            crud.setDialogError('Name is required.');
            return;
        }
        if (!form.endpoint_url.trim()) {
            crud.setDialogError('Endpoint URL is required.');
            return;
        }

        const headersObj = headersArrayToObject(form.headers);
        const authCredentials = buildAuthCredentials(form.auth_type, authFields);

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
                if (form.endpoint_url.trim() !== crud.editingChannel.endpoint_url) {
                    body.endpoint_url = form.endpoint_url.trim();
                }
                if (form.http_method !== crud.editingChannel.http_method) {
                    body.http_method = form.http_method;
                }
                if (
                    JSON.stringify(headersObj) !==
                    JSON.stringify(crud.editingChannel.headers)
                ) {
                    body.headers = headersObj;
                }
                if (form.auth_type !== crud.editingChannel.auth_type) {
                    body.auth_type = form.auth_type;
                }
                if (authCredentials !== crud.editingChannel.auth_credentials) {
                    body.auth_credentials = authCredentials;
                }
                if (
                    form.template_alert_fire.trim() !==
                    (crud.editingChannel.template_alert_fire || '')
                ) {
                    body.template_alert_fire = form.template_alert_fire.trim();
                }
                if (
                    form.template_alert_clear.trim() !==
                    (crud.editingChannel.template_alert_clear || '')
                ) {
                    body.template_alert_clear = form.template_alert_clear.trim();
                }
                if (
                    form.template_reminder.trim() !==
                    (crud.editingChannel.template_reminder || '')
                ) {
                    body.template_reminder = form.template_reminder.trim();
                }

                await apiPut(
                    `/api/v1/notification-channels/${crud.editingChannel.id}`,
                    body,
                );
                crud.setSuccess(`Channel "${form.name.trim()}" updated successfully.`);
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
                    ...(form.template_alert_fire.trim()
                        ? { template_alert_fire: form.template_alert_fire.trim() }
                        : {}),
                    ...(form.template_alert_clear.trim()
                        ? { template_alert_clear: form.template_alert_clear.trim() }
                        : {}),
                    ...(form.template_reminder.trim()
                        ? { template_reminder: form.template_reminder.trim() }
                        : {}),
                };
                await apiPost('/api/v1/notification-channels', body);
                crud.setSuccess(`Channel "${form.name.trim()}" created successfully.`);
            }

            crud.closeDialog();
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
    }, [form, authFields, crud]);

    // --- Render ---

    const isEditing = crud.editingChannel !== null;
    const dialogTitle = isEditing
        ? `Edit channel: ${crud.editingChannel?.name}`
        : 'Create webhook channel';

    return (
        <>
            <ChannelTable
                channels={crud.channels}
                loading={crud.loading}
                testingChannelId={crud.testingChannelId}
                onEdit={handleOpenEdit}
                onDelete={crud.openDelete}
                onToggleEnabled={crud.toggleEnabled}
                onTest={crud.testChannel}
                onAdd={handleOpenCreate}
                emptyMessage="No webhook channels configured."
                testTooltip="Send test notification"
                testAriaLabel="send test notification"
                testingAriaLabel="Sending test"
                title="Webhook channels"
                error={crud.error}
                success={crud.success}
                onClearError={() => { crud.setError(null); }}
                onClearSuccess={() => { crud.setSuccess(null); }}
            />

            <ChannelDialogShell
                open={crud.dialogOpen}
                onClose={handleCloseDialog}
                title={dialogTitle}
                tabs={DIALOG_TABS}
                activeTab={crud.dialogTab}
                onTabChange={crud.setDialogTab}
                error={crud.dialogError}
                saving={crud.saving}
                onSave={handleSaveChannel}
                saveDisabled={!form.name.trim() || !form.endpoint_url.trim()}
                saveLabel={isEditing ? 'Save' : 'Create'}
                maxWidth="md"
            >
                <WebhookSettingsTab
                    form={form}
                    onChange={handleFormChange}
                    saving={crud.saving}
                    visible={crud.dialogTab === 0}
                />
                <WebhookHeadersTab
                    headers={form.headers}
                    onAddHeader={handleAddHeader}
                    onChangeHeader={handleHeaderChange}
                    onRemoveHeader={handleRemoveHeader}
                    saving={crud.saving}
                    visible={crud.dialogTab === 1}
                />
                <WebhookAuthTab
                    authType={form.auth_type}
                    authFields={authFields}
                    onAuthTypeChange={handleAuthTypeChange}
                    onAuthFieldChange={handleAuthFieldChange}
                    saving={crud.saving}
                    visible={crud.dialogTab === 2}
                />
                <WebhookTemplatesTab
                    form={form}
                    onChange={handleFormChange}
                    saving={crud.saving}
                    visible={crud.dialogTab === 3}
                />
            </ChannelDialogShell>

            <DeleteConfirmationDialog
                open={crud.deleteOpen}
                onClose={crud.closeDelete}
                onConfirm={crud.confirmDelete}
                title="Delete Webhook Channel"
                message="Are you sure you want to delete the webhook channel"
                itemName={
                    crud.deleteChannel?.name ? `"${crud.deleteChannel.name}"?` : '?'
                }
                loading={crud.deleteLoading}
            />
        </>
    );
};

export default AdminWebhookChannels;
