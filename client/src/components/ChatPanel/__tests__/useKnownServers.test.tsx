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
import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import ClusterDataContext, {
    type ClusterDataContextValue,
    type ClusterGroup,
} from '../../../contexts/ClusterDataContext';
import { buildKnownServers, useKnownServers } from '../useKnownServers';

const clusterData: ClusterGroup[] = [
    {
        id: 'g1',
        name: 'Group',
        clusters: [
            {
                id: 'c1',
                name: 'Cluster',
                servers: [
                    {
                        id: 1,
                        name: 'primary',
                        database_name: 'app',
                        children: [{ id: 2, name: 'standby', database_name: '' }],
                    },
                ],
            },
            {
                id: 'c2',
                name: 'Duplicate',
                servers: [{ id: 1, name: 'primary again' }],
            },
        ],
    },
    { id: 'g2', name: 'Empty', clusters: null },
];

describe('buildKnownServers', () => {
    it('flattens nested servers and keeps the first entry per ID', () => {
        const servers = buildKnownServers(clusterData);
        expect(Array.from(servers.entries())).toEqual([
            [1, { name: 'primary', databaseName: 'app' }],
            [2, { name: 'standby', databaseName: undefined }],
        ]);
    });

    it('returns an empty map without data', () => {
        expect(buildKnownServers(undefined).size).toBe(0);
    });
});

describe('useKnownServers', () => {
    it('returns an empty map outside a ClusterDataProvider', () => {
        const { result } = renderHook(() => useKnownServers());
        expect(result.current.size).toBe(0);
    });

    it('reads servers from the cluster data context', () => {
        const value = { clusterData } as unknown as ClusterDataContextValue;
        const wrapper = ({ children }: { children: React.ReactNode }) => (
            <ClusterDataContext.Provider value={value}>
                {children}
            </ClusterDataContext.Provider>
        );
        const { result } = renderHook(() => useKnownServers(), { wrapper });
        expect(result.current.get(1)?.name).toBe('primary');
    });
});
