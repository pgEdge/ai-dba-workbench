/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useChannelCRUD, BaseChannel } from '../index';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiPut = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

// ---------------------------------------------------------------------------
// Test Types and Helpers
// ---------------------------------------------------------------------------

interface TestChannel extends BaseChannel {
    extra_field: string;
}

const mapTestChannel = (raw: Record<string, unknown>): TestChannel => ({
    id: raw.id as number,
    name: raw.name as string,
    description: (raw.description as string) || '',
    enabled: raw.enabled as boolean,
    is_estate_default: raw.is_estate_default as boolean,
    extra_field: (raw.extra_field as string) || '',
});

function makeRawChannels(type: 'email' | 'webhook' = 'email'): Record<string, unknown>[] {
    return [
        {
            id: 1,
            name: 'Channel One',
            description: 'First channel',
            enabled: true,
            is_estate_default: false,
            channel_type: type,
            extra_field: 'extra1',
        },
        {
            id: 2,
            name: 'Channel Two',
            description: 'Second channel',
            enabled: false,
            is_estate_default: true,
            channel_type: type,
            extra_field: 'extra2',
        },
    ];
}

function makeApiResponse(channels: Record<string, unknown>[]): Record<string, unknown> {
    return { notification_channels: channels };
}

function createMockMouseEvent(): React.MouseEvent {
    return {
        stopPropagation: vi.fn(),
    } as unknown as React.MouseEvent;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useChannelCRUD', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    // -----------------------------------------------------------------------
    // Initial State and Fetch Tests
    // -----------------------------------------------------------------------

    describe('fetching channels', () => {
        it('fetches channels on mount and filters by channel_type', async () => {
            const emailChannels = makeRawChannels('email');
            const webhookChannels = makeRawChannels('webhook').map((ch) => ({
                ...ch,
                id: ch.id as number + 10,
                channel_type: 'webhook',
            }));
            const allChannels = [...emailChannels, ...webhookChannels];
            mockApiGet.mockResolvedValueOnce(makeApiResponse(allChannels));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            // Initially loading
            expect(result.current.loading).toBe(true);

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(mockApiGet).toHaveBeenCalledTimes(1);
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/notification-channels');
            expect(result.current.channels).toHaveLength(2);
            expect(result.current.channels[0].id).toBe(1);
            expect(result.current.channels[1].id).toBe(2);
            expect(result.current.error).toBeNull();
        });

        it('handles fetch error', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('Network error'));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.error).toBe('Network error');
            });

            expect(result.current.channels).toHaveLength(0);
            expect(result.current.loading).toBe(false);
        });

        it('handles non-Error fetch failure', async () => {
            mockApiGet.mockRejectedValueOnce('Some string error');

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.error).toBe('An unexpected error occurred');
            });
        });

        it('handles response without notification_channels wrapper', async () => {
            const channels = makeRawChannels('email');
            mockApiGet.mockResolvedValueOnce(channels);

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.channels).toHaveLength(2);
        });

        it('refetches when fetchChannels is called', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledTimes(1);
            });

            await act(async () => {
                await result.current.fetchChannels();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    // -----------------------------------------------------------------------
    // Toggle Enabled Tests
    // -----------------------------------------------------------------------

    describe('toggleEnabled', () => {
        it('calls apiPut with negated enabled and refetches', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPut.mockResolvedValueOnce({});

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const channel = result.current.channels[0];
            expect(channel.enabled).toBe(true);

            await act(async () => {
                await result.current.toggleEnabled(channel);
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/notification-channels/1',
                { enabled: false }
            );
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        it('sets error on toggleEnabled failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPut.mockRejectedValueOnce(new Error('Toggle failed'));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            await act(async () => {
                await result.current.toggleEnabled(result.current.channels[0]);
            });

            expect(result.current.error).toBe('Toggle failed');
        });

        it('handles non-Error toggleEnabled failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPut.mockRejectedValueOnce('String error');

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            await act(async () => {
                await result.current.toggleEnabled(result.current.channels[0]);
            });

            expect(result.current.error).toBe('An unexpected error occurred');
        });
    });

    // -----------------------------------------------------------------------
    // Test Channel Tests
    // -----------------------------------------------------------------------

    describe('testChannel', () => {
        it('sets testingChannelId, calls apiPost, and sets success', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPost.mockResolvedValueOnce({});

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();
            const channel = result.current.channels[0];

            // Use a promise to track the test operation
            let testPromise: Promise<void>;
            act(() => {
                testPromise = result.current.testChannel(mockEvent, channel);
            });

            // Check testingChannelId was set
            expect(result.current.testingChannelId).toBe(1);

            await act(async () => {
                await testPromise;
            });

            expect(mockEvent.stopPropagation).toHaveBeenCalled();
            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/notification-channels/1/test');
            expect(result.current.success).toBe('Test notification sent successfully for "Channel One".');
            expect(result.current.testingChannelId).toBeNull();
        });

        it('sets error on testChannel failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPost.mockRejectedValueOnce(new Error('Test failed'));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();

            await act(async () => {
                await result.current.testChannel(mockEvent, result.current.channels[0]);
            });

            expect(result.current.error).toBe('Test failed');
            expect(result.current.testingChannelId).toBeNull();
        });

        it('handles non-Error testChannel failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPost.mockRejectedValueOnce('String error');

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();

            await act(async () => {
                await result.current.testChannel(mockEvent, result.current.channels[0]);
            });

            expect(result.current.error).toBe('Failed to send test notification');
        });

        it('clears previous error when testChannel is called', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiPost.mockResolvedValueOnce({});

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Set an error first
            act(() => {
                result.current.setError('Previous error');
            });
            expect(result.current.error).toBe('Previous error');

            const mockEvent = createMockMouseEvent();

            await act(async () => {
                await result.current.testChannel(mockEvent, result.current.channels[0]);
            });

            // Error should be null after successful test
            expect(result.current.error).toBeNull();
        });
    });

    // -----------------------------------------------------------------------
    // Delete Flow Tests
    // -----------------------------------------------------------------------

    describe('delete flow', () => {
        it('openDelete sets deleteChannel and deleteOpen', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();

            act(() => {
                result.current.openDelete(mockEvent, result.current.channels[0]);
            });

            expect(mockEvent.stopPropagation).toHaveBeenCalled();
            expect(result.current.deleteOpen).toBe(true);
            expect(result.current.deleteChannel).toEqual(result.current.channels[0]);
        });

        it('confirmDelete calls apiDelete, refetches, and clears state', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiDelete.mockResolvedValueOnce({});

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Open delete dialog
            const mockEvent = createMockMouseEvent();
            act(() => {
                result.current.openDelete(mockEvent, result.current.channels[0]);
            });

            // Confirm delete
            await act(async () => {
                await result.current.confirmDelete();
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/notification-channels/1');
            expect(result.current.deleteOpen).toBe(false);
            expect(result.current.deleteChannel).toBeNull();
            expect(result.current.success).toBe('Channel "Channel One" deleted successfully.');
            expect(mockApiGet).toHaveBeenCalledTimes(2); // Initial + refetch
        });

        it('confirmDelete does nothing if deleteChannel is null', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Try to confirm without opening delete dialog
            await act(async () => {
                await result.current.confirmDelete();
            });

            expect(mockApiDelete).not.toHaveBeenCalled();
        });

        it('confirmDelete sets error on failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiDelete.mockRejectedValueOnce(new Error('Delete failed'));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Open delete dialog
            const mockEvent = createMockMouseEvent();
            act(() => {
                result.current.openDelete(mockEvent, result.current.channels[0]);
            });

            // Confirm delete
            await act(async () => {
                await result.current.confirmDelete();
            });

            expect(result.current.error).toBe('Delete failed');
            expect(result.current.deleteLoading).toBe(false);
        });

        it('handles non-Error delete failure', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));
            mockApiDelete.mockRejectedValueOnce('String error');

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();
            act(() => {
                result.current.openDelete(mockEvent, result.current.channels[0]);
            });

            await act(async () => {
                await result.current.confirmDelete();
            });

            expect(result.current.error).toBe('An unexpected error occurred');
        });

        it('closeDelete clears state', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Open delete dialog
            const mockEvent = createMockMouseEvent();
            act(() => {
                result.current.openDelete(mockEvent, result.current.channels[0]);
            });

            expect(result.current.deleteOpen).toBe(true);

            // Close
            act(() => {
                result.current.closeDelete();
            });

            expect(result.current.deleteOpen).toBe(false);
            expect(result.current.deleteChannel).toBeNull();
        });
    });

    // -----------------------------------------------------------------------
    // Dialog State Tests
    // -----------------------------------------------------------------------

    describe('dialog state', () => {
        it('openCreate resets editing to null and opens dialog', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            act(() => {
                result.current.openCreate();
            });

            expect(result.current.dialogOpen).toBe(true);
            expect(result.current.editingChannel).toBeNull();
            expect(result.current.dialogTab).toBe(0);
            expect(result.current.dialogError).toBeNull();
        });

        it('openEdit stores channel and opens dialog', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            const mockEvent = createMockMouseEvent();
            const channel = result.current.channels[0];

            act(() => {
                result.current.openEdit(mockEvent, channel);
            });

            expect(mockEvent.stopPropagation).toHaveBeenCalled();
            expect(result.current.dialogOpen).toBe(true);
            expect(result.current.editingChannel).toEqual(channel);
            expect(result.current.dialogTab).toBe(0);
            expect(result.current.dialogError).toBeNull();
        });

        it('closeDialog clears dialog state when not saving', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Open edit dialog
            const mockEvent = createMockMouseEvent();
            act(() => {
                result.current.openEdit(mockEvent, result.current.channels[0]);
            });

            expect(result.current.dialogOpen).toBe(true);
            expect(result.current.editingChannel).not.toBeNull();

            // Close
            act(() => {
                result.current.closeDialog();
            });

            expect(result.current.dialogOpen).toBe(false);
            expect(result.current.editingChannel).toBeNull();
        });

        it('closeDialog does nothing when saving', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.channels).toHaveLength(2);
            });

            // Open dialog and set saving
            act(() => {
                result.current.openCreate();
            });

            act(() => {
                result.current.setSaving(true);
            });

            expect(result.current.dialogOpen).toBe(true);
            expect(result.current.saving).toBe(true);

            // Try to close - should not work
            act(() => {
                result.current.closeDialog();
            });

            expect(result.current.dialogOpen).toBe(true);
        });

        it('setDialogTab updates the tab', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            act(() => {
                result.current.openCreate();
            });

            expect(result.current.dialogTab).toBe(0);

            act(() => {
                result.current.setDialogTab(2);
            });

            expect(result.current.dialogTab).toBe(2);
        });

        it('setDialogError updates the dialog error', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            act(() => {
                result.current.setDialogError('Validation error');
            });

            expect(result.current.dialogError).toBe('Validation error');
        });

        it('setSaving updates the saving state', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.saving).toBe(false);

            act(() => {
                result.current.setSaving(true);
            });

            expect(result.current.saving).toBe(true);
        });
    });

    // -----------------------------------------------------------------------
    // State Setter Tests
    // -----------------------------------------------------------------------

    describe('state setters', () => {
        it('setError updates error state', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            act(() => {
                result.current.setError('Custom error');
            });

            expect(result.current.error).toBe('Custom error');

            act(() => {
                result.current.setError(null);
            });

            expect(result.current.error).toBeNull();
        });

        it('setSuccess updates success state', async () => {
            mockApiGet.mockResolvedValue(makeApiResponse(makeRawChannels('email')));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('email', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            act(() => {
                result.current.setSuccess('Operation completed');
            });

            expect(result.current.success).toBe('Operation completed');

            act(() => {
                result.current.setSuccess(null);
            });

            expect(result.current.success).toBeNull();
        });
    });

    // -----------------------------------------------------------------------
    // Channel Type Filtering Tests
    // -----------------------------------------------------------------------

    describe('channel type filtering', () => {
        it('filters webhook channels when channelType is webhook', async () => {
            const emailChannels = makeRawChannels('email');
            const webhookChannels = makeRawChannels('webhook').map((ch) => ({
                ...ch,
                id: (ch.id as number) + 100,
                channel_type: 'webhook',
            }));
            const allChannels = [...emailChannels, ...webhookChannels];
            mockApiGet.mockResolvedValueOnce(makeApiResponse(allChannels));

            const { result } = renderHook(() =>
                useChannelCRUD<TestChannel>('webhook', mapTestChannel)
            );

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.channels).toHaveLength(2);
            expect(result.current.channels[0].id).toBe(101);
            expect(result.current.channels[1].id).toBe(102);
        });
    });
});
