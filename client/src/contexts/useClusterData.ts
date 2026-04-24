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
import ClusterDataContext from './ClusterDataContext';
import type { ClusterDataContextValue } from './ClusterDataContext';

export const useClusterData = (): ClusterDataContextValue => {
    const context = useContext(ClusterDataContext);
    if (!context) {
        throw new Error('useClusterData must be used within a ClusterDataProvider');
    }
    return context;
};
