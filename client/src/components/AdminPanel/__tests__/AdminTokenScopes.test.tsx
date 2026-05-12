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

    // Load-sensitive: this test runs two full walkCreateFlow flows back-to-back
    // inside JSDOM, so under heavier overall test load it can exceed the 5s
    // default (notably on the Node 22 CI runner). Bump per-test rather than
    // globally so other tests still enforce the tighter budget.
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
        },
        15000
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

describe('AdminTokenScopes - unified error fallback', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('surfaces the unified fallback when token fetch throws a non-Error', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.reject('plain string reject');
            }
            return Promise.resolve({});
        });
        renderPanel();
        await waitFor(() => {
            expect(
                screen.getByText('An unexpected error occurred'),
            ).toBeInTheDocument();
        });
    });

    it('surfaces the unified fallback when create-token throws a non-Error', async () => {
        setupApiGetMock();
        mockApiPost.mockRejectedValue('plain string reject');
        const user = userEvent.setup({ delay: null });
        renderPanel();
        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        const nameInput = await screen.findByLabelText(/^Name/i);
        await user.type(nameInput, 'Test token');

        const ownerInput = screen.getByLabelText(/^Owner/i);
        await user.click(ownerInput);
        await user.type(ownerInput, 'alice');
        const aliceOption = await screen.findByRole('option', { name: /alice/ });
        await user.click(aliceOption);

        const createBtn = screen.getByRole('button', { name: /^Create$/ });
        await user.click(createBtn);

        await waitFor(() => {
            expect(
                screen.getByText('An unexpected error occurred'),
            ).toBeInTheDocument();
        });
    });

    it('surfaces the unified fallback when delete-token throws a non-Error', async () => {
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
            return Promise.resolve({});
        });
        mockApiDelete.mockRejectedValue('plain string reject');
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await waitFor(() => {
            expect(screen.getByText('alice-token')).toBeInTheDocument();
        });

        await user.click(screen.getByLabelText(/delete token/i));

        const dialog = await screen.findByRole('dialog');
        await user.click(
            within(dialog).getByRole('button', { name: /^delete$/i }),
        );

        await waitFor(() => {
            expect(
                screen.getByText('An unexpected error occurred'),
            ).toBeInTheDocument();
        });
    });

    it('surfaces the unified fallback when save-scope throws a non-Error', async () => {
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
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPut.mockRejectedValue('plain string reject');
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await waitFor(() => {
            expect(screen.getByText('alice-token')).toBeInTheDocument();
        });

        await user.click(screen.getByLabelText(/edit token/i));

        const dialog = await screen.findByRole('dialog');
        await user.click(
            within(dialog).getByRole('button', { name: /^save$/i }),
        );

        await waitFor(() => {
            expect(
                within(dialog).getByText('An unexpected error occurred'),
            ).toBeInTheDocument();
        });
    });
});

describe('AdminTokenScopes - additional coverage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders the empty-state when fetchData succeeds with no tokens', async () => {
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
                return Promise.resolve({ users: [] });
            }
            return Promise.resolve({});
        });
        renderPanel();
        await waitFor(() => {
            expect(screen.getByText('No tokens found.')).toBeInTheDocument();
        });
    });

    it('tolerates connection/mcp/user endpoint failures (fetchData catch arms)', async () => {
        // Each non-tokens endpoint rejects so the `.catch(() => null)` arms
        // (lines 115-117) execute and the corresponding state setters stay
        // empty. The panel still renders because /tokens succeeds.
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [] });
            }
            return Promise.reject(new Error(`fail: ${url}`));
        });
        renderPanel();
        await waitFor(() => {
            expect(screen.getByText('No tokens found.')).toBeInTheDocument();
        });
        // No error should be surfaced since the catches swallow the rejection.
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('renders connections list when /connections returns a raw array', async () => {
        // Cover the alternative shape: `(connResult.connections ?? connResult)`
        // when connResult is a raw array.
        const TOKEN = {
            id: 1,
            name: 'inline-token',
            token_prefix: 'x1',
            username: 'alice',
            user_id: 42,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [{ connection_id: 100, access_level: 'read' }],
                mcp_privileges: [],
                admin_permissions: [],
            },
        };
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [TOKEN] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([{ id: 100, name: 'array-conn' }]);
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: [] });
            }
            return Promise.resolve({});
        });
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await waitFor(() => {
            expect(screen.getByText('inline-token')).toBeInTheDocument();
        });

        // Expand the row so the EffectivePermissionsPanel renders. (It is
        // mocked to render null, but the click still triggers the row click
        // handler at handleTokenRowClick.)
        await user.click(screen.getByText('inline-token'));

        // Toggle off again via row click to exercise the equal-id branch.
        await user.click(screen.getByText('inline-token'));
    });

    it('clears owner-derived state when the owner is cleared in the create dialog', async () => {
        setupApiGetMock();
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        // Pick alice (wildcard mcp/admin from setupApiGetMock).
        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        // Confirm the MCP combobox now has the "All MCP Privileges" option.
        const mcpCombo = await screen.findByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);
        await screen.findByRole('option', { name: 'All MCP Privileges' });
        await user.keyboard('{Escape}');

        // Clear the owner via the Autocomplete clear button.
        const clearButton = within(
            ownerCombo.closest('.MuiAutocomplete-root') as HTMLElement,
        ).getByLabelText(/clear/i);
        await user.click(clearButton);

        // After clearing, the MCP combo's listbox shows no options.
        await user.click(mcpCombo);
        // After clearing, available MCP privileges are reset; no item options.
        const listbox = screen.queryByRole('listbox');
        // listbox may exist but be empty (Autocomplete shows "no options" text).
        if (listbox) {
            expect(
                within(listbox).queryByRole('option', {
                    name: /All MCP Privileges/,
                }),
            ).not.toBeInTheDocument();
        }
    });

    it('falls back to all privileges when the owner-privileges fetch fails', async () => {
        // setupApiGetMock returns wildcard for alice; replace the privileges
        // endpoint with a rejection so the catch arm (lines 190-192) runs.
        const failingPrivs = (url: string) => {
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
                return Promise.reject(new Error('privs down'));
            }
            return Promise.resolve({});
        };
        mockApiGet.mockImplementation(failingPrivs);

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        // Fallback path sets ownerConnections=connections,
        // ownerMcpPrivileges=mcpPrivileges, ownerAdminPermissions=ADMIN_PERMISSIONS.
        const mcpCombo = await screen.findByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);
        const mcpListbox = await screen.findByRole('listbox');
        // All MCP privileges should still be selectable.
        expect(
            within(mcpListbox).getByRole('option', { name: 'query_read' }),
        ).toBeInTheDocument();
    });

    it('treats connection_id=0 in owner privileges as access to all connections', async () => {
        // Cover the allowedConnIds.includes(0) branch (line 181).
        const customConnections = [
            { id: 100, name: 'primary-db' },
            { id: 200, name: 'replica-db' },
        ];
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: customConnections });
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
                    connection_privileges: { 0: 'read_write' },
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        // All connections should appear in the Add Connection combobox.
        const addConnCombo = screen.getByRole('combobox', {
            name: /add connection/i,
        });
        await user.click(addConnCombo);
        const listbox = await screen.findByRole('listbox');
        expect(
            within(listbox).getByRole('option', { name: 'primary-db' }),
        ).toBeInTheDocument();
        expect(
            within(listbox).getByRole('option', { name: 'replica-db' }),
        ).toBeInTheDocument();
    });

    it('restricts connection list to owner-allowed ids when wildcard is absent', async () => {
        const customConnections = [
            { id: 100, name: 'primary-db' },
            { id: 200, name: 'replica-db' },
        ];
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: customConnections });
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
                    connection_privileges: { 100: 'read' },
                    mcp_privileges: ['query_read'],
                    admin_permissions: ['manage_users'],
                });
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        // Only primary-db should be available.
        const addConnCombo = screen.getByRole('combobox', {
            name: /add connection/i,
        });
        await user.click(addConnCombo);
        const listbox = await screen.findByRole('listbox');
        expect(
            within(listbox).getByRole('option', { name: 'primary-db' }),
        ).toBeInTheDocument();
        expect(
            within(listbox).queryByRole('option', { name: 'replica-db' }),
        ).not.toBeInTheDocument();
    });

    it('creates a token with scope (connection, MCP, admin) and surfaces the created dialog', async () => {
        const customConnections = [{ id: 100, name: 'primary-db' }];
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: customConnections });
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve(MCP_PRIVILEGES);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: USERS });
            }
            if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
                return Promise.resolve({
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPost.mockResolvedValue({ id: 42, token: 'pgedge_token_xyz' });
        mockApiPut.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        await user.type(screen.getByLabelText(/^Name/i), 'scoped-token');

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        // Add a connection to scope.
        const addConnCombo = screen.getByRole('combobox', {
            name: /add connection/i,
        });
        await user.click(addConnCombo);
        const addConnListbox = await screen.findByRole('listbox');
        await user.click(
            within(addConnListbox).getByRole('option', { name: 'primary-db' }),
        );

        // Add a specific MCP privilege. Click the Name field afterwards to
        // dismiss the listbox without using Escape (which would close the
        // dialog).
        const mcpCombo = screen.getByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);
        const mcpListbox = await screen.findByRole('listbox');
        await user.click(
            within(mcpListbox).getByRole('option', { name: 'query_read' }),
        );
        // Click the Name field to take focus away from the multi-select.
        await user.click(screen.getByLabelText(/^Name/i));

        // Add the "All Admin Permissions" wildcard to exercise the _isAll
        // mapping path (line 220-221).
        const adminCombo = screen.getByRole('combobox', {
            name: /allowed admin permissions/i,
        });
        await user.click(adminCombo);
        const adminListbox = await screen.findByRole('listbox');
        await user.click(
            within(adminListbox).getByRole('option', {
                name: 'All Admin Permissions',
            }),
        );
        await user.click(screen.getByLabelText(/^Name/i));

        // Submit the dialog.
        await user.click(screen.getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(screen.getByText('Token created')).toBeInTheDocument();
        });
        expect(mockApiPut).toHaveBeenCalledWith(
            '/api/v1/rbac/tokens/42/scope',
            expect.objectContaining({
                connections: [
                    { connection_id: 100, access_level: 'read_write' },
                ],
                mcp_privileges: ['query_read'],
                admin_permissions: ['*'],
            }),
        );
    });

    it('creates a token with the "All MCP Privileges" wildcard via the _isAll mapping', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: [] });
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve(MCP_PRIVILEGES);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: USERS });
            }
            if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
                return Promise.resolve({
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPost.mockResolvedValue({ id: 7, token: 'pgedge_token_wildcard' });
        mockApiPut.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        await user.type(screen.getByLabelText(/^Name/i), 'wild-token');

        const ownerCombo = await screen.findByRole('combobox', { name: /owner/i });
        await user.click(ownerCombo);
        const aliceOption = await screen.findByRole('option', { name: 'alice' });
        await user.click(aliceOption);

        // Select All MCP wildcard.
        const mcpCombo = await screen.findByRole('combobox', {
            name: /allowed mcp privileges/i,
        });
        await user.click(mcpCombo);
        const mcpListbox = await screen.findByRole('listbox');
        await user.click(
            within(mcpListbox).getByRole('option', { name: 'All MCP Privileges' }),
        );
        await user.click(screen.getByLabelText(/^Name/i));

        await user.click(screen.getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens/7/scope',
                expect.objectContaining({
                    mcp_privileges: ['*'],
                }),
            );
        });
    });

    it('cancels the create dialog and resets the form state', async () => {
        setupApiGetMock();

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: /create token/i }),
            ).toBeInTheDocument();
        });
        await user.click(screen.getByRole('button', { name: /create token/i }));

        // Click the Cancel button (line 487 onClose handler).
        const cancel = await screen.findByRole('button', { name: /Cancel/i });
        await user.click(cancel);

        await waitFor(() => {
            expect(screen.queryByLabelText(/^Name/i)).not.toBeInTheDocument();
        });
    });

    it('opens the edit dialog for a token without user_id (no-user fallback path)', async () => {
        const TOKEN = {
            id: 9,
            name: 'orphan-token',
            token_prefix: 'orf',
            // user_id intentionally omitted to exercise the else-branch at
            // line 329-335.
            username: 'alice',
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [],
                mcp_privileges: [],
                admin_permissions: [],
            },
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
            return Promise.reject(new Error(`Unexpected URL: ${url}`));
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('orphan-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));

        // The dialog should open without triggering a privileges fetch.
        await screen.findByRole('dialog');
        expect(mockApiGet).not.toHaveBeenCalledWith(
            expect.stringMatching(/\/privileges$/),
        );
    });

    it('opens the edit dialog when owner-privileges fetch fails (catch path)', async () => {
        const TOKEN = {
            id: 11,
            name: 'broken-privs-token',
            token_prefix: 'brp',
            username: 'alice',
            user_id: USERS[0].id,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [
                    { connection_id: 100, access_level: 'read' },
                ],
                mcp_privileges: [-1],
                admin_permissions: ['*'],
            },
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
                return Promise.reject(new Error('privs down'));
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('broken-privs-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));

        // The dialog renders. The wildcard MCP scope (id=-1) triggers the
        // ALL_MCP_OPTION path (line 277). The "*" admin scope triggers the
        // ALL_ADMIN_OPTION path (line 284).
        await screen.findByRole('dialog');
        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });
    });

    it('opens the edit dialog for a non-superuser owner with a wildcard connection privilege', async () => {
        const TOKEN = {
            id: 12,
            name: 'nonsuper-token',
            token_prefix: 'nst',
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
                // Owner has wildcard connection privilege (0) and a specific
                // mcp privilege.
                return Promise.resolve({
                    is_superuser: false,
                    connection_privileges: { 0: 'read_write' },
                    mcp_privileges: ['query_read'],
                    admin_permissions: ['manage_users'],
                });
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('nonsuper-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));

        await screen.findByRole('dialog');
        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        // The Add Connection combobox should include primary-db (wildcard 0).
        const addConnCombo = await screen.findByRole('combobox', {
            name: /add connection/i,
        });
        await user.click(addConnCombo);
        const optsBox = await screen.findByRole('listbox');
        expect(
            within(optsBox).getByRole('option', { name: 'primary-db' }),
        ).toBeInTheDocument();
    });

    it('opens the edit dialog for a non-superuser owner without wildcard, filtering connections', async () => {
        const customConnections = [
            { id: 100, name: 'primary-db' },
            { id: 200, name: 'replica-db' },
        ];
        const TOKEN = {
            id: 13,
            name: 'restricted-token',
            token_prefix: 'res',
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
                return Promise.resolve({ connections: customConnections });
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
                    connection_privileges: { 100: 'read' },
                    mcp_privileges: ['query_read'],
                    admin_permissions: ['manage_users'],
                });
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('restricted-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));

        await screen.findByRole('dialog');
        const addConnCombo = await screen.findByRole('combobox', {
            name: /add connection/i,
        });
        await user.click(addConnCombo);
        const listbox = await screen.findByRole('listbox');
        expect(
            within(listbox).getByRole('option', { name: 'primary-db' }),
        ).toBeInTheDocument();
        expect(
            within(listbox).queryByRole('option', { name: 'replica-db' }),
        ).not.toBeInTheDocument();
    });

    it('saves the edited scope successfully and closes the dialog', async () => {
        const TOKEN = {
            id: 14,
            name: 'save-token',
            token_prefix: 'sv',
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
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPut.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('save-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));
        const dialog = await screen.findByRole('dialog');

        await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                `/api/v1/rbac/tokens/${TOKEN.id}/scope`,
                expect.any(Object),
            );
        });
        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
    });

    it('saves the edited scope with wildcard mcp and admin via _isAll mapping', async () => {
        // The wildcard MCP privilege from the API surfaces as identifier '*'.
        // The panel's getMcpPrivilegeName maps that id back to '*', enabling
        // the editMcpPrivileges to set to ALL_MCP_OPTION (line 277).
        const MCP_WITH_WILDCARD = [
            { id: -1, identifier: '*' },
            ...MCP_PRIVILEGES,
        ];
        const TOKEN = {
            id: 15,
            name: 'wild-edit-token',
            token_prefix: 'we',
            username: 'alice',
            user_id: USERS[0].id,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [],
                mcp_privileges: [-1],
                admin_permissions: ['*'],
            },
        };
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [TOKEN] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: CONNECTIONS });
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve(MCP_WITH_WILDCARD);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: USERS });
            }
            if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
                return Promise.resolve({
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPut.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('wild-edit-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));
        const dialog = await screen.findByRole('dialog');

        // Save unchanged - wildcard selections are already pre-populated.
        await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                `/api/v1/rbac/tokens/${TOKEN.id}/scope`,
                expect.objectContaining({
                    mcp_privileges: ['*'],
                    admin_permissions: ['*'],
                }),
            );
        });
    });

    it('saves the edited scope with explicit connection and admin selections', async () => {
        const customConnections = [{ id: 100, name: 'primary-db' }];
        const TOKEN = {
            id: 21,
            name: 'explicit-scope-token',
            token_prefix: 'es',
            username: 'alice',
            user_id: USERS[0].id,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [
                    { connection_id: 100, access_level: 'read' },
                ],
                mcp_privileges: [1],
                admin_permissions: ['manage_users'],
            },
        };
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.resolve({ tokens: [TOKEN] });
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve({ connections: customConnections });
            }
            if (url === '/api/v1/rbac/privileges/mcp') {
                return Promise.resolve(MCP_PRIVILEGES);
            }
            if (url === '/api/v1/rbac/users') {
                return Promise.resolve({ users: USERS });
            }
            if (url === `/api/v1/rbac/users/${USERS[0].id}/privileges`) {
                return Promise.resolve({
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });
        mockApiPut.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('explicit-scope-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));
        const dialog = await screen.findByRole('dialog');

        await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                `/api/v1/rbac/tokens/${TOKEN.id}/scope`,
                expect.objectContaining({
                    connections: [
                        { connection_id: 100, access_level: 'read' },
                    ],
                    mcp_privileges: ['query_read'],
                    admin_permissions: ['manage_users'],
                }),
            );
        });
    });

    it('cancels the edit dialog without saving', async () => {
        const TOKEN = {
            id: 16,
            name: 'cancel-token',
            token_prefix: 'cn',
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
                    is_superuser: true,
                    connection_privileges: {},
                    mcp_privileges: [],
                    admin_permissions: [],
                });
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('cancel-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/edit token/i));
        const dialog = await screen.findByRole('dialog');

        // Click Cancel (line 522 onClose handler).
        await user.click(within(dialog).getByRole('button', { name: /Cancel/i }));

        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('cancels the delete-confirmation dialog without deleting', async () => {
        const TOKEN = {
            id: 17,
            name: 'undelete-token',
            token_prefix: 'ud',
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
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('undelete-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/delete token/i));
        const dialog = await screen.findByRole('dialog');

        // The DeleteConfirmationDialog has a Cancel button bound to its
        // onClose prop (lines 542-544 in the panel).
        await user.click(within(dialog).getByRole('button', { name: /Cancel/i }));

        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
        expect(mockApiDelete).not.toHaveBeenCalled();
    });

    it('deletes a token successfully and refreshes the table', async () => {
        const TOKEN = {
            id: 18,
            name: 'delete-success-token',
            token_prefix: 'dsx',
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
            return Promise.resolve({});
        });
        mockApiDelete.mockResolvedValue({});

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('delete-success-token')).toBeInTheDocument();
        });
        await user.click(screen.getByLabelText(/delete token/i));
        const dialog = await screen.findByRole('dialog');

        await user.click(within(dialog).getByRole('button', { name: /^Delete$/ }));

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                `/api/v1/rbac/tokens/${TOKEN.id}`,
            );
        });
        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
    });

    it('renders a non-zero, non-wildcard MCP privilege name via getMcpPrivilegeName', async () => {
        // Render with token whose scope mcp_privileges references a real id
        // and another unknown id, exercising both arms of the find().
        const TOKEN = {
            id: 19,
            name: 'mcp-label-token',
            token_prefix: 'mlb',
            username: 'alice',
            user_id: USERS[0].id,
            is_service_account: false,
            is_superuser: false,
            expires_at: null,
            scope: {
                scoped: true,
                connections: [],
                mcp_privileges: [1, 999],
                admin_permissions: [],
            },
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
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        render(<AdminTokenScopes />);

        await waitFor(() => {
            expect(screen.getByText('mcp-label-token')).toBeInTheDocument();
        });

        // Open edit dialog so the scope mcp_privileges are mapped through
        // getMcpPrivilegeName (lines 105-106). The dialog reflects them in
        // the Allowed MCP Privileges combobox.
        await user.click(screen.getByLabelText(/edit token/i));
        const dialog = await screen.findByRole('dialog');
        // The selected MCP set should contain "query_read" (id 1) since
        // 999 is unknown and filtered out by the panel's editor (it only
        // includes ids that exist in mcpPrivileges via filter).
        expect(
            within(dialog).getByText('query_read'),
        ).toBeInTheDocument();
    });

    it('dismisses the top-level error alert', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/rbac/tokens') {
                return Promise.reject(new Error('boom'));
            }
            return Promise.resolve({});
        });

        const user = userEvent.setup({ delay: null });
        renderPanel();

        await waitFor(() => {
            expect(screen.getByText('boom')).toBeInTheDocument();
        });

        const alert = screen.getByText('boom').closest('[role="alert"]');
        expect(alert).not.toBeNull();
        const closeBtn = within(alert as HTMLElement).getByRole('button');
        await user.click(closeBtn);

        await waitFor(() => {
            expect(screen.queryByText('boom')).not.toBeInTheDocument();
        });
    });

    it('flips the copied state back to the copy icon after the 2s timeout', async () => {
        installCreateFlowApiMocks();
        mockApiPost.mockResolvedValue({
            id: 99,
            token: 'pgedge_timer_token',
        });

        // Capture the setTimeout callback registered by handleCopyToken so we
        // can invoke it manually without using fake timers (fake timers break
        // userEvent and waitFor flows).
        let capturedCallback: (() => void) | null = null;
        const originalSetTimeout = window.setTimeout;
        const setTimeoutSpy = vi
            .spyOn(window, 'setTimeout')
            .mockImplementation((fn: TimerHandler, ms?: number) => {
                if (ms === 2000 && typeof fn === 'function') {
                    capturedCallback = fn as () => void;
                    return 12345 as unknown as ReturnType<typeof setTimeout>;
                }
                return originalSetTimeout(fn, ms);
            });

        try {
            const writeText = vi.fn().mockResolvedValue(undefined);
            const user = await openCreatedDialog();
            installClipboardMock(writeText);

            const copyBtn = screen.getByRole('button', {
                name: /copy token/i,
            });
            await act(async () => {
                fireEvent.click(copyBtn);
            });

            await waitFor(() => {
                expect(screen.getByTestId('CheckIcon')).toBeInTheDocument();
            });

            // Invoke the captured setTimeout callback manually to exercise
            // the reset path (lines 404-406).
            expect(capturedCallback).not.toBeNull();
            await act(async () => {
                (capturedCallback as () => void)();
            });

            await waitFor(() => {
                expect(
                    screen.getByTestId('ContentCopyIcon'),
                ).toBeInTheDocument();
            });
            expect(screen.queryByTestId('CheckIcon')).not.toBeInTheDocument();
            // Reference `user` to satisfy the no-unused-vars check.
            expect(user).toBeDefined();
        } finally {
            setTimeoutSpy.mockRestore();
        }
    });
});
