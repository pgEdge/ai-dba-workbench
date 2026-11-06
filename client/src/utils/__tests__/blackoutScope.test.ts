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
    isScopeAvailable,
    resolveEntityId,
    scopeLabel,
} from '../blackoutScope';
import type {
    ClusterSelection,
    EstateSelection,
    ServerSelection,
} from '../../types/selection';

const makeServerSelection = (
    overrides: Partial<ServerSelection> = {},
): ServerSelection => ({
    type: 'server',
    id: 7,
    name: 'pg-7',
    status: 'online',
    description: '',
    host: '192.0.2.7',
    port: 5432,
    role: 'primary',
    version: '17.0',
    database: 'postgres',
    username: 'postgres',
    os: 'linux',
    platform: 'amd64',
    ...overrides,
});

const makeClusterSelection = (
    overrides: Partial<ClusterSelection> = {},
): ClusterSelection => ({
    type: 'cluster',
    id: 'server-7',
    name: 'Auto cluster',
    status: 'online',
    description: '',
    servers: [],
    serverIds: [],
    ...overrides,
});

const estate: EstateSelection = {
    type: 'estate',
    name: 'All Servers',
    status: 'online',
    groups: [],
};

describe('isScopeAvailable', () => {
    it('offers only estate scope when nothing is selected', () => {
        expect(isScopeAvailable('estate', null)).toBe(true);
        expect(isScopeAvailable('server', undefined)).toBe(false);
        expect(isScopeAvailable('cluster', null)).toBe(false);
    });

    it('always offers estate scope for a selection', () => {
        expect(isScopeAvailable('estate', estate)).toBe(true);
        expect(isScopeAvailable('estate', makeServerSelection())).toBe(true);
    });

    it('offers group scope only for a database-backed group', () => {
        expect(isScopeAvailable('group', makeServerSelection({ groupId: 'group-3' }))).toBe(true);
        expect(isScopeAvailable('group', makeClusterSelection({ groupId: 'group-3' }))).toBe(true);
        expect(isScopeAvailable('group', makeServerSelection({ groupId: 'group-auto' }))).toBe(false);
        expect(isScopeAvailable('group', makeClusterSelection())).toBe(false);
        expect(isScopeAvailable('group', estate)).toBe(false);
    });

    it('offers cluster scope for a server in an auto-detected cluster (issue #809)', () => {
        const sel = makeServerSelection({
            clusterId: 'server-7',
            dbClusterId: 12,
            isStandalone: false,
        });
        expect(isScopeAvailable('cluster', sel)).toBe(true);
    });

    it('offers cluster scope for an auto-detected cluster (issue #809)', () => {
        expect(isScopeAvailable('cluster', makeClusterSelection({ dbClusterId: 12 }))).toBe(true);
    });

    it('withholds cluster scope without a database row', () => {
        expect(isScopeAvailable('cluster', makeServerSelection({ clusterId: 'server-7' }))).toBe(false);
        expect(isScopeAvailable('cluster', makeClusterSelection())).toBe(false);
    });

    it('withholds cluster scope for a standalone server', () => {
        const sel = makeServerSelection({ dbClusterId: 12, isStandalone: true });
        expect(isScopeAvailable('cluster', sel)).toBe(false);
    });

    it('withholds cluster scope for the estate', () => {
        expect(isScopeAvailable('cluster', estate)).toBe(false);
    });

    it('offers server scope only for a server selection', () => {
        expect(isScopeAvailable('server', makeServerSelection())).toBe(true);
        expect(isScopeAvailable('server', makeClusterSelection())).toBe(false);
    });

    it('rejects an unknown scope', () => {
        expect(isScopeAvailable('bogus', makeServerSelection())).toBe(false);
    });
});

describe('scopeLabel', () => {
    it('returns undefined when nothing is selected', () => {
        expect(scopeLabel('estate', null)).toBeUndefined();
    });

    it('labels estate scope', () => {
        expect(scopeLabel('estate', estate)).toBe('All Servers');
    });

    it('labels group scope with the group name', () => {
        expect(scopeLabel('group', makeServerSelection({ groupName: 'Prod' }))).toBe('Prod');
        expect(scopeLabel('group', makeClusterSelection({ groupName: 'Prod' }))).toBe('Prod');
        expect(scopeLabel('group', makeServerSelection())).toBeUndefined();
        expect(scopeLabel('group', estate)).toBeUndefined();
    });

    it('labels cluster scope with the cluster name', () => {
        expect(scopeLabel('cluster', makeServerSelection({ clusterName: 'Auto cluster' }))).toBe('Auto cluster');
        expect(scopeLabel('cluster', makeServerSelection())).toBeUndefined();
        expect(scopeLabel('cluster', makeClusterSelection())).toBe('Auto cluster');
        expect(scopeLabel('cluster', makeClusterSelection({ name: '' }))).toBeUndefined();
        expect(scopeLabel('cluster', estate)).toBeUndefined();
    });

    it('labels server scope with the server name', () => {
        expect(scopeLabel('server', makeServerSelection())).toBe('pg-7');
        expect(scopeLabel('server', makeServerSelection({ name: '' }))).toBeUndefined();
        expect(scopeLabel('server', makeClusterSelection())).toBeUndefined();
    });

    it('returns undefined for an unknown scope', () => {
        expect(scopeLabel('bogus', makeServerSelection())).toBeUndefined();
    });
});

describe('resolveEntityId', () => {
    it('returns undefined when nothing is selected', () => {
        expect(resolveEntityId('server', null)).toBeUndefined();
    });

    it('resolves the connection ID for server scope', () => {
        expect(resolveEntityId('server', makeServerSelection())).toBe(7);
        expect(resolveEntityId('server', makeClusterSelection())).toBeUndefined();
    });

    it('resolves the database cluster ID for cluster scope (issue #809)', () => {
        expect(resolveEntityId('cluster', makeServerSelection({ dbClusterId: 12 }))).toBe(12);
        expect(resolveEntityId('cluster', makeClusterSelection({ dbClusterId: 12 }))).toBe(12);
        expect(resolveEntityId('cluster', makeClusterSelection())).toBeUndefined();
        expect(resolveEntityId('cluster', estate)).toBeUndefined();
    });

    it('resolves the group ID for group scope', () => {
        expect(resolveEntityId('group', makeServerSelection({ groupId: 'group-3' }))).toBe(3);
        expect(resolveEntityId('group', makeClusterSelection({ groupId: 'group-3' }))).toBe(3);
        expect(resolveEntityId('group', makeServerSelection())).toBeUndefined();
        expect(resolveEntityId('group', makeServerSelection({ groupId: 'auto' }))).toBeUndefined();
        expect(resolveEntityId('group', estate)).toBeUndefined();
    });

    it('returns undefined for estate and unknown scopes', () => {
        expect(resolveEntityId('estate', estate)).toBeUndefined();
        expect(resolveEntityId('bogus', makeServerSelection())).toBeUndefined();
    });
});
