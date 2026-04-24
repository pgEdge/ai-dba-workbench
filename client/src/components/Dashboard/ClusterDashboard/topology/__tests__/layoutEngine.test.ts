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
    computeLayout,
    computeContainerHeight,
    NODE_WIDTH,
    NODE_HEIGHT,
} from '../layoutEngine';
import type { TopologyGraph, TopoNode } from '../types';
import type { ClusterServer } from '../../../../../contexts/ClusterDataContext';

/**
 * Helper to create a minimal TopoNode for testing layout.
 */
const makeNode = (
    id: number,
    role: string,
    name?: string,
): TopoNode => ({
    id,
    name: name || `node-${id}`,
    role,
    status: 'healthy',
    x: 0,
    y: 0,
    server: { id, name: name || `node-${id}`, status: 'healthy' } as ClusterServer,
});

const CONTAINER_WIDTH = 800;
const PADDING = 20;

describe('computeLayout', () => {
    it('returns the graph unchanged when there are no nodes', () => {
        const graph: TopologyGraph = {
            nodes: [],
            edges: [],
            topologyType: 'standalone',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        expect(result).toBe(graph);
    });

    it('centers a single standalone node horizontally', () => {
        const graph: TopologyGraph = {
            nodes: [makeNode(1, 'standalone')],
            edges: [],
            topologyType: 'standalone',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        expect(result.nodes).toHaveLength(1);
        const node = result.nodes[0];

        // Node should be centered: PADDING + (usableWidth - NODE_WIDTH) / 2
        const usableWidth = CONTAINER_WIDTH - 2 * PADDING;
        const expectedX = PADDING + (usableWidth - NODE_WIDTH) / 2;
        expect(node.x).toBe(expectedX);
        expect(node.y).toBe(PADDING);
    });

    it('places binary tree primary above standbys', () => {
        const graph: TopologyGraph = {
            nodes: [
                makeNode(1, 'binary_primary', 'primary'),
                makeNode(2, 'binary_standby', 'standby-1'),
                makeNode(3, 'binary_standby', 'standby-2'),
            ],
            edges: [
                {
                    sourceId: 1,
                    targetId: 2,
                    replicationType: 'streaming',
                    bidirectional: false,
                },
                {
                    sourceId: 1,
                    targetId: 3,
                    replicationType: 'streaming',
                    bidirectional: false,
                },
            ],
            topologyType: 'binary_tree',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        const primary = result.nodes.find(n => n.id === 1)!;
        const standby1 = result.nodes.find(n => n.id === 2)!;
        const standby2 = result.nodes.find(n => n.id === 3)!;

        // Primary should be on the top level
        expect(primary.y).toBe(PADDING);

        // Standbys should be on a lower level
        expect(standby1.y).toBeGreaterThan(primary.y);
        expect(standby2.y).toBe(standby1.y);

        // Standbys should be side by side
        expect(standby2.x).toBeGreaterThan(standby1.x);
    });

    it('places spock nodes in a horizontal row', () => {
        const graph: TopologyGraph = {
            nodes: [
                makeNode(1, 'spock_node', 'n1'),
                makeNode(2, 'spock_node', 'n2'),
                makeNode(3, 'spock_node', 'n3'),
            ],
            edges: [
                {
                    sourceId: 1,
                    targetId: 2,
                    replicationType: 'spock',
                    bidirectional: true,
                },
                {
                    sourceId: 1,
                    targetId: 3,
                    replicationType: 'spock',
                    bidirectional: true,
                },
                {
                    sourceId: 2,
                    targetId: 3,
                    replicationType: 'spock',
                    bidirectional: true,
                },
            ],
            topologyType: 'spock_mesh',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        // All spock nodes should be on the same row, shifted down
        // to reserve space for arcing edges above non-adjacent pairs.
        // With 3 spock nodes there is 1 intermediate node, so the
        // arc height is ceil((NODE_HEIGHT + 40) / 2) + 15 = 65.
        const ys = result.nodes.map(n => n.y);
        expect(new Set(ys).size).toBe(1);
        expect(ys[0]).toBe(
            PADDING +
            Math.ceil((NODE_HEIGHT + 40) / 2) + 15,
        );

        // Nodes should be positioned left to right
        expect(result.nodes[1].x).toBeGreaterThan(result.nodes[0].x);
        expect(result.nodes[2].x).toBeGreaterThan(result.nodes[1].x);
    });

    it('positions standby children below their parent spock node', () => {
        const graph: TopologyGraph = {
            nodes: [
                makeNode(1, 'spock_node', 'spock-1'),
                makeNode(2, 'spock_node', 'spock-2'),
                makeNode(10, 'binary_standby', 'standby-of-1'),
            ],
            edges: [
                {
                    sourceId: 1,
                    targetId: 2,
                    replicationType: 'spock',
                    bidirectional: true,
                },
                {
                    sourceId: 1,
                    targetId: 10,
                    replicationType: 'streaming',
                    bidirectional: false,
                },
            ],
            topologyType: 'spock_mesh',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        const spock1 = result.nodes.find(n => n.id === 1)!;
        const standby = result.nodes.find(n => n.id === 10)!;

        // Standby should be below the spock row
        expect(standby.y).toBeGreaterThan(spock1.y);
    });

    it('handles logical_flow layout using the binary tree algorithm', () => {
        const graph: TopologyGraph = {
            nodes: [
                makeNode(1, 'logical_publisher', 'pub'),
                makeNode(2, 'logical_subscriber', 'sub'),
            ],
            edges: [
                {
                    sourceId: 1,
                    targetId: 2,
                    replicationType: 'logical',
                    bidirectional: false,
                },
            ],
            topologyType: 'logical_flow',
        };

        const result = computeLayout(graph, CONTAINER_WIDTH);

        const pub = result.nodes.find(n => n.id === 1)!;
        const sub = result.nodes.find(n => n.id === 2)!;

        // Publisher above subscriber
        expect(pub.y).toBe(PADDING);
        expect(sub.y).toBeGreaterThan(pub.y);
    });

    it('does not mutate the original graph nodes', () => {
        const original: TopologyGraph = {
            nodes: [makeNode(1, 'standalone')],
            edges: [],
            topologyType: 'standalone',
        };

        const result = computeLayout(original, CONTAINER_WIDTH);

        // Original node x/y should still be 0
        expect(original.nodes[0].x).toBe(0);
        expect(original.nodes[0].y).toBe(0);
        // Result should have positioned nodes
        expect(result.nodes[0].x).not.toBe(0);
    });
});

describe('computeContainerHeight', () => {
    it('returns a minimum height for an empty graph', () => {
        const graph: TopologyGraph = {
            nodes: [],
            edges: [],
            topologyType: 'standalone',
        };

        expect(computeContainerHeight(graph)).toBe(100);
    });

    it('returns height based on the lowest node position', () => {
        const graph: TopologyGraph = {
            nodes: [
                { ...makeNode(1, 'primary'), y: PADDING },
                { ...makeNode(2, 'standby'), y: 200 },
            ],
            edges: [],
            topologyType: 'binary_tree',
        };

        const height = computeContainerHeight(graph);

        // maxY (200) + NODE_HEIGHT + PADDING
        expect(height).toBe(200 + NODE_HEIGHT + PADDING);
    });

    it('accounts for NODE_HEIGHT so the bottom node is fully visible', () => {
        const graph: TopologyGraph = {
            nodes: [{ ...makeNode(1, 'solo'), y: 50 }],
            edges: [],
            topologyType: 'standalone',
        };

        const height = computeContainerHeight(graph);

        expect(height).toBe(50 + NODE_HEIGHT + PADDING);
    });
});
