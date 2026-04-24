/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ClusterServer } from '../../../../contexts/ClusterDataContext';

export interface TopoNode {
    id: number;
    name: string;
    role: string;
    status: string;
    x: number;
    y: number;
    server: ClusterServer;
}

export interface TopoEdge {
    sourceId: number;
    targetId: number;
    replicationType: 'streaming' | 'spock' | 'logical';
    bidirectional: boolean;
}

export type ClusterTopologyType =
    | 'binary_tree'
    | 'spock_mesh'
    | 'logical_flow'
    | 'standalone';

export interface TopologyGraph {
    nodes: TopoNode[];
    edges: TopoEdge[];
    topologyType: ClusterTopologyType;
}
