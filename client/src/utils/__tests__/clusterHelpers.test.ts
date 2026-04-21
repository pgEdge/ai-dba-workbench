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
import { collectServers, ServerLike } from '../clusterHelpers';

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
