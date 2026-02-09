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
/* eslint-disable react-refresh/only-export-components */

import React, { createContext, useContext } from 'react';
import { ClusterDataProvider, useClusterData, ClusterDataContextValue } from './ClusterDataContext';
import { ClusterSelectionProvider, useClusterSelection, ClusterSelectionContextValue } from './ClusterSelectionContext';
import { ClusterActionsProvider, useClusterActions, ClusterActionsContextValue } from './ClusterActionsContext';

// Re-export the helper functions for external use
export {
    generateDataFingerprint,
    collectServerFingerprints,
    transformConnectionsToHierarchy,
} from './ClusterDataContext';

// Re-export the individual hooks for focused usage
export { useClusterData } from './ClusterDataContext';
export { useClusterSelection } from './ClusterSelectionContext';
export { useClusterActions } from './ClusterActionsContext';

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

/**
 * useCluster - Combined hook for backward compatibility.
 *
 * Returns all values from ClusterDataContext, ClusterSelectionContext,
 * and ClusterActionsContext combined into a single object.
 *
 * For new code, prefer using the focused hooks:
 * - useClusterData() - for data and refresh state
 * - useClusterSelection() - for selection state
 * - useClusterActions() - for CRUD operations
 */
export const useCluster = (): ClusterCombinedContextValue => {
    const context = useContext(ClusterCombinedContext);
    if (!context) {
        throw new Error('useCluster must be used within a ClusterProvider');
    }
    return context;
};

export default ClusterCombinedContext;
