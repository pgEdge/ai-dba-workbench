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

// API responses no longer include `auth_credentials` (redacted by the
// server, issue #187). Channels indicate whether credentials are
// configured via the `auth_credentials_set` boolean instead.
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
        auth_credentials_set: true,
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
        auth_credentials_set: true,
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

    it('opens edit dialog populated with non-secret channel data', async () => {
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

        // Non-secret fields are pre-populated.
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
        // The auth credentials must not be in the body when the form
        // fields are blank, even though `auth_credentials_set` is true.
        // Otherwise the server would clear the redacted secret.
        expect(body).not.toHaveProperty('auth_credentials');
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
        it('shows blank credential fields and a configured hint when editing a bearer channel', async () => {
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

            // Bearer token must NOT be pre-populated; the API redacts it.
            const tokenField = screen.getByLabelText('Token');
            expect(tokenField).toHaveValue('');
            // The placeholder/helper text should communicate that
            // credentials are configured server-side.
            expect(tokenField).toHaveAttribute('placeholder', 'Leave blank to keep existing');
            expect(
                screen.getByText(/Existing credentials are configured/i),
            ).toBeInTheDocument();
        });

        it('shows blank basic auth fields with hint when credentials are configured', async () => {
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

            expect(screen.getByLabelText('Username')).toHaveValue('');
            expect(screen.getByLabelText('Password')).toHaveValue('');
            expect(
                screen.getByText(/Existing credentials are configured/i),
            ).toBeInTheDocument();
        });

        it('keeps existing credentials when the user saves without typing new ones', async () => {
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

            // Tweak only the description.
            await user.click(screen.getByRole('tab', { name: 'Settings' }));
            const descField = screen.getByLabelText('Description');
            fireEvent.change(descField, { target: { value: 'tweaked' } });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });

            const [, body] = mockApiPut.mock.calls[0];
            // Must NOT send auth_credentials at all; sending an empty
            // string would clear the secret on the server.
            expect(body).not.toHaveProperty('auth_credentials');
            expect(body).not.toHaveProperty('auth_type');
            expect(body).toHaveProperty('description', 'tweaked');
        });

        it('sends typed credentials in the PUT body', async () => {
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

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // User types a new token.
            fireEvent.change(screen.getByLabelText('Token'), {
                target: { value: 'rotated-token' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    expect.objectContaining({
                        auth_credentials: 'rotated-token',
                    }),
                );
            });
        });

        it('clears credentials when switching auth_type to none', async () => {
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

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Switch from bearer to none. The Auth Type field is a MUI
            // Select; opening it requires a mousedown, then we click
            // the desired option in the listbox.
            fireEvent.mouseDown(screen.getByLabelText('Auth Type'));
            const noneOption = await screen.findByRole('option', { name: 'None' });
            fireEvent.click(noneOption);

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });

            const [, body] = mockApiPut.mock.calls[0];
            expect(body.auth_type).toBe('none');
            // Switching to `none` is the one case where we send the
            // empty string explicitly: it tells the server to clear
            // the stored credentials.
            expect(body.auth_credentials).toBe('');
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
                auth_credentials_set: false,
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

        it('shows blank api key fields with hint', async () => {
            // Create a channel with api_key auth type and credentials set
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
                auth_credentials_set: true,
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

            // API key fields are blank but a hint indicates the
            // credentials are configured.
            expect(screen.getByLabelText('Header Name')).toHaveValue('');
            expect(screen.getByLabelText('API Key Value')).toHaveValue('');
            expect(
                screen.getByText(/Existing credentials are configured/i),
            ).toBeInTheDocument();
        });

        it('masks bearer token field with password input type', async () => {
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

        it('masks api key value with password input type', async () => {
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
                auth_credentials_set: true,
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

        it('sends typed basic auth credentials in the PUT body', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]); // basic auth channel

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type into both basic-auth fields, exercising both
            // onChange handlers in WebhookAuthTab.
            fireEvent.change(screen.getByLabelText('Username'), {
                target: { value: 'newuser' },
            });
            fireEvent.change(screen.getByLabelText('Password'), {
                target: { value: 'newpass' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [, body] = mockApiPut.mock.calls[0];
            expect(body.auth_credentials).toBe('newuser:newpass');
        });

        it('sends typed api_key credentials in the PUT body', async () => {
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
                auth_credentials_set: true,
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithApiKey });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('API Key Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type both api_key fields, exercising both onChange handlers.
            fireEvent.change(screen.getByLabelText('Header Name'), {
                target: { value: 'X-Token' },
            });
            fireEvent.change(screen.getByLabelText('API Key Value'), {
                target: { value: 'topsecret' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [, body] = mockApiPut.mock.calls[0];
            expect(body.auth_credentials).toBe('X-Token:topsecret');
        });

        it('rejects partial basic auth input on edit when only username is typed', async () => {
            // Regression test for the partial-credential replacement bug.
            // After issue #187 redacts existing credentials, edit forms
            // start blank — typing only one half of a basic auth pair
            // must NOT silently clear the other half on the server.
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]); // basic auth channel

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type only the username; leave the password blank.
            fireEvent.change(screen.getByLabelText('Username'), {
                target: { value: 'newuser' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            // The save handler must surface a dialog error and skip
            // the PUT request entirely.
            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).getByText(
                    /Re-enter both username and password/i,
                ),
            ).toBeInTheDocument();
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('rejects partial basic auth input on edit when only password is typed', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Another Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[1]); // basic auth channel

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type only the password; leave the username blank.
            fireEvent.change(screen.getByLabelText('Password'), {
                target: { value: 'newpass' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).getByText(
                    /Re-enter both username and password/i,
                ),
            ).toBeInTheDocument();
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('rejects partial api_key input on edit when only header name is typed', async () => {
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
                auth_credentials_set: true,
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithApiKey });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('API Key Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type only the header name; leave the API key value blank.
            fireEvent.change(screen.getByLabelText('Header Name'), {
                target: { value: 'X-Token' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).getByText(
                    /Re-enter both header name and API key value/i,
                ),
            ).toBeInTheDocument();
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('rejects partial api_key input on edit when only API key value is typed', async () => {
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
                auth_credentials_set: true,
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channelWithApiKey });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('API Key Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Type only the API key value; leave the header name blank.
            fireEvent.change(screen.getByLabelText('API Key Value'), {
                target: { value: 'topsecret' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).getByText(
                    /Re-enter both header name and API key value/i,
                ),
            ).toBeInTheDocument();
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('hides hint when the user switches auth type away from the channel original', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]); // Bearer auth originally

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Webhook')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            // Switch from bearer to basic via the MUI Select.
            fireEvent.mouseDown(screen.getByLabelText('Auth Type'));
            const basicOption = await screen.findByRole('option', { name: 'Basic' });
            fireEvent.click(basicOption);

            // After the switch, the existing credentials no longer
            // apply, so the helper text should disappear.
            expect(
                screen.queryByText(/Existing credentials are configured/i),
            ).not.toBeInTheDocument();
        });

        it('does not show configured hint when channel has no credentials set', async () => {
            const channel = [{
                id: 5,
                channel_type: 'webhook',
                name: 'Bearer Empty',
                description: '',
                enabled: true,
                is_estate_default: false,
                endpoint_url: 'https://example.com/hook',
                http_method: 'POST',
                headers: {},
                auth_type: 'bearer',
                auth_credentials_set: false,
                template_alert_fire: '',
                template_alert_clear: '',
                template_reminder: '',
            }];
            mockApiGet.mockResolvedValue({ notification_channels: channel });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Bearer Empty')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Bearer Empty')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: 'Authentication' }));

            expect(
                screen.queryByText(/Existing credentials are configured/i),
            ).not.toBeInTheDocument();
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

    describe('change detection on edit', () => {
        it('sends description, enabled, is_estate_default, http_method when toggled', async () => {
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

            // Tweak description.
            fireEvent.change(screen.getByLabelText('Description'), {
                target: { value: 'updated' },
            });
            const dialog = screen.getByRole('dialog');
            // Toggle enabled inside the dialog (the table renders one too).
            const dialogEnabledToggle = within(dialog).getByRole('checkbox', {
                name: 'Toggle channel enabled',
            });
            fireEvent.click(dialogEnabledToggle);
            // Toggle is_estate_default inside the dialog.
            const estateToggle = within(dialog).getByRole('checkbox', {
                name: 'Toggle estate default',
            });
            fireEvent.click(estateToggle);

            // Change HTTP method via the MUI Select.
            fireEvent.mouseDown(screen.getByLabelText('HTTP Method'));
            const putOption = await screen.findByRole('option', { name: 'PUT' });
            fireEvent.click(putOption);

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [, body] = mockApiPut.mock.calls[0];
            expect(body).toMatchObject({
                description: 'updated',
                enabled: false,
                is_estate_default: true,
                http_method: 'PUT',
            });
        });

        it('sends template_alert_clear and template_reminder when changed', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockWebhookChannels });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Webhook')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: 'edit channel' });
            await user.click(editButtons[0]);

            await user.click(screen.getByRole('tab', { name: 'Templates' }));

            fireEvent.change(screen.getByLabelText('Alert Clear Template'), {
                target: { value: '{"event": "clear"}' },
            });
            fireEvent.change(screen.getByLabelText('Alert Reminder Template'), {
                target: { value: '{"event": "reminder"}' },
            });

            await user.click(screen.getByRole('button', { name: 'Save' }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalled();
            });
            const [, body] = mockApiPut.mock.calls[0];
            expect(body).toMatchObject({
                template_alert_clear: '{"event": "clear"}',
                template_reminder: '{"event": "reminder"}',
            });
        });

        it('shows fallback dialog error when save throws non-Error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockRejectedValue('boom');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminWebhookChannels />);

            await waitFor(() => {
                expect(
                    screen.getByText('No webhook channels configured.'),
                ).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));
            fireEvent.change(screen.getByLabelText('Name *'), {
                target: { value: 'X' },
            });
            fireEvent.change(screen.getByLabelText('Endpoint URL *'), {
                target: { value: 'https://x' },
            });

            await user.click(screen.getByRole('button', { name: 'Create' }));

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
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
