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

import AdminEmailChannels from '../AdminEmailChannels';

/**
 * Helper to find form fields in the dialog by their label text.
 * MUI labels may have "(optional)" or "*" appended.
 */
const getFieldByLabel = (container: HTMLElement, labelText: string): HTMLElement => {
    const labels = within(container).getAllByText(new RegExp(`^${labelText}`, 'i'));
    // Find the label that is actually a form label
    for (const label of labels) {
        const forAttr = label.getAttribute('for');
        if (forAttr) {
            // Use getElementById to avoid CSS selector escaping issues with colons
            const input = document.getElementById(forAttr);
            if (input) {
                return input as HTMLElement;
            }
        }
    }
    throw new Error(`Could not find field with label: ${labelText}`);
};

const mockEmailChannels = [
    {
        id: 1,
        channel_type: 'email',
        name: 'Test Email',
        description: 'Test description',
        enabled: true,
        is_estate_default: false,
        smtp_host: 'smtp.example.com',
        smtp_port: 587,
        smtp_username: 'user@example.com',
        smtp_use_tls: true,
        from_address: 'from@example.com',
        from_name: 'Test Sender',
        recipients: [],
    },
    {
        id: 2,
        channel_type: 'email',
        name: 'Production Email',
        description: 'Production notifications',
        enabled: false,
        is_estate_default: true,
        smtp_host: 'smtp.prod.com',
        smtp_port: 465,
        smtp_username: 'prod@example.com',
        smtp_use_tls: true,
        from_address: 'alerts@prod.com',
        from_name: 'Production Alerts',
        recipients: [{ id: 1 }, { id: 2 }],
    },
];

const mockRecipients = [
    {
        id: 1,
        email_address: 'recipient1@example.com',
        display_name: 'Recipient One',
        enabled: true,
    },
    {
        id: 2,
        email_address: 'recipient2@example.com',
        display_name: 'Recipient Two',
        enabled: false,
    },
];

describe('AdminEmailChannels', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Loading and Initial State', () => {
        it('renders loading state then channel list after fetch', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                return Promise.resolve({});
            });

            renderWithTheme(<AdminEmailChannels />);

            // Check loading state
            expect(screen.getByRole('progressbar')).toBeInTheDocument();

            // Wait for channels to load
            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            expect(screen.getByText('Production Email')).toBeInTheDocument();
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/notification-channels');
        });

        it('renders empty state when no email channels', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });
        });

        it('filters out non-email channels', async () => {
            mockApiGet.mockResolvedValue({
                notification_channels: [
                    ...mockEmailChannels,
                    {
                        id: 3,
                        channel_type: 'webhook',
                        name: 'Webhook Channel',
                        description: 'A webhook',
                        enabled: true,
                    },
                ],
            });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            expect(screen.queryByText('Webhook Channel')).not.toBeInTheDocument();
        });
    });

    describe('Create Dialog', () => {
        it('opens create dialog with empty form', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            // Check form fields are empty - use the dialog context
            const dialog = screen.getByRole('dialog');
            expect(getFieldByLabel(dialog, 'Name')).toHaveValue('');
            expect(getFieldByLabel(dialog, 'SMTP Host')).toHaveValue('');
            expect(getFieldByLabel(dialog, 'SMTP Port')).toHaveValue(587);
            expect(getFieldByLabel(dialog, 'From Address')).toHaveValue('');
        });

        it('validates required fields', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            // Create button should be disabled when required fields are empty
            const dialog = screen.getByRole('dialog');
            const createButton = within(dialog).getByRole('button', { name: /Create/i });
            expect(createButton).toBeDisabled();
        });

        it('creates channel via API on save', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 10 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            // Fill in required fields
            await user.type(getFieldByLabel(dialog, 'Name'), 'New Email Channel');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.new.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'new@example.com');

            // Create button should now be enabled
            const createButton = within(dialog).getByRole('button', { name: /Create/i });
            expect(createButton).not.toBeDisabled();

            await user.click(createButton);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels',
                    expect.objectContaining({
                        channel_type: 'email',
                        name: 'New Email Channel',
                        smtp_host: 'smtp.new.com',
                        from_address: 'new@example.com',
                    })
                );
            });
        });

        it('shows validation error for invalid port', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            // Fill in required fields with invalid port
            await user.type(getFieldByLabel(dialog, 'Name'), 'Test');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.test.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'test@test.com');

            // Clear port and type invalid value
            const portField = getFieldByLabel(dialog, 'SMTP Port');
            await user.clear(portField);
            await user.type(portField, '99999');

            await user.click(within(dialog).getByRole('button', { name: /Create/i }));

            await waitFor(() => {
                expect(screen.getByText(/SMTP port must be a valid port number/)).toBeInTheDocument();
            });
        });
    });

    describe('Edit Dialog', () => {
        it('opens edit dialog populated with channel data', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            // Click edit on first channel
            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            // Check form is populated
            expect(getFieldByLabel(dialog, 'Name')).toHaveValue('Test Email');
            expect(getFieldByLabel(dialog, 'SMTP Host')).toHaveValue('smtp.example.com');
            expect(getFieldByLabel(dialog, 'SMTP Port')).toHaveValue(587);
            expect(getFieldByLabel(dialog, 'From Address')).toHaveValue('from@example.com');
        });

        it('updates channel via API on save (sends only changed fields)', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: [] });
                }
                return Promise.resolve({});
            });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            // Change only the name
            const nameField = getFieldByLabel(dialog, 'Name');
            await user.clear(nameField);
            await user.type(nameField, 'Updated Email');

            await user.click(within(dialog).getByRole('button', { name: /Save/i }));

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    expect.objectContaining({
                        name: 'Updated Email',
                    })
                );
            });

            // Should NOT include unchanged fields
            const putCall = mockApiPut.mock.calls[0];
            expect(putCall[1]).not.toHaveProperty('smtp_host');
            expect(putCall[1]).not.toHaveProperty('smtp_port');
        });
    });

    describe('Delete Channel', () => {
        it('deletes channel via confirmation dialog', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: /delete channel/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Delete Email Channel')).toBeInTheDocument();
            });

            expect(screen.getByText(/Are you sure you want to delete the email channel/)).toBeInTheDocument();
            expect(screen.getByText(/"Test Email"\?/)).toBeInTheDocument();

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/notification-channels/1');
            });
        });

        it('closes delete dialog on cancel', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: /delete channel/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Delete Email Channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Cancel/i }));

            await waitFor(() => {
                expect(screen.queryByText('Delete Email Channel')).not.toBeInTheDocument();
            });
        });
    });

    describe('Toggle Enabled', () => {
        it('toggles channel enabled state', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiPut.mockResolvedValue({});

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const enabledSwitches = screen.getAllByRole('checkbox', {
                name: 'Toggle channel enabled',
            });

            // First channel is enabled
            expect(enabledSwitches[0]).toBeChecked();

            fireEvent.click(enabledSwitches[0]);

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1',
                    { enabled: false }
                );
            });
        });
    });

    describe('Test Email', () => {
        it('sends test email', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiPost.mockResolvedValue({});

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', { name: /send test email/i });
            fireEvent.click(testButtons[0]);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith('/api/v1/notification-channels/1/test');
            });

            await waitFor(() => {
                expect(screen.getByText(/Test notification sent successfully/)).toBeInTheDocument();
            });
        });

        it('shows spinner while testing', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiPost.mockImplementation(() => new Promise((resolve) => setTimeout(resolve, 100)));

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const testButtons = screen.getAllByRole('button', { name: /send test email/i });
            fireEvent.click(testButtons[0]);

            await waitFor(() => {
                expect(screen.getByLabelText('Sending test email')).toBeInTheDocument();
            });
        });
    });

    describe('Error Handling', () => {
        it('displays fetch error', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'));

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Network error')).toBeInTheDocument();
            });
        });

        it('displays save error in dialog', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockRejectedValue(new Error('Failed to create channel'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            await user.type(getFieldByLabel(dialog, 'Name'), 'Test');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.test.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'test@test.com');

            await user.click(within(dialog).getByRole('button', { name: /Create/i }));

            await waitFor(() => {
                expect(screen.getByText('Failed to create channel')).toBeInTheDocument();
            });
        });

        it('displays fallback error when save throws non-Error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            // Reject with a non-Error value
            mockApiPost.mockRejectedValue('network error string');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            await user.type(getFieldByLabel(dialog, 'Name'), 'Test');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.test.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'test@test.com');

            await user.click(within(dialog).getByRole('button', { name: /Create/i }));

            await waitFor(() => {
                expect(screen.getByText('An unexpected error occurred')).toBeInTheDocument();
            });
        });

        it('displays delete error', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiDelete.mockRejectedValue(new Error('Cannot delete channel'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: /delete channel/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Delete Email Channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(screen.getByText('Cannot delete channel')).toBeInTheDocument();
            });
        });
    });

    describe('Recipients Tab', () => {
        it('renders recipients in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            // Click Recipients tab
            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            expect(screen.getByText('Recipient One')).toBeInTheDocument();
            expect(screen.getByText('recipient2@example.com')).toBeInTheDocument();
        });

        it('adds pending recipient in create mode', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            // Click Recipients tab
            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            // Add a pending recipient
            const emailInput = screen.getByPlaceholderText('Email address');
            const nameInput = screen.getByPlaceholderText('Display name');

            await user.type(emailInput, 'newrecipient@test.com');
            await user.type(nameInput, 'New Recipient');

            await user.click(screen.getByRole('button', { name: /Add/i }));

            await waitFor(() => {
                expect(screen.getByText('newrecipient@test.com')).toBeInTheDocument();
            });
            expect(screen.getByText('New Recipient')).toBeInTheDocument();
        });

        it('removes pending recipient in create mode', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            const emailInput = screen.getByPlaceholderText('Email address');
            await user.type(emailInput, 'temp@test.com');

            await user.click(screen.getByRole('button', { name: /Add/i }));

            await waitFor(() => {
                expect(screen.getByText('temp@test.com')).toBeInTheDocument();
            });

            // Remove the pending recipient
            await user.click(screen.getByRole('button', { name: /remove pending recipient/i }));

            await waitFor(() => {
                expect(screen.queryByText('temp@test.com')).not.toBeInTheDocument();
            });
        });

        it('adds recipient via API in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: [] });
                }
                return Promise.resolve({});
            });
            mockApiPost.mockResolvedValue({ id: 5 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            const emailInput = screen.getByPlaceholderText('Email address');
            await user.type(emailInput, 'newuser@example.com');

            await user.click(screen.getByRole('button', { name: /Add/i }));

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1/recipients',
                    expect.objectContaining({
                        email_address: 'newuser@example.com',
                        enabled: true,
                    })
                );
            });
        });

        it('shows error when add recipient fails in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: [] });
                }
                return Promise.resolve({});
            });
            mockApiPost.mockRejectedValue(new Error('Email already exists'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            const emailInput = screen.getByPlaceholderText('Email address');
            await user.type(emailInput, 'newuser@example.com');

            await user.click(screen.getByRole('button', { name: /Add/i }));

            await waitFor(() => {
                expect(screen.getByText('Email already exists')).toBeInTheDocument();
            });
        });

        it('shows fallback error when add recipient throws non-Error in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: [] });
                }
                return Promise.resolve({});
            });
            // Reject with a non-Error value
            mockApiPost.mockRejectedValue('network failure');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            const emailInput = screen.getByPlaceholderText('Email address');
            await user.type(emailInput, 'newuser@example.com');

            await user.click(screen.getByRole('button', { name: /Add/i }));

            await waitFor(() => {
                expect(screen.getByText('Failed to add recipient')).toBeInTheDocument();
            });
        });

        it('toggles recipient enabled via API in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            // Find the toggle switch for the first recipient (enabled: true)
            const recipientToggles = screen.getAllByRole('checkbox', {
                name: 'Toggle recipient enabled',
            });
            expect(recipientToggles[0]).toBeChecked();

            await user.click(recipientToggles[0]);

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1/recipients/1',
                    { enabled: false }
                );
            });
        });

        it('shows error when toggle recipient fails', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            mockApiPut.mockRejectedValue(new Error('Failed to update recipient'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            const recipientToggles = screen.getAllByRole('checkbox', {
                name: 'Toggle recipient enabled',
            });

            await user.click(recipientToggles[0]);

            await waitFor(() => {
                expect(screen.getByText('Failed to update recipient')).toBeInTheDocument();
            });
        });

        it('deletes recipient via API in edit mode', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            // Find and click the delete button for the first recipient
            const deleteButtons = screen.getAllByRole('button', { name: /delete recipient/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/1/recipients/1'
                );
            });
        });

        it('shows error when delete recipient fails', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            mockApiDelete.mockRejectedValue(new Error('Failed to delete recipient'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: /delete recipient/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Failed to delete recipient')).toBeInTheDocument();
            });
        });

        it('shows fallback error when toggle recipient throws non-Error', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            // Reject with a string instead of an Error object
            mockApiPut.mockRejectedValue('string error');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            const recipientToggles = screen.getAllByRole('checkbox', {
                name: 'Toggle recipient enabled',
            });

            await user.click(recipientToggles[0]);

            await waitFor(() => {
                expect(screen.getByText('Failed to update recipient')).toBeInTheDocument();
            });
        });

        it('shows fallback error when delete recipient throws non-Error', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/notification-channels') {
                    return Promise.resolve({ notification_channels: mockEmailChannels });
                }
                if (url.includes('/recipients')) {
                    return Promise.resolve({ recipients: mockRecipients });
                }
                return Promise.resolve({});
            });
            // Reject with a string instead of an Error object
            mockApiDelete.mockRejectedValue('string error');
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', { name: /edit channel/i });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit channel: Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('tab', { name: /Recipients/i }));

            await waitFor(() => {
                expect(screen.getByText('recipient1@example.com')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', { name: /delete recipient/i });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Failed to delete recipient')).toBeInTheDocument();
            });
        });
    });

    describe('Close Dialog', () => {
        it('closes dialog properly', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Cancel/i }));

            await waitFor(() => {
                expect(screen.queryByText('Create email channel')).not.toBeInTheDocument();
            });
        });
    });

    describe('Recipient Count Column', () => {
        it('displays recipient count in table', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            // Check that "Recipients" header exists
            expect(screen.getByText('Recipients')).toBeInTheDocument();

            // Production Email has 2 recipients
            expect(screen.getByText('2')).toBeInTheDocument();
            // Test Email has 0 recipients
            expect(screen.getByText('0')).toBeInTheDocument();
        });
    });

    describe('Success Messages', () => {
        it('shows success message after channel creation', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 10 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            await waitFor(() => {
                expect(screen.getByText('Create email channel')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');

            await user.type(getFieldByLabel(dialog, 'Name'), 'New Channel');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.new.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'new@example.com');

            await user.click(within(dialog).getByRole('button', { name: /Create/i }));

            await waitFor(() => {
                expect(screen.getByText(/Channel "New Channel" created successfully/)).toBeInTheDocument();
            });
        });

        it('clears success message on dismiss', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: mockEmailChannels });
            mockApiPost.mockResolvedValue({});

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('Test Email')).toBeInTheDocument();
            });

            // Trigger success via test email
            const testButtons = screen.getAllByRole('button', { name: /send test email/i });
            fireEvent.click(testButtons[0]);

            await waitFor(() => {
                expect(screen.getByText(/Test notification sent successfully/)).toBeInTheDocument();
            });

            // Dismiss the alert
            const closeButton = screen.getByRole('button', { name: /close/i });
            fireEvent.click(closeButton);

            await waitFor(() => {
                expect(screen.queryByText(/Test notification sent successfully/)).not.toBeInTheDocument();
            });
        });
    });

    describe('Creates channel with pending recipients', () => {
        it('creates channel then adds pending recipients', async () => {
            mockApiGet.mockResolvedValue({ notification_channels: [] });
            mockApiPost.mockResolvedValue({ id: 10 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminEmailChannels />);

            await waitFor(() => {
                expect(screen.getByText('No email channels configured.')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Add Channel/i }));

            const dialog = screen.getByRole('dialog');

            // Fill in required fields
            await user.type(getFieldByLabel(dialog, 'Name'), 'New Channel');
            await user.type(getFieldByLabel(dialog, 'SMTP Host'), 'smtp.new.com');
            await user.type(getFieldByLabel(dialog, 'From Address'), 'new@example.com');

            // Add a pending recipient
            await user.click(within(dialog).getByRole('tab', { name: /Recipients/i }));
            await user.type(within(dialog).getByPlaceholderText('Email address'), 'pending@test.com');
            await user.click(within(dialog).getByRole('button', { name: /^Add$/i }));

            await waitFor(() => {
                expect(within(dialog).getByText('pending@test.com')).toBeInTheDocument();
            });

            // Save the channel
            await user.click(within(dialog).getByRole('button', { name: /Create/i }));

            await waitFor(() => {
                // First call creates the channel
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels',
                    expect.objectContaining({
                        name: 'New Channel',
                    })
                );
            });

            await waitFor(() => {
                // Second call adds the recipient
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/notification-channels/10/recipients',
                    expect.objectContaining({
                        email_address: 'pending@test.com',
                    })
                );
            });
        });
    });
});
