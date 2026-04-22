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
    ServerCounts,
    countEstateServers,
} from '../clusterHelpers';

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
        expect(extractEstateServerIds({})).toEqual([]);
    });

    it('returns empty array when groups is undefined', () => {
        expect(extractEstateServerIds({ groups: undefined })).toEqual([]);
    });

    it('returns empty array when groups have no clusters', () => {
        const selection = { groups: [{ name: 'g1' }] };
        expect(extractEstateServerIds(selection)).toEqual([]);
    });

    it('returns empty array when clusters have no servers', () => {
        const selection = {
            groups: [{ clusters: [{ name: 'c1' }] }],
        };
        expect(extractEstateServerIds(selection)).toEqual([]);
    });

    it('extracts server IDs from a single group and cluster', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        { id: 1, name: 's1' },
                        { id: 2, name: 's2' },
                    ],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2]);
    });

    it('extracts server IDs across multiple groups and clusters', () => {
        const selection = {
            groups: [
                {
                    clusters: [
                        { servers: [{ id: 1, name: 's1' }] },
                        { servers: [{ id: 2, name: 's2' }] },
                    ],
                },
                {
                    clusters: [
                        { servers: [{ id: 3, name: 's3' }] },
                    ],
                },
            ],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('includes nested children server IDs', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            children: [
                                { id: 2, name: 'child1' },
                                {
                                    id: 3,
                                    name: 'child2',
                                    children: [
                                        { id: 4, name: 'grandchild' },
                                    ],
                                },
                            ],
                        },
                    ],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2, 3, 4]);
    });

    it('deduplicates server IDs when children share parent IDs', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            children: [
                                { id: 1, name: 'same-id' },
                                { id: 2, name: 'child' },
                            ],
                        },
                    ],
                }],
            }],
        };
        expect(extractEstateServerIds(selection)).toEqual([1, 2]);
    });
});

describe('extractClusterServerIds', () => {
    it('returns empty array for empty selection', () => {
        expect(extractClusterServerIds({})).toEqual([]);
    });

    it('returns empty array when servers is undefined', () => {
        expect(extractClusterServerIds({ servers: undefined })).toEqual([]);
    });

    it('extracts server IDs from flat server list', () => {
        const selection = {
            servers: [
                { id: 1, name: 's1' },
                { id: 2, name: 's2' },
            ],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2]);
    });

    it('includes nested children server IDs', () => {
        const selection = {
            servers: [
                {
                    id: 1,
                    name: 'parent',
                    children: [
                        { id: 2, name: 'child1' },
                        { id: 3, name: 'child2' },
                    ],
                },
            ],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('handles deeply nested children', () => {
        const selection = {
            servers: [
                {
                    id: 1,
                    name: 'lvl1',
                    children: [{
                        id: 2,
                        name: 'lvl2',
                        children: [{ id: 3, name: 'lvl3' }],
                    }],
                },
            ],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2, 3]);
    });

    it('deduplicates server IDs when children share parent IDs', () => {
        const selection = {
            servers: [
                {
                    id: 1,
                    name: 'parent',
                    children: [
                        { id: 1, name: 'same-id-child' },
                        { id: 2, name: 'child2' },
                    ],
                },
            ],
        };
        expect(extractClusterServerIds(selection)).toEqual([1, 2]);
    });
});

describe('computeEstateServerCounts', () => {
    it('returns all zeros for empty selection', () => {
        expect(computeEstateServerCounts({})).toEqual({
            online: 0, warning: 0, offline: 0,
        });
    });

    it('counts online servers (no alerts, not offline)', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        { id: 1, name: 's1', status: 'online' },
                        { id: 2, name: 's2', status: 'online' },
                    ],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 2, warning: 0, offline: 0,
        });
    });

    it('counts offline servers', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        { id: 1, name: 's1', status: 'offline' },
                    ],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 0, warning: 0, offline: 1,
        });
    });

    it('counts warning servers (online with active alerts)', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        {
                            id: 1,
                            name: 's1',
                            status: 'online',
                            active_alert_count: 3,
                        },
                    ],
                }],
            }],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 0, warning: 1, offline: 0,
        });
    });

    it('counts mixed statuses across groups and clusters', () => {
        const selection = {
            groups: [
                {
                    clusters: [{
                        servers: [
                            { id: 1, name: 's1', status: 'online' },
                            { id: 2, name: 's2', status: 'offline' },
                        ],
                    }],
                },
                {
                    clusters: [{
                        servers: [
                            {
                                id: 3,
                                name: 's3',
                                status: 'online',
                                active_alert_count: 1,
                            },
                        ],
                    }],
                },
            ],
        };
        expect(computeEstateServerCounts(selection)).toEqual({
            online: 1, warning: 1, offline: 1,
        });
    });

    it('includes nested children in counts', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            status: 'online',
                            children: [
                                { id: 2, name: 'child', status: 'offline' },
                            ],
                        },
                    ],
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
        expect(countEstateServers({})).toBe(0);
    });

    it('returns 0 when groups have no clusters or servers', () => {
        const selection = { groups: [{ name: 'g1' }] };
        expect(countEstateServers(selection)).toBe(0);
    });

    it('counts flat servers', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        { id: 1, name: 's1' },
                        { id: 2, name: 's2' },
                        { id: 3, name: 's3' },
                    ],
                }],
            }],
        };
        expect(countEstateServers(selection)).toBe(3);
    });

    it('counts across multiple groups and clusters', () => {
        const selection = {
            groups: [
                {
                    clusters: [
                        { servers: [{ id: 1, name: 's1' }] },
                        { servers: [{ id: 2, name: 's2' }] },
                    ],
                },
                {
                    clusters: [
                        {
                            servers: [
                                { id: 3, name: 's3' },
                                { id: 4, name: 's4' },
                            ],
                        },
                    ],
                },
            ],
        };
        expect(countEstateServers(selection)).toBe(4);
    });

    it('includes nested children in count', () => {
        const selection = {
            groups: [{
                clusters: [{
                    servers: [
                        {
                            id: 1,
                            name: 'parent',
                            children: [
                                { id: 2, name: 'child1' },
                                {
                                    id: 3,
                                    name: 'child2',
                                    children: [
                                        { id: 4, name: 'grandchild' },
                                    ],
                                },
                            ],
                        },
                    ],
                }],
            }],
        };
        expect(countEstateServers(selection)).toBe(4);
    });
});
