/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import type { Action } from '../../../types/actions';
import type { User } from '../../../contexts/AuthContext';
import {
    REVIEWING,
    actionIdToLoad,
    buildCreateRequest,
    dangerLabelOf,
    deriveActionFlags,
    mayRetryAction,
    pluralise,
    reviewLabelOf,
    rowsAffectedOf,
    selectCurrentAction,
} from '../actionBlockState';

const makeAction = (over: Partial<Action> = {}): Action => ({
    id: 7,
    connection_id: 1,
    statements: ['DELETE FROM t WHERE a = 1'],
    source: 'chat_tool',
    review_status: 'passed',
    review: { reviewer: 'supervisor', danger: 'low', reasons: [] },
    approval: 'none',
    exec_status: 'not_started',
    ...over,
});

const user = (username: string, isSuperuser = false): User => ({
    authenticated: true,
    username,
    isSuperuser,
});

describe('selectCurrentAction', () => {
    const polled = makeAction({ id: 7 });
    const initial = makeAction({ id: 9 });

    it.each([
        ['polled when created from SQL', polled, undefined, undefined, 'x', polled],
        ['polled when nothing is wanted', polled, undefined, undefined, undefined, polled],
        ['polled when its id matches', polled, undefined, 7, undefined, polled],
        ['the initial action when polled is stale', polled, initial, undefined, undefined, initial],
        ['null when polled is stale and none given', polled, undefined, 8, undefined, null],
        ['null when nothing has loaded', null, undefined, 8, undefined, null],
    ] as const)('returns %s', (_name, p, i, id, sql, want) => {
        expect(selectCurrentAction(p, i, id, sql)).toBe(want);
    });
});

describe('buildCreateRequest', () => {
    it('includes only the options given', () => {
        expect(buildCreateRequest('SELECT 1', 'chat_block', {})).toEqual({
            sql: 'SELECT 1', source: 'chat_block', execute: true,
        });
    });

    it('includes every option given', () => {
        expect(buildCreateRequest('SELECT 1', 'alert_analysis', {
            databaseName: 'db', context: 'ctx', alertId: 0,
            conversationId: 'c1',
        })).toEqual({
            sql: 'SELECT 1',
            source: 'alert_analysis',
            execute: true,
            database_name: 'db',
            context: 'ctx',
            alert_id: 0,
            conversation_id: 'c1',
        });
    });
});

describe('actionIdToLoad', () => {
    it.each([
        ['the given id with no action', undefined, 4, 4],
        ['nothing with neither', undefined, undefined, undefined],
        ['a running action', makeAction({ exec_status: 'executing' }), 4, 7],
        ['nothing for a settled action', makeAction({ exec_status: 'succeeded' }), 4, undefined],
    ] as const)('loads %s', (_name, action, id, want) => {
        expect(actionIdToLoad(action, id)).toBe(want);
    });
});

describe('mayRetryAction', () => {
    it.each([
        ['a superuser', user('other', true), 'someone', true],
        ['the approver', user('alice'), 'alice', true],
        ['another user', user('bob'), 'alice', false],
        ['a user with no name', user(''), '', false],
        ['no user', null, 'alice', false],
    ] as const)('allows %s: %s', (_name, u, approvedBy, want) => {
        expect(mayRetryAction(makeAction({ approved_by: approvedBy }), u))
            .toBe(want);
    });
});

describe('deriveActionFlags', () => {
    it('offers Execute for a passed action awaiting approval', () => {
        const flags = deriveActionFlags(makeAction(), user('alice'), 'srv');
        expect(flags).toMatchObject({
            canExecute: true, canOverride: false, canRetry: false,
            canRetryOverride: false, showOverrideDialog: false,
            reviewing: false, vetoed: false, finished: false,
        });
    });

    it('offers an override for a vetoed action only with a server name', () => {
        const vetoed = makeAction({ review_status: 'vetoed' });
        expect(deriveActionFlags(vetoed, null, 'srv')).toMatchObject({
            canOverride: true, showOverrideDialog: true, vetoed: true,
        });
        expect(deriveActionFlags(vetoed, null, undefined)).toMatchObject({
            canOverride: false, showOverrideDialog: false,
        });
    });

    it('offers a retry to the approver', () => {
        const approved = makeAction({ approval: 'approved', approved_by: 'alice' });
        expect(deriveActionFlags(approved, user('alice'), 'srv')).toMatchObject({
            canRetry: true, canRetryOverride: false, canExecute: false,
        });
    });

    it('retries an override through the dialog', () => {
        const overridden = makeAction({
            review_status: 'vetoed', approval: 'overridden', approved_by: 'alice',
        });
        expect(deriveActionFlags(overridden, user('alice'), 'srv')).toMatchObject({
            canRetryOverride: true, canOverride: false, showOverrideDialog: true,
        });
    });

    it('marks a pending review and a finished run', () => {
        expect(deriveActionFlags(
            makeAction({ review_status: 'pending', exec_status: 'failed' }),
            null,
            undefined,
        )).toMatchObject({ reviewing: true, finished: true });
    });

    it.each([
        ['an action awaiting approval', {}, false],
        ['a finished run', { exec_status: 'succeeded' }, true],
        ['an unstarted veto', { review_status: 'vetoed' }, true],
        ['a running override', {
            review_status: 'vetoed', approval: 'overridden',
            exec_status: 'executing',
        }, false],
        ['a finished override', {
            review_status: 'vetoed', approval: 'overridden',
            exec_status: 'failed',
        }, true],
    ] as const)('lets %s be dismissed: %s', (_name, over, want) => {
        expect(deriveActionFlags(
            makeAction(over as Partial<Action>), null, 'srv',
        ).dismissible).toBe(want);
    });
});

describe('labels', () => {
    it.each([
        [{ review_status: 'pending' }, REVIEWING],
        [{ review_status: 'vetoed' }, 'Vetoed by Ellie\'s supervisor'],
        [{ review: { reviewer: 'none' } }, 'Not yet reviewed'],
        [{ review: null }, 'Approved by Ellie\'s supervisor'],
    ] as const)('labels review %o', (over, want) => {
        expect(reviewLabelOf(makeAction(over as Partial<Action>))).toBe(want);
    });

    it('formats the danger chip and plurals', () => {
        expect(dangerLabelOf('high')).toBe('Danger: high');
        expect(pluralise(1, 'row')).toBe('1 row');
        expect(pluralise(2, 'row')).toBe('2 rows');
    });

    it('counts rows only from statements that succeeded', () => {
        expect(rowsAffectedOf(makeAction())).toBe(0);
        expect(rowsAffectedOf(makeAction({
            results: [
                { statement: 'a', rows_affected: 2 },
                { statement: 'b' },
                { statement: 'c', rows_affected: 5, error: 'boom' },
            ],
        }))).toBe(2);
    });
});
