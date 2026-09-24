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
 * An empty scope category means "no restriction", on the server as in
 * this panel, and the server refuses an empty array on the scope PUT
 * (issue #471 review). These tests check that the panel never sends
 * one: empty categories are left out, a scope emptied of every category
 * is cleared with DELETE, and a category that was restricted cannot be
 * emptied alone, since leaving it out of the PUT would keep it.
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

import AdminTokenScopes from '../AdminTokenScopes';
import {
    NO_CONNECTION_RESTRICTION_TEXT,
    NO_MCP_RESTRICTION_TEXT,
    NO_ADMIN_RESTRICTION_TEXT,
} from '../tokens/tokenTypes';

const theme = createTheme();

const USERS = [{ id: 42, username: 'alice' }];
const CONNECTIONS = [{ id: 100, name: 'primary-db' }];
const MCP_PRIVILEGES = [{ id: 1, identifier: 'query_read' }];

interface TestScope {
    scoped: boolean;
    connections?: { connection_id: number; access_level: string }[];
    mcp_privileges?: number[];
    admin_permissions?: string[];
}

const makeToken = (scope: TestScope) => ({
    id: 9,
    name: 'scope-token',
    token_prefix: 'st',
    username: 'alice',
    user_id: USERS[0].id,
    is_service_account: false,
    is_superuser: false,
    expires_at: null,
    scope,
});

const mockApi = (tokens: unknown[]) => {
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
};

const renderPanel = () =>
    render(
        <ThemeProvider theme={theme}>
            <AdminTokenScopes />
        </ThemeProvider>,
    );

/** Opens the edit dialog for the single token and returns it. */
const openEdit = async (user: ReturnType<typeof userEvent.setup>) => {
    await waitFor(() => {
        expect(screen.getByText('scope-token')).toBeInTheDocument();
    });
    await user.click(screen.getByLabelText(/edit token/i));
    return screen.findByRole('dialog');
};

/** Removes the last chip from a multi-select with Backspace. */
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

describe('AdminTokenScopes - empty scope categories', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
    });

    it('explains that an empty category is no restriction', async () => {
        mockApi([makeToken({ scoped: true, admin_permissions: ['manage_users'] })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);

        expect(within(dialog).getByText(NO_CONNECTION_RESTRICTION_TEXT))
            .toBeInTheDocument();
        expect(within(dialog).getByText(NO_MCP_RESTRICTION_TEXT))
            .toBeInTheDocument();
        // The admin category is restricted, so it carries no such text.
        expect(within(dialog).queryByText(NO_ADMIN_RESTRICTION_TEXT))
            .not.toBeInTheDocument();
    });

    it('explains empty categories in the create dialog too', async () => {
        mockApi([]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await user.click(
            await screen.findByRole('button', { name: /create token/i }),
        );
        const dialog = await screen.findByRole('dialog');

        expect(within(dialog).getByText(NO_CONNECTION_RESTRICTION_TEXT))
            .toBeInTheDocument();
        expect(within(dialog).getByText(NO_MCP_RESTRICTION_TEXT))
            .toBeInTheDocument();
        expect(within(dialog).getByText(NO_ADMIN_RESTRICTION_TEXT))
            .toBeInTheDocument();
    });

    it('sends only the restricted categories when saving', async () => {
        mockApi([makeToken({ scoped: true, admin_permissions: ['manage_users'] })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens/9/scope',
                { admin_permissions: ['manage_users'] },
            );
        });
        expect(mockApiDelete).not.toHaveBeenCalled();
    });

    it('clears the scope with DELETE when every category is emptied', async () => {
        mockApi([makeToken({ scoped: true, admin_permissions: ['manage_users'] })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await removeLastChip(user, dialog, /allowed admin permissions/i);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens/9/scope',
            );
        });
        expect(mockApiPut).not.toHaveBeenCalled();
        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
    });

    it('reports a failed DELETE and keeps the dialog open', async () => {
        mockApi([makeToken({ scoped: true, admin_permissions: ['manage_users'] })]);
        mockApiDelete.mockRejectedValue(new Error('scope clear failed'));
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await removeLastChip(user, dialog, /allowed admin permissions/i);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(within(dialog).getByText('scope clear failed'))
                .toBeInTheDocument();
        });
    });

    it('makes no call when an unscoped token is saved with nothing selected', async () => {
        mockApi([makeToken({ scoped: false })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });
        expect(mockApiPut).not.toHaveBeenCalled();
        expect(mockApiDelete).not.toHaveBeenCalled();
    });

    it('refuses to lift one restricted category whilst others stay', async () => {
        mockApi([makeToken({
            scoped: true,
            connections: [{ connection_id: 100, access_level: 'read' }],
            admin_permissions: ['manage_users'],
        })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await removeLastChip(user, dialog, /allowed admin permissions/i);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(within(dialog).getByText(
                /admin permissions restriction cannot be lifted on its own/,
            )).toBeInTheDocument();
        });
        expect(mockApiPut).not.toHaveBeenCalled();
        expect(mockApiDelete).not.toHaveBeenCalled();
        expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('names every category being lifted, connections included', async () => {
        mockApi([makeToken({
            scoped: true,
            connections: [{ connection_id: 100, access_level: 'read' }],
            mcp_privileges: [1],
            admin_permissions: ['manage_users'],
        })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await user.click(
            within(dialog).getByRole('button', { name: 'remove primary-db' }),
        );
        await removeLastChip(user, dialog, /allowed mcp privileges/i);
        await clickSave(user, dialog);

        await waitFor(() => {
            expect(within(dialog).getByText(
                /connections and MCP privileges restriction cannot be lifted/,
            )).toBeInTheDocument();
        });
        // Lifting every restriction and then restoring the others would
        // leave the token unrestricted in between, so that is not offered.
        expect(within(dialog).queryByText(/remove every restriction/))
            .not.toBeInTheDocument();
        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('refuses to save when a stored restriction could not be shown', async () => {
        mockApi([makeToken({ scoped: true, mcp_privileges: [1] })]);
        const listing = mockApiGet.getMockImplementation() as (url: string) => Promise<unknown>;
        mockApiGet.mockImplementation((url: string) =>
            url === '/api/v1/rbac/privileges/mcp'
                ? Promise.reject(new Error('privileges unavailable'))
                : listing(url));
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        expect(within(dialog).getByText(/could not all be shown/))
            .toBeInTheDocument();

        await clickSave(user, dialog);

        expect(within(dialog).getByText(/could not all be shown/))
            .toBeInTheDocument();
        expect(mockApiDelete).not.toHaveBeenCalled();
        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('refuses to save when a stored admin permission is not recognised', async () => {
        mockApi([makeToken({ scoped: true, admin_permissions: ['retired_permission'] })]);
        const user = userEvent.setup({ delay: null });
        renderPanel();

        const dialog = await openEdit(user);
        await clickSave(user, dialog);

        expect(within(dialog).getByText(/admin permissions could not all be shown/))
            .toBeInTheDocument();
        expect(mockApiDelete).not.toHaveBeenCalled();
        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('omits empty categories when creating a scoped token', async () => {
        mockApi([]);
        mockApiPost.mockResolvedValue({ id: 51, token: 'pgedge_token_new' });
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await user.click(
            await screen.findByRole('button', { name: /create token/i }),
        );
        await user.type(screen.getByLabelText(/^Name/i), 'narrow-token');
        await user.click(await screen.findByRole('combobox', { name: /owner/i }));
        await user.click(await screen.findByRole('option', { name: 'alice' }));
        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith(
                `/api/v1/rbac/users/${USERS[0].id}/privileges`,
            );
        });

        await user.click(screen.getByRole('combobox', {
            name: /allowed mcp privileges/i,
        }));
        const listbox = await screen.findByRole('listbox');
        await user.click(within(listbox).getByRole('option', { name: 'query_read' }));
        await user.click(screen.getByLabelText(/^Name/i));

        await user.click(screen.getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/rbac/tokens/51/scope',
                { mcp_privileges: ['query_read'] },
            );
        });
    }, 15000);

    it('makes no scope call when creating an unscoped token', async () => {
        mockApi([]);
        mockApiPost.mockResolvedValue({ id: 52, token: 'pgedge_token_open' });
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await user.click(
            await screen.findByRole('button', { name: /create token/i }),
        );
        await user.type(screen.getByLabelText(/^Name/i), 'open-token');
        await user.click(await screen.findByRole('combobox', { name: /owner/i }));
        await user.click(await screen.findByRole('option', { name: 'alice' }));
        await user.click(screen.getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(screen.getByText('Token created')).toBeInTheDocument();
        });
        expect(mockApiPut).not.toHaveBeenCalled();
    }, 15000);
});
