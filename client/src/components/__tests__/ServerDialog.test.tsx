/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ServerDialog from '../ServerDialog';

// Mock AlertOverridesPanel to avoid fetch calls during ServerDialog tests
vi.mock('../AlertOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="alert-overrides-panel">
            AlertOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

// Helper functions to get form fields reliably
const getNameField = () => screen.getByRole('textbox', { name: /^name/i });
const getHostField = () => screen.getByRole('textbox', { name: /^host/i });
const getPortField = () => screen.getByRole('spinbutton', { name: /^port/i });
const getDatabaseField = () => screen.getByRole('textbox', { name: /maintenance database/i });
const getUsernameField = () => screen.getByRole('textbox', { name: /^username/i });
const getPasswordField = () => screen.getByLabelText(/^password/i);

describe('ServerDialog', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onSave: vi.fn(),
        mode: 'create',
        server: null,
        isSuperuser: false,
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders dialog with Add Server title in create mode', () => {
            render(<ServerDialog {...defaultProps} />);
            expect(screen.getByText('Add Server')).toBeInTheDocument();
        });

        it('renders dialog with Server Settings title in edit mode', () => {
            const server = { id: 1, name: 'Test Server', host: 'localhost', port: 5432, database: 'postgres', username: 'user', password: '', sslmode: 'prefer' };
            render(<ServerDialog {...defaultProps} mode="edit" server={server} />);
            expect(screen.getByText('Server Settings: Test Server')).toBeInTheDocument();
        });

        it('renders all required form fields', () => {
            render(<ServerDialog {...defaultProps} />);

            expect(getNameField()).toBeInTheDocument();
            expect(getHostField()).toBeInTheDocument();
            expect(getPortField()).toBeInTheDocument();
            expect(getDatabaseField()).toBeInTheDocument();
            expect(getUsernameField()).toBeInTheDocument();
            expect(getPasswordField()).toBeInTheDocument();
        });

        it('renders monitor checkbox checked by default', () => {
            render(<ServerDialog {...defaultProps} />);
            const monitorCheckbox = screen.getByLabelText(/monitor this server/i);
            expect(monitorCheckbox).toBeChecked();
        });

        it('does not render shared checkbox for non-superusers', () => {
            render(<ServerDialog {...defaultProps} isSuperuser={false} />);
            expect(screen.queryByLabelText(/share with all users/i)).not.toBeInTheDocument();
        });

        it('renders shared checkbox for superusers', () => {
            render(<ServerDialog {...defaultProps} isSuperuser={true} />);
            expect(screen.getByLabelText(/share with all users/i)).toBeInTheDocument();
        });

        it('renders SSL Settings accordion', () => {
            render(<ServerDialog {...defaultProps} />);
            expect(screen.getByText(/ssl settings/i)).toBeInTheDocument();
        });

        it('renders Cancel and Save buttons', () => {
            render(<ServerDialog {...defaultProps} />);
            expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument();
        });

        it('does not render when open is false', () => {
            render(<ServerDialog {...defaultProps} open={false} />);
            expect(screen.queryByText('Add Server')).not.toBeInTheDocument();
        });
    });

    describe('edit mode pre-population', () => {
        const existingServer = {
            name: 'Production DB',
            host: 'prod.example.com',
            port: 5433,
            database_name: 'mydb',
            username: 'admin',
            ssl_mode: 'require',
            ssl_cert_path: '/path/to/cert.pem',
            ssl_key_path: '',
            ssl_root_cert_path: '',
            is_monitored: true,
            is_shared: true,
        };

        it('pre-populates fields with existing server data', () => {
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={existingServer}
                />
            );

            expect(getNameField()).toHaveValue('Production DB');
            expect(getHostField()).toHaveValue('prod.example.com');
            expect(getPortField()).toHaveValue(5433);
            expect(getDatabaseField()).toHaveValue('mydb');
            expect(getUsernameField()).toHaveValue('admin');
        });

        it('does not pre-populate password field', () => {
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={existingServer}
                />
            );

            expect(getPasswordField()).toHaveValue('');
        });

        it('shows helper text for password in edit mode', () => {
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={existingServer}
                />
            );

            expect(screen.getByText(/leave blank to keep unchanged/i)).toBeInTheDocument();
        });
    });

    describe('validation', () => {
        it('shows error when name is empty', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.click(screen.getByRole('button', { name: /save/i }));

            // Use getAllByText since validation shows multiple errors, then check first one
            const errors = screen.getAllByText(/name is required/i);
            expect(errors.length).toBeGreaterThan(0);
        });

        it('shows error when host is empty', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');
            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/host is required/i)).toBeInTheDocument();
        });

        it('shows error for invalid port', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.clear(getPortField());
            await user.type(getPortField(), '99999');
            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/port must be between 1 and 65535/i)).toBeInTheDocument();
        });

        it('shows error when maintenance database is empty', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            // Clear the default 'postgres' value
            await user.clear(getDatabaseField());
            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/maintenance database is required/i)).toBeInTheDocument();
        });

        it('shows error when username is empty', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            // Clear the default 'postgres' username
            await user.clear(getUsernameField());
            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/username is required/i)).toBeInTheDocument();
        });

        it('shows error when password is empty in create mode', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.click(screen.getByRole('button', { name: /save/i }));

            expect(screen.getByText(/password is required/i)).toBeInTheDocument();
        });

        it('does not require password in edit mode', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            const existingServer = {
                name: 'Test Server',
                host: 'localhost',
                port: 5432,
                database_name: 'postgres',
                username: 'admin',
            };

            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={existingServer}
                    onSave={onSave}
                />
            );

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalled();
            });
        });

        it('clears field error when user types', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            // Trigger validation error
            await user.click(screen.getByRole('button', { name: /save/i }));
            // Verify the name error exists (exact match to avoid matching "Username is required")
            expect(screen.getByText('Name is required')).toBeInTheDocument();

            // Type in the field
            await user.type(getNameField(), 'Test');

            // Name error should be cleared (exact match)
            expect(screen.queryByText('Name is required')).not.toBeInTheDocument();
        });
    });

    describe('form submission', () => {
        it('calls onSave with trimmed form data', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            await user.type(getNameField(), '  Test Server  ');
            await user.type(getHostField(), '  localhost  ');
            // Clear defaults and type new values
            await user.clear(getDatabaseField());
            await user.type(getDatabaseField(), '  mydb  ');
            await user.clear(getUsernameField());
            await user.type(getUsernameField(), '  admin  ');
            await user.type(getPasswordField(), 'secret');

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith(
                    expect.objectContaining({
                        name: 'Test Server',
                        host: 'localhost',
                        port: 5432,
                        database_name: 'mydb',
                        username: 'admin',
                        password: 'secret',
                    })
                );
            });
        });

        it('shows success message after successful save', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(screen.getByText('Server settings saved successfully.')).toBeInTheDocument();
            });
        });

        it('shows error alert when save fails', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockRejectedValue(new Error('Connection refused'));
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
            });
        });

        it('does not call onClose when save fails', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockRejectedValue(new Error('Failed'));
            const onClose = vi.fn();
            render(<ServerDialog {...defaultProps} onSave={onSave} onClose={onClose} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(screen.getByText(/failed/i)).toBeInTheDocument();
            });

            expect(onClose).not.toHaveBeenCalled();
        });

        it('disables form during save', async () => {
            const user = userEvent.setup();
            // Create a promise that we control
            let resolvePromise: (value: unknown) => void;
            const savePromise = new Promise((resolve) => {
                resolvePromise = resolve;
            });
            const onSave = vi.fn().mockReturnValue(savePromise);
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            await user.click(screen.getByRole('button', { name: /save/i }));

            // Check that inputs are disabled
            await waitFor(() => {
                expect(getNameField()).toBeDisabled();
            });
            expect(getHostField()).toBeDisabled();

            // Resolve the promise to clean up
            resolvePromise();
        });
    });

    describe('cancel behavior', () => {
        it('calls onClose when Cancel button is clicked', async () => {
            const user = userEvent.setup();
            const onClose = vi.fn();
            render(<ServerDialog {...defaultProps} onClose={onClose} />);

            await user.click(screen.getByRole('button', { name: /cancel/i }));

            expect(onClose).toHaveBeenCalled();
        });

        it('resets form when reopened', async () => {
            const user = userEvent.setup();
            const { rerender } = render(<ServerDialog {...defaultProps} />);

            await user.type(getNameField(), 'Test Server');

            // Close and reopen
            rerender(<ServerDialog {...defaultProps} open={false} />);
            rerender(<ServerDialog {...defaultProps} open={true} />);

            expect(getNameField()).toHaveValue('');
        });
    });

    describe('SSL settings', () => {
        it('expands SSL section when clicked', async () => {
            const user = userEvent.setup();
            render(<ServerDialog {...defaultProps} />);

            // Click the accordion
            await user.click(screen.getByText(/ssl settings/i));

            // SSL fields should now be visible
            await waitFor(() => {
                expect(screen.getByRole('textbox', { name: /ssl certificate path/i })).toBeVisible();
            });
            expect(screen.getByRole('textbox', { name: /ssl key path/i })).toBeVisible();
            expect(screen.getByRole('textbox', { name: /ssl root certificate path/i })).toBeVisible();
        });

        it('includes SSL settings in save data', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            // Fill required fields
            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            // Expand and fill SSL fields
            await user.click(screen.getByText(/ssl settings/i));
            await waitFor(() => {
                expect(screen.getByRole('textbox', { name: /ssl certificate path/i })).toBeVisible();
            });
            await user.type(screen.getByRole('textbox', { name: /ssl certificate path/i }), '/path/to/cert.pem');

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith(
                    expect.objectContaining({
                        ssl_cert_path: '/path/to/cert.pem',
                    })
                );
            });
        });
    });

    describe('checkbox options', () => {
        it('includes is_monitored in save data', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            render(<ServerDialog {...defaultProps} onSave={onSave} />);

            // Fill required fields
            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            // Uncheck monitor checkbox
            await user.click(screen.getByLabelText(/monitor this server/i));

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith(
                    expect.objectContaining({
                        is_monitored: false,
                    })
                );
            });
        });

        it('includes is_shared in save data when superuser', async () => {
            const user = userEvent.setup();
            const onSave = vi.fn().mockResolvedValue();
            render(<ServerDialog {...defaultProps} onSave={onSave} isSuperuser={true} />);

            // Fill required fields
            await user.type(getNameField(), 'Test Server');
            await user.type(getHostField(), 'localhost');
            await user.type(getDatabaseField(), 'postgres');
            await user.type(getUsernameField(), 'admin');
            await user.type(getPasswordField(), 'secret');

            // Check shared checkbox
            await user.click(screen.getByLabelText(/share with all users/i));

            await user.click(screen.getByRole('button', { name: /save/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith(
                    expect.objectContaining({
                        is_shared: true,
                    })
                );
            });
        });
    });

    describe('tabs and alert overrides', () => {
        const editServer = {
            id: 42,
            name: 'Production DB',
            host: 'prod.example.com',
            port: 5433,
            database_name: 'mydb',
            username: 'admin',
        };

        it('does not render tabs in create mode', () => {
            render(<ServerDialog {...defaultProps} mode="create" />);

            expect(screen.queryByRole('tab', { name: /details/i })).not.toBeInTheDocument();
            expect(screen.queryByRole('tab', { name: /alert overrides/i })).not.toBeInTheDocument();
        });

        it('renders Details and Alert Overrides tabs in edit mode', () => {
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={editServer}
                />
            );

            expect(screen.getByRole('tab', { name: /details/i })).toBeInTheDocument();
            expect(screen.getByRole('tab', { name: /alert overrides/i })).toBeInTheDocument();
        });

        it('renders AlertOverridesPanel when Alert Overrides tab is clicked', async () => {
            const user = userEvent.setup();
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={editServer}
                />
            );

            await user.click(screen.getByRole('tab', { name: /alert overrides/i }));

            await waitFor(() => {
                expect(screen.getByTestId('alert-overrides-panel')).toBeInTheDocument();
            });
            expect(screen.getByText(/AlertOverridesPanel: server 42/)).toBeInTheDocument();
        });

        it('shows Save button on Details tab and Close button on Alert Overrides tab', async () => {
            const user = userEvent.setup();
            render(
                <ServerDialog
                    {...defaultProps}
                    mode="edit"
                    server={editServer}
                />
            );

            // On the Details tab, Save and Cancel buttons should be present
            expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();

            // Switch to Alert Overrides tab
            await user.click(screen.getByRole('tab', { name: /alert overrides/i }));

            await waitFor(() => {
                expect(screen.getByTestId('alert-overrides-panel')).toBeInTheDocument();
            });

            // Save button should not be visible; Close button should be present
            expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument();
            expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
        });
    });
});
