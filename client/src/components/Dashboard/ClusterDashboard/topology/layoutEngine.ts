/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { TopologyGraph, TopoNode } from './types';

export const NODE_WIDTH = 160;
export const NODE_HEIGHT = 60;
const VERTICAL_GAP = 180;
const HORIZONTAL_GAP = 60;
const SIBLING_EDGE_GAP = 160;
const PADDING = 20;
const DEFAULT_MAX_WIDTH = 650;
const CASTELLATION_OFFSET = 50;
const CASTELLATED_GAP = 20;

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

    // Build a set of sibling-edge pairs so we can widen the gap
    // between nodes at the same level that share an edge.
    const siblingEdgePairs = new Set<string>();
    for (const edge of graph.edges) {
        siblingEdgePairs.add(
            `${Math.min(edge.sourceId, edge.targetId)}-${Math.max(edge.sourceId, edge.targetId)}`,
        );
    }

    // Position each level
    const usableWidth = Math.max(
        containerWidth - 2 * PADDING,
        NODE_WIDTH,
    );

    for (let level = 0; level < levels.length; level++) {
        const ids = levels[level];
        const idSet = new Set(ids);

        // Check whether any pair of adjacent siblings in this
        // level has an edge between them; if so, use wider spacing
        // for the entire level so horizontal arrows have room.
        let hasSiblingEdge = false;
        for (const edge of graph.edges) {
            if (
                idSet.has(edge.sourceId) &&
                idSet.has(edge.targetId)
            ) {
                hasSiblingEdge = true;
                break;
            }
        }
        const gap = hasSiblingEdge
            ? SIBLING_EDGE_GAP
            : HORIZONTAL_GAP;

        const totalWidth =
            ids.length * NODE_WIDTH + (ids.length - 1) * gap;
        const startX = Math.max(
            PADDING,
            PADDING + (usableWidth - totalWidth) / 2,
        );

        for (let i = 0; i < ids.length; i++) {
            const node = nodeMap.get(ids[i]);
            if (node) {
                node.x = startX + i * (NODE_WIDTH + gap);
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
    let effectiveGap = Math.max(
        SPOCK_GAP,
        maxChildRowWidth - NODE_WIDTH + HORIZONTAL_GAP,
    );

    // Ensure spock nodes fit within the container width by
    // reducing the gap when the row would otherwise overflow.
    const maxSpockWidth = containerWidth - 2 * PADDING;
    const neededWidth =
        spockNodes.length * NODE_WIDTH +
        (spockNodes.length - 1) * effectiveGap;
    if (neededWidth > maxSpockWidth && spockNodes.length > 1) {
        const availableGap =
            (maxSpockWidth - spockNodes.length * NODE_WIDTH) /
            (spockNodes.length - 1);
        effectiveGap = Math.max(HORIZONTAL_GAP, availableGap);
    }

    // Calculate the vertical space needed above the spock nodes for
    // arcing edges between non-adjacent pairs.  TopologyEdges draws
    // arcs whose control point is at:
    //   y_node_top - (nodeHeight + 40 * intermediateCount)
    // The maximum number of intermediate spock nodes between any two
    // nodes is (spockNodes.length - 2).  Reserve enough top padding
    // so the arc (and its label) do not clip above the container.
    const maxIntermediateCount = Math.max(0, spockNodes.length - 2);
    const arcHeight = maxIntermediateCount > 0
        ? Math.ceil(
            (NODE_HEIGHT + 40 * maxIntermediateCount) / 2,
        ) + 15
        : 0;
    const spockTopY = PADDING + arcHeight;

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
        spockNodes[i].y = spockTopY;
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
            children[i].y = spockTopY + VERTICAL_GAP;
        }
    }

    // Place orphan non-spock nodes in a row below everything
    if (orphans.length > 0) {
        const orphanY = spockTopY + VERTICAL_GAP;
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
 * Apply castellated (staggered) layout as a post-processing step.
 * When nodes at the same vertical level exceed the available width,
 * compress horizontal spacing and offset every other node downward
 * to create a zigzag pattern that fits more nodes horizontally.
 */
const applyCastellation = (
    graph: TopologyGraph,
    maxWidth: number,
): TopologyGraph => {
    if (graph.nodes.length === 0) {
        return graph;
    }

    const nodes = graph.nodes.map(n => ({ ...n }));
    const nodeMap = new Map(nodes.map(n => [n.id, n]));

    // Group nodes by their y position (level)
    const levelMap = new Map<number, TopoNode[]>();
    for (const node of nodes) {
        const existing = levelMap.get(node.y) || [];
        existing.push(node);
        levelMap.set(node.y, existing);
    }

    // Sort levels by y value for stable processing
    const sortedLevels = Array.from(levelMap.entries()).sort(
        (a, b) => a[0] - b[0],
    );

    // Track the cumulative vertical shift applied to each original
    // y level so that subsequent levels move down accordingly.
    let cumulativeShift = 0;

    for (const [originalY, levelNodes] of sortedLevels) {
        if (levelNodes.length <= 1) {
            // Apply accumulated shift from prior castellated levels
            for (const node of levelNodes) {
                node.y = originalY + cumulativeShift;
            }
            continue;
        }

        // Sort by x to process left-to-right
        levelNodes.sort((a, b) => a.x - b.x);

        const rightEdge =
            levelNodes[levelNodes.length - 1].x + NODE_WIDTH;
        const leftEdge = levelNodes[0].x;
        const totalWidth = rightEdge - leftEdge;

        if (totalWidth <= maxWidth - 2 * PADDING) {
            // Level fits; just apply the accumulated shift
            for (const node of levelNodes) {
                node.y = originalY + cumulativeShift;
            }
            continue;
        }

        // Compute compressed spacing so nodes fit within maxWidth
        const availableWidth = maxWidth - 2 * PADDING - NODE_WIDTH;
        const compressedGap = Math.max(
            CASTELLATED_GAP,
            availableWidth / (levelNodes.length - 1) - NODE_WIDTH,
        );
        const compressedTotalWidth =
            levelNodes.length * NODE_WIDTH +
            (levelNodes.length - 1) * compressedGap;
        const startX = Math.max(
            PADDING,
            PADDING +
                (maxWidth - 2 * PADDING - compressedTotalWidth) / 2,
        );

        const baseY = originalY + cumulativeShift;

        for (let i = 0; i < levelNodes.length; i++) {
            const node = nodeMap.get(levelNodes[i].id);
            if (node) {
                node.x =
                    startX + i * (NODE_WIDTH + compressedGap);
                node.y =
                    baseY +
                    (i % 2 === 1 ? CASTELLATION_OFFSET : 0);
            }
        }

        // Increase cumulative shift so the next level starts
        // below the lowest castellated node.
        cumulativeShift += CASTELLATION_OFFSET;
    }

    return { ...graph, nodes };
};

/**
 * Compute positioned layout for a topology graph based on its type.
 * An optional maxWidth parameter controls the threshold at which
 * castellated (staggered) layout is applied; defaults to 650px.
 */
export const computeLayout = (
    graph: TopologyGraph,
    containerWidth: number,
    maxWidth?: number,
): TopologyGraph => {
    if (graph.nodes.length === 0) {
        return graph;
    }

    let result: TopologyGraph;

    switch (graph.topologyType) {
        case 'binary_tree':
            result = layoutBinaryTree(graph, containerWidth);
            break;
        case 'spock_mesh':
            result = layoutSpockMesh(graph, containerWidth);
            break;
        case 'logical_flow':
            result = layoutBinaryTree(graph, containerWidth);
            break;
        default:
            result = layoutStandalone(graph, containerWidth);
            break;
    }

    // Spock mesh layouts handle their own spacing and should
    // not be castellated; staggering spock nodes would break
    // the mesh edge routing and centering.
    if (graph.topologyType === 'spock_mesh') {
        return result;
    }

    return applyCastellation(
        result,
        maxWidth ?? Math.max(containerWidth, DEFAULT_MAX_WIDTH),
    );
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
