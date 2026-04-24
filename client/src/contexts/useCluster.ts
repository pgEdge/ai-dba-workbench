/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { useContext } from 'react';
import ClusterCombinedContext from './ClusterContext';
import type { ClusterCombinedContextValue } from './ClusterContext';

// Re-export the individual hooks for focused usage
export { useClusterData } from './useClusterData';
export { useClusterSelection } from './useClusterSelection';
export { useClusterActions } from './useClusterActions';

export const useCluster = (): ClusterCombinedContextValue => {
    const context = useContext(ClusterCombinedContext);
    if (!context) {
        throw new Error('useCluster must be used within a ClusterProvider');
    }
    return context;
};
