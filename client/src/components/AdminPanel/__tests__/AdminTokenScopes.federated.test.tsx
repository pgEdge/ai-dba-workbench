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
 * A user who signs in through an identity provider may not own API
 * tokens while the installation keeps them off for federated logins
 * (issue #742). These tests check that the panel does not offer such a
 * user token creation, says why, and still shows the server's 403 when
 * a creation it does offer is refused.
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

interface MockUser {
    username: string;
    isSuperuser: boolean;
    apiTokensAllowed?: boolean;
}

let mockCurrentUser: MockUser = { username: 'bob', isSuperuser: false };

vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({
        user: mockCurrentUser,
        adminPermissions: ['manage_token_scopes'],
    }),
}));

import AdminTokenScopes from '../AdminTokenScopes';
import {
    FEDERATED_TOKENS_DISABLED_TEXT,
    FEDERATED_OWN_TOKENS_REFUSED_TEXT,
    FEDERATED_OWNER_EXCLUDED_TEXT,
} from '../tokens/tokenTypes';

const theme = createTheme();

const ALICE = { id: 42, username: 'alice' };
const BOB = { id: 43, username: 'bob' };

const BOB_TOKEN = {
    id: 10,
    name: 'bob-token',
    token_prefix: 'tk',
    is_service_account: false,
    is_superuser: false,
    expires_at: null,
    username: BOB.username,
    user_id: BOB.id,
};

const SERVER_REFUSAL = `Permission denied: ${FEDERATED_TOKENS_DISABLED_TEXT}`;

const mockApi = (tokens: unknown[]) => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/tokens') {
            return Promise.resolve({ tokens });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: [] });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve([]);
        }
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({ users: [ALICE, BOB] });
        }
        if (url.endsWith('/privileges')) {
            return Promise.resolve({ is_superuser: false });
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

const waitForTable = () => screen.findByRole('heading', { name: 'Tokens' });

describe('AdminTokenScopes - federated logins without API tokens', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
    });

    it('offers creation and no notice when tokens are allowed', async () => {
        mockCurrentUser = {
            username: 'bob', isSuperuser: false, apiTokensAllowed: true,
        };
        mockApi([BOB_TOKEN]);
        renderPanel();

        expect(await screen.findByRole('button', { name: /create token/i }))
            .toBeInTheDocument();
        expect(screen.queryByText(FEDERATED_TOKENS_DISABLED_TEXT))
            .not.toBeInTheDocument();
    });

    it('treats a server that does not report the field as allowing tokens', async () => {
        mockCurrentUser = { username: 'bob', isSuperuser: false };
        mockApi([]);
        renderPanel();

        expect(await screen.findByRole('button', { name: /create token/i }))
            .toBeInTheDocument();
        expect(screen.queryByText(FEDERATED_TOKENS_DISABLED_TEXT))
            .not.toBeInTheDocument();
    });

    it('does not offer creation to a federated user, and says why', async () => {
        mockCurrentUser = {
            username: 'bob', isSuperuser: false, apiTokensAllowed: false,
        };
        mockApi([]);
        renderPanel();

        await waitForTable();
        const notice = await screen.findByText(FEDERATED_TOKENS_DISABLED_TEXT);
        expect(notice.closest('[role="alert"]')).toHaveClass(
            'MuiAlert-standardInfo',
        );
        expect(screen.queryByRole('button', { name: /create token/i }))
            .not.toBeInTheDocument();
        // No tokens of their own, so nothing to say about them.
        expect(screen.queryByText(FEDERATED_OWN_TOKENS_REFUSED_TEXT))
            .not.toBeInTheDocument();
        expect(mockApiPost).not.toHaveBeenCalled();
    });

    it('notes that the federated user\'s existing tokens are refused', async () => {
        mockCurrentUser = {
            username: 'bob', isSuperuser: false, apiTokensAllowed: false,
        };
        mockApi([BOB_TOKEN]);
        renderPanel();

        expect(await screen.findByText('bob-token')).toBeInTheDocument();
        expect(screen.getByText(FEDERATED_TOKENS_DISABLED_TEXT))
            .toBeInTheDocument();
        expect(screen.getByText(FEDERATED_OWN_TOKENS_REFUSED_TEXT))
            .toBeInTheDocument();
    });

    it('lets a federated superuser create tokens for others only', async () => {
        mockCurrentUser = {
            username: 'bob', isSuperuser: true, apiTokensAllowed: false,
        };
        mockApi([]);
        mockApiPost.mockResolvedValue({ id: 77, token: 'secret-token' });
        const user = userEvent.setup({ delay: null });
        renderPanel();

        expect(await screen.findByText(FEDERATED_TOKENS_DISABLED_TEXT))
            .toBeInTheDocument();
        await user.click(
            await screen.findByRole('button', { name: /create token/i }),
        );
        const dialog = await screen.findByRole('dialog');
        expect(within(dialog).getByText(FEDERATED_OWNER_EXCLUDED_TEXT))
            .toBeInTheDocument();

        const owner = within(dialog).getByRole('combobox', { name: /owner/i });
        await user.click(owner);
        const listbox = await screen.findByRole('listbox');
        const names = within(listbox).getAllByRole('option')
            .map((o) => o.textContent);
        expect(names).toEqual(['alice']);
        await user.click(within(listbox).getByRole('option', { name: 'alice' }));

        await user.type(
            within(dialog).getByRole('textbox', { name: /name/i }),
            'alice-token',
        );
        await user.click(within(dialog).getByRole('button', { name: /^Create$/ }));

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/rbac/tokens', {
                owner_username: 'alice',
                annotation: 'alice-token',
                expires_in: '90d',
            });
        });
    });

    it('shows the server\'s 403 when it refuses a federated owner', async () => {
        // The client cannot tell whether another user is federated, so
        // the server's refusal is the fallback.
        mockCurrentUser = {
            username: 'bob', isSuperuser: true, apiTokensAllowed: true,
        };
        mockApi([]);
        mockApiPost.mockRejectedValue(new Error(SERVER_REFUSAL));
        const user = userEvent.setup({ delay: null });
        renderPanel();

        await user.click(
            await screen.findByRole('button', { name: /create token/i }),
        );
        const dialog = await screen.findByRole('dialog');
        await user.click(within(dialog).getByRole('combobox', { name: /owner/i }));
        await user.click(await screen.findByRole('option', { name: 'alice' }));
        await user.type(
            within(dialog).getByRole('textbox', { name: /name/i }),
            'alice-token',
        );
        await user.click(within(dialog).getByRole('button', { name: /^Create$/ }));

        expect(await within(dialog).findByText(SERVER_REFUSAL))
            .toBeInTheDocument();
    });
});
