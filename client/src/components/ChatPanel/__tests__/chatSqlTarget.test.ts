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
import {
    collectReplyTargets,
    extractToolTarget,
    planSqlBlock,
    type KnownServer,
    type ReplyTargets,
} from '../chatSqlTarget';
import type { ToolActivity } from '../ToolStatus';

const servers = new Map<number, KnownServer>([
    [1, { name: 'alpha', databaseName: 'postgres' }],
    [2, { name: 'beta', databaseName: 'appdb' }],
    [3, { name: 'gamma' }],
]);

const reply = (overrides: Partial<ReplyTargets> = {}): ReplyTargets => ({
    targets: [],
    unattributed: false,
    usedDatastore: false,
    ...overrides,
});

const tool = (
    name: string,
    extra: Partial<ToolActivity> = {},
): ToolActivity => ({ name, status: 'completed', ...extra });

describe('extractToolTarget', () => {
    it('returns nothing for missing input', () => {
        expect(extractToolTarget(undefined)).toEqual({});
    });

    it('takes a numeric connection_id and non-empty database_name', () => {
        expect(extractToolTarget({ connection_id: 4, database_name: 'app' }))
            .toEqual({ connectionId: 4, databaseName: 'app' });
    });

    it('ignores the database when no valid connection_id is given', () => {
        expect(extractToolTarget({ database_name: 'app' })).toEqual({});
    });

    it.each([
        ['a string', '4'],
        ['zero', 0],
        ['a negative number', -1],
        ['a fraction', 1.5],
    ])('ignores a connection_id that is %s', (_label, value) => {
        expect(extractToolTarget({ connection_id: value })).toEqual({});
    });

    it('ignores an empty or non-string database_name', () => {
        expect(extractToolTarget({ connection_id: 4, database_name: '' }))
            .toEqual({ connectionId: 4 });
        expect(extractToolTarget({ connection_id: 4, database_name: 7 }))
            .toEqual({ connectionId: 4 });
    });
});

describe('collectReplyTargets', () => {
    it('returns an empty summary without activity', () => {
        expect(collectReplyTargets(undefined)).toEqual(reply());
    });

    it('collects distinct targets from monitored-database tools in order', () => {
        const result = collectReplyTargets([
            tool('query_database', { connectionId: 2, databaseName: 'app' }),
            tool('get_schema_info', { connectionId: 2, databaseName: 'app' }),
            tool('execute_explain', { connectionId: 1 }),
            tool('test_query', { connectionId: 2, databaseName: 'app', status: 'error' }),
        ]);
        expect(result).toEqual(reply({
            targets: [
                { connectionId: 2, databaseName: 'app' },
                { connectionId: 1, databaseName: undefined },
            ],
        }));
    });

    it('ignores tools that do not run SQL on a monitored database', () => {
        const result = collectReplyTargets([
            tool('list_connections'),
            tool('get_alert_history', { connectionId: 5 }),
            tool('search_knowledgebase'),
        ]);
        expect(result).toEqual(reply());
    });

    it('flags monitored-database calls without a valid connection', () => {
        const result = collectReplyTargets([
            tool('query_database'),
            tool('count_rows', { connectionId: -3 } as Partial<ToolActivity>),
        ]);
        expect(result.unattributed).toBe(true);
        expect(result.targets).toEqual([]);
    });

    it('flags datastore queries', () => {
        expect(collectReplyTargets([tool('query_datastore')]).usedDatastore)
            .toBe(true);
    });

    it('drops a stored database name that is not a string', () => {
        const stored = {
            name: 'query_database',
            status: 'completed',
            connectionId: 1,
            databaseName: 42,
        } as unknown as ToolActivity;
        expect(collectReplyTargets([stored]).targets).toEqual([
            { connectionId: 1, databaseName: undefined },
        ]);
    });
});

describe('planSqlBlock', () => {
    it.each([
        'SELECT * FROM t WHERE id = $1',
        'SELECT * FROM <table_name>',
        'SELECT * FROM {{ schema }}.t',
        'SELECT * FROM t WHERE id = :id',
    ])('treats %s as a template', (sql) => {
        const plan = planSqlBlock(
            sql,
            reply({ targets: [{ connectionId: 1 }] }),
            servers,
        );
        expect(plan).toEqual({ kind: 'template', code: sql });
    });

    it('runs on the single target the reply used', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 2, databaseName: 'sales' }] }),
            servers,
        );
        expect(plan).toEqual({
            kind: 'run',
            code: 'SELECT 1',
            target: { connectionId: 2, serverName: 'beta', databaseName: 'sales' },
        });
    });

    it('names the default database when the tool call did not', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 2 }] }),
            servers,
        );
        expect(plan).toMatchObject({
            kind: 'run',
            target: { connectionId: 2, databaseName: 'appdb' },
        });
    });

    it('treats an explicit default database as the same target', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({
                targets: [
                    { connectionId: 2 },
                    { connectionId: 2, databaseName: 'appdb' },
                ],
            }),
            servers,
        );
        expect(plan.kind).toBe('run');
    });

    it('asks when the reply ran on a server the user cannot see', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 9 }] }),
            servers,
        );
        expect(plan).toMatchObject({ kind: 'select' });
        expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
            .toEqual([1, 2, 3]);
    });

    it('asks when the reply also ran on a server the user cannot see', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 1 }, { connectionId: 9 }] }),
            servers,
        );
        expect(plan).toMatchObject({ kind: 'select' });
        expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
            .toEqual([1, 2, 3]);
    });

    it('offers no Run button for a reply target when no server is known', () => {
        expect(planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 1 }] }),
            new Map(),
        )).toEqual({ kind: 'copy' });
    });

    it('shows the selector when the reply used several targets', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({
                targets: [
                    { connectionId: 1 },
                    { connectionId: 2, databaseName: 'sales' },
                ],
            }),
            servers,
        );
        expect(plan).toEqual({
            kind: 'select',
            code: 'SELECT 1',
            options: [
                { connectionId: 1, serverName: 'alpha', databaseName: 'postgres' },
                { connectionId: 2, serverName: 'beta', databaseName: 'sales' },
                { connectionId: 3, serverName: 'gamma', databaseName: undefined },
            ],
        });
    });

    it('shows the selector when a call used the selected connection', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 1 }], unattributed: true }),
            servers,
        );
        expect(plan.kind).toBe('select');
    });

    it('shows the selector when the reply also queried the datastore', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 1 }], usedDatastore: true }),
            servers,
        );
        expect(plan.kind).toBe('select');
    });

    it('asks when the reply queried the datastore and a hidden server', () => {
        const plan = planSqlBlock(
            'SELECT 1',
            reply({ targets: [{ connectionId: 9 }], usedDatastore: true }),
            servers,
        );
        expect(plan).toMatchObject({ kind: 'select' });
        expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
            .toEqual([1, 2, 3]);
    });

    it('shows every known server when the reply ran no SQL tools', () => {
        const plan = planSqlBlock('SELECT 1', reply(), servers);
        expect(plan).toMatchObject({ kind: 'select' });
        expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
            .toEqual([1, 2, 3]);
    });

    it('offers no Run button for datastore-only replies', () => {
        const plan = planSqlBlock(
            'SELECT * FROM alerts',
            reply({ usedDatastore: true }),
            servers,
        );
        expect(plan).toEqual({ kind: 'copy' });
    });

    it('asks when a later comment names a server', () => {
        const plan = planSqlBlock(
            'SELECT 1;\n-- connection_id: 2',
            reply({ targets: [{ connectionId: 1 }] }),
            servers,
        );
        expect(plan.kind).toBe('select');
    });

    it('asks when a comment after code on the same line names a server', () => {
        const plan = planSqlBlock(
            '-- connection_id: 1\nSELECT 1; -- connection_id: 2',
            reply({ targets: [{ connectionId: 1 }] }),
            servers,
        );
        expect(plan.kind).toBe('select');
    });

    it('offers no Run button when no server is known', () => {
        expect(planSqlBlock('SELECT 1', reply(), new Map())).toEqual({ kind: 'copy' });
    });

    describe('with a connection_id comment', () => {
        it('runs on the named server, stripping the comment', () => {
            const plan = planSqlBlock(
                '-- connection_id: 3\nSELECT 1',
                reply({ targets: [{ connectionId: 3 }] }),
                servers,
            );
            expect(plan).toEqual({
                kind: 'run',
                code: 'SELECT 1',
                target: { connectionId: 3, serverName: 'gamma', databaseName: undefined },
            });
        });

        it('runs on the named server when the reply named no server', () => {
            const plan = planSqlBlock(
                '-- connection_id: 3\nSELECT 1',
                reply({ unattributed: true }),
                servers,
            );
            expect(plan).toMatchObject({
                kind: 'run',
                target: { connectionId: 3, serverName: 'gamma' },
            });
        });

        it('asks when the reply also ran on the selected connection', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\nSELECT 1',
                reply({ targets: [{ connectionId: 2 }], unattributed: true }),
                servers,
            );
            expect(plan).toMatchObject({ kind: 'select' });
            expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
                .toEqual([2, 1, 3]);
        });

        it('asks when the reply used only a server the user cannot see', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\nSELECT 1',
                reply({ targets: [{ connectionId: 9 }] }),
                servers,
            );
            expect(plan).toMatchObject({ kind: 'select' });
            expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
                .toEqual([1, 2, 3]);
        });

        it('asks when the comment names a server the reply did not use', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\nSELECT 1',
                reply({ targets: [{ connectionId: 1, databaseName: 'app' }] }),
                servers,
            );
            expect(plan).toEqual({
                kind: 'select',
                code: 'SELECT 1',
                options: [
                    { connectionId: 1, serverName: 'alpha', databaseName: 'app' },
                    { connectionId: 2, serverName: 'beta', databaseName: 'appdb' },
                    { connectionId: 3, serverName: 'gamma', databaseName: undefined },
                ],
            });
        });

        it('offers no Run button for a server the user cannot see', () => {
            expect(planSqlBlock(
                '-- connection_id: 99\nSELECT 1',
                reply({ targets: [{ connectionId: 99 }] }),
                servers,
            )).toEqual({ kind: 'copy' });
        });

        it('asks when a second comment names another server', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\n-- connection_id: 1\nSELECT 1;',
                reply({ targets: [{ connectionId: 2 }] }),
                servers,
            );
            expect(plan).toMatchObject({
                kind: 'select',
                code: '-- connection_id: 1\nSELECT 1;',
            });
            expect(plan.kind === 'select' && plan.options.map((o) => o.connectionId))
                .toEqual([2, 1, 3]);
        });

        it('offers no Run button for a block that is only the comment', () => {
            expect(planSqlBlock(
                '-- connection_id: 1\n',
                reply({ targets: [{ connectionId: 1 }] }),
                servers,
            )).toEqual({ kind: 'copy' });
        });

        it('uses the database the reply used on that server', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\nSELECT 1',
                reply({
                    targets: [
                        { connectionId: 1 },
                        { connectionId: 2, databaseName: 'sales' },
                    ],
                    usedDatastore: true,
                }),
                servers,
            );
            expect(plan).toMatchObject({
                kind: 'run',
                target: { connectionId: 2, databaseName: 'sales' },
            });
        });

        it('asks when the reply used several databases on that server', () => {
            const plan = planSqlBlock(
                '-- connection_id: 2\nSELECT 1',
                reply({
                    targets: [
                        { connectionId: 2, databaseName: 'sales' },
                        { connectionId: 2, databaseName: 'hr' },
                    ],
                }),
                servers,
            );
            expect(plan).toEqual({
                kind: 'select',
                code: 'SELECT 1',
                options: [
                    { connectionId: 2, serverName: 'beta', databaseName: 'sales' },
                    { connectionId: 2, serverName: 'beta', databaseName: 'hr' },
                ],
            });
        });

        it('still treats SQL with placeholders as a template', () => {
            expect(planSqlBlock('-- connection_id: 2\nSELECT $1', reply(), servers))
                .toEqual({ kind: 'template', code: 'SELECT $1' });
        });
    });
});
