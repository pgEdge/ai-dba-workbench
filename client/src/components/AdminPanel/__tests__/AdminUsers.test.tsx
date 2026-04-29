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
const mockApiPut = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

vi.mock('../EffectivePermissionsPanel', () => ({
    default: () => (
        <div data-testid="effective-permissions">Effective permissions</div>
    ),
}));

const mockUseAuth = vi.fn();
vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => mockUseAuth(),
}));

import AdminUsers from '../AdminUsers';

const mockUsers = [
    {
        id: 1,
        username: 'alice',
        display_name: 'Alice',
        email: 'alice@example.com',
        annotation: 'admin',
        is_service_account: false,
        is_superuser: true,
        enabled: true,
    },
    {
        id: 2,
        username: 'bob',
        display_name: 'Bob',
        email: 'bob@example.com',
        annotation: '',
        is_service_account: false,
        is_superuser: false,
        enabled: true,
    },
    {
        id: 3,
        username: 'service-bot',
        display_name: 'Service Bot',
        email: '',
        annotation: 'automation',
        is_service_account: true,
        is_superuser: false,
        enabled: true,
    },
];

const installListMocks = (overrides?: {
    users?: typeof mockUsers;
    privileges?: unknown;
    privilegesRejection?: unknown;
}) => {
    mockApiGet.mockImplementation((url: string) => {
        if (url === '/api/v1/rbac/users') {
            return Promise.resolve({ users: overrides?.users ?? mockUsers });
        }
        if (url === '/api/v1/connections') {
            return Promise.resolve({ connections: [] });
        }
        if (/^\/api\/v1\/rbac\/users\/\d+\/privileges$/.test(url)) {
            if (overrides?.privilegesRejection) {
                return Promise.reject(overrides.privilegesRejection);
            }
            return Promise.resolve(
                overrides?.privileges ?? {
                    connection_privileges: [],
                    admin_permissions: [],
                    mcp_privileges: [],
                    groups: [],
                }
            );
        }
        return Promise.resolve({});
    });
};

const findFieldByLabel = (
    container: HTMLElement,
    labelText: string | RegExp
): HTMLInputElement => {
    const labels = within(container).getAllByText(
        typeof labelText === 'string'
            ? new RegExp(`^${labelText}`, 'i')
            : labelText
    );
    for (const label of labels) {
        const forAttr = label.getAttribute('for');
        if (forAttr) {
            const input = document.getElementById(forAttr) as HTMLInputElement
                | null;
            if (input) {
                return input;
            }
        }
    }
    throw new Error(`Could not find field with label: ${labelText}`);
};

describe('AdminUsers', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUseAuth.mockReturnValue({
            user: { username: 'alice', isSuperuser: true },
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Initial rendering', () => {
        it('shows a loading spinner while fetching users', () => {
            mockApiGet.mockReturnValue(new Promise(() => {}));
            renderWithTheme(<AdminUsers />);
            expect(
                screen.getByLabelText('Loading users')
            ).toBeInTheDocument();
        });

        it('renders the user list once loaded', async () => {
            installListMocks();
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            expect(screen.getByText('bob')).toBeInTheDocument();
            expect(screen.getByText('service-bot')).toBeInTheDocument();
            expect(screen.getByText('Service Account')).toBeInTheDocument();
        });

        it('renders the empty state when no users are returned', async () => {
            installListMocks({ users: [] });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(
                    screen.getByText('No users found.')
                ).toBeInTheDocument();
            });
        });

        it('shows an error alert when the list fetch fails', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/v1/rbac/users') {
                    return Promise.reject(new Error('list failed'));
                }
                return Promise.resolve({});
            });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('list failed')).toBeInTheDocument();
            });
        });
    });

    describe('Create user dialog', () => {
        it('opens with the password field present and the policy hint', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).getByText(/create user/i)
            ).toBeInTheDocument();
            expect(findFieldByLabel(dialog, 'Password')).toBeInTheDocument();
            expect(
                within(dialog).getByText(
                    /at least 12 characters\. avoid common passwords/i
                )
            ).toBeInTheDocument();
        });

        it('disables Create when the password is non-empty but below 12 chars', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'charlie'
            );
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'short'
            );
            const createBtn = within(dialog).getByRole('button', {
                name: /^create$/i,
            });
            expect(createBtn).toBeDisabled();
            expect(
                within(dialog).getByText(
                    /password is 5 of 12 minimum characters/i
                )
            ).toBeInTheDocument();
        });

        it('enables Create once the password reaches 12 characters', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'charlie'
            );
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'Correct-Horse-99'
            );
            const createBtn = within(dialog).getByRole('button', {
                name: /^create$/i,
            });
            expect(createBtn).toBeEnabled();
        });

        it('hides the password field when Service Account is enabled', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            expect(findFieldByLabel(dialog, 'Password')).toBeInTheDocument();
            const serviceSwitch = within(dialog).getByLabelText(
                /service account/i
            );
            await user.click(serviceSwitch);
            expect(
                within(dialog).queryByLabelText(/password/i)
            ).not.toBeInTheDocument();
            // The Create button should now be enabled solely on username.
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'svc'
            );
            expect(
                within(dialog).getByRole('button', { name: /^create$/i })
            ).toBeEnabled();
        });

        it('submits the create call when the form is valid', async () => {
            installListMocks();
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'charlie'
            );
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'Correct-Horse-99'
            );
            await user.type(
                findFieldByLabel(dialog, 'Display Name'),
                'Charlie'
            );
            await user.type(
                findFieldByLabel(dialog, 'Email'),
                'c@example.com'
            );
            await user.type(
                findFieldByLabel(dialog, 'Notes'),
                'temp note'
            );
            await user.click(
                within(dialog).getByLabelText(/superuser/i)
            );
            await user.click(
                within(dialog).getByRole('button', { name: /^create$/i })
            );
            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/rbac/users',
                    expect.objectContaining({
                        username: 'charlie',
                        password: 'Correct-Horse-99',
                        display_name: 'Charlie',
                        email: 'c@example.com',
                        annotation: 'temp note',
                        is_superuser: true,
                    })
                );
            });
        });

        it('surfaces server-returned validation errors', async () => {
            installListMocks();
            const policyError = new Error(
                'password is too common; choose a less predictable password'
            );
            mockApiPost.mockRejectedValue(policyError);
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'charlie'
            );
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'CommonPassword123!'
            );
            await user.click(
                within(dialog).getByRole('button', { name: /^create$/i })
            );
            await waitFor(() => {
                expect(
                    within(dialog).getByText(
                        /password is too common/i
                    )
                ).toBeInTheDocument();
            });
        });

        it('disables Create when the password field is left empty', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'charlie'
            );
            const createBtn = within(dialog).getByRole('button', {
                name: /^create$/i,
            });
            expect(createBtn).toBeDisabled();
        });

        it('closes the dialog without saving when Cancel is clicked', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /cancel/i })
            );
            await waitFor(() => {
                expect(
                    screen.queryByRole('dialog')
                ).not.toBeInTheDocument();
            });
            expect(mockApiPost).not.toHaveBeenCalled();
        });

        it('disables Create when the Disabled toggle is off', async () => {
            installListMocks();
            mockApiPost.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('alice')).toBeInTheDocument();
            });
            await user.click(
                screen.getByRole('button', { name: /create user/i })
            );
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Username'),
                'newuser'
            );
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'Correct-Horse-99'
            );
            // Toggle the "Enabled" switch off.
            await user.click(
                within(dialog).getByLabelText(/^enabled$/i)
            );
            await user.click(
                within(dialog).getByRole('button', { name: /^create$/i })
            );
            await waitFor(() => {
                expect(mockApiPost).toHaveBeenCalledWith(
                    '/api/v1/rbac/users',
                    expect.objectContaining({
                        username: 'newuser',
                        enabled: false,
                    })
                );
            });
        });
    });

    describe('Edit user dialog', () => {
        it('hides the password feedback while the field is empty', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            const editButtons = screen.getAllByLabelText('edit user');
            // Open Bob's edit dialog (second user).
            await user.click(editButtons[1]);
            const dialog = await screen.findByRole('dialog');
            // The feedback hint must not display while the field is empty
            // (Edit semantics: leave blank to keep current).
            expect(
                within(dialog).queryByText(/at least 12 characters/i)
            ).not.toBeInTheDocument();
        });

        it('disables Save when an edit password is non-empty but below 12 chars', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'short'
            );
            const saveBtn = within(dialog).getByRole('button', {
                name: /^save$/i,
            });
            expect(saveBtn).toBeDisabled();
        });

        it('keeps Save enabled when no password is provided', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            const saveBtn = within(dialog).getByRole('button', {
                name: /^save$/i,
            });
            expect(saveBtn).toBeEnabled();
        });

        it('hides the password field for service accounts', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('service-bot')).toBeInTheDocument();
            });
            // Service account is the third row.
            await user.click(screen.getAllByLabelText('edit user')[2]);
            const dialog = await screen.findByRole('dialog');
            expect(
                within(dialog).queryByLabelText(/password/i)
            ).not.toBeInTheDocument();
        });

        it('submits the edit call with the new password when valid', async () => {
            installListMocks();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'Correct-Horse-99'
            );
            await user.click(
                within(dialog).getByRole('button', { name: /^save$/i })
            );
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2',
                    expect.objectContaining({
                        password: 'Correct-Horse-99',
                    })
                );
            });
        });

        it('shows server validation errors for the new policy', async () => {
            installListMocks();
            mockApiPut.mockRejectedValue(
                new Error('password must be at least 12 characters')
            );
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.type(
                findFieldByLabel(dialog, 'Password'),
                'Correct-Horse-99'
            );
            await user.click(
                within(dialog).getByRole('button', { name: /^save$/i })
            );
            await waitFor(() => {
                expect(
                    within(dialog).getByText(
                        /must be at least 12 characters/i
                    )
                ).toBeInTheDocument();
            });
        });

        it('submits non-password edits without the password field', async () => {
            installListMocks();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            const displayInput = findFieldByLabel(dialog, 'Display Name');
            // Clear and re-type the display name to a new value.
            fireEvent.change(displayInput, { target: { value: 'Bobby' } });
            await user.click(
                within(dialog).getByRole('button', { name: /^save$/i })
            );
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2',
                    expect.objectContaining({ display_name: 'Bobby' })
                );
            });
            const submittedBody = mockApiPut.mock.calls[0][1] as {
                password?: string;
            };
            expect(submittedBody.password).toBeUndefined();
        });

        it('submits enabled and superuser changes when toggled', async () => {
            installListMocks();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.click(
                within(dialog).getByLabelText(/^enabled$/i)
            );
            await user.click(
                within(dialog).getByLabelText(/^superuser$/i)
            );
            // Also tweak email and annotation to exercise both branches.
            const emailInput = findFieldByLabel(dialog, 'Email');
            fireEvent.change(emailInput, {
                target: { value: 'bob2@example.com' },
            });
            const notesInput = findFieldByLabel(dialog, 'Notes');
            fireEvent.change(notesInput, { target: { value: 'updated' } });
            await user.click(
                within(dialog).getByRole('button', { name: /^save$/i })
            );
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2',
                    expect.objectContaining({
                        enabled: false,
                        is_superuser: true,
                        email: 'bob2@example.com',
                        annotation: 'updated',
                    })
                );
            });
        });

        it('cancels the edit without calling the API', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('edit user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /cancel/i })
            );
            await waitFor(() => {
                expect(
                    screen.queryByRole('dialog')
                ).not.toBeInTheDocument();
            });
            expect(mockApiPut).not.toHaveBeenCalled();
        });
    });

    describe('Row interactions', () => {
        it('expands a row and loads permissions on click', async () => {
            installListMocks();
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getByText('bob'));
            await waitFor(() => {
                expect(
                    screen.getByTestId('effective-permissions')
                ).toBeInTheDocument();
            });
            // Clicking again should collapse.
            await user.click(screen.getByText('bob'));
            await waitFor(() => {
                expect(
                    screen.queryByTestId('effective-permissions')
                ).not.toBeInTheDocument();
            });
        });

        it('shows an error if loading permissions fails', async () => {
            installListMocks({
                privilegesRejection: new Error('priv fail'),
            });
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getByText('bob'));
            await waitFor(() => {
                expect(screen.getByText('priv fail')).toBeInTheDocument();
            });
        });

        it('toggles superuser via the inline switch', async () => {
            installListMocks();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            // The current user (alice) is the first row; her switch is
            // disabled. Toggle Bob's instead.
            const supSwitches = screen.getAllByLabelText('Toggle superuser');
            await user.click(supSwitches[1]);
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2',
                    { is_superuser: true }
                );
            });
        });

        it('toggles enabled via the inline switch', async () => {
            installListMocks();
            mockApiPut.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            const enabledSwitches = screen.getAllByLabelText(
                'Toggle enabled'
            );
            await user.click(enabledSwitches[1]);
            await waitFor(() => {
                expect(mockApiPut).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2',
                    { enabled: false }
                );
            });
        });

        it('handles an inline toggle failure by surfacing the error', async () => {
            installListMocks();
            mockApiPut.mockRejectedValue(new Error('toggle fail'));
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            const supSwitches = screen.getAllByLabelText('Toggle superuser');
            await user.click(supSwitches[1]);
            await waitFor(() => {
                expect(screen.getByText('toggle fail')).toBeInTheDocument();
            });
        });
    });

    describe('Delete user', () => {
        it('opens the confirmation dialog and calls delete on confirm', async () => {
            installListMocks();
            mockApiDelete.mockResolvedValue({});
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('delete user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /delete/i })
            );
            await waitFor(() => {
                expect(mockApiDelete).toHaveBeenCalledWith(
                    '/api/v1/rbac/users/2'
                );
            });
        });

        it('shows an error when delete fails', async () => {
            installListMocks();
            mockApiDelete.mockRejectedValue(new Error('delete fail'));
            const user = userEvent.setup({ delay: null });
            renderWithTheme(<AdminUsers />);
            await waitFor(() => {
                expect(screen.getByText('bob')).toBeInTheDocument();
            });
            await user.click(screen.getAllByLabelText('delete user')[1]);
            const dialog = await screen.findByRole('dialog');
            await user.click(
                within(dialog).getByRole('button', { name: /delete/i })
            );
            await waitFor(() => {
                expect(screen.getByText('delete fail')).toBeInTheDocument();
            });
        });
    });
});
