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
import { buildGraph } from '../graphBuilder';
import { ClusterServer } from '../../../../../contexts/ClusterDataContext';

/**
 * Helper to create a minimal ClusterServer for testing.
 */
const makeServer = (
    overrides: Partial<ClusterServer> & { id: number; name: string },
): ClusterServer => ({
    status: 'healthy',
    ...overrides,
});

describe('buildGraph', () => {
    it('returns an empty graph for an empty server list', () => {
        const graph = buildGraph([]);

        expect(graph.nodes).toHaveLength(0);
        expect(graph.edges).toHaveLength(0);
        expect(graph.topologyType).toBe('standalone');
    });

    it('creates a standalone topology for a single server with no role', () => {
        const servers: ClusterServer[] = [
            makeServer({ id: 1, name: 'solo' }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('standalone');
        expect(graph.nodes).toHaveLength(1);
        expect(graph.nodes[0].role).toBe('standalone');
        expect(graph.edges).toHaveLength(0);
    });

    it('creates a binary_tree topology for a primary with one standby', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'primary',
                primary_role: 'binary_primary',
                children: [
                    makeServer({
                        id: 2,
                        name: 'standby',
                        primary_role: 'binary_standby',
                    }),
                ],
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('binary_tree');
        expect(graph.nodes).toHaveLength(2);
        expect(graph.edges).toHaveLength(1);
        expect(graph.edges[0]).toMatchObject({
            sourceId: 1,
            targetId: 2,
            replicationType: 'streaming',
            bidirectional: false,
        });
    });

    it('creates correct edges for cascading standbys', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'primary',
                primary_role: 'binary_primary',
                children: [
                    makeServer({
                        id: 2,
                        name: 'standby-1',
                        primary_role: 'binary_standby',
                        children: [
                            makeServer({
                                id: 3,
                                name: 'standby-2',
                                primary_role: 'binary_standby',
                            }),
                        ],
                    }),
                ],
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('binary_tree');
        expect(graph.nodes).toHaveLength(3);
        expect(graph.edges).toHaveLength(2);

        // Edge from primary to first standby
        expect(graph.edges[0]).toMatchObject({
            sourceId: 1,
            targetId: 2,
        });
        // Edge from first standby to second standby (cascade)
        expect(graph.edges[1]).toMatchObject({
            sourceId: 2,
            targetId: 3,
        });
    });

    it('creates a spock_mesh with bidirectional edges for 3 spock nodes', () => {
        const servers: ClusterServer[] = [
            makeServer({ id: 1, name: 'n1', primary_role: 'spock_node' }),
            makeServer({ id: 2, name: 'n2', primary_role: 'spock_node' }),
            makeServer({ id: 3, name: 'n3', primary_role: 'spock_node' }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('spock_mesh');
        expect(graph.nodes).toHaveLength(3);

        // Full mesh: C(3,2) = 3 bidirectional edges
        expect(graph.edges).toHaveLength(3);

        for (const edge of graph.edges) {
            expect(edge.replicationType).toBe('spock');
            expect(edge.bidirectional).toBe(true);
        }

        // Verify all pairs are covered
        const pairs = graph.edges.map(
            e => `${e.sourceId}-${e.targetId}`,
        );
        expect(pairs).toContain('1-2');
        expect(pairs).toContain('1-3');
        expect(pairs).toContain('2-3');
    });

    it('preserves streaming edges to non-spock standbys in a spock cluster', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'spock-1',
                primary_role: 'spock_node',
                children: [
                    makeServer({
                        id: 10,
                        name: 'standby-of-1',
                        primary_role: 'binary_standby',
                    }),
                ],
            }),
            makeServer({
                id: 2,
                name: 'spock-2',
                primary_role: 'spock_node',
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('spock_mesh');
        expect(graph.nodes).toHaveLength(3);

        // 1 spock bidirectional edge + 1 streaming edge to standby
        const spockEdges = graph.edges.filter(
            e => e.replicationType === 'spock',
        );
        const streamingEdges = graph.edges.filter(
            e => e.replicationType === 'streaming',
        );
        expect(spockEdges).toHaveLength(1);
        expect(streamingEdges).toHaveLength(1);
        expect(streamingEdges[0]).toMatchObject({
            sourceId: 1,
            targetId: 10,
            replicationType: 'streaming',
        });
    });

    it('creates a logical_flow topology for publisher and subscriber', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'publisher',
                primary_role: 'logical_publisher',
            }),
            makeServer({
                id: 2,
                name: 'subscriber',
                primary_role: 'logical_subscriber',
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('logical_flow');
        expect(graph.nodes).toHaveLength(2);
        expect(graph.edges).toHaveLength(1);
        expect(graph.edges[0]).toMatchObject({
            sourceId: 1,
            targetId: 2,
            replicationType: 'logical',
            bidirectional: false,
        });
    });

    it('creates logical edges from every publisher to every subscriber', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'pub-1',
                primary_role: 'logical_publisher',
            }),
            makeServer({
                id: 2,
                name: 'pub-2',
                primary_role: 'logical_publisher',
            }),
            makeServer({
                id: 3,
                name: 'sub-1',
                primary_role: 'logical_subscriber',
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.topologyType).toBe('logical_flow');
        // 2 publishers x 1 subscriber = 2 edges
        expect(graph.edges).toHaveLength(2);
        expect(graph.edges).toContainEqual(
            expect.objectContaining({ sourceId: 1, targetId: 3 }),
        );
        expect(graph.edges).toContainEqual(
            expect.objectContaining({ sourceId: 2, targetId: 3 }),
        );
    });

    it('deduplicates servers that appear both at top level and as children', () => {
        const sharedChild = makeServer({
            id: 2,
            name: 'standby',
            primary_role: 'binary_standby',
        });

        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'primary',
                primary_role: 'binary_primary',
                children: [sharedChild],
            }),
            // Same server appears again at top level
            sharedChild,
        ];

        const graph = buildGraph(servers);

        expect(graph.nodes).toHaveLength(2);
        const nodeIds = graph.nodes.map(n => n.id);
        expect(new Set(nodeIds).size).toBe(2);
    });

    it('uses role as fallback when primary_role is absent', () => {
        const servers: ClusterServer[] = [
            makeServer({
                id: 1,
                name: 'srv',
                role: 'binary_primary',
            }),
        ];

        const graph = buildGraph(servers);

        expect(graph.nodes[0].role).toBe('binary_primary');
        expect(graph.topologyType).toBe('binary_tree');
    });

    it('stores the original server reference on each node', () => {
        const server = makeServer({ id: 1, name: 'srv' });
        const graph = buildGraph([server]);

        expect(graph.nodes[0].server).toBe(server);
    });
});
