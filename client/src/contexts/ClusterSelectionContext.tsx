/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import type React from 'react';
import { createContext, useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { useAuth } from './useAuth';
import { useClusterData } from './useClusterData';
import type { ClusterServer, ClusterEntry } from './ClusterDataContext';
import { apiPost, apiGet, apiDelete } from '../utils/apiClient';
import { logger } from '../utils/logger';

export type SelectionType = 'server' | 'cluster' | 'estate' | null;

export interface CurrentConnection {
    connection_id: number;
    [key: string]: unknown;
}

export interface ClusterSelectionContextValue {
    selectedServer: ClusterServer | null;
    selectedCluster: ClusterEntry | null;
    selectionType: SelectionType;
    currentConnection: CurrentConnection | null;
    selectServer: (server: ClusterServer) => Promise<void>;
    selectCluster: (cluster: ClusterEntry) => void;
    selectEstate: () => void;
    clearSelection: () => Promise<void>;
}

interface ClusterSelectionProviderProps {
    children: React.ReactNode;
}

const ClusterSelectionContext = createContext<ClusterSelectionContextValue | null>(null);

export const ClusterSelectionProvider = ({ children }: ClusterSelectionProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const { clusterData } = useClusterData();
    const [selectedServer, setSelectedServer] = useState<ClusterServer | null>(null);
    const [selectedCluster, setSelectedCluster] = useState<ClusterEntry | null>(null);
    const [selectionType, setSelectionType] = useState<SelectionType>(null);
    const [currentConnection, setCurrentConnection] = useState<CurrentConnection | null>(null);

    /**
     * Select a server and set it as the current connection
     */
    const selectServer = useCallback(async (server: ClusterServer): Promise<void> => {
        if (!user || !server) {return;}

        setSelectedServer(server);
        setSelectedCluster(null);
        setSelectionType('server');

        try {
            const data = await apiPost<CurrentConnection>(
                '/api/v1/connections/current',
                { connection_id: server.id },
            );
            setCurrentConnection(data);
        } catch (err) {
            logger.error('Error setting current connection:', err);
        }
    }, [user]);

    /**
     * Select a cluster (all servers in the cluster)
     */
    const selectCluster = useCallback((cluster: ClusterEntry): void => {
        setSelectedCluster(cluster);
        setSelectedServer(null);
        setCurrentConnection(null);
        setSelectionType('cluster');
    }, []);

    /**
     * Select the entire estate (all servers across all groups)
     */
    const selectEstate = useCallback((): void => {
        setSelectedServer(null);
        setSelectedCluster(null);
        setCurrentConnection(null);
        setSelectionType('estate');
    }, []);

    /**
     * Clear the current selection
     */
    const clearSelection = useCallback(async (): Promise<void> => {
        if (!user) {return;}

        setSelectedServer(null);
        setSelectedCluster(null);
        setSelectionType(null);

        try {
            await apiDelete('/api/v1/connections/current');
            setCurrentConnection(null);
        } catch (err) {
            logger.error('Error clearing current connection:', err);
        }
    }, [user]);

    /**
     * Get the current connection from the server on initial load
     */
    const fetchCurrentConnection = useCallback(async (): Promise<void> => {
        if (!user) {return;}

        try {
            const data = await apiGet<CurrentConnection>('/api/v1/connections/current');
            setCurrentConnection(data);
            // Find and set the selected server from cluster data
            for (const group of clusterData) {
                for (const cluster of group.clusters || []) {
                    const server = cluster.servers?.find(s => s.id === data.connection_id);
                    if (server) {
                        setSelectedServer(server);
                        setSelectionType('server');
                        return;
                    }
                }
            }
        } catch {
            // Ignore errors - current connection might not be set
        }
    }, [user, clusterData]);

    // Clear selection when user logs out
    useEffect(() => {
        if (!user) {
            setSelectedServer(null);
            setSelectedCluster(null);
            setSelectionType(null);
            setCurrentConnection(null);
        }
    }, [user]);

    // Fetch current connection after cluster data is loaded
    useEffect(() => {
        if (clusterData.length > 0) {
            fetchCurrentConnection();
        }
    }, [clusterData, fetchCurrentConnection]);

    // Refs for reading current selection without adding as
    // effect dependencies.
    const selectedClusterRef = useRef(selectedCluster);
    const selectedServerRef = useRef(selectedServer);
    const selectionTypeRef = useRef(selectionType);
    selectedClusterRef.current = selectedCluster;
    selectedServerRef.current = selectedServer;
    selectionTypeRef.current = selectionType;

    // Re-sync selected cluster and server with fresh clusterData
    // references when cluster data refreshes (e.g. after rename).
    useEffect(() => {
        if (clusterData.length === 0) {return;}

        const curCluster = selectedClusterRef.current;
        const curServer = selectedServerRef.current;
        const curType = selectionTypeRef.current;

        if (curCluster && curType === 'cluster') {
            for (const group of clusterData) {
                const fresh = group.clusters?.find(
                    c => c.id === curCluster.id,
                );
                if (fresh && fresh !== curCluster) {
                    setSelectedCluster(fresh);
                    return;
                }
            }
        }

        if (curServer && curType === 'server') {
            for (const group of clusterData) {
                for (const cluster of group.clusters || []) {
                    const findServer = (
                        servers: ClusterServer[],
                    ): ClusterServer | undefined => {
                        for (const s of servers) {
                            if (s.id === curServer.id) {return s;}
                            if (s.children) {
                                const found = findServer(s.children);
                                if (found) {return found;}
                            }
                        }
                        return undefined;
                    };
                    const fresh = findServer(cluster.servers || []);
                    if (fresh && fresh !== curServer) {
                        setSelectedServer(fresh);
                        return;
                    }
                }
            }
        }
    }, [clusterData]);

    const value: ClusterSelectionContextValue = useMemo(() => ({
        // Selection state
        selectedServer,
        selectedCluster,
        selectionType,
        currentConnection,
        // Selection functions
        selectServer,
        selectCluster,
        selectEstate,
        clearSelection,
    }), [
        selectedServer,
        selectedCluster,
        selectionType,
        currentConnection,
        selectServer,
        selectCluster,
        selectEstate,
        clearSelection,
    ]);

    return (
        <ClusterSelectionContext.Provider value={value}>
            {children}
        </ClusterSelectionContext.Provider>
    );
};

export default ClusterSelectionContext;
