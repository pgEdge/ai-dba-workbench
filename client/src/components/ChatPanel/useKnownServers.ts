/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Hook listing the servers the user can see,
 * for naming the targets of runnable SQL in chat replies.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useContext, useMemo } from 'react';
import ClusterDataContext from '../../contexts/ClusterDataContext';
import type { ClusterGroup } from '../../contexts/ClusterDataContext';
import { collectServers } from '../../utils/clusterHelpers';
import type { KnownServer } from './chatSqlTarget';

/**
 * Flatten the cluster hierarchy into a map of connection ID to server
 * name and default database. Groups with `clusters: null` are skipped
 * (see issue #242).
 */
export const buildKnownServers = (
    clusterData: ClusterGroup[] | undefined,
): Map<number, KnownServer> => {
    const servers = new Map<number, KnownServer>();
    for (const group of clusterData ?? []) {
        for (const cluster of group.clusters ?? []) {
            for (const server of collectServers(cluster.servers ?? [])) {
                if (servers.has(server.id)) {
                    continue;
                }
                servers.set(server.id, {
                    name: server.name,
                    databaseName: server.database_name || undefined,
                });
            }
        }
    }
    return servers;
};

/**
 * Return the servers the user can see, keyed by connection ID. Outside
 * a ClusterDataProvider the map is empty rather than an error, so chat
 * messages still render in isolation.
 */
export const useKnownServers = (): Map<number, KnownServer> => {
    const context = useContext(ClusterDataContext);
    const clusterData = context?.clusterData;
    return useMemo(() => buildKnownServers(clusterData), [clusterData]);
};
