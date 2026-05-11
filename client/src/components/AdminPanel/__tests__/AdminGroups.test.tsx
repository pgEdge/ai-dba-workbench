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

// useAuth is required by AdminGroups to drive the isSuperuser flag for
// the expanded EffectivePermissionsPanel. We stub a non-superuser user
// so the panel renders in its standard mode.
vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: { isSuperuser: false } }),
}));

import AdminGroups from '../AdminGroups';

const GROUPS = [
    { id: 1, name: 'eng', description: 'Engineering', member_count: 2 },
    { id: 2, name: 'ops', description: 'Operations', member_count: 1 },
];

const CONNECTIONS = [{ id: 11, name: 'prod-db' }];

/**
 * Routes the apiGet mock to per-URL fixtures so each test only needs to
 * specify what differs from the defaults. Each route may be overridden
 * by passing a custom map.
 */
function setupApiGetRouter(overrides: Record<string, unknown> = {}) {
    const defaults: Record<string, unknown> = {
        '/api/v1/rbac/groups': { groups: GROUPS },
        '/api/v1/connections': { connections: CONNECTIONS },
    };
    const routes = { ...defaults, ...overrides };
    mockApiGet.mockImplementation((url: string) => {
        const matchKey = Object.keys(routes).find((k) => url === k || url.startsWith(`${k}/`) || url.startsWith(k));
        if (!matchKey) {
            return Promise.reject(new Error(`No fixture for ${url}`));
        }
        const fixture = routes[matchKey];
        if (fixture instanceof Error) {
            return Promise.reject(fixture);
        }
        return Promise.resolve(fixture);
    });
}

describe('AdminGroups', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('shows the loading indicator then the groups table', async () => {
        // First call (the list fetch) hangs forever — we rely on this
        // to assert that the loading spinner is rendered.
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<AdminGroups />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(screen.getByLabelText('Loading groups')).toBeInTheDocument();
    });

    it('renders the group list returned by the API', async () => {
        setupApiGetRouter();

        renderWithTheme(<AdminGroups />);

        await waitFor(() => {
            expect(screen.getByText('eng')).toBeInTheDocument();
        });
        expect(screen.getByText('ops')).toBeInTheDocument();
        expect(screen.getByText('Engineering')).toBeInTheDocument();
        expect(screen.getByText('Operations')).toBeInTheDocument();
    });

    it('shows the empty state when no groups exist', async () => {
        setupApiGetRouter({ '/api/v1/rbac/groups': { groups: [] } });

        renderWithTheme(<AdminGroups />);

        await waitFor(() => {
            expect(screen.getByText('No groups found.')).toBeInTheDocument();
        });
    });

    it('shows an error alert when the group fetch fails', async () => {
        setupApiGetRouter({
            '/api/v1/rbac/groups': new Error('Server unavailable'),
        });

        renderWithTheme(<AdminGroups />);

        await waitFor(() => {
            expect(screen.getByText('Server unavailable')).toBeInTheDocument();
        });
    });

    describe('Create group dialog', () => {
        it('opens, accepts input, and posts to the API', async () => {
            setupApiGetRouter({ '/api/v1/rbac/groups': { groups: [] } });
            mockApiPost.mockResolvedValue({ id: 99 });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);

            await waitFor(() => {
                expect(screen.getByText('No groups found.')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /Create Group/i }),
            );
            await waitFor(() => {
                expect(screen.getByText('Create group')).toBeInTheDocument();
            });

            const dialog = screen.getByRole('dialog');
            const createButton = within(dialog).getByRole('button', {
                name: /Create/i,
            });
            expect(createButton).toBeDisabled();

            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'new-group' },
            });
            fireEvent.change(within(dialog).getByLabelText('Description'), {
                target: { value: 'Brand new' },
            });
            expect(createButton).not.toBeDisabled();

            await user.click(createButton);

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups',
                    { name: 'new-group', description: 'Brand new' },
                );
            });
        });

        it('surfaces a server error in the create dialog', async () => {
            setupApiGetRouter({ '/api/v1/rbac/groups': { groups: [] } });
            mockApiPost.mockRejectedValue(new Error('duplicate name'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);

            await waitFor(() => {
                expect(screen.getByText('No groups found.')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /Create Group/i }),
            );
            const dialog = screen.getByRole('dialog');
            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: 'eng' },
            });
            await user.click(
                within(dialog).getByRole('button', { name: /Create/i }),
            );

            await waitFor(() => {
                expect(within(dialog).getByText('duplicate name')).toBeInTheDocument();
            });
        });

        it('closes the create dialog on Cancel', async () => {
            setupApiGetRouter({ '/api/v1/rbac/groups': { groups: [] } });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('No groups found.')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /Create Group/i }),
            );
            await waitFor(() => {
                expect(screen.getByText('Create group')).toBeInTheDocument();
            });
            await user.click(screen.getByRole('button', { name: /Cancel/i }));
            await waitFor(() => {
                expect(screen.queryByText('Create group')).not.toBeInTheDocument();
            });
        });
    });

    describe('Edit group dialog', () => {
        it('pre-populates fields with the selected group and PUTs changes', async () => {
            setupApiGetRouter();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            const editButtons = screen.getAllByRole('button', {
                name: /edit group/i,
            });
            await user.click(editButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Edit group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            expect(within(dialog).getByLabelText('Name *')).toHaveValue('eng');
            expect(within(dialog).getByLabelText('Description')).toHaveValue(
                'Engineering',
            );

            fireEvent.change(within(dialog).getByLabelText('Description'), {
                target: { value: 'Updated' },
            });
            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith('/api/v1/rbac/groups/1', {
                    name: 'eng',
                    description: 'Updated',
                });
            });
        });
    });

    describe('Delete group', () => {
        it('opens the confirmation dialog and deletes via the API', async () => {
            setupApiGetRouter();
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            const deleteButtons = screen.getAllByRole('button', {
                name: /delete group/i,
            });
            await user.click(deleteButtons[0]);

            await waitFor(() => {
                expect(screen.getByText('Delete Group')).toBeInTheDocument();
            });

            await user.click(screen.getByRole('button', { name: /Delete/i }));

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/rbac/groups/1');
            });
        });
    });
});
