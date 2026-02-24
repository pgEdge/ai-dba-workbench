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
 * Map a relationship_type string from the API to the edge
 * replication type used in the topology diagram.
 */
const mapRelationshipType = (
    relType: string,
): 'streaming' | 'spock' | 'logical' => {
    switch (relType) {
        case 'streams_from':
            return 'streaming';
        case 'replicates_with':
            return 'spock';
        case 'subscribes_to':
            return 'logical';
        default:
            return 'streaming';
    }
};

/**
 * Check whether any server in the list carries explicit
 * relationship data from the API.
 */
const hasRelationships = (servers: ClusterServer[]): boolean => {
    for (const server of servers) {
        if (server.relationships && server.relationships.length > 0) {
            return true;
        }
        if (server.children) {
            if (hasRelationships(server.children)) {
                return true;
            }
        }
    }
    return false;
};

/**
 * Build edges from the explicit relationships field on servers.
 * Deduplicates bidirectional pairs so that A-B and B-A become a
 * single edge with bidirectional=true.
 */
const buildEdgesFromRelationships = (
    nodes: TopoNode[],
): TopoEdge[] => {
    const edges: TopoEdge[] = [];
    const seen = new Set<string>();
    const nodeIds = new Set(nodes.map(n => n.id));

    for (const node of nodes) {
        const rels = node.server.relationships;
        if (!rels) {
            continue;
        }
        for (const rel of rels) {
            if (!nodeIds.has(rel.target_server_id)) {
                continue;
            }
            const repType = mapRelationshipType(rel.relationship_type);

            // For directional relationships like "streams_from" and
            // "subscribes_to", the API source is the node that
            // receives data and the API target provides data.
            // The visual convention is parent (provider) at the top
            // with edges flowing down to children (receivers), so
            // swap source and target to match the visual direction.
            const isDirectional =
                rel.relationship_type === 'streams_from' ||
                rel.relationship_type === 'subscribes_to';
            const edgeSource = isDirectional
                ? rel.target_server_id
                : node.id;
            const edgeTarget = isDirectional
                ? node.id
                : rel.target_server_id;

            // Create a canonical key to detect bidirectional pairs
            const minId = Math.min(edgeSource, edgeTarget);
            const maxId = Math.max(edgeSource, edgeTarget);
            const key = `${minId}-${maxId}-${repType}`;

            if (seen.has(key)) {
                // Mark existing edge as bidirectional
                const existing = edges.find(
                    e =>
                        ((e.sourceId === minId && e.targetId === maxId) ||
                            (e.sourceId === maxId && e.targetId === minId)) &&
                        e.replicationType === repType,
                );
                if (existing) {
                    existing.bidirectional = true;
                }
                continue;
            }
            seen.add(key);

            edges.push({
                sourceId: edgeSource,
                targetId: edgeTarget,
                replicationType: repType,
                bidirectional: false,
            });
        }
    }

    return edges;
};

/**
 * Build a TopologyGraph from a flat or hierarchical array of
 * ClusterServer objects.
 *
 * When servers carry explicit relationship data from the API,
 * edges are built from those relationships. Otherwise edges are
 * inferred from the parent-child hierarchy (streaming replication)
 * or from role analysis (spock mesh, logical flow).
 */
export const buildGraph = (servers: ClusterServer[]): TopologyGraph => {
    const nodes: TopoNode[] = [];
    const edges: TopoEdge[] = [];
    const roles = new Set<string>();
    const visited = new Set<number>();

    walkServers(servers, null, nodes, edges, roles, visited);

    const topologyType = detectTopologyType(roles);

    // When explicit relationships are available, use them for edges
    // instead of the inferred hierarchy-based edges.
    if (hasRelationships(servers)) {
        const relationshipEdges = buildEdgesFromRelationships(nodes);
        edges.length = 0;
        edges.push(...relationshipEdges);
        return { nodes, edges, topologyType };
    }

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
