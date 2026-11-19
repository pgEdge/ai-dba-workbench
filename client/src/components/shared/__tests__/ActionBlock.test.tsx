/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ActionBlock from '../ActionBlock';
import AuthContext from '../../../contexts/AuthContext';
import type { AuthContextValue } from '../../../contexts/AuthContext';
import { renderWithTheme } from '../../../test/renderWithTheme';
import type { Action } from '../../../types/actions';
import type { UseActionResult } from '../../../hooks/useAction';

const hook: UseActionResult = {
    action: null,
    busy: false,
    error: null,
    create: vi.fn(),
    execute: vi.fn(),
    cancel: vi.fn(),
    load: vi.fn(),
};
vi.mock('../../../hooks/useAction', () => ({
    useAction: () => hook,
}));

const makeAction = (over: Partial<Action> = {}): Action => ({
    id: 7,
    connection_id: 1,
    statements: ['DELETE FROM t WHERE a = 1'],
    source: 'chat_tool',
    proposed_by: 'testuser',
    review_status: 'passed',
    review: { reviewer: 'supervisor', danger: 'low', reasons: [] },
    approval: 'none',
    exec_status: 'not_started',
    ...over,
});

const auth = (
    username = 'testuser',
    isSuperuser = false,
): AuthContextValue => ({
    user: { authenticated: true, username, isSuperuser },
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    forceLogout: vi.fn(),
    adminPermissions: [],
    hasPermission: () => false,
    hasAnyAdminAccess: false,
});

const baseProps = {
    connectionId: 1,
    serverName: 'prod-1',
    source: 'chat_block' as const,
};

const renderBlock = (
    props: Partial<React.ComponentProps<typeof ActionBlock>> = {},
    authValue: AuthContextValue = auth(),
) =>
    renderWithTheme(
        <AuthContext.Provider value={authValue}>
            <ActionBlock {...baseProps} {...props} />
        </AuthContext.Provider>,
    );

describe('ActionBlock', () => {
    beforeEach(() => {
        hook.action = null;
        hook.busy = false;
        hook.error = null;
        hook.create = vi.fn().mockResolvedValue(null);
        hook.execute = vi.fn().mockResolvedValue(true);
        hook.cancel = vi.fn().mockResolvedValue(true);
        hook.load = vi.fn().mockResolvedValue(null);
    });

    describe('creating from SQL', () => {
        it('creates the action once, with execute set and the block context', () => {
            hook.busy = true;
            renderBlock({
                sql: 'DELETE FROM t',
                databaseName: 'appdb',
                source: 'alert_analysis',
                context: 'Alert: disk full',
                alertId: 42,
                conversationId: 'conv-1',
            });

            expect(hook.create).toHaveBeenCalledTimes(1);
            expect(hook.create).toHaveBeenCalledWith(1, {
                sql: 'DELETE FROM t',
                database_name: 'appdb',
                source: 'alert_analysis',
                context: 'Alert: disk full',
                alert_id: 42,
                conversation_id: 'conv-1',
                execute: true,
            });
            expect(screen.getByText('Supervisor reviewing…')).toBeInTheDocument();
        });

        it('omits optional fields that are not set', () => {
            hook.busy = true;
            renderBlock({ sql: 'DELETE FROM t' });

            expect(hook.create).toHaveBeenCalledWith(1, {
                sql: 'DELETE FROM t',
                source: 'chat_block',
                execute: true,
            });
        });

        it('shows the execute error from a 201 response', () => {
            hook.action = makeAction({
                approval: 'none',
                exec_status: 'not_started',
            });
            hook.error = 'could not start';
            renderBlock({ sql: 'DELETE FROM t' });

            expect(screen.getByRole('alert')).toHaveTextContent('could not start');
        });
    });

    describe('review states', () => {
        it('shows the supervisor reviewing while the review is pending', () => {
            renderBlock({ action: makeAction({ review_status: 'pending' }) });
            expect(screen.getByText('Supervisor reviewing…')).toBeInTheDocument();
        });

        it('shows Approved by Ellie\'s supervisor for a real reviewer', () => {
            renderBlock({ action: makeAction() });
            expect(
                screen.getByText('Approved by Ellie\'s supervisor'),
            ).toBeInTheDocument();
        });

        it('shows Not yet reviewed when no reviewer ran', () => {
            renderBlock({
                action: makeAction({ review: { reviewer: 'none' } }),
            });
            expect(screen.getByText('Not yet reviewed')).toBeInTheDocument();
            expect(
                screen.queryByText('Approved by Ellie\'s supervisor'),
            ).not.toBeInTheDocument();
        });

        it('shows the veto with danger chip and reasons', () => {
            renderBlock({
                action: makeAction({
                    review_status: 'vetoed',
                    review: {
                        reviewer: 'supervisor',
                        danger: 'critical',
                        reasons: ['Drops customer data', 'No WHERE clause'],
                    },
                }),
            });

            expect(
                screen.getByText('Vetoed by Ellie\'s supervisor'),
            ).toBeInTheDocument();
            expect(screen.getByText('Danger: critical')).toBeInTheDocument();
            expect(screen.getByText('Drops customer data')).toBeInTheDocument();
            expect(screen.getByText('No WHERE clause')).toBeInTheDocument();
        });

        it('lists the statements when the action is shown from chat', () => {
            renderBlock({ action: makeAction() });
            expect(
                screen.getByText('DELETE FROM t WHERE a = 1'),
            ).toBeInTheDocument();
        });
    });

    describe('execution states', () => {
        it('executes a passed action when asked', async () => {
            const user = userEvent.setup();
            renderBlock({ action: makeAction() });

            await user.click(screen.getByRole('button', { name: 'Execute' }));
            expect(hook.execute).toHaveBeenCalledWith(7);
        });

        it('shows Running with a Cancel button that cancels', async () => {
            const user = userEvent.setup();
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'executing',
            });
            renderBlock({ action: makeAction() });

            expect(screen.getByText('Running…')).toBeInTheDocument();
            await user.click(screen.getByRole('button', { name: 'Cancel' }));
            expect(hook.cancel).toHaveBeenCalledWith(7);
        });

        it('shows success with rows affected and result rows', () => {
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'succeeded',
                results: [
                    {
                        statement: 'DELETE FROM t',
                        rows_affected: 3,
                        applied: true,
                    },
                    {
                        statement: 'SELECT a FROM t',
                        rows_affected: 1,
                        columns: ['a'],
                        rows: [['42']],
                    },
                ],
            });
            renderBlock({ action: makeAction() });

            expect(screen.getByText('Succeeded')).toBeInTheDocument();
            expect(screen.getByText('4 rows affected')).toBeInTheDocument();
            expect(screen.getByText('42')).toBeInTheDocument();
        });

        it('uses the singular for one row affected', () => {
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'succeeded',
                results: [{ statement: 'x', rows_affected: 1 }],
            });
            renderBlock({ action: makeAction() });
            expect(screen.getByText('1 row affected')).toBeInTheDocument();
        });

        it('shows the failure with the statement error', () => {
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'failed',
                results: [
                    { statement: 'DELETE FROM t', error: 'relation "t" does not exist' },
                ],
            });
            renderBlock({ action: makeAction() });

            expect(screen.getByText('Failed')).toBeInTheDocument();
            expect(
                screen.getByText('relation "t" does not exist'),
            ).toBeInTheDocument();
        });

        it('shows Outcome unknown for an unknown state', () => {
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'unknown',
            });
            renderBlock({ action: makeAction() });
            expect(
                screen.getByText('Outcome unknown: check the database'),
            ).toBeInTheDocument();
        });

        it('shows Cancelled for a cancelled state', () => {
            hook.action = makeAction({
                approval: 'approved',
                exec_status: 'cancelled',
            });
            renderBlock({ action: makeAction() });
            expect(screen.getByText('Cancelled')).toBeInTheDocument();
        });

        it('offers Retry to the approver of an action that has not started', async () => {
            const user = userEvent.setup();
            hook.action = makeAction({
                approval: 'approved',
                approved_by: 'testuser',
                exec_status: 'not_started',
            });
            renderBlock({ action: makeAction() });

            await user.click(screen.getByRole('button', { name: 'Retry' }));
            expect(hook.execute).toHaveBeenCalledWith(7);
        });

        it('offers Retry for an approved action to a superuser', () => {
            hook.action = makeAction({
                approval: 'approved',
                approved_by: 'someone-else',
                exec_status: 'not_started',
            });
            renderBlock({ action: makeAction() }, auth('root', true));
            expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
        });

        it('hides Retry from a user who did not approve the action', () => {
            hook.action = makeAction({
                approval: 'approved',
                approved_by: 'someone-else',
                exec_status: 'not_started',
            });
            renderBlock({ action: makeAction() });
            expect(
                screen.queryByRole('button', { name: 'Retry' }),
            ).not.toBeInTheDocument();
        });

        it('retries an override through the typed-name dialog', async () => {
            const user = userEvent.setup();
            hook.action = makeAction({
                review_status: 'vetoed',
                review: { reviewer: 'supervisor', danger: 'high', reasons: ['Risky'] },
                approval: 'overridden',
                approved_by: 'testuser',
                exec_status: 'not_started',
            });
            renderBlock({ action: makeAction() });

            expect(
                screen.queryByRole('button', { name: 'Override and run' }),
            ).not.toBeInTheDocument();
            await user.click(screen.getByRole('button', { name: 'Retry' }));
            expect(hook.execute).not.toHaveBeenCalled();
            await user.type(
                screen.getByRole('textbox', { name: 'Connection name' }),
                'prod-1',
            );
            await user.click(screen.getByRole('button', { name: 'Run anyway' }));
            expect(hook.execute).toHaveBeenCalledWith(7, { confirm: 'prod-1' });
        });

        it('hides the override retry from another user and without a name', () => {
            hook.action = makeAction({
                review_status: 'vetoed',
                approval: 'overridden',
                approved_by: 'someone-else',
                exec_status: 'not_started',
            });
            const { unmount } = renderWithTheme(
                <AuthContext.Provider value={auth()}>
                    <ActionBlock {...baseProps} action={makeAction()} />
                </AuthContext.Provider>,
            );
            expect(
                screen.queryByRole('button', { name: 'Retry' }),
            ).not.toBeInTheDocument();
            unmount();

            hook.action = makeAction({
                review_status: 'vetoed',
                approval: 'overridden',
                approved_by: 'testuser',
                exec_status: 'not_started',
            });
            renderBlock({ action: makeAction(), serverName: undefined });
            expect(
                screen.queryByRole('button', { name: 'Retry' }),
            ).not.toBeInTheDocument();
        });

        it('shows a request error', () => {
            hook.error = 'The server is shutting down';
            renderBlock({ action: makeAction() });
            expect(screen.getByRole('alert')).toHaveTextContent(
                'The server is shutting down',
            );
        });
    });

    describe('sql without a source', () => {
        it('shows an error instead of waiting forever', () => {
            renderBlock({ sql: 'DELETE FROM t', source: undefined });
            expect(screen.getByRole('alert')).toHaveTextContent(
                'origin is unknown',
            );
            expect(hook.create).not.toHaveBeenCalled();
        });
    });

    describe('loading by id', () => {
        it('loads again when the action id changes, but not on a re-render', () => {
            const { rerender } = renderWithTheme(
                <AuthContext.Provider value={auth()}>
                    <ActionBlock {...baseProps} actionId={7} />
                </AuthContext.Provider>,
            );
            rerender(
                <AuthContext.Provider value={auth()}>
                    <ActionBlock {...baseProps} actionId={7} />
                </AuthContext.Provider>,
            );
            expect(hook.load).toHaveBeenCalledTimes(1);
            rerender(
                <AuthContext.Provider value={auth()}>
                    <ActionBlock {...baseProps} actionId={8} />
                </AuthContext.Provider>,
            );
            expect(hook.load).toHaveBeenCalledTimes(2);
            expect(hook.load).toHaveBeenLastCalledWith(8);
        });

        it('ignores a stale action for a different id', () => {
            hook.action = makeAction({ id: 7, exec_status: 'succeeded' });
            renderBlock({ actionId: 8 });
            expect(screen.queryByText('Succeeded')).not.toBeInTheDocument();
        });

        it('loads the action when only an id is given', () => {
            renderBlock({ actionId: 7 });
            expect(hook.load).toHaveBeenCalledWith(7);
        });

        it('follows a supplied action that is already running', () => {
            renderBlock({
                action: makeAction({
                    approval: 'approved',
                    exec_status: 'executing',
                }),
            });
            expect(hook.load).toHaveBeenCalledWith(7);
        });

        it('does not reload a supplied action that awaits approval', () => {
            renderBlock({ action: makeAction() });
            expect(hook.load).not.toHaveBeenCalled();
        });
    });

    describe('override', () => {
        const vetoed = makeAction({
            review_status: 'vetoed',
            review: {
                reviewer: 'supervisor',
                danger: 'high',
                reasons: ['Risky'],
            },
        });

        it('requires the connection name before it runs the action', async () => {
            const user = userEvent.setup();
            renderBlock({ action: vetoed });

            await user.click(
                screen.getByRole('button', { name: 'Override and run' }),
            );
            const confirm = screen.getByRole('button', { name: 'Run anyway' });
            expect(confirm).toBeDisabled();

            await user.type(
                screen.getByRole('textbox', { name: 'Connection name' }),
                'prod-',
            );
            expect(confirm).toBeDisabled();

            await user.type(
                screen.getByRole('textbox', { name: 'Connection name' }),
                '1',
            );
            expect(confirm).toBeEnabled();
            await user.click(confirm);

            expect(hook.execute).toHaveBeenCalledWith(7, { confirm: 'prod-1' });
        });

        it('shows danger and reasons in the dialog and clears typed text on reopen', async () => {
            const user = userEvent.setup();
            renderBlock({ action: vetoed });

            await user.click(
                screen.getByRole('button', { name: 'Override and run' }),
            );
            const dialog = screen.getByRole('dialog');
            expect(
                dialog.querySelector('[data-testid="override-danger-chip"]'),
            ).toHaveTextContent('Danger: high');
            expect(
                dialog.querySelector('[data-testid="override-reasons"]'),
            ).toHaveTextContent('Risky');

            await user.type(
                screen.getByRole('textbox', { name: 'Connection name' }),
                'prod-1',
            );
            await user.click(screen.getByRole('button', { name: 'Cancel' }));
            await user.click(
                screen.getByRole('button', { name: 'Override and run' }),
            );
            expect(
                screen.getByRole('textbox', { name: 'Connection name' }),
            ).toHaveValue('');
        });

        it('is hidden when the connection name is unknown', () => {
            renderBlock({ action: vetoed, serverName: undefined });
            expect(
                screen.queryByRole('button', { name: 'Override and run' }),
            ).not.toBeInTheDocument();
        });

        it('is offered to a user who did not propose the action', () => {
            renderBlock({ action: vetoed }, auth('someone-else'));
            expect(
                screen.getByRole('button', { name: 'Override and run' }),
            ).toBeInTheDocument();
        });

        it('shows the server refusal when the user lacks write access', () => {
            hook.error = 'write access required';
            renderBlock({ action: vetoed }, auth('reader'));
            expect(screen.getByRole('alert')).toHaveTextContent(
                'write access required',
            );
            expect(
                screen.getByRole('button', { name: 'Override and run' }),
            ).toBeInTheDocument();
        });

        it('is not offered once the action has run', () => {
            hook.action = { ...vetoed, approval: 'overridden', exec_status: 'succeeded' };
            renderBlock({ action: vetoed });
            expect(
                screen.queryByRole('button', { name: 'Override and run' }),
            ).not.toBeInTheDocument();
        });
    });

    it('calls onDismiss from the close button once finished', async () => {
        const user = userEvent.setup();
        const onDismiss = vi.fn();
        hook.action = makeAction({
            approval: 'approved',
            exec_status: 'succeeded',
        });
        renderBlock({ action: makeAction(), onDismiss });

        await user.click(screen.getByRole('button', { name: 'Dismiss' }));
        expect(onDismiss).toHaveBeenCalled();
    });

    it('offers Dismiss on the error when the action could not be created', async () => {
        const user = userEvent.setup();
        const onDismiss = vi.fn();
        hook.error = 'could not propose the action';
        renderBlock({ sql: 'DELETE FROM t', onDismiss });

        expect(screen.getByRole('alert')).toHaveTextContent(
            'could not propose the action',
        );
        await user.click(screen.getByRole('button', { name: 'Dismiss' }));
        expect(onDismiss).toHaveBeenCalledTimes(1);
    });

    it('keeps Dismiss off a request error while the action is showing', () => {
        hook.error = 'The server is shutting down';
        renderBlock({ action: makeAction(), onDismiss: vi.fn() });
        expect(
            screen.queryByRole('button', { name: 'Dismiss' }),
        ).not.toBeInTheDocument();
    });
});
