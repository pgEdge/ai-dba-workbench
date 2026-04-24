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
import ClusterSelectionContext from './ClusterSelectionContext';
import type { ClusterSelectionContextValue } from './ClusterSelectionContext';

export const useClusterSelection = (): ClusterSelectionContextValue => {
    const context = useContext(ClusterSelectionContext);
    if (!context) {
        throw new Error('useClusterSelection must be used within a ClusterSelectionProvider');
    }
    return context;
};
