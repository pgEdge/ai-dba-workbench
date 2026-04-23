/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiPut = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

import AdminWebhookChannels from '../AdminWebhookChannels';

let uuidCounter = 0;

const mockWebhookChannels = [
    {
        id: 1,
        channel_type: 'webhook',
        name: 'Test Webhook',
        description: 'Test webhook description',
        enabled: true,
        is_estate_default: false,
        endpoint_url: 'https://example.com/webhook',
        http_method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        auth_type: 'bearer',
        auth_credentials: 'test-token',
        template_alert_fire: '',
        template_alert_clear: '',
        template_reminder: '',
    },
    {
        id: 2,
        channel_type: 'webhook',
        name: 'Another Webhook',
        description: 'Another description',
        enabled: false,
        is_estate_default: true,
        endpoint_url: 'https://other.com/api',
        http_method: 'PUT',
        headers: {},
        auth_type: 'basic',
        auth_credentials: 'user:pass',
        template_alert_fire: '{"custom": true}',
        template_alert_clear: '',
        template_reminder: '',
    },
];

describe('AdminWebhookChannels', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        uuidCounter = 0;
        vi.stubGlobal('crypto', {
            randomUUID: vi.fn(() => `test-uuid-${++uuidCounter}`),
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('displays loading state initially', () => {
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<AdminWebhookChannels />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(screen.getByLabelText('Loading webhook channels')).toBeInTheDocument();
    });

    it('renders channel list after loading', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        expect(screen.getByText('Another Webhook')).toBeInTheDocument();
        expect(screen.getByText('Webhook channels')).toBeInTheDocument();
    });

    it('renders empty state when no webhook channels exist', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: [] });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
        });
    });

    it('filters out non-webhook channels', async () => {
        const mixedChannels = [
            ...mockWebhookChannels,
            { id: 3, channel_type: 'email', name: 'Email Channel' },
        ];
        mockApiGet.mockResolvedValue({ notification_channels: mixedChannels });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        expect(screen.queryByText('Email Channel')).not.toBeInTheDocument();
    });

    it('opens create dialog with empty form', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: [] });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
        });

        const addButton = screen.getByRole('button', { name: /Add Channel/i });
        await user.click(addButton);

        await waitFor(() => {
            expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
        });

        expect(screen.getByLabelText('Name *')).toHaveValue('');
        expect(screen.getByLabelText('Endpoint URL *')).toHaveValue('');
    });

    it('opens edit dialog populated with channel data', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
        });

        expect(screen.getByLabelText('Name *')).toHaveValue('Test Webhook');
        expect(screen.getByLabelText('Endpoint URL *')).toHaveValue('https://example.com/webhook');
    });

    it('validates required fields on save', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: [] });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Add Channel/i }));

        await waitFor(() => {
            expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
        });

        // Try to save without filling in required fields
        // The Create button should be disabled
        const createButton = screen.getByRole('button', { name: 'Create' });
        expect(createButton).toBeDisabled();

        // Fill in name only
        fireEvent.change(screen.getByLabelText('Name *'), { target: { value: 'New Webhook' } });

        // Should still be disabled (missing endpoint_url)
        expect(createButton).toBeDisabled();

        // Fill in endpoint URL
        fireEvent.change(screen.getByLabelText('Endpoint URL *'), { target: { value: 'https://test.com/hook' } });

        // Now should be enabled
        expect(createButton).not.toBeDisabled();
    });

    it('creates channel via API on save', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: [] });
        mockApiPost.mockResolvedValue({ id: 99 });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Add Channel/i }));

        await waitFor(() => {
            expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
        });

        fireEvent.change(screen.getByLabelText('Name *'), { target: { value: 'New Webhook' } });
        fireEvent.change(screen.getByLabelText('Endpoint URL *'), { target: { value: 'https://test.com/hook' } });

        await user.click(screen.getByRole('button', { name: 'Create' }));

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/notification-channels',
                expect.objectContaining({
                    channel_type: 'webhook',
                    name: 'New Webhook',
                    endpoint_url: 'https://test.com/hook',
                }),
            );
        });
    });

    it('updates channel via API with only changed fields', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
        });

        // Clear and change the name
        const nameField = screen.getByLabelText('Name *');
        fireEvent.change(nameField, { target: { value: 'Updated Webhook Name' } });

        await user.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/notification-channels/1',
                expect.objectContaining({
                    name: 'Updated Webhook Name',
                }),
            );
        });

        // Should not include unchanged fields
        const [, body] = mockApiPut.mock.calls[0];
        expect(body).not.toHaveProperty('endpoint_url');
        expect(body).not.toHaveProperty('http_method');
    });

    it('deletes channel via confirmation dialog', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
        mockApiDelete.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: 'delete channel' });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Webhook Channel')).toBeInTheDocument();
        });

        expect(screen.getByText(/"Test Webhook"\?/)).toBeInTheDocument();

        await user.click(screen.getByRole('button', { name: 'Delete' }));

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/notification-channels/1');
        });
    });

    it('toggles channel enabled state', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
        mockApiPut.mockResolvedValue({});

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        const switches = screen.getAllByRole('checkbox', { name: 'Toggle channel enabled' });
        fireEvent.click(switches[0]);

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/notification-channels/1',
                { enabled: false },
            );
        });
    });

    it('sends test notification', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
        mockApiPost.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Test Webhook')).toBeInTheDocument();
        });

        const testButtons = screen.getAllByRole('button', { name: 'send test notification' });
        await user.click(testButtons[0]);

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/notification-channels/1/test');
        });

        await waitFor(() => {
            expect(screen.getByText(/Test notification sent successfully/)).toBeInTheDocument();
        });
    });

    it('displays error messages from API', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });
    });

    it('displays success message after channel creation', async () => {
        mockApiGet.mockResolvedValue({ notification_channels: [] });
        mockApiPost.mockResolvedValue({ id: 99 });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminWebhookChannels />);

        await waitFor(() => {
            expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Add Channel/i }));

        await waitFor(() => {
            expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
        });

        fireEvent.change(screen.getByLabelText('Name *'), { target: { value: 'New Webhook' } });
        fireEvent.change(screen.getByLabelText('Endpoint URL *'), { target: { value: 'https://test.com/hook' } });
        await user.click(screen.getByRole('button', { name: 'Create' }));

        await waitFor(() => {
            expect(screen.getByText(/Channel "New Webhook" created successfully/)).toBeInTheDocument();
        });
    });

    describe('Headers tab', () => {
        it('adds headers in the Headers tab', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 99 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
            });

            // Fill required fields first
            fireEvent.change(screen.getByLabelText('Name *'), { target: { value: 'Test' } });
            fireEvent.change(screen.getByLabelText('Endpoint URL *'), { target: { value: 'https://test.com' } });

            // Navigate to Headers tab
            await user.click(screen.getByRole('tab', { name: 'Headers' }));

            expect(screen.getByText('No custom headers configured.')).toBeInTheDocument();

            // Add a header
            await user.click(screen.getByRole('button', { name: /Add Header/i }));

            // Fill in header key and value
            const keyField = screen.getByLabelText('Key');
            const valueField = screen.getByLabelText('Value');
            fireEvent.change(keyField, { target: { value: 'X-Custom-Header' } });
            fireEvent.change(valueField, { target: { value: 'custom-value' } });

            // Verify the "No custom headers" message is gone
            expect(screen.queryByText('No custom headers configured.')).not.toBeInTheDocument();
        });

        it('removes headers in the Headers tab', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            // Navigate to Headers tab
            await user.click(screen.getByRole('tab', { name: 'Headers' }));

            // The existing header should be displayed
            expect(screen.getByDisplayValue('Content-Type')).toBeInTheDocument();

            // Remove the header
            const removeButton = screen.getByRole('button', { name: 'remove header' });
            await user.click(removeButton);

            // Should now show empty state
            expect(screen.getByText('No custom headers configured.')).toBeInTheDocument();
        });

        it('changes header key and value', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Headers' }));

            const keyField = screen.getByDisplayValue('Content-Type');
            fireEvent.change(keyField, { target: { value: 'X-New-Header' } });

            expect(screen.getByDisplayValue('X-New-Header')).toBeInTheDocument();
        });
    });

    describe('Authentication tab', () => {
        it('displays bearer auth fields when bearer type is selected', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Bearer is already selected for this channel
            expect(screen.getByLabelText('Token')).toHaveValue('test-token');
        });

        it('displays basic auth fields when basic type is selected', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]); // Second channel has basic auth

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Another Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            expect(screen.getByLabelText('Username')).toHaveValue('user');
            expect(screen.getByLabelText('Password')).toHaveValue('pass');
        });

        it('displays different auth fields based on channel auth type', async () => {
            // Test that basic auth shows username/password
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            // Second channel has basic auth type
            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Another Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Basic auth fields should be visible
            expect(screen.getByLabelText('Username')).toHaveValue('user');
            expect(screen.getByLabelText('Password')).toHaveValue('pass');

            // Bearer token field should not be visible
            expect(screen.queryByLabelText('Token')).not.toBeInTheDocument();
            // API Key fields should not be visible
            expect(screen.queryByLabelText('Header Name')).not.toBeInTheDocument();
        });

        it('shows no auth fields for none auth type', async () => {
            // Create a channel with 'none' auth type
            const channelWithNoAuth = [{
                id: 3,
                channel_type: 'webhook',
                name: 'No Auth Webhook',
                description: 'Test',
                enabled: true,
                is_estate_default: false,
                endpoint_url: 'https://example.com/hook',
                http_method: 'POST',
                headers: {},
                auth_type: 'none',
                auth_credentials: '',
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithNoAuth });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('No Auth Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: No Auth Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // No credential fields should be visible for 'none' auth type
            expect(screen.queryByLabelText('Token')).not.toBeInTheDocument();
            expect(screen.queryByLabelText('Username')).not.toBeInTheDocument();
            expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
            expect(screen.queryByLabelText('Header Name')).not.toBeInTheDocument();
            expect(screen.queryByLabelText('API Key Value')).not.toBeInTheDocument();
        });

        it('displays api key auth fields', async () => {
            // Create a channel with api_key auth type
            const channelWithApiKey = [{
                id: 4,
                channel_type: 'webhook',
                name: 'API Key Webhook',
                description: 'Test',
                enabled: true,
                is_estate_default: false,
                endpoint_url: 'https://example.com/hook',
                http_method: 'POST',
                headers: {},
                auth_type: 'api_key',
                auth_credentials: 'X-Api-Key:secret123',
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithApiKey });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('API Key Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: API Key Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // API Key fields should be visible
            expect(screen.getByLabelText('Header Name')).toHaveValue('X-Api-Key');
            expect(screen.getByLabelText('API Key Value')).toHaveValue('secret123');

            // Other auth fields should not be visible
            expect(screen.queryByLabelText('Token')).not.toBeInTheDocument();
            expect(screen.queryByLabelText('Username')).not.toBeInTheDocument();
        });

        it('masks sensitive credentials with password input type', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            // Edit the first channel (bearer token auth)
            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Bearer token should be masked with password type
            const tokenField = screen.getByLabelText('Token');
            expect(tokenField).toHaveAttribute('type', 'password');
            expect(tokenField).toHaveAttribute('autocomplete', 'off');
        });

        it('masks API key value with password input type', async () => {
            // Create a channel with api_key auth type
            const channelWithApiKey = [{
                id: 4,
                channel_type: 'webhook',
                name: 'API Key Webhook',
                description: 'Test',
                enabled: true,
                is_estate_default: false,
                endpoint_url: 'https://example.com/hook',
                http_method: 'POST',
                headers: {},
                auth_type: 'api_key',
                auth_credentials: 'X-Api-Key:secret123',
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithApiKey });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('API Key Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: API Key Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // API Key Value should be masked with password type
            const apiKeyField = screen.getByLabelText('API Key Value');
            expect(apiKeyField).toHaveAttribute('type', 'password');
            expect(apiKeyField).toHaveAttribute('autocomplete', 'off');

            // Header Name should NOT be masked (not a secret)
            const headerNameField = screen.getByLabelText('Header Name');
            expect(headerNameField).not.toHaveAttribute('type', 'password');
        });
    });

    describe('Templates tab', () => {
        it('displays template fields', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]); // Second channel has custom template

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Another Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Templates' }));

            expect(screen.getByLabelText('Alert Fire Template')).toHaveValue('{"custom": true}');
            expect(screen.getByText(/Go template syntax/)).toBeInTheDocument();
        });

        it('edits template content', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Templates' }));

            const alertFireField = screen.getByLabelText('Alert Fire Template');
            // Use fireEvent.change to avoid userEvent curly brace parsing issues
            fireEvent.change(alertFireField, { target: { value: '{"event": "test"}' } });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    expect.objectContaining({
                        template_alert_fire: '{"event": "test"}',
                    }),
                );
            });
        });
    });

    describe('dialog behavior', () => {
        it('closes dialog when Cancel is clicked', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: 'Cancel' }));

            await waitFor(() => {
                expect(screen.queryByText('Create webhook channel')).not.toBeInTheDocument();
            });
        });

        it('displays dialog error when save fails', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockRejectedValue(new Error('Server error'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('No webhook channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create webhook channel')).toBeInTheDocument();
            });

            fireEvent.change(screen.getByLabelText('Name *'), { target: { value: 'New Webhook' } });
            fireEvent.change(screen.getByLabelText('Endpoint URL *'), { target: { value: 'https://test.com/hook' } });
            await user.click(screen.getByRole('button', { name: 'Create' }));

            await waitFor(() => {
                const dialog = screen.getByRole('dialog');
                expect(within(dialog).getByText('Server error')).toBeInTheDocument();
            });
        });

        it('closes delete confirmation dialog when Cancel is clicked', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: 'delete channel' });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Delete Webhook Channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: 'Cancel' }));

            await waitFor(() => {
                expect(screen.queryByText('Delete Webhook Channel')).not.toBeInTheDocument();
            });
        });
    });

    describe('error dismissal', () => {
        it('can dismiss error alerts', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'));

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Network error')).toBeInTheDocument();
            });

            const closeButton = screen.getByRole('button', { name: /close/i });
            fireEvent.click(closeButton);

            await waitFor(() => {
                expect(screen.queryByText('Network error')).not.toBeInTheDocument();
            });
        });

        it('can dismiss success alerts', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', { name: 'send test notification' });
            await user.click(testButtons[0]);

            await waitFor(() => {
                expect(screen.getByText(/Test notification sent successfully/)).toBeInTheDocument();
            });

            const closeButtons = screen.getAllByRole('button', { name: /close/i });
            fireEvent.click(closeButtons[0]);

            await waitFor(() => {
                expect(screen.queryByText(/Test notification sent successfully/)).not.toBeInTheDocument();
            });
        });
    });
});
