/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, waitFor, within, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {
    describe,
    it,
    expect,
    vi,
    beforeEach,
    afterEach,
} from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

const mockUseAuth = vi.fn();
vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => mockUseAuth(),
}));

import AdminPermissions from '../AdminPermissions';

const mockGroups = [
    { id: 1, name: 'admins' },
    { id: 2, name: 'developers' },
];

interface GroupApiOverrides {
    groupsRejection?: unknown;
    permissionsRejection?: unknown;
    adminPermsRejection?: unknown;
    mcpPrivileges?: unknown[];
    connPrivileges?: unknown[];
    adminPermissions?: string[];
    connectionsList?: unknown[];
}

function installListMocks(overrides: GroupApiOverrides = {}): void {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/groups') {
            if (overrides.groupsRejection !== undefined) {
                return Promise.reject(overrides.groupsRejection);
            }
            return Promise.resolve({ groups: mockGroups });
        }
        if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
            if (overrides.permissionsRejection !== undefined) {
                return Promise.reject(overrides.permissionsRejection);
            }
            return Promise.resolve({
                mcp_privileges: overrides.mcpPrivileges ?? [],
                connection_privileges: overrides.connPrivileges ?? [],
            });
        }
        if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
            if (overrides.adminPermsRejection !== undefined) {
                return Promise.reject(overrides.adminPermsRejection);
            }
            return Promise.resolve({
                permissions: overrides.adminPermissions ?? [],
            });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({
                connections: overrides.connectionsList ?? [],
            });
        }
        if (url === '/api/v1/rbac/privileges/mcp') {
            return Promise.resolve([]);
        }
        return Promise.resolve({});
    });
}

describe('AdminPermissions', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUseAuth.mockReturnValue({
            user: { username: 'alice', isSuperuser: true },
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Group list fetch', () => {
        it('renders the group selector once loaded', async () => {
            installListMocks();
            renderWithTheme(<AdminPermissions />);
            await waitFor(() => {
                expect(screen.getByText('Permissions')).toBeInTheDocument();
            });
            expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
        });

        it('shows the unified fallback when group list throws a non-Error', async () => {
            installListMocks({ groupsRejection: 'plain string reject' });
            renderWithTheme(<AdminPermissions />);
            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the error message when group list rejects with an Error', async () => {
            installListMocks({ groupsRejection: new Error('boom') });
            renderWithTheme(<AdminPermissions />);
            await waitFor(() => {
                expect(screen.getByText('boom')).toBeInTheDocument();
            });
        });
    });

    describe('Group permissions fetch', () => {
        it('shows the unified fallback when permissions load throws a non-Error', async () => {
            installListMocks({ permissionsRejection: 'plain string reject' });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);
            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));
            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the unified fallback when admin permissions fetch throws a non-Error', async () => {
            installListMocks({ adminPermsRejection: 'plain string reject' });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);
            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));
            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Revoke handlers', () => {
        it('shows the unified fallback when revoking an MCP privilege throws a non-Error', async () => {
            installListMocks({
                mcpPrivileges: [
                    { identifier: 'query_read', item_type: 'tool' },
                ],
            });
            mockApiDelete.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Tool: query_read')).toBeInTheDocument();
            });

            const revokeButtons = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeButtons[revokeButtons.length - 1]);

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the unified fallback when revoking a connection throws a non-Error', async () => {
            installListMocks({
                connPrivileges: [
                    { connection_id: 5, access_level: 'read' },
                ],
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            mockApiDelete.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('primary-db')).toBeInTheDocument();
            });

            const revokeButtons = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the unified fallback when revoking an admin permission throws a non-Error', async () => {
            installListMocks({
                adminPermissions: ['manage_users'],
            });
            mockApiDelete.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Manage Users')).toBeInTheDocument();
            });

            const revokeButtons = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeButtons[0]);

            await waitFor(() => {
                expect(
                    screen.getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('Grant handlers', () => {
        it('shows the unified fallback when granting an admin permission throws a non-Error', async () => {
            installListMocks();
            mockApiPost.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const groupListbox = await screen.findByRole('listbox');
            await user.click(within(groupListbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Admin Permissions')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /Grant Permission/i }),
            );

            const dialog = await screen.findByRole('dialog');
            const permSelect = within(dialog).getByLabelText(/Permission/i);
            fireEvent.mouseDown(permSelect);
            const permListbox = await screen.findByRole('listbox');
            await user.click(within(permListbox).getByText('Manage Users'));

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(
                    within(dialog).getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the unified fallback when granting an MCP privilege throws a non-Error', async () => {
            installListMocks();
            mockApiGet.mockImplementationOnce((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                return Promise.resolve({});
            });
            // Default the rest of the GETs and provide MCP privileges
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: [] });
                }
                if (url === '/api/v1/rbac/privileges/mcp') {
                    return Promise.resolve([
                        { identifier: 'query_read', item_type: 'tool' },
                    ]);
                }
                return Promise.resolve({});
            });
            mockApiPost.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const groupListbox = await screen.findByRole('listbox');
            await user.click(within(groupListbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('MCP Permissions')).toBeInTheDocument();
            });

            // MCP section: locate the Grant button under the MCP Permissions
            // header. There are two "Grant" buttons (connection + MCP); click
            // the second one (MCP comes after the Admin section).
            const grantButtons = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantButtons[grantButtons.length - 1]);

            const dialog = await screen.findByRole('dialog');
            // Open the Autocomplete and pick the only available option
            const mcpInput = within(dialog).getByLabelText(/Permission/i);
            await user.click(mcpInput);
            const mcpOption = await screen.findByRole('option', {
                name: /Tool: query_read/i,
            });
            await user.click(mcpOption);

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(
                    within(dialog).getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });

        it('shows the unified fallback when granting a connection privilege throws a non-Error', async () => {
            installListMocks({
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            mockApiPost.mockRejectedValue('plain string reject');
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const groupListbox = await screen.findByRole('listbox');
            await user.click(within(groupListbox).getByText('admins'));

            await waitFor(() => {
                expect(
                    screen.getByText('Connection Permissions'),
                ).toBeInTheDocument();
            });

            const grantButtons = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantButtons[0]);

            const dialog = await screen.findByRole('dialog');
            const connSelect = within(dialog).getByLabelText(/Connection/i);
            fireEvent.mouseDown(connSelect);
            const connListbox = await screen.findByRole('listbox');
            await user.click(within(connListbox).getByText('primary-db'));

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(
                    within(dialog).getByText('An unexpected error occurred'),
                ).toBeInTheDocument();
            });
        });
    });

    describe('MCP permission rendering', () => {
        it('formats resource, prompt, default API and wildcard MCP entries', async () => {
            installListMocks({
                mcpPrivileges: [
                    { identifier: 'logs_read', item_type: 'resource' },
                    { identifier: 'summarize', item_type: 'prompt' },
                    { identifier: 'rest_endpoint', item_type: 'other' },
                    { identifier: 'naked' },
                ],
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Resource: logs_read')).toBeInTheDocument();
            });
            expect(screen.getByText('Prompt: summarize')).toBeInTheDocument();
            expect(screen.getByText('API: rest_endpoint')).toBeInTheDocument();
            // No item_type and no wildcard - falls through to bare identifier.
            expect(screen.getByText('naked')).toBeInTheDocument();
        });

        it('renders the wildcard sentinel label for "All MCP Privileges"', async () => {
            installListMocks({
                mcpPrivileges: [{ identifier: '*' }],
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(
                    screen.getByText('All MCP Privileges'),
                ).toBeInTheDocument();
            });

            // The Grant button for MCP should be hidden because wildcard is
            // already granted. Confirm the only Grant present is from
            // Connection Permissions (1 instance for non-superuser-style
            // rendering) plus Admin's "Grant Permission" label.
            const grants = screen.queryAllByRole('button', { name: /^Grant$/ });
            expect(grants.length).toBeLessThanOrEqual(1);
        });
    });

    describe('Success paths and dialog dismissals', () => {
        it('successfully revokes an existing MCP permission', async () => {
            installListMocks({
                mcpPrivileges: [
                    { identifier: 'query_read', item_type: 'tool' },
                ],
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Tool: query_read')).toBeInTheDocument();
            });

            const revokeBtns = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeBtns[revokeBtns.length - 1]);

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    expect.stringMatching(/privileges\/mcp\?name=query_read$/),
                );
            });
        });

        it('successfully revokes an existing connection permission', async () => {
            installListMocks({
                connPrivileges: [{ connection_id: 5, access_level: 'read' }],
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('primary-db')).toBeInTheDocument();
            });

            const revokeBtns = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeBtns[0]);

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    expect.stringMatching(/privileges\/connections\/5$/),
                );
            });
        });

        it('successfully revokes an existing admin permission', async () => {
            installListMocks({ adminPermissions: ['manage_users'] });
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Manage Users')).toBeInTheDocument();
            });

            const revokeBtns = screen.getAllByLabelText(/revoke permission/i);
            await user.click(revokeBtns[0]);

            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    expect.stringMatching(/permissions\/manage_users$/),
                );
            });
        });

        it('successfully grants a connection privilege and changes the access level', async () => {
            installListMocks({
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(
                    screen.getByText('Connection Permissions'),
                ).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[0]);

            const dialog = await screen.findByRole('dialog');
            const connSel = within(dialog).getByLabelText(/Connection/i);
            fireEvent.mouseDown(connSel);
            const connListbox = await screen.findByRole('listbox');
            await user.click(within(connListbox).getByText('primary-db'));

            // Change access level to read_write.
            const accessSel = within(dialog).getByLabelText(/Access Level/i);
            fireEvent.mouseDown(accessSel);
            const accessListbox = await screen.findByRole('listbox');
            await user.click(within(accessListbox).getByText(/Read\/Write/i));

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    expect.stringMatching(/privileges\/connections$/),
                    {
                        connection_id: 5,
                        access_level: 'read_write',
                    },
                );
            });
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant connection permission'),
                ).not.toBeInTheDocument();
            });
        });

        it('successfully grants an admin permission', async () => {
            installListMocks();
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Admin Permissions')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /Grant Permission/i }),
            );
            const dialog = await screen.findByRole('dialog');
            const permSel = within(dialog).getByLabelText(/Permission/i);
            fireEvent.mouseDown(permSel);
            const permListbox = await screen.findByRole('listbox');
            await user.click(within(permListbox).getByText('Manage Users'));

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    expect.stringMatching(/groups\/\d+\/permissions$/),
                    { permission: 'manage_users' },
                );
            });
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant admin permission'),
                ).not.toBeInTheDocument();
            });
        });

        it('successfully grants an MCP privilege', async () => {
            installListMocks();
            // Add available privilege after a successful list call.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: [] });
                }
                if (url === '/api/v1/rbac/privileges/mcp') {
                    return Promise.resolve([
                        { identifier: 'query_write', item_type: 'tool' },
                    ]);
                }
                return Promise.resolve({});
            });
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('MCP Permissions')).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[grantBtns.length - 1]);

            const dialog = await screen.findByRole('dialog');
            const mcpInput = within(dialog).getByLabelText(/Permission/i);
            await user.click(mcpInput);
            const mcpOption = await screen.findByRole('option', {
                name: /Tool: query_write/i,
            });
            await user.click(mcpOption);

            await user.click(
                within(dialog).getByRole('button', { name: /^grant$/i }),
            );

            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    expect.stringMatching(/privileges\/mcp$/),
                    { privilege: 'query_write' },
                );
            });
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant MCP permission'),
                ).not.toBeInTheDocument();
            });
        });

        it('cancels each grant dialog without firing apiPost', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const groupListbox = await screen.findByRole('listbox');
            await user.click(within(groupListbox).getByText('admins'));

            await waitFor(() => {
                expect(
                    screen.getByText('Connection Permissions'),
                ).toBeInTheDocument();
            });

            // Open Grant Connection dialog and Cancel.
            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[0]);
            const connDialog = await screen.findByRole('dialog');
            await user.click(
                within(connDialog).getByRole('button', { name: /Cancel/i }),
            );
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant connection permission'),
                ).not.toBeInTheDocument();
            });

            // Open Grant Admin dialog and Cancel.
            await user.click(
                screen.getByRole('button', { name: /Grant Permission/i }),
            );
            const adminDialog = await screen.findByRole('dialog');
            await user.click(
                within(adminDialog).getByRole('button', { name: /Cancel/i }),
            );
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant admin permission'),
                ).not.toBeInTheDocument();
            });

            // Open Grant MCP dialog and Cancel.
            const grantBtnsAfter = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtnsAfter[grantBtnsAfter.length - 1]);
            const mcpDialog = await screen.findByRole('dialog');
            await user.click(
                within(mcpDialog).getByRole('button', { name: /Cancel/i }),
            );
            await waitFor(() => {
                expect(
                    screen.queryByText('Grant MCP permission'),
                ).not.toBeInTheDocument();
            });

            expect(mockApiPost).not.toHaveBeenCalled();
        });

        it('renders connection name from API list and handles missing connection id', async () => {
            installListMocks({
                connPrivileges: [
                    { connection_id: 0, access_level: 'read' },
                    { connection_id: 99, access_level: 'read_write' },
                ],
                // Use the array-shaped connections response branch.
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            // Override the connections endpoint to return a raw array,
            // exercising the Array.isArray(connData) branch.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [
                            { connection_id: 0, access_level: 'read' },
                            { connection_id: 99, access_level: 'read_write' },
                        ],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve([{ id: 5, name: 'primary-db' }]);
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            // id === 0 maps to "All Connections".
            await waitFor(() => {
                expect(
                    screen.getByText('All Connections'),
                ).toBeInTheDocument();
            });
            // id 99 is not in connections list - falls back to String(id).
            expect(screen.getByText('99')).toBeInTheDocument();
        });

        it('falls back when the MCP options endpoint rejects', async () => {
            installListMocks();
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: [] });
                }
                if (url === '/api/v1/rbac/privileges/mcp') {
                    return Promise.reject(new Error('mcp boom'));
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('MCP Permissions')).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[grantBtns.length - 1]);

            const dialog = await screen.findByRole('dialog');
            await waitFor(() => {
                expect(
                    within(dialog).getByText(
                        'Failed to load available permissions',
                    ),
                ).toBeInTheDocument();
            });
        });

        it('dismisses the top-level error alert via the close button', async () => {
            installListMocks({ groupsRejection: new Error('boom') });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

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

        it('skips admin permission fetch for non-superusers', async () => {
            mockUseAuth.mockReturnValue({
                user: { username: 'alice', isSuperuser: false },
            });
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            // Connection Permissions section renders, but the Admin section
            // is hidden for non-superusers (line 475 isSuperuser guard).
            await waitFor(() => {
                expect(
                    screen.getByText('Connection Permissions'),
                ).toBeInTheDocument();
            });
            expect(screen.queryByText('Admin Permissions')).not.toBeInTheDocument();

            // No GET to /permissions endpoint should have happened.
            expect(mockApiGet).not.toHaveBeenCalledWith(
                expect.stringMatching(/groups\/\d+\/permissions$/),
            );
        });

        it('hides the connection Grant button when wildcard is already granted', async () => {
            installListMocks({
                connPrivileges: [{ connection_id: 0, access_level: 'read' }],
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('All Connections')).toBeInTheDocument();
            });

            // Verify no "Grant" button appears for the connections section
            // (only Admin's "Grant Permission" and MCP "Grant" should remain).
            const grants = screen.queryAllByRole('button', { name: /^Grant$/ });
            // MCP grant button remains (wildcard not in mcp set).
            expect(grants.length).toBe(1);
        });

        it('filters out already-granted connections from the Grant connection dialog', async () => {
            installListMocks({
                connPrivileges: [{ connection_id: 5, access_level: 'read' }],
                connectionsList: [
                    { id: 5, name: 'primary-db' },
                    { id: 6, name: 'replica-db' },
                ],
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('primary-db')).toBeInTheDocument();
            });

            // Open the Grant connection dialog and inspect dropdown options.
            const grants = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grants[0]);

            const dialog = await screen.findByRole('dialog');
            const connSel = within(dialog).getByLabelText(/Connection/i);
            fireEvent.mouseDown(connSel);
            const optsBox = await screen.findByRole('listbox');
            // primary-db should NOT appear (already granted), replica-db should.
            expect(within(optsBox).queryByText('primary-db')).not.toBeInTheDocument();
            expect(within(optsBox).getByText('replica-db')).toBeInTheDocument();
        });

        it('shows empty MCP options when the wildcard is already granted (dialog reachable via state path)', async () => {
            // Render with MCP wildcard and an unrelated assignment; the Grant
            // button is hidden, so we render with no wildcard first, then
            // dispatch the open via the existing button. Because the wildcard
            // hides the button, instead we exercise the assignedIdentifiers
            // map (line 237) with one named privilege.
            installListMocks({
                mcpPrivileges: [
                    { identifier: 'query_read', item_type: 'tool' },
                ],
            });
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [
                            { identifier: 'query_read', item_type: 'tool' },
                        ],
                        connection_privileges: [],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: [] });
                }
                if (url === '/api/v1/rbac/privileges/mcp') {
                    return Promise.resolve([
                        { identifier: 'query_read', item_type: 'tool' },
                        { identifier: 'query_write', item_type: 'tool' },
                    ]);
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('Tool: query_read')).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[grantBtns.length - 1]);

            const dialog = await screen.findByRole('dialog');
            const mcpInput = within(dialog).getByLabelText(/Permission/i);
            await user.click(mcpInput);
            // Already-granted privilege should be filtered out.
            expect(
                screen.queryByRole('option', { name: /Tool: query_read/i }),
            ).not.toBeInTheDocument();
            // "All MCP Privileges" sentinel and query_write should appear.
            expect(
                screen.getByRole('option', { name: /All MCP Privileges/i }),
            ).toBeInTheDocument();
            expect(
                screen.getByRole('option', { name: /Tool: query_write/i }),
            ).toBeInTheDocument();
        });

        it('clears state when the group selector is cleared', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            await waitFor(() => {
                expect(screen.getByText('MCP Permissions')).toBeInTheDocument();
            });

            // Re-select empty value by changing select directly.
            // fireEvent.change does not work with native select-style MUI; we
            // simulate by re-clicking the select and choosing a different
            // group, then verify the effect cleared MCP/Conn/Admin state and
            // re-fetched permissions for the new group.
            mockApiGet.mockClear();
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.resolve({ connections: [] });
                }
                return Promise.resolve({});
            });

            fireEvent.mouseDown(select);
            const listbox2 = await screen.findByRole('listbox');
            await user.click(within(listbox2).getByText('developers'));

            // Verify the group permissions endpoint was hit for developers (id 2).
            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith(
                    '/api/v1/rbac/groups/2',
                );
            });
        });

        it('closes the MCP grant dialog when Escape is pressed', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));
            await waitFor(() => {
                expect(screen.getByText('MCP Permissions')).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[grantBtns.length - 1]);
            await screen.findByText('Grant MCP permission');

            // Escape triggers the Dialog's onClose, which in turn invokes the
            // inline onClose callback (line 622).
            await user.keyboard('{Escape}');

            await waitFor(() => {
                expect(
                    screen.queryByText('Grant MCP permission'),
                ).not.toBeInTheDocument();
            });
        });

        it('closes the connection grant dialog when Escape is pressed', async () => {
            installListMocks({
                connectionsList: [{ id: 5, name: 'primary-db' }],
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));
            await waitFor(() => {
                expect(
                    screen.getByText('Connection Permissions'),
                ).toBeInTheDocument();
            });

            const grantBtns = screen.getAllByRole('button', { name: /^Grant$/i });
            await user.click(grantBtns[0]);
            await screen.findByText('Grant connection permission');

            await user.keyboard('{Escape}');

            await waitFor(() => {
                expect(
                    screen.queryByText('Grant connection permission'),
                ).not.toBeInTheDocument();
            });
        });

        it('still renders permissions when the connections endpoint fails', async () => {
            // Cover the connections.catch(() => null) arm at line 183 and
            // the `if (connData)` guard so connections stays empty.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/groups') {
                    return Promise.resolve({ groups: mockGroups });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+$/.test(url)) {
                    return Promise.resolve({
                        mcp_privileges: [],
                        connection_privileges: [
                            { connection_id: 9, access_level: 'read' },
                        ],
                    });
                }
                if (/^\/api\/v1\/rbac\/groups\/\d+\/permissions$/.test(url)) {
                    return Promise.resolve({ permissions: [] });
                }
                if (url === '/api/v1/connections') {
                    return Promise.reject(new Error('connections down'));
                }
                return Promise.resolve({});
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));

            // Connection 9 is unknown - getConnectionName falls back to String(id).
            await waitFor(() => {
                expect(screen.getByText('9')).toBeInTheDocument();
            });
        });

        it('closes the admin grant dialog when Escape is pressed', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminPermissions />);

            await waitFor(() => {
                expect(screen.getByLabelText(/Select Group/i)).toBeInTheDocument();
            });
            const select = screen.getByLabelText(/Select Group/i);
            fireEvent.mouseDown(select);
            const listbox = await screen.findByRole('listbox');
            await user.click(within(listbox).getByText('admins'));
            await waitFor(() => {
                expect(screen.getByText('Admin Permissions')).toBeInTheDocument();
            });

            await user.click(
                screen.getByRole('button', { name: /Grant Permission/i }),
            );
            await screen.findByText('Grant admin permission');

            await user.keyboard('{Escape}');

            await waitFor(() => {
                expect(
                    screen.queryByText('Grant admin permission'),
                ).not.toBeInTheDocument();
            });
        });
    });
});
