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
 * The server refuses a connection scope that names a connection twice
 * or mixes "all connections" (connection 0) with particular connections
 * (issue #471). These tests check that the panel shows such a stored
 * scope with an explanation and blocks saving it until it is resolved,
 * shows the server's message when a save is refused anyway, and never
 * leaves behind a token created without the scope it was meant to have.
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
import { ALL_CONNECTIONS_LABEL } from '../tokens/tokenTypes';

const theme = createTheme();

const USERS = [{ id: 42, username: 'alice' }];
const CONNECTIONS = [
    { id: 100, name: 'primary-db' },
    { id: 101, name: 'replica-db' },
];

const SERVER_REFUSAL =
    'invalid connection scope: the all-connections entry (connection 0) ' +
    'cannot be combined with entries for particular connections';

const MIXED_TOKEN = {
    id: 9,
    name: 'mixed-token',
    token_prefix: 'mt',
    username: 'alice',
    user_id: USERS[0].id,
    is_service_account: false,
    is_superuser: false,
    expires_at: null,
    scope: {
        scoped: true,
        connections: [
            { connection_id: 0, access_level: 'read' },
            { connection_id: 100, access_level: 'read_write' },
        ],
    },
};

const mockApi = (tokens: unknown[]) => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: CONNECTIONS });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve([]);
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

const openEdit = async (user: ReturnType<typeof userEvent.setup>) => {
    await waitFor(() => {
        expect(screen.getByText('mixed-token')).toBeInTheDocument();
    });
    await user.click(screen.getByLabelText(/edit token/i));
    return screen.findByRole('dialog');
};

/** Opens the create dialog and fills in a token scoped to primary-db. */
const fillCreate = async (user: ReturnType<typeof userEvent.setup>) => {
    await waitFor(() => {
        expect(screen.getByRole('button', { name: /create token/i })).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /create token/i }));
    const dialog = await screen.findByRole('dialog');
    await user.type(within(dialog).getByLabelText(/^Name/i), 'scoped-token');
    await user.click(within(dialog).getByRole('combobox', { name: /owner/i }));
    await user.click(await screen.findByRole('option', { name: 'alice' }));
    await waitFor(() => {
        expect(mockApiGet).toHaveBeenCalledWith(
            `/api/v1/rbac/users/${USERS[0].id}/privileges`,
        );
    });
    await user.click(within(dialog).getByRole('combobox', { name: /add connection/i }));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByRole('option', { name: 'primary-db' }));
    return dialog;
};

describe('AdminTokenScopes - connection scope rules', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
        mockApiPost.mockResolvedValue({ id: 77, token: 'pgedge_token_xyz' });
    });

    it('explains a stored mixed scope and blocks saving it', async () => {
        mockApi([MIXED_TOKEN]);
        const user = userEvent.setup({ delay: null });
        renderPanel();
        const dialog = await openEdit(user);

        expect(within(dialog).getByText(ALL_CONNECTIONS_LABEL)).toBeInTheDocument();
        expect(within(dialog).getByRole('alert')).toHaveTextContent(
            /combines "All the owner's connections" with entries for particular connections/,
        );
        expect(within(dialog).getByRole('button', { name: /^Save$/ })).toBeDisabled();
    });

    it('saves a mixed scope once the user resolves it', async () => {
        mockApi([MIXED_TOKEN]);
        const user = userEvent.setup({ delay: null });
        renderPanel();
        const dialog = await openEdit(user);

        await user.click(
            within(dialog).getByLabelText(`remove ${ALL_CONNECTIONS_LABEL}`),
        );
        expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument();
        await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/rbac/tokens/9/scope', {
                connections: [{ connection_id: 100, access_level: 'read_write' }],
            });
        });
    });

    it('shows the server message when a save is refused', async () => {
        mockApi([MIXED_TOKEN]);
        mockApiPut.mockRejectedValue(new Error(SERVER_REFUSAL));
        const user = userEvent.setup({ delay: null });
        renderPanel();
        const dialog = await openEdit(user);

        await user.click(
            within(dialog).getByLabelText(`remove ${ALL_CONNECTIONS_LABEL}`),
        );
        await user.click(within(dialog).getByRole('button', { name: /^Save$/ }));

        expect(await within(dialog).findByText(SERVER_REFUSAL)).toBeInTheDocument();
        expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('deletes a created token whose scope is refused', async () => {
        mockApi([]);
        mockApiPut.mockRejectedValue(new Error(SERVER_REFUSAL));
        const user = userEvent.setup({ delay: null });
        renderPanel();
        const dialog = await fillCreate(user);

        await user.click(within(dialog).getByRole('button', { name: /^Create$/ }));

        expect(
            await within(dialog).findByText(
                `The token was not created because its scope was refused: ${SERVER_REFUSAL}`,
            ),
        ).toBeInTheDocument();
        expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/rbac/tokens/77');
        expect(screen.queryByText('Token created')).not.toBeInTheDocument();
    });

    it('warns when a created token whose scope is refused cannot be deleted', async () => {
        mockApi([]);
        mockApiPut.mockRejectedValue(new Error(SERVER_REFUSAL));
        mockApiDelete.mockRejectedValue(new Error('delete failed'));
        const user = userEvent.setup({ delay: null });
        renderPanel();
        await fillCreate(user);

        await user.click(screen.getByRole('button', { name: /^Create$/ }));

        expect(
            await screen.findByText(/the token could not be deleted, so it holds its owner's whole access/),
        ).toBeInTheDocument();
        expect(screen.queryByText('Token created')).not.toBeInTheDocument();
    });
});
