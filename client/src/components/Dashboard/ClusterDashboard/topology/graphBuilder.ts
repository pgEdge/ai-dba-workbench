/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { ClusterServer } from '../../../../contexts/ClusterDataContext';
import {
    TopoNode,
    TopoEdge,
    TopologyGraph,
    ClusterTopologyType,
} from './types';

/**
 * Determine the canonical role string for a server.
 */
const resolveRole = (server: ClusterServer): string => {
    return server.primary_role || server.role || 'standalone';
};

/**
 * Detect the topology type from the set of roles present.
 */
const detectTopologyType = (roles: Set<string>): ClusterTopologyType => {
    if (roles.has('spock_node')) {
        return 'spock_mesh';
    }
    if (roles.has('binary_primary')) {
        return 'binary_tree';
    }
    if (roles.has('logical_publisher') || roles.has('logical_subscriber')) {
        return 'logical_flow';
    }
    return 'standalone';
};

/**
 * Recursively walk the server tree to collect nodes and
 * parent-child streaming edges.
 */
const walkServers = (
    servers: ClusterServer[],
    parentId: number | null,
    nodes: TopoNode[],
    edges: TopoEdge[],
    roles: Set<string>,
    visited: Set<number>,
): void => {
    for (const server of servers) {
        if (visited.has(server.id)) {
            continue;
        }
        visited.add(server.id);

        const role = resolveRole(server);
        roles.add(role);

        nodes.push({
            id: server.id,
            name: server.name,
            role,
            status: server.status || 'unknown',
            x: 0,
            y: 0,
            server,
        });

        if (parentId !== null) {
            edges.push({
                sourceId: parentId,
                targetId: server.id,
                replicationType: 'streaming',
                bidirectional: false,
            });
        }

        if (server.children && server.children.length > 0) {
            walkServers(server.children, server.id, nodes, edges, roles, visited);
        }
    }
};

/**
 * Build a TopologyGraph from a flat or hierarchical array of
 * ClusterServer objects.
 *
 * Edges are inferred from the parent-child hierarchy (streaming
 * replication) or from role analysis (spock mesh, logical flow).
 */
export const buildGraph = (servers: ClusterServer[]): TopologyGraph => {
    const nodes: TopoNode[] = [];
    const edges: TopoEdge[] = [];
    const roles = new Set<string>();
    const visited = new Set<number>();

    walkServers(servers, null, nodes, edges, roles, visited);

    const topologyType = detectTopologyType(roles);

    // For spock clusters, replace spock-to-spock edges with a full
    // mesh of bidirectional connections, while preserving streaming
    // edges from spock nodes to their non-spock children (standbys).
    if (topologyType === 'spock_mesh') {
        const streamingEdges = edges.filter(e => {
            const targetNode = nodes.find(n => n.id === e.targetId);
            return targetNode && targetNode.role !== 'spock_node';
        });
        edges.length = 0;
        const spockNodes = nodes.filter(n => n.role === 'spock_node');
        for (let i = 0; i < spockNodes.length; i++) {
            for (let j = i + 1; j < spockNodes.length; j++) {
                edges.push({
                    sourceId: spockNodes[i].id,
                    targetId: spockNodes[j].id,
                    replicationType: 'spock',
                    bidirectional: true,
                });
            }
        }
        edges.push(...streamingEdges);
    }

    // For logical clusters, replace edges with publisher-to-subscriber
    // connections.
    if (topologyType === 'logical_flow') {
        edges.length = 0;
        const publishers = nodes.filter(
            n => n.role === 'logical_publisher',
        );
        const subscribers = nodes.filter(
            n => n.role === 'logical_subscriber',
        );
        for (const pub of publishers) {
            for (const sub of subscribers) {
                edges.push({
                    sourceId: pub.id,
                    targetId: sub.id,
                    replicationType: 'logical',
                    bidirectional: false,
                });
            }
        }
    }

    return { nodes, edges, topologyType };
};
