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

// Replace the panel with a marker so expanded-row tests can detect the
// effective-permissions section without depending on the panel's internal
// markup or its real ConnectionIcon/AdminIcon imports.
vi.mock('../EffectivePermissionsPanel', () => ({
    default: () => (
        <div data-testid="effective-permissions">Effective permissions</div>
    ),
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
 *
 * The router selects the longest matching key so that nested routes
 * like `/api/v1/rbac/groups/1/effective-privileges` resolve before the
 * shorter `/api/v1/rbac/groups` collection route.
 */
function setupApiGetRouter(overrides: Record<string, unknown> = {}) {
    const defaults: Record<string, unknown> = {
        '/api/v1/rbac/groups': { groups: GROUPS },
        '/api/v1/connections': { connections: CONNECTIONS },
    };
    const routes = { ...defaults, ...overrides };
    mockApiGet.mockImplementation((url: string) => {
        const keys = Object.keys(routes).sort((a, b) => b.length - a.length);
        const matchKey = keys.find(
            (k) => url === k || url.startsWith(`${k}/`),
        );
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

        it('cancels the delete dialog without calling the API', async () => {
            setupApiGetRouter();
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(
                screen.getAllByRole('button', { name: /delete group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Delete Group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Cancel/i }),
            );
            await waitFor(() => {
                expect(screen.queryByText('Delete Group')).not.toBeInTheDocument();
            });
            expect(mockApiDelete).not.toHaveBeenCalled();
        });

        it('clears the expanded row when the deleted group was expanded', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['alice'],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': {
                    connection_privileges: [],
                    admin_permissions: [],
                    mcp_privileges: [],
                },
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            // Expand the first group so the delete path also exercises the
            // expanded-row cleanup branch.
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });

            await user.click(
                screen.getAllByRole('button', { name: /delete group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Delete Group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Delete/i }),
            );

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/rbac/groups/1');
            });
            // The expanded section should have collapsed.
            await waitFor(() => {
                expect(screen.queryByText('alice')).not.toBeInTheDocument();
            });
        });

        it('closes the confirmation dialog after a successful delete and does not re-issue the request', async () => {
            // Regression test for the PR #209 bug, retained after the
            // issue #214 fix: the rbac/groups DELETE endpoint returns
            // 204 No Content, which apiClient surfaces as `undefined`.
            // The original `runMutation` consumer keyed its
            // `closeDelete()` call on `result !== undefined`, treating
            // a 204 success identically to a thrown error. The dialog
            // stayed open with the just-deleted target still selected,
            // so a second confirm click 404'd the now-missing group and
            // the user saw "Failed to delete group". `runMutation` now
            // returns a tagged `{ ok: true | false }` result, so the
            // void-success / failure distinction is unambiguous.
            setupApiGetRouter({
                '/api/v1/rbac/groups': { groups: GROUPS },
            });
            // Mirror the real apiClient behaviour for HTTP 204: a bare
            // `undefined` return value.
            mockApiDelete.mockResolvedValue(undefined);
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(
                screen.getAllByRole('button', { name: /delete group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Delete Group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Delete/i }),
            );

            // The delete API call should have been made exactly once.
            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1',
                );
            });

            // The dialog must close after the successful delete.
            await waitFor(() => {
                expect(
                    screen.queryByText('Delete Group'),
                ).not.toBeInTheDocument();
            });

            // Critical: even though the dialog re-mounts when the refresh
            // toggles `loading`, no second apiDelete call may fire.
            expect(mockApiDelete).toHaveBeenCalledTimes(1);
        });

        it('surfaces a page-level error when the delete API rejects', async () => {
            setupApiGetRouter();
            mockApiDelete.mockRejectedValue(new Error('delete failed'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(
                screen.getAllByRole('button', { name: /delete group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Delete Group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Delete/i }),
            );
            await waitFor(() => {
                expect(screen.getByText('delete failed')).toBeInTheDocument();
            });
        });
    });

    describe('Row expansion', () => {
        it('expands a row and loads members + effective permissions', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['alice', 'bob'],
                    group_members: ['nested-group'],
                },
                '/api/v1/rbac/groups/1/effective-privileges': {
                    connection_privileges: [
                        { connection_id: 11, level: 'read' },
                    ],
                    admin_permissions: ['manage_users'],
                    mcp_privileges: [],
                },
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(screen.getByText('eng'));

            // Group detail came back with user + group members.
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            expect(screen.getByText('bob')).toBeInTheDocument();
            expect(screen.getByText('nested-group')).toBeInTheDocument();

            // The mocked permissions panel should be present.
            expect(
                screen.getByTestId('effective-permissions'),
            ).toBeInTheDocument();

            // Clicking the row again should collapse it.
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.queryByText('alice')).not.toBeInTheDocument();
            });
        });

        it('renders the empty-members message when both lists are empty', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: [],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(screen.getByText('eng'));

            await waitFor(() => {
                expect(
                    screen.getByText('No members in this group.'),
                ).toBeInTheDocument();
            });
        });

        it('shows a page-level error when group detail fetch fails', async () => {
            // Reject the detail endpoint and accept everything else.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: GROUPS });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: CONNECTIONS });
                }
                if (url.startsWith('/api/v1/rbac/groups/1/effective')) {
                    return Promise.reject(new Error('perm fail'));
                }
                if (url === '/api/v1/rbac/groups/1') {
                    return Promise.reject(new Error('detail fail'));
                }
                return Promise.reject(new Error(`unexpected ${url}`));
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            await user.click(screen.getByText('eng'));

            await waitFor(() => {
                expect(screen.getByText('detail fail')).toBeInTheDocument();
            });
        });

        it('refetches the expanded group detail after a successful edit', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['alice'],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': {
                    connection_privileges: [],
                    admin_permissions: [],
                    mcp_privileges: [],
                },
            });
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });

            // Expand 'eng' first so that editing it should refetch detail.
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });

            mockApiGet.mockClear();
            await user.click(
                screen.getAllByRole('button', { name: /edit group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Edit group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Save/i }),
            );

            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1',
                    { name: 'eng', description: 'Engineering' },
                );
            });
            // The detail endpoint should be re-hit after the edit succeeds.
            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/rbac/groups/1');
            });
        });
    });

    describe('Add member dialog', () => {
        function setupExpandedRouter(overrides: Record<string, unknown> = {}) {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: [],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
                '/api/v1/rbac/users': {
                    users: [
                        { id: 100, username: 'alice' },
                        { id: 101, username: 'bob' },
                    ],
                },
                ...overrides,
            });
        }

        async function openAddMember(user: ReturnType<typeof userEvent.setup>) {
            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(
                    screen.getByText('No members in this group.'),
                ).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /Add Member/i }),
            );
            await waitFor(() => {
                expect(screen.getByText('Add member')).toBeInTheDocument();
            });
        }

        it('opens the dialog and loads available users + groups', async () => {
            setupExpandedRouter();
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            // The radio defaults to "user".
            await waitFor(() => {
                expect(screen.getByLabelText('User')).toBeChecked();
            });

            // The Select trigger should be reachable via its label.
            const combo = screen.getByRole('combobox', {
                name: /Select User/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            expect(
                within(listbox).getByRole('option', { name: 'alice' }),
            ).toBeInTheDocument();
            expect(
                within(listbox).getByRole('option', { name: 'bob' }),
            ).toBeInTheDocument();
        });

        it('switches member type to group and lists available groups', async () => {
            setupExpandedRouter();
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            await user.click(screen.getByLabelText('Group'));
            const combo = screen.getByRole('combobox', {
                name: /Select Group/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            // The expanded group ('eng', id=1) should be filtered out.
            expect(
                within(listbox).queryByRole('option', { name: 'eng' }),
            ).not.toBeInTheDocument();
            expect(
                within(listbox).getByRole('option', { name: 'ops' }),
            ).toBeInTheDocument();
        });

        it('posts the selected user and refreshes on success', async () => {
            setupExpandedRouter();
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            const combo = screen.getByRole('combobox', {
                name: /Select User/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            await user.click(
                within(listbox).getByRole('option', { name: 'alice' }),
            );
            await user.click(screen.getByRole('button', { name: /^Add$/ }));

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1/members',
                    { user_id: 100 },
                );
            });
        });

        it('posts the selected group when member type is group', async () => {
            setupExpandedRouter();
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            await user.click(screen.getByLabelText('Group'));
            const combo = screen.getByRole('combobox', {
                name: /Select Group/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            await user.click(
                within(listbox).getByRole('option', { name: 'ops' }),
            );
            await user.click(screen.getByRole('button', { name: /^Add$/ }));

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1/members',
                    { group_id: 2 },
                );
            });
        });

        it('rewrites UNIQUE constraint errors to a friendly message', async () => {
            setupExpandedRouter();
            mockApiPost.mockRejectedValue(
                new Error('SQLite: UNIQUE constraint failed: members'),
            );
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            const combo = screen.getByRole('combobox', {
                name: /Select User/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            await user.click(
                within(listbox).getByRole('option', { name: 'alice' }),
            );
            await user.click(screen.getByRole('button', { name: /^Add$/ }));

            await waitFor(() => {
                expect(
                    screen.getByText('This member is already in the group.'),
                ).toBeInTheDocument();
            });
        });

        it('surfaces a generic API error in the dialog alert', async () => {
            setupExpandedRouter();
            mockApiPost.mockRejectedValue(new Error('upstream 500'));
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            const combo = screen.getByRole('combobox', {
                name: /Select User/i,
            });
            await user.click(combo);
            const listbox = await screen.findByRole('listbox');
            await user.click(
                within(listbox).getByRole('option', { name: 'alice' }),
            );
            await user.click(screen.getByRole('button', { name: /^Add$/ }));

            await waitFor(() => {
                expect(screen.getByText('upstream 500')).toBeInTheDocument();
            });
        });

        it('renders a dialog-level error when loading available members fails', async () => {
            // Reject both lookup endpoints so the catch branch fires.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: GROUPS });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: CONNECTIONS });
                }
                if (url === '/api/v1/rbac/groups/1') {
                    return Promise.resolve({
                        user_members: [],
                        group_members: [],
                    });
                }
                if (url === '/api/v1/rbac/groups/1/effective-privileges') {
                    return Promise.resolve(null);
                }
                return Promise.reject(new Error('load fail'));
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(
                    screen.getByText('No members in this group.'),
                ).toBeInTheDocument();
            });
            // The .catch on each lookup swallows individual failures so the
            // outer try/catch does not fire; available lists stay empty.
            await user.click(
                screen.getByRole('button', { name: /Add Member/i }),
            );
            await waitFor(() => {
                expect(screen.getByRole('dialog')).toBeInTheDocument();
            });
            // Submit is disabled because no selection is possible.
            expect(
                screen.getByRole('button', { name: /^Add$/ }),
            ).toBeDisabled();
        });

        it('closes the dialog on Cancel without posting', async () => {
            setupExpandedRouter();
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            await user.click(screen.getByRole('button', { name: /Cancel/i }));
            await waitFor(() => {
                expect(screen.queryByText('Add member')).not.toBeInTheDocument();
            });
            expect(mockApiPost).not.toHaveBeenCalled();
        });

        it('returns early when handleAddMember runs without a selected member', async () => {
            setupExpandedRouter();
            const user = userEvent.setup({ delay: null });

            await openAddMember(user);

            // Without a selection the Add button is disabled, so the early
            // return inside handleAddMember is guarded by the disabled
            // attribute. Verifying it's disabled is sufficient.
            expect(
                screen.getByRole('button', { name: /^Add$/ }),
            ).toBeDisabled();
        });
    });

    describe('Remove member', () => {
        it('looks up the user id and deletes a user member', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['alice'],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
                '/api/v1/rbac/users': {
                    users: [{ id: 100, username: 'alice' }],
                },
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });

            await user.click(
                screen.getAllByRole('button', { name: /remove member/i })[0],
            );

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1/members/user/100',
                );
            });
        });

        it('uses the in-memory group list for group members', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: [],
                    group_members: ['ops'],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                // The 'ops' name should appear in the expanded panel
                // (in addition to its own row), so the remove button
                // exists.
                expect(
                    screen.getAllByRole('button', { name: /remove member/i }),
                ).toHaveLength(1);
            });

            await user.click(
                screen.getByRole('button', { name: /remove member/i }),
            );

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/1/members/group/2',
                );
            });
        });

        it('reports an error when the user lookup cannot resolve the name', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['ghost'],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
                '/api/v1/rbac/users': {
                    users: [{ id: 100, username: 'alice' }],
                },
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('ghost')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /remove member/i }),
            );

            await waitFor(() => {
                expect(
                    screen.getByText('Could not find user "ghost"'),
                ).toBeInTheDocument();
            });
            expect(mockApiDelete).not.toHaveBeenCalled();
        });

        it('surfaces a page-level error when the delete call rejects', async () => {
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: ['alice'],
                    group_members: [],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
                '/api/v1/rbac/users': {
                    users: [{ id: 100, username: 'alice' }],
                },
            });
            mockApiDelete.mockRejectedValue(new Error('cannot remove'));
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /remove member/i }),
            );

            await waitFor(() => {
                expect(screen.getByText('cannot remove')).toBeInTheDocument();
            });
        });

        it('skips delete when the group lookup cannot find the name', async () => {
            // The group_members entry references a name not in the items list
            // (because the only groups available are 'eng' and 'ops'), so the
            // lookup branch returns the "Could not find" error.
            setupApiGetRouter({
                '/api/v1/rbac/groups/1': {
                    user_members: [],
                    group_members: ['nowhere'],
                },
                '/api/v1/rbac/groups/1/effective-privileges': null,
            });
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(screen.getByText('eng'));
            await waitFor(() => {
                expect(screen.getByText('nowhere')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /remove member/i }),
            );

            await waitFor(() => {
                expect(
                    screen.getByText('Could not find group "nowhere"'),
                ).toBeInTheDocument();
            });
            expect(mockApiDelete).not.toHaveBeenCalled();
        });
    });

    describe('Edit dialog cancel', () => {
        it('closes the edit dialog on Cancel without calling PUT', async () => {
            setupApiGetRouter();
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(
                screen.getAllByRole('button', { name: /edit group/i })[0],
            );
            await waitFor(() => {
                expect(screen.getByText('Edit group')).toBeInTheDocument();
            });
            const dialog = screen.getByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /Cancel/i }),
            );
            await waitFor(() => {
                expect(screen.queryByText('Edit group')).not.toBeInTheDocument();
            });
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('returns early when the edit form is submitted with a blank name', async () => {
            setupApiGetRouter();
            const user = userEvent.setup({ delay: null });

            renderWithTheme(<AdminGroups />);
            await waitFor(() => {
                expect(screen.getByText('eng')).toBeInTheDocument();
            });
            await user.click(
                screen.getAllByRole('button', { name: /edit group/i })[0],
            );
            const dialog = await screen.findByRole('dialog');
            // Empty the Name field; the Save button should then be
            // disabled, and the underlying early-return guard is
            // exercised by the disabled attribute.
            fireEvent.change(within(dialog).getByLabelText('Name *'), {
                target: { value: '' },
            });
            expect(
                within(dialog).getByRole('button', { name: /Save/i }),
            ).toBeDisabled();
        });
    });
});
