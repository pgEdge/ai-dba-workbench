/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect } from 'react';
import { apiGet, apiPost, apiPut, apiDelete } from '../../../utils/apiClient';
import { BaseChannel } from './channelTypes';

/**
 * Result type for the useChannelCRUD hook.
 * Contains all state and handlers for managing channel CRUD operations.
 */
export interface ChannelCRUDResult<T extends BaseChannel> {
    /** List of channels of the specified type. */
    channels: T[];
    /** Whether the initial fetch is in progress. */
    loading: boolean;
    /** Error message from the last failed operation. */
    error: string | null;
    /** Set the error message. */
    setError: (error: string | null) => void;
    /** Success message from the last successful operation. */
    success: string | null;
    /** Set the success message. */
    setSuccess: (success: string | null) => void;
    /** Fetch all channels from the API. */
    fetchChannels: () => Promise<void>;
    /** Toggle the enabled state of a channel. */
    toggleEnabled: (channel: T) => Promise<void>;
    /** Send a test notification for the channel. */
    testChannel: (e: React.MouseEvent, channel: T) => Promise<void>;
    /** ID of the channel currently being tested. */
    testingChannelId: number | null;

    // Delete state
    /** Whether the delete confirmation dialog is open. */
    deleteOpen: boolean;
    /** The channel being deleted. */
    deleteChannel: T | null;
    /** Whether a delete operation is in progress. */
    deleteLoading: boolean;
    /** Open the delete confirmation dialog. */
    openDelete: (e: React.MouseEvent, channel: T) => void;
    /** Confirm and execute the delete operation. */
    confirmDelete: () => Promise<void>;
    /** Close the delete confirmation dialog. */
    closeDelete: () => void;

    // Dialog state
    /** Whether the create/edit dialog is open. */
    dialogOpen: boolean;
    /** The channel being edited, or null for create mode. */
    editingChannel: T | null;
    /** The currently selected tab in the dialog. */
    dialogTab: number;
    /** Whether a save operation is in progress. */
    saving: boolean;
    /** Error message displayed in the dialog. */
    dialogError: string | null;
    /** Open the dialog in create mode. */
    openCreate: () => void;
    /** Open the dialog in edit mode. */
    openEdit: (e: React.MouseEvent, channel: T) => void;
    /** Close the dialog. */
    closeDialog: () => void;
    /** Set the active tab in the dialog. */
    setDialogTab: (tab: number) => void;
    /** Set the saving state. */
    setSaving: (saving: boolean) => void;
    /** Set the dialog error message. */
    setDialogError: (error: string | null) => void;
}

/**
 * Generic hook for managing channel CRUD operations.
 *
 * This hook encapsulates all the state management shared by email and webhook
 * channel components, including fetching, toggling, testing, deleting, and
 * dialog state management.
 *
 * @param channelType - The type of channel ('email' or 'webhook')
 * @param mapChannel - Function to map raw API data to the channel type
 * @returns All state and handlers for channel CRUD operations
 */
export function useChannelCRUD<T extends BaseChannel>(
    channelType: 'email' | 'webhook',
    mapChannel: (raw: Record<string, unknown>) => T,
): ChannelCRUDResult<T> {
    // Main list state
    const [channels, setChannels] = useState<T[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Test channel state
    const [testingChannelId, setTestingChannelId] = useState<number | null>(null);

    // Delete confirmation state
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteChannel, setDeleteChannel] = useState<T | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // Create/Edit dialog state
    const [dialogOpen, setDialogOpen] = useState(false);
    const [editingChannel, setEditingChannel] = useState<T | null>(null);
    const [dialogTab, setDialogTab] = useState(0);
    const [saving, setSaving] = useState(false);
    const [dialogError, setDialogError] = useState<string | null>(null);

    // --- Data fetching ---

    const fetchChannels = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<Record<string, unknown>>('/api/v1/notification-channels');
            const allChannels = (data.notification_channels || data || []) as Record<string, unknown>[];
            const filtered: T[] = allChannels
                .filter((ch: Record<string, unknown>) => ch.channel_type === channelType)
                .map(mapChannel);
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
    }, [channelType, mapChannel]);

    useEffect(() => {
        void fetchChannels();
    }, [fetchChannels]);

    // --- Toggle enabled ---

    const toggleEnabled = useCallback(async (channel: T) => {
        try {
            await apiPut(`/api/v1/notification-channels/${channel.id}`, {
                enabled: !channel.enabled,
            });
            await fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        }
    }, [fetchChannels]);

    // --- Test channel ---

    const testChannel = useCallback(async (e: React.MouseEvent, channel: T) => {
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
    }, []);

    // --- Delete operations ---

    const openDelete = useCallback((e: React.MouseEvent, channel: T) => {
        e.stopPropagation();
        setDeleteChannel(channel);
        setDeleteOpen(true);
    }, []);

    const confirmDelete = useCallback(async () => {
        if (!deleteChannel) { return; }
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/notification-channels/${deleteChannel.id}`);
            setDeleteOpen(false);
            setSuccess(`Channel "${deleteChannel.name}" deleted successfully.`);
            setDeleteChannel(null);
            await fetchChannels();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setDeleteLoading(false);
        }
    }, [deleteChannel, fetchChannels]);

    const closeDelete = useCallback(() => {
        setDeleteOpen(false);
        setDeleteChannel(null);
    }, []);

    // --- Dialog operations ---

    const openCreate = useCallback(() => {
        setEditingChannel(null);
        setDialogError(null);
        setDialogTab(0);
        setDialogOpen(true);
    }, []);

    const openEdit = useCallback((e: React.MouseEvent, channel: T) => {
        e.stopPropagation();
        setEditingChannel(channel);
        setDialogError(null);
        setDialogTab(0);
        setDialogOpen(true);
    }, []);

    const closeDialog = useCallback(() => {
        if (saving) { return; }
        setDialogOpen(false);
        setEditingChannel(null);
    }, [saving]);

    return {
        channels,
        loading,
        error,
        setError,
        success,
        setSuccess,
        fetchChannels,
        toggleEnabled,
        testChannel,
        testingChannelId,
        // Delete state
        deleteOpen,
        deleteChannel,
        deleteLoading,
        openDelete,
        confirmDelete,
        closeDelete,
        // Dialog state
        dialogOpen,
        editingChannel,
        dialogTab,
        saving,
        dialogError,
        openCreate,
        openEdit,
        closeDialog,
        setDialogTab,
        setSaving,
        setDialogError,
    };
}
