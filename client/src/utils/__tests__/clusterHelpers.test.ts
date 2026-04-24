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
    collectServers,
    ServerLike,
    extractEstateServerIds,
    extractClusterServerIds,
    computeEstateServerCounts,
    countEstateServers,
} from '../clusterHelpers';
import type { EstateSelection, ClusterSelection } from '../../types/selection';

describe('collectServers', () => {
    describe('flat server lists', () => {
        it('returns empty array for empty input', () => {
            expect(collectServers([])).toEqual([]);
        });

        it('returns single server unchanged', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'server1', status: 'healthy' },
            ];
            const result = collectServers(servers);
            expect(result).toEqual(servers);
            expect(result).toHaveLength(1);
        });

        it('returns multiple servers in order', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'server1' },
                { id: 2, name: 'server2' },
                { id: 3, name: 'server3' },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(3);
            expect(result.map(s => s.id)).toEqual([1, 2, 3]);
        });
    });

    describe('nested server hierarchies', () => {
        it('flattens servers with children', () => {
            const servers: ServerLike[] = [
                {
                    id: 1,
                    name: 'parent',
                    children: [
                        { id: 2, name: 'child1' },
                        { id: 3, name: 'child2' },
                    ],
                },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(3);
            expect(result.map(s => s.id)).toEqual([1, 2, 3]);
        });

        it('preserves parent before children', () => {
            const servers: ServerLike[] = [
                {
                    id: 1,
                    name: 'parent',
                    children: [{ id: 2, name: 'child' }],
                },
            ];
            const result = collectServers(servers);
            expect(result[0].name).toBe('parent');
            expect(result[1].name).toBe('child');
        });

        it('handles deeply nested hierarchies', () => {
            const servers: ServerLike[] = [
                {
                    id: 1,
                    name: 'level1',
                    children: [
                        {
                            id: 2,
                            name: 'level2',
                            children: [
                                {
                                    id: 3,
                                    name: 'level3',
                                    children: [{ id: 4, name: 'level4' }],
                                },
                            ],
                        },
                    ],
                },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(4);
            expect(result.map(s => s.name)).toEqual(['level1', 'level2', 'level3', 'level4']);
        });

        it('handles multiple top-level servers with children', () => {
            const servers: ServerLike[] = [
                {
                    id: 1,
                    name: 'parent1',
                    children: [{ id: 2, name: 'child1' }],
                },
                {
                    id: 3,
                    name: 'parent2',
                    children: [{ id: 4, name: 'child2' }],
                },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(4);
            expect(result.map(s => s.id)).toEqual([1, 2, 3, 4]);
        });

        it('handles mixed flat and nested servers', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'flat1' },
                {
                    id: 2,
                    name: 'nested',
                    children: [{ id: 3, name: 'child' }],
                },
                { id: 4, name: 'flat2' },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(4);
            expect(result.map(s => s.id)).toEqual([1, 2, 3, 4]);
        });
    });

    describe('edge cases', () => {
        it('handles servers with empty children array', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'server', children: [] },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(1);
            expect(result[0].name).toBe('server');
        });

        it('handles servers without children property', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'server1' },
                { id: 2, name: 'server2', children: undefined },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(2);
        });

        it('preserves all server properties', () => {
            const servers: ServerLike[] = [
                {
                    id: 1,
                    name: 'server',
                    status: 'healthy',
                    customProp: 'value',
                    children: [
                        {
                            id: 2,
                            name: 'child',
                            status: 'warning',
                            anotherProp: 123,
                        },
                    ],
                },
            ];
            const result = collectServers(servers);
            expect(result[0]).toHaveProperty('customProp', 'value');
            expect(result[1]).toHaveProperty('anotherProp', 123);
        });

        it('handles complex hierarchies with varying depths', () => {
            const servers: ServerLike[] = [
                { id: 1, name: 'standalone' },
                {
                    id: 2,
                    name: 'shallow',
                    children: [{ id: 3, name: 'child-shallow' }],
                },
                {
                    id: 4,
                    name: 'deep',
                    children: [
                        {
                            id: 5,
                            name: 'level1',
                            children: [
                                {
                                    id: 6,
                                    name: 'level2',
                                    children: [],
                                },
                            ],
                        },
                    ],
                },
            ];
            const result = collectServers(servers);
            expect(result).toHaveLength(6);
            expect(result.map(s => s.id)).toEqual([1, 2, 3, 4, 5, 6]);
        });
    });

    describe('type preservation', () => {
        it('preserves generic type through recursion', () => {
            interface ExtendedServer extends ServerLike {
                region: string;
            }

            const servers: ExtendedServer[] = [
                {
                    id: 1,
                    name: 'us-server',
                    region: 'us-east-1',
                    children: [
                        { id: 2, name: 'replica1', region: 'us-east-1' },
                    ],
                },
            ];

            const result = collectServers(servers);
            expect(result[0].region).toBe('us-east-1');
            expect(result[1].region).toBe('us-east-1');
        });
    });
});

describe('extractEstateServerIds', () => {
    it('returns empty array for empty selection', () => {
        const emptyEstate: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [],
        };
        expect(extractEstateServerIds(emptyEstate)).toEqual([]);
    });

    it('returns empty array when groups is undefined', () => {
        const emptyEstate: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [],
        };
        expect(extractEstateServerIds(emptyEstate)).toEqual([]);
    });

    it('returns empty array when groups have no clusters', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{ id: 'g1', name: 'g1', status: 'online', clusters: [] }],
        };
        expect(extractEstateServerIds(selection)).toEqual([]);
    });

    it('returns empty array when clusters have no servers', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [],
                    serverIds: [],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([]);
    });

    it('extracts server IDs from a single group and cluster', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        { id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' },
                        { id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' },
                    ],
                    serverIds: [1, 2],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2]);
    });

    it('extracts server IDs across multiple groups and clusters', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [
                {
                    id: 'g1',
                    name: 'g1',
                    status: 'online',
                    clusters: [
                        {
                            id: 'c1',
                            name: 'c1',
                            description: '',
                            status: 'online',
                            servers: [{ id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' }],
                            serverIds: [1],
                        },
                        {
                            id: 'c2',
                            name: 'c2',
                            description: '',
                            status: 'online',
                            servers: [{ id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' }],
                            serverIds: [2],
                        },
                    ],
                },
                {
                    id: 'g2',
                    name: 'g2',
                    status: 'online',
                    clusters: [
                        {
                            id: 'c3',
                            name: 'c3',
                            description: '',
                            status: 'online',
                            servers: [{ id: 3, name: 's3', status: 'online', host: '', port: 5432, database: '' }],
                            serverIds: [3],
                        },
                    ],
                },
            ],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('includes nested children server IDs', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            status: 'online',
                            host: '',
                            port: 5432,
                            database: '',
                            children: [
                                { id: 2, name: 'child1', status: 'online', host: '', port: 5432, database: '' },
                                {
                                    id: 3,
                                    name: 'child2',
                                    status: 'online',
                                    host: '',
                                    port: 5432,
                                    database: '',
                                    children: [
                                        { id: 4, name: 'grandchild', status: 'online', host: '', port: 5432, database: '' },
                                    ],
                                },
                            ],
                        },
                    ],
                    serverIds: [1, 2, 3, 4],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2, 3, 4]);
    });

    it('deduplicates server IDs when children share parent IDs', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            status: 'online',
                            host: '',
                            port: 5432,
                            database: '',
                            children: [
                                { id: 1, name: 'same-id', status: 'online', host: '', port: 5432, database: '' },
                                { id: 2, name: 'child', status: 'online', host: '', port: 5432, database: '' },
                            ],
                        },
                    ],
                    serverIds: [1, 2],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2]);
    });
});

describe('extractClusterServerIds', () => {
    it('returns empty array for empty selection', () => {
        const emptyCluster: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [],
            serverIds: [],
        };
        expect(extractClusterServerIds(emptyCluster)).toEqual([]);
    });

    it('returns empty array when servers is undefined', () => {
        const emptyCluster: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [],
            serverIds: [],
        };
        expect(extractClusterServerIds(emptyCluster)).toEqual([]);
    });

    it('extracts server IDs from flat server list', () => {
        const selection: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [
                { id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' },
                { id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' },
            ],
            serverIds: [1, 2],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2]);
    });

    it('includes nested children server IDs', () => {
        const selection: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [
                {
                    id: 1,
                    name: 'parent',
                    status: 'online',
                    host: '',
                    port: 5432,
                    database: '',
                    children: [
                        { id: 2, name: 'child1', status: 'online', host: '', port: 5432, database: '' },
                        { id: 3, name: 'child2', status: 'online', host: '', port: 5432, database: '' },
                    ],
                },
            ],
            serverIds: [1, 2, 3],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('handles deeply nested children', () => {
        const selection: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [
                {
                    id: 1,
                    name: 'lvl1',
                    status: 'online',
                    host: '',
                    port: 5432,
                    database: '',
                    children: [{
                        id: 2,
                        name: 'lvl2',
                        status: 'online',
                        host: '',
                        port: 5432,
                        database: '',
                        children: [{ id: 3, name: 'lvl3', status: 'online', host: '', port: 5432, database: '' }],
                    }],
                },
            ],
            serverIds: [1, 2, 3],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('deduplicates server IDs when children share parent IDs', () => {
        const selection: ClusterSelection = {
            type: 'cluster',
            id: 'cluster-1',
            name: 'Test Cluster',
            description: '',
            status: 'online',
            servers: [
                {
                    id: 1,
                    name: 'parent',
                    status: 'online',
                    host: '',
                    port: 5432,
                    database: '',
                    children: [
                        { id: 1, name: 'same-id-child', status: 'online', host: '', port: 5432, database: '' },
                        { id: 2, name: 'child2', status: 'online', host: '', port: 5432, database: '' },
                    ],
                },
            ],
            serverIds: [1, 2],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2]);
    });
});

describe('computeEstateServerCounts', () => {
    it('returns all zeros for empty selection', () => {
        const emptyEstate: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [],
        };
        expect(computeEstateServerCounts(emptyEstate)).toEqual({
            online: 0, warning: 0, offline: 0,
        });
    });

    it('counts online servers (no alerts, not offline)', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        { id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' },
                        { id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' },
                    ],
                    serverIds: [1, 2],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 2, warning: 0, offline: 0,
        });
    });

    it('counts offline servers', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        { id: 1, name: 's1', status: 'offline', host: '', port: 5432, database: '' },
                    ],
                    serverIds: [1],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 0, warning: 0, offline: 1,
        });
    });

    it('counts warning servers (online with active alerts)', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        {
                            id: 1,
                            name: 's1',
                            status: 'online',
                            host: '',
                            port: 5432,
                            database: '',
                            active_alert_count: 3,
                        },
                    ],
                    serverIds: [1],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 0, warning: 1, offline: 0,
        });
    });

    it('counts mixed statuses across groups and clusters', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [
                {
                    id: 'g1',
                    name: 'g1',
                    status: 'online',
                    clusters: [{
                        id: 'c1',
                        name: 'c1',
                        description: '',
                        status: 'online',
                        servers: [
                            { id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' },
                            { id: 2, name: 's2', status: 'offline', host: '', port: 5432, database: '' },
                        ],
                        serverIds: [1, 2],
                    }],
                },
                {
                    id: 'g2',
                    name: 'g2',
                    status: 'online',
                    clusters: [{
                        id: 'c2',
                        name: 'c2',
                        description: '',
                        status: 'online',
                        servers: [
                            {
                                id: 3,
                                name: 's3',
                                status: 'online',
                                host: '',
                                port: 5432,
                                database: '',
                                active_alert_count: 1,
                            },
                        ],
                        serverIds: [3],
                    }],
                },
            ],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 1, warning: 1, offline: 1,
        });
    });

    it('includes nested children in counts', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            status: 'online',
                            host: '',
                            port: 5432,
                            database: '',
                            children: [
                                { id: 2, name: 'child', status: 'offline', host: '', port: 5432, database: '' },
                            ],
                        },
                    ],
                    serverIds: [1, 2],
                }],
            }],
        };
        const counts = computeEstateServerCounts(selection);
        expect(counts.online).toBe(1);
        expect(counts.offline).toBe(1);
    });
});

describe('countEstateServers', () => {
    it('returns 0 for empty selection', () => {
        const emptyEstate: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [],
        };
        expect(countEstateServers(emptyEstate)).toBe(0);
    });

    it('returns 0 when groups have no clusters or servers', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [],
            }],
        };
        expect(countEstateServers(selection)).toBe(0);
    });

    it('counts flat servers', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        { id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' },
                        { id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' },
                        { id: 3, name: 's3', status: 'online', host: '', port: 5432, database: '' },
                    ],
                    serverIds: [1, 2, 3],
                }],
            }],
        };
        expect(countEstateServers(selection)).toBe(3);
    });

    it('counts across multiple groups and clusters', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [
                {
                    id: 'g1',
                    name: 'g1',
                    status: 'online',
                    clusters: [
                        {
                            id: 'c1',
                            name: 'c1',
                            description: '',
                            status: 'online',
                            servers: [{ id: 1, name: 's1', status: 'online', host: '', port: 5432, database: '' }],
                            serverIds: [1],
                        },
                        {
                            id: 'c2',
                            name: 'c2',
                            description: '',
                            status: 'online',
                            servers: [{ id: 2, name: 's2', status: 'online', host: '', port: 5432, database: '' }],
                            serverIds: [2],
                        },
                    ],
                },
                {
                    id: 'g2',
                    name: 'g2',
                    status: 'online',
                    clusters: [
                        {
                            id: 'c3',
                            name: 'c3',
                            description: '',
                            status: 'online',
                            servers: [
                                { id: 3, name: 's3', status: 'online', host: '', port: 5432, database: '' },
                                { id: 4, name: 's4', status: 'online', host: '', port: 5432, database: '' },
                            ],
                            serverIds: [3, 4],
                        },
                    ],
                },
            ],
        };
        expect(countEstateServers(selection)).toBe(4);
    });

    it('includes nested children in count', () => {
        const selection: EstateSelection = {
            type: 'estate',
            name: 'All Servers',
            status: 'online',
            groups: [{
                id: 'g1',
                name: 'g1',
                status: 'online',
                clusters: [{
                    id: 'c1',
                    name: 'c1',
                    description: '',
                    status: 'online',
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            status: 'online',
                            host: '',
                            port: 5432,
                            database: '',
                            children: [
                                { id: 2, name: 'child1', status: 'online', host: '', port: 5432, database: '' },
                                {
                                    id: 3,
                                    name: 'child2',
                                    status: 'online',
                                    host: '',
                                    port: 5432,
                                    database: '',
                                    children: [
                                        { id: 4, name: 'grandchild', status: 'online', host: '', port: 5432, database: '' },
                                    ],
                                },
                            ],
                        },
                    ],
                    serverIds: [1, 2, 3, 4],
                }],
            }],
        };
        expect(countEstateServers(selection)).toBe(4);
    });
});
