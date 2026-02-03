/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import React, { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { useAuth } from './AuthContext';
import { useClusterData, ClusterServer, ClusterEntry } from './ClusterDataContext';

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
        if (!user || !server) return;

        setSelectedServer(server);
        setSelectedCluster(null);
        setSelectionType('server');

        try {
            const response = await fetch('/api/v1/connections/current', {
                method: 'POST',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    connection_id: server.id,
                }),
            });

            if (response.ok) {
                const data: CurrentConnection = await response.json();
                setCurrentConnection(data);
            } else {
                console.error('Failed to set current connection');
            }
        } catch (err) {
            console.error('Error setting current connection:', err);
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
        if (!user) return;

        setSelectedServer(null);
        setSelectedCluster(null);
        setSelectionType(null);

        try {
            await fetch('/api/v1/connections/current', {
                method: 'DELETE',
                credentials: 'include',
            });
            setCurrentConnection(null);
        } catch (err) {
            console.error('Error clearing current connection:', err);
        }
    }, [user]);

    /**
     * Get the current connection from the server on initial load
     */
    const fetchCurrentConnection = useCallback(async (): Promise<void> => {
        if (!user) return;

        try {
            const response = await fetch('/api/v1/connections/current', {
                credentials: 'include',
            });

            if (response.ok) {
                const data: CurrentConnection = await response.json();
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
            }
        } catch (_err) {
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

    const value: ClusterSelectionContextValue = {
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
    };

    return (
        <ClusterSelectionContext.Provider value={value}>
            {children}
        </ClusterSelectionContext.Provider>
    );
};

export const useClusterSelection = (): ClusterSelectionContextValue => {
    const context = useContext(ClusterSelectionContext);
    if (!context) {
        throw new Error('useClusterSelection must be used within a ClusterSelectionProvider');
    }
    return context;
};

export default ClusterSelectionContext;
