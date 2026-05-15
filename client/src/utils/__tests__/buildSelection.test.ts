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
import { buildSelection } from '../buildSelection';
import type {
    ClusterEntry,
    ClusterGroup,
    ClusterServer,
} from '../../contexts/ClusterDataContext';
import type { Selection } from '../../types/selection';

/**
 * Asserts the selection is non-null and narrows the type. Used in
 * place of a non-null assertion to satisfy
 * `@typescript-eslint/no-non-null-assertion`.
 */
function assertSelection(sel: Selection | null): asserts sel is Selection {
    expect(sel).not.toBeNull();
    if (sel === null) {
        throw new Error('expected selection to be non-null');
    }
}

const makeServer = (id: number, overrides: Partial<ClusterServer> = {}): ClusterServer => ({
    id,
    name: `pg-${id}`,
    status: 'online',
    host: `10.0.0.${id}`,
    port: 5432,
    role: 'primary',
    version: '17.0',
    database_name: 'postgres',
    username: 'postgres',
    os: 'linux',
    platform: 'amd64',
    ...overrides,
});

const makeCluster = (id: string, servers: ClusterServer[]): ClusterEntry => ({
    id,
    name: `cluster-${id}`,
    description: `cluster ${id} description`,
    servers,
});

const makeGroup = (
    id: string,
    clusters: ClusterEntry[] | null,
): ClusterGroup => ({
    id,
    name: `group-${id}`,
    clusters,
});

describe('buildSelection', () => {
    describe('null selection', () => {
        it('returns null when selectionType is null', () => {
            expect(buildSelection(null, null, null, [])).toBeNull();
        });

        it('returns null when selectionType is "server" but selectedServer is null', () => {
            expect(buildSelection('server', null, null, [])).toBeNull();
        });

        it('returns null when selectionType is "cluster" but selectedCluster is null', () => {
            expect(buildSelection('cluster', null, null, [])).toBeNull();
        });
    });

    describe('estate selection', () => {
        it('returns an EstateSelection that wraps the cluster data', () => {
            const groups = [makeGroup('g1', [makeCluster('c1', [makeServer(1)])])];

            const sel = buildSelection('estate', null, null, groups);

            assertSelection(sel);
            expect(sel.type).toBe('estate');
            if (sel.type === 'estate') {
                expect(sel.name).toBe('All Servers');
                expect(sel.status).toBe('online');
                expect(sel.groups).toBe(groups);
            }
        });

        it('returns an EstateSelection even when groups contain null clusters', () => {
            // Estate selection just passes groups through; it must not
            // throw on null-clusters groups either.
            const groups = [makeGroup('empty', null)];

            const sel = buildSelection('estate', null, null, groups);

            assertSelection(sel);
            expect(sel.type).toBe('estate');
        });
    });

    describe('cluster selection - happy path', () => {
        it('resolves groupId and groupName when the cluster is present in clusterData', () => {
            const server = makeServer(10);
            const cluster = makeCluster('c1', [server]);
            const group = makeGroup('g1', [cluster]);

            const sel = buildSelection('cluster', null, cluster, [group]);

            assertSelection(sel);
            expect(sel.type).toBe('cluster');
            if (sel.type === 'cluster') {
                expect(sel.id).toBe('c1');
                expect(sel.name).toBe('cluster-c1');
                expect(sel.description).toBe('cluster c1 description');
                expect(sel.groupId).toBe('g1');
                expect(sel.groupName).toBe('group-g1');
                expect(sel.serverIds).toEqual([10]);
                expect(sel.servers).toHaveLength(1);
            }
        });

        it('computes "online" status when all servers are online', () => {
            const cluster = makeCluster('c1', [makeServer(1), makeServer(2)]);
            const sel = buildSelection('cluster', null, cluster, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            expect(sel.status).toBe('online');
        });

        it('computes "warning" status when any server is offline or warning', () => {
            const cluster = makeCluster('c1', [
                makeServer(1),
                makeServer(2, { status: 'warning' }),
            ]);
            const sel = buildSelection('cluster', null, cluster, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            expect(sel.status).toBe('warning');
        });

        it('computes "offline" status when every server is offline', () => {
            const cluster = makeCluster('c1', [
                makeServer(1, { status: 'offline' }),
                makeServer(2, { status: 'offline' }),
            ]);
            const sel = buildSelection('cluster', null, cluster, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            expect(sel.status).toBe('offline');
        });

        it('handles clusters with no servers (servers field absent)', () => {
            // Exercises the `selectedCluster.servers ?: []` branch:
            // when the selected cluster carries no servers field at
            // all, the helper must return an empty servers/serverIds
            // pair rather than crashing.
            const cluster: ClusterEntry = {
                id: 'c-empty',
                name: 'empty',
                // servers omitted on purpose to drive the falsy branch.
                servers: undefined as unknown as ClusterServer[],
            };
            const sel = buildSelection('cluster', null, cluster, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            if (sel.type === 'cluster') {
                expect(sel.servers).toEqual([]);
                expect(sel.serverIds).toEqual([]);
                // No servers means the "every offline" branch is false
                // and the "some offline/warning" branch is false; result
                // is "online".
                expect(sel.status).toBe('online');
            }
        });
    });

    describe('cluster selection - null clusters guard (issue #242 regression)', () => {
        it('does not throw when clusterData contains a group whose clusters field is null', () => {
            const cluster = makeCluster('c-orphan', [makeServer(99)]);
            // The user has the cluster selected, but clusterData was
            // refreshed and now contains a group with `clusters: null`.
            // This is the exact shape that triggered issue #242.
            const groups: ClusterGroup[] = [
                makeGroup('g-empty', null),
                makeGroup('g-other', [makeCluster('c-other', [makeServer(1)])]),
            ];

            // The selection should be built successfully (no TypeError).
            expect(() =>
                buildSelection('cluster', null, cluster, groups),
            ).not.toThrow();
        });

        it('returns undefined groupId/groupName when the cluster is not located', () => {
            const cluster = makeCluster('c-orphan', [makeServer(99)]);
            const groups: ClusterGroup[] = [makeGroup('g-empty', null)];

            const sel = buildSelection('cluster', null, cluster, groups);

            assertSelection(sel);
            if (sel.type === 'cluster') {
                expect(sel.id).toBe('c-orphan');
                expect(sel.groupId).toBeUndefined();
                expect(sel.groupName).toBeUndefined();
                // The cluster's own fields are still surfaced from the
                // selectedCluster argument; only the parent group lookup
                // returns undefined.
                expect(sel.serverIds).toEqual([99]);
            }
        });

        it('finds the cluster in a non-null group even when sibling groups are null', () => {
            const cluster = makeCluster('c-real', [makeServer(7)]);
            const groups: ClusterGroup[] = [
                makeGroup('g-empty', null),
                makeGroup('g-real', [cluster]),
            ];

            const sel = buildSelection('cluster', null, cluster, groups);

            assertSelection(sel);
            if (sel.type === 'cluster') {
                expect(sel.groupId).toBe('g-real');
                expect(sel.groupName).toBe('group-g-real');
            }
        });
    });

    describe('server selection - happy path', () => {
        it('resolves clusterId, clusterName, groupId, and groupName for the owning cluster', () => {
            const server = makeServer(42, { spock_node_name: 'node-a', spock_version: '5.0' });
            const cluster: ClusterEntry = {
                ...makeCluster('c-owner', [server]),
                isStandalone: false,
            };
            const group = makeGroup('g-owner', [cluster]);

            const sel = buildSelection('server', server, null, [group]);

            assertSelection(sel);
            expect(sel.type).toBe('server');
            if (sel.type === 'server') {
                expect(sel.id).toBe(42);
                expect(sel.name).toBe('pg-42');
                expect(sel.clusterId).toBe('c-owner');
                expect(sel.clusterName).toBe('cluster-c-owner');
                expect(sel.isStandalone).toBe(false);
                expect(sel.groupId).toBe('g-owner');
                expect(sel.groupName).toBe('group-g-owner');
                expect(sel.spockNodeName).toBe('node-a');
                expect(sel.spockVersion).toBe('5.0');
                expect(sel.host).toBe('10.0.0.42');
                expect(sel.port).toBe(5432);
            }
        });

        it('falls back to "unknown" status and empty strings for missing optional server fields', () => {
            // A bare-minimum server with no status, role, version, etc.
            const server: ClusterServer = {
                id: 1,
                name: 'minimal',
            };
            const cluster = makeCluster('c1', [server]);
            const group = makeGroup('g1', [cluster]);

            const sel = buildSelection('server', server, null, [group]);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.status).toBe('unknown');
                expect(sel.description).toBe('');
                expect(sel.host).toBe('');
                expect(sel.port).toBe(0);
                expect(sel.role).toBe('');
                expect(sel.version).toBe('');
                expect(sel.database).toBe('');
                expect(sel.username).toBe('');
                expect(sel.os).toBe('');
                expect(sel.platform).toBe('');
            }
        });

        it('falls back to an empty server list when a candidate cluster has no servers field', () => {
            // Exercises the `cluster.servers || []` branch in the
            // server-lookup loop: a sibling cluster with an absent
            // servers field must not cause the lookup to throw.
            const server = makeServer(7);
            const sparseCluster: ClusterEntry = {
                id: 'c-sparse',
                name: 'sparse',
                // servers omitted on purpose; the runtime API has been
                // observed to omit this field for empty clusters.
                servers: undefined as unknown as ClusterServer[],
            };
            const realCluster = makeCluster('c-real', [server]);
            const groups: ClusterGroup[] = [
                makeGroup('g-sparse', [sparseCluster]),
                makeGroup('g-real', [realCluster]),
            ];

            const sel = buildSelection('server', server, null, groups);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.clusterId).toBe('c-real');
                expect(sel.groupId).toBe('g-real');
            }
        });

        it('finds servers inside nested children', () => {
            const child = makeServer(99, { name: 'child' });
            const parent: ClusterServer = {
                ...makeServer(98, { name: 'parent' }),
                children: [child],
            };
            const cluster = makeCluster('c1', [parent]);
            const sel = buildSelection('server', child, null, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.clusterId).toBe('c1');
                expect(sel.groupId).toBe('g1');
            }
        });

        it('uses database_name in preference to database when both are present', () => {
            const server = makeServer(1, { database_name: 'app_db', database: 'fallback' });
            const cluster = makeCluster('c1', [server]);
            const sel = buildSelection('server', server, null, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.database).toBe('app_db');
            }
        });

        it('falls back to database when database_name is absent', () => {
            const server = makeServer(1, { database_name: undefined, database: 'legacy' });
            const cluster = makeCluster('c1', [server]);
            const sel = buildSelection('server', server, null, [makeGroup('g1', [cluster])]);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.database).toBe('legacy');
            }
        });
    });

    describe('server selection - null clusters guard (issue #242 regression)', () => {
        it('does not throw when clusterData contains a group whose clusters field is null', () => {
            const server = makeServer(7);
            const groups: ClusterGroup[] = [
                makeGroup('g-empty', null),
                makeGroup('g-other', [makeCluster('c-other', [makeServer(99)])]),
            ];

            expect(() =>
                buildSelection('server', server, null, groups),
            ).not.toThrow();
        });

        it('returns undefined cluster/group fields when no group contains the server', () => {
            const server = makeServer(7);
            const groups: ClusterGroup[] = [makeGroup('g-empty', null)];

            const sel = buildSelection('server', server, null, groups);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.id).toBe(7);
                expect(sel.clusterId).toBeUndefined();
                expect(sel.clusterName).toBeUndefined();
                expect(sel.isStandalone).toBeUndefined();
                expect(sel.groupId).toBeUndefined();
                expect(sel.groupName).toBeUndefined();
            }
        });

        it('locates the server in a non-null group even when sibling groups are null', () => {
            const server = makeServer(7);
            const cluster = makeCluster('c-real', [server]);
            const groups: ClusterGroup[] = [
                makeGroup('g-empty', null),
                makeGroup('g-real', [cluster]),
            ];

            const sel = buildSelection('server', server, null, groups);

            assertSelection(sel);
            if (sel.type === 'server') {
                expect(sel.clusterId).toBe('c-real');
                expect(sel.groupId).toBe('g-real');
            }
        });
    });
});
