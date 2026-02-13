/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { TopologyGraph, TopoNode } from './types';

export const NODE_WIDTH = 160;
export const NODE_HEIGHT = 60;
const VERTICAL_GAP = 180;
const HORIZONTAL_GAP = 60;
const PADDING = 20;

/**
 * Build a mapping from node id to its children (for tree layouts).
 */
const buildChildMap = (
    nodes: TopoNode[],
    graph: TopologyGraph,
): Map<number, number[]> => {
    const childMap = new Map<number, number[]>();
    for (const node of nodes) {
        childMap.set(node.id, []);
    }
    for (const edge of graph.edges) {
        const children = childMap.get(edge.sourceId);
        if (children) {
            children.push(edge.targetId);
        }
    }
    return childMap;
};

/**
 * Find root nodes (nodes that are not the target of any edge).
 */
const findRoots = (graph: TopologyGraph): number[] => {
    const targetIds = new Set(graph.edges.map(e => e.targetId));
    return graph.nodes
        .filter(n => !targetIds.has(n.id))
        .map(n => n.id);
};

/**
 * Lay out a binary replication tree top-down.
 */
const layoutBinaryTree = (
    graph: TopologyGraph,
    containerWidth: number,
): TopologyGraph => {
    const nodeMap = new Map(graph.nodes.map(n => [n.id, { ...n }]));
    const childMap = buildChildMap(graph.nodes, graph);
    const roots = findRoots(graph);

    // BFS to assign levels
    const levels: number[][] = [];
    const visited = new Set<number>();
    let queue = [...roots];

    while (queue.length > 0) {
        levels.push([...queue]);
        const nextQueue: number[] = [];
        for (const id of queue) {
            visited.add(id);
            const children = childMap.get(id) || [];
            for (const childId of children) {
                if (!visited.has(childId)) {
                    nextQueue.push(childId);
                }
            }
        }
        queue = nextQueue;
    }

    // Add any unvisited nodes to the last level
    for (const node of graph.nodes) {
        if (!visited.has(node.id)) {
            if (levels.length === 0) {
                levels.push([]);
            }
            levels[levels.length - 1].push(node.id);
        }
    }

    // Position each level
    const usableWidth = Math.max(
        containerWidth - 2 * PADDING,
        NODE_WIDTH,
    );

    for (let level = 0; level < levels.length; level++) {
        const ids = levels[level];
        const totalWidth =
            ids.length * NODE_WIDTH + (ids.length - 1) * HORIZONTAL_GAP;
        const startX = Math.max(
            PADDING,
            PADDING + (usableWidth - totalWidth) / 2,
        );

        for (let i = 0; i < ids.length; i++) {
            const node = nodeMap.get(ids[i]);
            if (node) {
                node.x = startX + i * (NODE_WIDTH + HORIZONTAL_GAP);
                node.y = PADDING + level * VERTICAL_GAP;
            }
        }
    }

    return {
        ...graph,
        nodes: graph.nodes.map(n => nodeMap.get(n.id) || n),
    };
};

/**
 * Lay out a spock mesh with spock nodes in a top row and any
 * standby children positioned below their parent spock node.
 */
const SPOCK_GAP = 120;

const layoutSpockMesh = (
    graph: TopologyGraph,
    containerWidth: number,
): TopologyGraph => {
    const nodes = graph.nodes.map(n => ({ ...n }));
    const nodeMap = new Map(nodes.map(n => [n.id, n]));

    const spockNodes = nodes.filter(n => n.role === 'spock_node');
    const nonSpockNodes = nodes.filter(n => n.role !== 'spock_node');

    // Build a map from parent spock node id to its non-spock children
    // using the streaming edges in the graph.
    const childrenOf = new Map<number, TopoNode[]>();
    for (const spock of spockNodes) {
        childrenOf.set(spock.id, []);
    }
    for (const edge of graph.edges) {
        const target = nodeMap.get(edge.targetId);
        if (
            target &&
            target.role !== 'spock_node' &&
            childrenOf.has(edge.sourceId)
        ) {
            childrenOf.get(edge.sourceId)!.push(target);
        }
    }

    // Collect any non-spock nodes that have no parent edge (orphans)
    const assignedIds = new Set(
        Array.from(childrenOf.values())
            .flat()
            .map(n => n.id),
    );
    const orphans = nonSpockNodes.filter(n => !assignedIds.has(n.id));

    // Calculate a dynamic gap between spock nodes so that standby
    // rows beneath adjacent spock nodes do not overlap.
    const maxChildren = Math.max(
        0,
        ...spockNodes.map(
            s => (childrenOf.get(s.id) || []).length,
        ),
    );
    const maxChildRowWidth =
        maxChildren * NODE_WIDTH +
        (maxChildren - 1) * HORIZONTAL_GAP;
    const effectiveGap = Math.max(
        SPOCK_GAP,
        maxChildRowWidth - NODE_WIDTH + HORIZONTAL_GAP,
    );

    // Place spock nodes in a horizontal top row with wider spacing
    const usableWidth = Math.max(
        containerWidth - 2 * PADDING,
        NODE_WIDTH,
    );
    const spockTotalWidth =
        spockNodes.length * NODE_WIDTH +
        (spockNodes.length - 1) * effectiveGap;
    const spockStartX = Math.max(
        PADDING,
        PADDING + (usableWidth - spockTotalWidth) / 2,
    );

    for (let i = 0; i < spockNodes.length; i++) {
        spockNodes[i].x =
            spockStartX + i * (NODE_WIDTH + effectiveGap);
        spockNodes[i].y = PADDING;
    }

    // Position each non-spock child below its parent spock node
    for (const spock of spockNodes) {
        const children = childrenOf.get(spock.id) || [];
        if (children.length === 0) {
            continue;
        }
        const childTotalWidth =
            children.length * NODE_WIDTH +
            (children.length - 1) * HORIZONTAL_GAP;
        const childStartX = Math.max(
            PADDING,
            spock.x + NODE_WIDTH / 2 - childTotalWidth / 2,
        );
        for (let i = 0; i < children.length; i++) {
            children[i].x =
                childStartX + i * (NODE_WIDTH + HORIZONTAL_GAP);
            children[i].y = PADDING + VERTICAL_GAP;
        }
    }

    // Place orphan non-spock nodes in a row below everything
    if (orphans.length > 0) {
        const orphanY = PADDING + VERTICAL_GAP;
        const orphanTotalWidth =
            orphans.length * NODE_WIDTH +
            (orphans.length - 1) * HORIZONTAL_GAP;
        const orphanStartX = Math.max(
            PADDING,
            PADDING + (usableWidth - orphanTotalWidth) / 2,
        );
        for (let i = 0; i < orphans.length; i++) {
            orphans[i].x =
                orphanStartX + i * (NODE_WIDTH + HORIZONTAL_GAP);
            orphans[i].y = orphanY;
        }
    }

    return { ...graph, nodes };
};

/**
 * Center a single standalone node.
 */
const layoutStandalone = (
    graph: TopologyGraph,
    containerWidth: number,
): TopologyGraph => {
    const nodes = graph.nodes.map(n => ({ ...n }));
    const usableWidth = Math.max(
        containerWidth - 2 * PADDING,
        NODE_WIDTH,
    );

    if (nodes.length === 1) {
        nodes[0].x = PADDING + (usableWidth - NODE_WIDTH) / 2;
        nodes[0].y = PADDING;
    } else {
        // Multiple standalone nodes in a row
        const totalWidth =
            nodes.length * NODE_WIDTH +
            (nodes.length - 1) * HORIZONTAL_GAP;
        const startX = Math.max(
            PADDING,
            PADDING + (usableWidth - totalWidth) / 2,
        );
        for (let i = 0; i < nodes.length; i++) {
            nodes[i].x = startX + i * (NODE_WIDTH + HORIZONTAL_GAP);
            nodes[i].y = PADDING;
        }
    }

    return { ...graph, nodes };
};

/**
 * Compute positioned layout for a topology graph based on its type.
 */
export const computeLayout = (
    graph: TopologyGraph,
    containerWidth: number,
): TopologyGraph => {
    if (graph.nodes.length === 0) {
        return graph;
    }

    switch (graph.topologyType) {
        case 'binary_tree':
            return layoutBinaryTree(graph, containerWidth);
        case 'spock_mesh':
            return layoutSpockMesh(graph, containerWidth);
        case 'logical_flow':
            return layoutBinaryTree(graph, containerWidth);
        case 'standalone':
        default:
            return layoutStandalone(graph, containerWidth);
    }
};

/**
 * Calculate the minimum container height needed for the laid-out
 * graph.
 */
export const computeContainerHeight = (
    graph: TopologyGraph,
): number => {
    if (graph.nodes.length === 0) {
        return 100;
    }
    const maxY = Math.max(...graph.nodes.map(n => n.y));
    return maxY + NODE_HEIGHT + PADDING;
};
