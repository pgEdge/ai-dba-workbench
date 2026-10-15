/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/*
 * A user who is not a superuser may create a token only for themselves,
 * and may only narrow or delete another user's token; only a superuser
 * may change a superuser's token (issue #522). These tests check that
 * the panel offers such a user only what the server allows, and shows
 * the server's message when it refuses anyway.
 */

import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
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

vi.mock('../EffectivePermissionsPanel', () => ({
    default: () => null,
}));

vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({
        user: { username: 'bob', isSuperuser: false },
        adminPermissions: ['manage_token_scopes'],
    }),
}));

import AdminTokenScopes from '../AdminTokenScopes';
import {
    ALL_CONNECTIONS_LABEL,
    narrowOnlyLiftError,
} from '../tokens/tokenTypes';

const theme = createTheme();

const ALICE = { id: 42, username: 'alice' };
const BOB = { id: 43, username: 'bob' };
const CONNECTIONS = [
    { id: 100, name: 'primary-db' },
    { id: 101, name: 'replica-db' },
];
const MCP_PRIVILEGES = [
    { id: 1, identifier: 'query_read' },
    { id: 2, identifier: 'query_write' },
];

const baseToken = {
    token_prefix: 'tk',
    is_service_account: false,
    is_superuser: false,
    expires_at: null,
};

const ALICE_TOKEN = {
    ...baseToken,
    id: 9,
    name: 'alice-token',
    username: ALICE.username,
    user_id: ALICE.id,
    scope: {
        scoped: true,
        connections: [{ connection_id: 100, access_level: 'read' }],
        mcp_privileges: [1],
        admin_permissions: ['manage_users'],
    },
};

const BOB_TOKEN = {
    ...baseToken,
    id: 10,
    name: 'bob-token',
    username: BOB.username,
    user_id: BOB.id,
    scope: { scoped: true, admin_permissions: ['manage_users'] },
};

const ROOT_TOKEN = {
    ...baseToken,
    id: 11,
    name: 'root-token',
    username: 'root',
    user_id: 1,
    is_superuser: true,
};

const OWNER_PRIVILEGES = {
    is_superuser: false,
    connection_privileges: { 100: 'read_write', 101: 'read_write' },
    mcp_privileges: ['query_read', 'query_write'],
    admin_permissions: ['manage_users', 'manage_groups'],
};

const SERVER_REFUSAL =
    'Permission denied: only a superuser may widen or clear the scope of ' +
    'a token owned by another user; a change to such a token must narrow it';

const mockApi = (tokens: unknown[], usersReadable = true) => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: CONNECTIONS });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve(MCP_PRIVILEGES);
        }
        if (url === '/api/v1/rbac/users') {
            return usersReadable
                ? Promise.resolve({ users: [ALICE, BOB] })
                : Promise.reject(new Error('Permission denied'));
        }
        if (url.endsWith('/privileges')) {
            return Promise.resolve(OWNER_PRIVILEGES);
        }
        return Promise.resolve({});
    });
};

const renderPanel = () =>
    render(
        <ThemeProvider theme={theme}>
            <AdminTokenScopes />
        </ThemeProvider>,
    );

/** The table row holding the named token. */
const rowFor = async (name: string): Promise<HTMLElement> => {
    const cell = await screen.findByText(name);
    const row = cell.closest('tr');
    if (!row) {
        throw new Error(`no row for ${name}`);
    }
    return row;
};

const openEdit = async (
    user: ReturnType<typeof userEvent.setup>,
    name: string,
) => {
    const row = await rowFor(name);
    await user.click(within(row).getByLabelText(/edit token/i));
    return screen.findByRole('dialog');
};

const openCreate = async (user: ReturnType<typeof userEvent.setup>) => {
    await user.click(
        await screen.findByRole('button', { name: /create token/i }),
    );
    return screen.findByRole('dialog');
};

/** Opens a combobox's list and returns the option names it offers. */
const optionNames = async (
    user: ReturnType<typeof userEvent.setup>,
    dialog: HTMLElement,
    name: RegExp,
): Promise<string[]> => {
    await user.click(within(dialog).getByRole('combobox', { name }));
    const listbox = screen.queryByRole('listbox');
    const names = listbox
        ? within(listbox).queryAllByRole('option').map((o) => o.textContent ?? '')
        : [];
    await user.keyboard('{Escape}');
    return names;
};

const removeLastChip = async (
    user: ReturnType<typeof userEvent.setup>,
    dialog: HTMLElement,
    name: RegExp,
) => {
    await user.click(within(dialog).getByRole('combobox', { name }));
    await user.keyboard('{Backspace}');
};

const clickSave = async (
    user: ReturnType<typeof userEvent.setup>,
    dialog: HTMLElement,
) => {
    await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));
};

describe('AdminTokenScopes - token ownership for a non-superuser', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiPost.mockResolvedValue({ id: 77, token: 'secret-token' });
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
    });

    it('fixes the owner of a new token to the current user', async () => {
        mockApi([]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openCreate(user);
        const owner = within(dialog).getByRole('combobox', { name: /owner/i });
        await waitFor(() => {
            expect(owner).toHaveValue('bob');
        });
        expect(owner).toBeDisabled();
        expect(within(dialog).getByText(
            /only a superuser can create a token for another user/i,
        )).toBeInTheDocument();
        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${BOB.id}/privileges`,
            );
        });

        await user.type(
            within(dialog).getByRole('textbox', { name: /name/i }),
            'my-token',
        );
        await user.click(within(dialog).getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/rbac/tokens', {
                owner_username: 'bob',
                annotation: 'my-token',
                expires_in: '90d',
            });
        });
    });

    it('creates for the current user when the user list is unreadable', async () => {
        mockApi([], false);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openCreate(user);
        const owner = within(dialog).getByRole('combobox', { name: /owner/i });
        await waitFor(() => {
            expect(owner).toHaveValue('bob');
        });

        // The session's own admin permissions are offered, and no
        // privileges lookup is tried for a user whose id is unknown.
        expect(await optionNames(user, dialog, /allowed admin permissions/i))
            .toEqual(["All the owner's admin permissions", 'Manage Token Scopes']);
        expect(mockApiGet).not.toHaveBeenCalledWith(
            expect.stringMatching(/\/rbac\/users\/\d+\/privileges$/),
        );

        await user.type(
            within(dialog).getByRole('textbox', { name: /name/i }),
            'my-token',
        );
        await user.click(within(dialog).getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens',
                expect.objectContaining({ owner_username: 'bob' }),
            );
        });
    });

    it('offers only narrowing choices on another user\'s token', async () => {
        mockApi([ALICE_TOKEN]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user, 'alice-token');
        expect(within(dialog).getByText(/this token belongs to alice/i))
            .toBeInTheDocument();
        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${ALICE.id}/privileges`,
            );
        });

        // Connection 100 is already in the scope, and 101 is not held,
        // so there is nothing to add, least of all every connection.
        const connections = await optionNames(user, dialog, /add connection/i);
        expect(connections).not.toContain('replica-db');
        expect(connections).not.toContain(ALL_CONNECTIONS_LABEL);

        // Only the held entries are listed, and no wildcard, although
        // the owner holds more.
        expect(await optionNames(user, dialog, /allowed mcp privileges/i))
            .toEqual(['query_read']);
        expect(await optionNames(user, dialog, /allowed admin permissions/i))
            .toEqual(['Manage Users']);

        // The held read level cannot be raised to read/write.
        await user.click(within(dialog).getByRole('combobox', { name: '' }));
        const levels = within(await screen.findByRole('listbox'))
            .getAllByRole('option')
            .map((o) => o.textContent);
        expect(levels).toEqual(['Read Only']);
    });

    it('refuses to lift a restriction on another user\'s token', async () => {
        mockApi([ALICE_TOKEN]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user, 'alice-token');
        await removeLastChip(user, dialog, /allowed admin permissions/i);
        await clickSave(user, dialog);

        expect(await within(dialog).findByText(
            narrowOnlyLiftError('alice', ['admin permissions']),
        )).toBeInTheDocument();
        expect(mockApiPut).not.toHaveBeenCalled();
        expect(mockApiDelete).not.toHaveBeenCalled();
    });

    it('shows the server\'s refusal when a narrowing save is refused', async () => {
        mockApi([ALICE_TOKEN]);
        mockApiPut.mockRejectedValue(new Error(SERVER_REFUSAL));
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user, 'alice-token');
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/rbac/tokens/9/scope', {
                connections: [{ connection_id: 100, access_level: 'read' }],
                mcp_privileges: ['query_read'],
                admin_permissions: ['manage_users'],
            });
        });
        expect(await within(dialog).findByText(SERVER_REFUSAL))
            .toBeInTheDocument();
    });

    it('lets the current user clear their own token', async () => {
        mockApi([BOB_TOKEN]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user, 'bob-token');
        expect(within(dialog).queryByText(/this token belongs to/i))
            .not.toBeInTheDocument();

        await removeLastChip(user, dialog, /allowed admin permissions/i);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens/10/scope',
            );
        });
    });

    it('hides the actions on a superuser\'s token', async () => {
        mockApi([ROOT_TOKEN, BOB_TOKEN]);
        renderPanel();

        const rootRow = await rowFor('root-token');
        expect(within(rootRow).queryByLabelText(/edit token/i))
            .not.toBeInTheDocument();
        expect(within(rootRow).queryByLabelText(/delete token/i))
            .not.toBeInTheDocument();

        const bobRow = await rowFor('bob-token');
        expect(within(bobRow).getByLabelText(/edit token/i)).toBeEnabled();
        expect(within(bobRow).getByLabelText(/delete token/i)).toBeEnabled();
    });
});
