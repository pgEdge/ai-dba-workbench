/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Cluster Context
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Combined provider and hook for cluster management.
 * This module provides backward compatibility by combining three focused
 * contexts: ClusterDataContext, ClusterSelectionContext, and
 * ClusterActionsContext.
 *
 *-------------------------------------------------------------------------
 */
import type React from 'react';
import { createContext } from 'react';
import { ClusterDataProvider } from './ClusterDataContext';
import type { ClusterDataContextValue } from './ClusterDataContext';
import { useClusterData } from './useClusterData';
import { ClusterSelectionProvider } from './ClusterSelectionContext';
import type { ClusterSelectionContextValue } from './ClusterSelectionContext';
import { useClusterSelection } from './useClusterSelection';
import { ClusterActionsProvider } from './ClusterActionsContext';
import type { ClusterActionsContextValue } from './ClusterActionsContext';
import { useClusterActions } from './useClusterActions';

export type ClusterCombinedContextValue =
    ClusterDataContextValue &
    ClusterSelectionContextValue &
    ClusterActionsContextValue;

/**
 * Internal context for the combined hook.
 * This is used to detect if we're inside a ClusterProvider.
 */
const ClusterCombinedContext = createContext<ClusterCombinedContextValue | null>(null);

interface ClusterProviderProps {
    children: React.ReactNode;
}

/**
 * Internal component that provides the combined context value.
 * Must be rendered inside all three focused providers.
 */
const ClusterCombinedProvider = ({ children }: ClusterProviderProps): React.ReactElement => {
    const dataContext = useClusterData();
    const selectionContext = useClusterSelection();
    const actionsContext = useClusterActions();

    // Combine all context values for backward compatibility
    const combinedValue: ClusterCombinedContextValue = {
        // From ClusterDataContext
        clusterData: dataContext.clusterData,
        loading: dataContext.loading,
        error: dataContext.error,
        lastRefresh: dataContext.lastRefresh,
        autoRefreshEnabled: dataContext.autoRefreshEnabled,
        setAutoRefreshEnabled: dataContext.setAutoRefreshEnabled,
        fetchClusterData: dataContext.fetchClusterData,

        // From ClusterSelectionContext
        selectedServer: selectionContext.selectedServer,
        selectedCluster: selectionContext.selectedCluster,
        selectionType: selectionContext.selectionType,
        currentConnection: selectionContext.currentConnection,
        selectServer: selectionContext.selectServer,
        selectCluster: selectionContext.selectCluster,
        selectEstate: selectionContext.selectEstate,
        clearSelection: selectionContext.clearSelection,

        // From ClusterActionsContext
        updateGroupName: actionsContext.updateGroupName,
        updateClusterName: actionsContext.updateClusterName,
        updateServerName: actionsContext.updateServerName,
        getServer: actionsContext.getServer,
        createServer: actionsContext.createServer,
        updateServer: actionsContext.updateServer,
        deleteServer: actionsContext.deleteServer,
        deleteCluster: actionsContext.deleteCluster,
        createGroup: actionsContext.createGroup,
        deleteGroup: actionsContext.deleteGroup,
        moveClusterToGroup: actionsContext.moveClusterToGroup,
    };

    return (
        <ClusterCombinedContext.Provider value={combinedValue}>
            {children}
        </ClusterCombinedContext.Provider>
    );
};

/**
 * ClusterProvider - Combined provider that wraps all three focused contexts.
 *
 * Provider hierarchy (innermost to outermost):
 * 1. ClusterDataProvider - provides clusterData and fetchClusterData
 * 2. ClusterSelectionProvider - needs clusterData for fetchCurrentConnection
 * 3. ClusterActionsProvider - needs fetchClusterData and selectedServer
 * 4. ClusterCombinedProvider - combines all for backward compatibility
 */
export const ClusterProvider = ({ children }: ClusterProviderProps): React.ReactElement => {
    return (
        <ClusterDataProvider>
            <ClusterSelectionProvider>
                <ClusterActionsProvider>
                    <ClusterCombinedProvider>
                        {children}
                    </ClusterCombinedProvider>
                </ClusterActionsProvider>
            </ClusterSelectionProvider>
        </ClusterDataProvider>
    );
};

export default ClusterCombinedContext;
