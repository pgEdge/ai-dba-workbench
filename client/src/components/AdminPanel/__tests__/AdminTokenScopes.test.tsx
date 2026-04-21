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
import {
    render,
    screen,
    fireEvent,
    waitFor,
    within,
    act,
} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {
    describe,
    it,
    expect,
    vi,
    beforeEach,
    afterEach,
} from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';

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

// Avoid pulling in the full EffectivePermissionsPanel rendering in the
// copy-token tests; the copy-token behaviour does not depend on it.
vi.mock('../EffectivePermissionsPanel', () => ({
    default: () => null,
}));

import AdminTokenScopes from '../AdminTokenScopes';

const theme = createTheme();

const renderPanel = () =>
    render(
        <ThemeProvider theme={theme}>
            <AdminTokenScopes />
        </ThemeProvider>
    );

const CREATED_TOKEN = 'pgedge_test_token_abcdef1234567890';

/**
 * Install a navigator.clipboard stub returning the given writeText spy.
 * Needed after userEvent.setup() because that replaces navigator.clipboard
 * with its own stub, overwriting any earlier mock.
 */
const installClipboardMock = (
    writeTextMock: ReturnType<typeof vi.fn>
) => {
    Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: writeTextMock },
    });
};

/**
 * Install mockApiGet responders covering the create-token flow. Extracted
 * so tests that reopen the dialog in the same render can re-use them.
 */
const installCreateFlowApiMocks = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens: [] });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: [] });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve([]);
        }
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({
                users: [{ id: 1, username: 'alice' }],
            });
        }
        // Owner privilege lookup when owner is selected
        if (url.startsWith('/api/v1/rbac/users/1/privileges')) {
            return Promise.resolve({
                is_superuser: false,
                connection_privileges: {},
                mcp_privileges: [],
                admin_permissions: [],
            });
        }
        return Promise.resolve({});
    });
};

/**
 * Walk the user through the "Create Token" flow in the currently rendered
 * panel, stopping once the "Token created" success dialog appears. Reusable
 * to reopen the dialog within a single render.
 */
const walkCreateFlow = async (
    user: ReturnType<typeof userEvent.setup>
) => {
    // Open the create dialog.
    await user.click(screen.getByText('Create Token'));

    // Fill the required Name field.
    const nameInput = screen.getByLabelText(/^Name/i);
    await user.type(nameInput, 'Test token');

    // Pick the owner via the autocomplete.
    const ownerInput = screen.getByLabelText(/^Owner/i);
    await user.click(ownerInput);
    await user.type(ownerInput, 'alice');
    const aliceOption = await screen.findByRole('option', { name: /alice/ });
    await user.click(aliceOption);

    // Submit the create form. Target the Create button in the dialog (not the
    // page-level "Create Token" header button).
    const createButton = screen.getByRole('button', { name: /^Create$/ });
    await user.click(createButton);

    // The created-token dialog should now be visible.
    await waitFor(() => {
        expect(screen.getByText('Token created')).toBeInTheDocument();
    });
};

/**
 * Drive the component into the "token created" dialog state. The cheapest
 * path is through the real handleCreateToken flow: list tokens/connections/
 * mcp/users, open the create dialog, fill required fields, submit.
 */
const openCreatedDialog = async () => {
    const user = userEvent.setup({ delay: null });

    // Initial fetchData() kicks off four parallel calls. Use implementation
    // based routing so we can respond by URL.
    installCreateFlowApiMocks();

    mockApiPost.mockResolvedValue({ id: 42, token: CREATED_TOKEN });

    renderPanel();

    // Wait for the initial load to complete.
    await waitFor(() => {
        expect(screen.getByText('Create Token')).toBeInTheDocument();
    });

    await walkCreateFlow(user);

    return user;
};

/**
 * Click the copy-to-clipboard icon button and wait for the UI to flip to
 * the "copied" state (CheckIcon visible). Asserts writeText was called.
 */
const clickCopyAndAwaitCopiedState = async (
    writeTextMock: ReturnType<typeof vi.fn>
) => {
    const copyButton = screen.getByRole('button', {
        name: /copy token/i,
    });

    await act(async () => {
        fireEvent.click(copyButton);
    });

    await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith(CREATED_TOKEN);
    });

    await waitFor(() => {
        expect(screen.getByTestId('CheckIcon')).toBeInTheDocument();
    });
};

const MCP_PRIVILEGES = [
    { id: 1, identifier: 'query_read' },
    { id: 2, identifier: 'query_write' },
    { id: 3, identifier: 'schema_read' },
];

const USERS = [
    { id: 42, username: 'alice' },
];

const CONNECTIONS = [
    { id: 100, name: 'primary-db' },
];

const setupApiGetMock = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens: [] });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: CONNECTIONS });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve(MCP_PRIVILEGES);
        }
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({ users: USERS });
        }
        if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
            return Promise.resolve({
                is_superuser: false,
                connection_privileges: {},
                mcp_privileges: ['*'],
                admin_permissions: ['*'],
            });
        }
        return Promise.reject(new Error(`Unexpected URL: ${url}`));
    });
};

const expectedAdminLabels = [
    'Manage Connections',
    'Manage Groups',
    'Manage Permissions',
    'Manage Users',
    'Manage Token Scopes',
    'Manage Blackouts',
    'Manage Probes',
    'Manage Alert Rules',
    'Manage Notification Channels',
];

describe('AdminTokenScopes - copy-to-clipboard behaviour', () => {
    let writeTextMock: ReturnType<typeof vi.fn>;

    beforeEach(() => {
        vi.clearAllMocks();
        writeTextMock = vi.fn().mockResolvedValue(undefined);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('copies the token and swaps to the Copied! state on success',
        async () => {
            await openCreatedDialog();
            // userEvent.setup() replaces navigator.clipboard with its own
            // stub; install ours now, after userEvent has done its work.
            installClipboardMock(writeTextMock);

            // Confirm the token value is rendered in the dialog.
            expect(screen.getByText(CREATED_TOKEN)).toBeInTheDocument();

            // Initially the CopyIcon is shown, not the CheckIcon.
            expect(
                screen.getByTestId('ContentCopyIcon')
            ).toBeInTheDocument();
            expect(
                screen.queryByTestId('CheckIcon')
            ).not.toBeInTheDocument();

            await clickCopyAndAwaitCopiedState(writeTextMock);

            // writeText should have been called exactly once with the token.
            expect(writeTextMock).toHaveBeenCalledTimes(1);

            // The CopyIcon should no longer be visible after the flip.
            expect(
                screen.queryByTestId('ContentCopyIcon')
            ).not.toBeInTheDocument();
        }
    );

    it('surfaces an error when the Clipboard API rejects',
        async () => {
            writeTextMock.mockRejectedValueOnce(
                new Error('denied by user agent')
            );

            await openCreatedDialog();
            installClipboardMock(writeTextMock);

            const copyButton = screen.getByRole('button', {
                name: /copy token/i,
            });

            await act(async () => {
                fireEvent.click(copyButton);
            });

            await waitFor(() => {
                expect(
                    screen.getByText(/Failed to copy token/i)
                ).toBeInTheDocument();
            });

            // UI should NOT have flipped into the copied state on failure.
            expect(
                screen.queryByTestId('CheckIcon')
            ).not.toBeInTheDocument();
            expect(
                screen.getByTestId('ContentCopyIcon')
            ).toBeInTheDocument();
        }
    );

    it('resets the copied state when the created-token dialog is closed',
        async () => {
            const user = await openCreatedDialog();
            installClipboardMock(writeTextMock);

            // Click copy so the dialog is in the "copied" (CheckIcon) state.
            await clickCopyAndAwaitCopiedState(writeTextMock);

            // Close the dialog via the Close action.
            const closeButton = screen.getByRole('button', { name: /^Close$/ });
            await act(async () => {
                fireEvent.click(closeButton);
            });

            await waitFor(() => {
                expect(
                    screen.queryByText('Token created')
                ).not.toBeInTheDocument();
            });

            // Reopen the created-token dialog via the real flow. The copy
            // icon should have reset: ContentCopyIcon present, CheckIcon
            // absent, demonstrating the copied-state did not leak across
            // dialog open/close cycles.
            await walkCreateFlow(user);

            expect(
                screen.getByTestId('ContentCopyIcon')
            ).toBeInTheDocument();
            expect(
                screen.queryByTestId('CheckIcon')
            ).not.toBeInTheDocument();
        }
    );

    it('resets the 2-second timer when copy is clicked again while already in copied state',
        async () => {
            await openCreatedDialog();
            installClipboardMock(writeTextMock);

            // Click copy and wait for the copied (CheckIcon) state.
            await clickCopyAndAwaitCopiedState(writeTextMock);

            // Click copy again while still in the copied state.
            const copyButton = screen.getByRole('button', {
                name: /copy token/i,
            });

            await act(async () => {
                fireEvent.click(copyButton);
            });

            // writeText should have been called twice total.
            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledTimes(2);
            });

            // The CheckIcon should still be visible (state did not
            // flash back to CopyIcon between the two clicks).
            expect(
                screen.getByTestId('CheckIcon')
            ).toBeInTheDocument();
            expect(
                screen.queryByTestId('ContentCopyIcon')
            ).not.toBeInTheDocument();
        }
    );
});

describe('AdminTokenScopes', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('exposes all MCP and admin options when owner has wildcard privileges', async () => {
        setupApiGetMock();

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /create token/i }))
                .toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /create token/i }));

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const ownerOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(ownerOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        const mcpCombo = screen.getByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);

        const mcpListbox = await screen.findByRole('listbox');
        expect(
            within(mcpListbox).getByRole('option', { name: 'All MCP Privileges' }),
        ).toBeInTheDocument();
        for (const priv of MCP_PRIVILEGES) {
            expect(
                within(mcpListbox).getByRole('option', { name: priv.identifier }),
            ).toBeInTheDocument();
        }

        await user.keyboard('{Escape}');

        const adminCombo = screen.getByRole('combobox', {
            name: /allowed admin permissions/i,
        });
        await user.click(adminCombo);

        const adminListbox = await screen.findByRole('listbox');
        expect(
            within(adminListbox).getByRole('option', { name: 'All Admin Permissions' }),
        ).toBeInTheDocument();
        for (const label of expectedAdminLabels) {
            expect(
                within(adminListbox).getByRole('option', { name: label }),
            ).toBeInTheDocument();
        }
    });

    it('exposes all MCP and admin options in edit dialog when owner has wildcard privileges', async () => {
        const TOKEN = {
            id: 7,
            name: 'alice-token',
            token_prefix: 'abc',
            username: 'alice',
            user_id: USERS[0].id,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: { scoped: false },
        };

        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [TOKEN] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: CONNECTIONS });
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve(MCP_PRIVILEGES);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: USERS });
            }
            if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
                return Promise.resolve({
                    is_superuser: false,
                    connection_privileges: {},
                    mcp_privileges: ['*'],
                    admin_permissions: ['*'],
                });
            }
            return Promise.reject(new Error(`Unexpected URL: ${url}`));
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        const editButton = await screen.findByRole('button', { name: /edit token/i });
        await user.click(editButton);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        const mcpCombo = await screen.findByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);

        const mcpListbox = await screen.findByRole('listbox');
        expect(
            within(mcpListbox).getByRole('option', { name: 'All MCP Privileges' }),
        ).toBeInTheDocument();
        for (const priv of MCP_PRIVILEGES) {
            expect(
                within(mcpListbox).getByRole('option', { name: priv.identifier }),
            ).toBeInTheDocument();
        }

        await user.keyboard('{Escape}');

        const adminCombo = screen.getByRole('combobox', {
            name: /allowed admin permissions/i,
        });
        await user.click(adminCombo);

        const adminListbox = await screen.findByRole('listbox');
        expect(
            within(adminListbox).getByRole('option', { name: 'All Admin Permissions' }),
        ).toBeInTheDocument();
        for (const label of expectedAdminLabels) {
            expect(
                within(adminListbox).getByRole('option', { name: label }),
            ).toBeInTheDocument();
        }
    });
});
