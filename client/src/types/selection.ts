/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ClusterServer, ClusterGroup } from '../contexts/ClusterDataContext';

interface SelectionBase {
    name: string;
    status: string;
}

export interface ServerSelection extends SelectionBase {
    type: 'server';
    id: number;
    description: string;
    host: string;
    port: number;
    role: string;
    version: string;
    database: string;
    username: string;
    os: string;
    platform: string;
    spockNodeName?: string;
    spockVersion?: string;
    clusterId?: string;
    clusterName?: string;
    isStandalone?: boolean;
    groupId?: string;
    groupName?: string;
}

export interface ClusterSelection extends SelectionBase {
    type: 'cluster';
    id: string;
    description: string;
    servers: ClusterServer[];
    serverIds: number[];
    groupId?: string;
    groupName?: string;
}

export interface EstateSelection extends SelectionBase {
    type: 'estate';
    groups: ClusterGroup[];
}

export type Selection =
    | ServerSelection
    | ClusterSelection
    | EstateSelection;
