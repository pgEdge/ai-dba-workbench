/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@mui/material/styles';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterFields from '../ServerDialog/ClusterFields';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

const theme = createPgedgeTheme('light');

/**
 * Mock apiGet / apiPut / apiPost / apiDelete so the component does
 * not make real HTTP requests.
 */
const mockApiGet = vi.fn();
const mockApiPut = vi.fn();
const mockApiPost = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

/**
 * Mock ResizeObserver since jsdom does not implement it.
 */
class MockResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
}

beforeEach(() => {
    vi.stubGlobal('ResizeObserver', MockResizeObserver);
});

/**
 * Render helper that wraps the component with the pgEdge theme.
 */
const renderWithTheme = (ui: React.ReactElement) =>
    render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);

/**
 * Build the standard cluster-info response returned by
 * GET /api/v1/connections/:id/cluster.
 */
const makeClusterInfoResponse = (
    clusterId: number | null,
    role: string | null,
) => ({
    info: {
        cluster_id: clusterId,
        role,
        cluster_override: clusterId !== null,
        cluster_name: clusterId !== null ? 'Test Cluster' : null,
        replication_type: 'binary',
        auto_cluster_key: null,
    },
    clusters: [
        {
            id: 1,
            name: 'Test Cluster',
            replication_type: 'binary',
            auto_cluster_key: null,
        },
    ],
});

/**
 * Build a topology response with two servers in a binary cluster.
 */
const makeTopologyResponse = () => [
    {
        id: '1',
        name: 'Default',
        is_default: true,
        clusters: [
            {
                id: '1',
                name: 'Test Cluster',
                cluster_type: 'binary',
                servers: [
                    {
                        id: 10,
                        name: 'primary-server',
                        status: 'online',
                        primary_role: 'binary_primary',
                        host: 'primary.example.com',
                        port: 5432,
                        children: [
                            {
                                id: 20,
                                name: 'standby-server',
                                status: 'online',
                                primary_role: 'binary_standby',
                                host: 'standby.example.com',
                                port: 5432,
                            },
                        ],
                    },
                ],
            },
        ],
    },
];

/**
 * Configure mock API responses for a server that belongs to
 * cluster 1 with a binary_primary role.
 */
const setupMocksWithCluster = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url.includes('/connections/')) {
            return Promise.resolve(
                makeClusterInfoResponse(1, 'binary_primary'),
            );
        }
        if (url === '/api/v1/clusters') {
            return Promise.resolve(makeTopologyResponse());
        }
        if (url.includes('/relationships')) {
            return Promise.resolve([]);
        }
        if (url.includes('/servers')) {
            return Promise.resolve([
                {
                    id: 10,
                    name: 'primary-server',
                    host: 'primary.example.com',
                    port: 5432,
                    status: 'online',
                },
                {
                    id: 20,
                    name: 'standby-server',
                    host: 'standby.example.com',
                    port: 5432,
                    status: 'online',
                },
            ]);
        }
        return Promise.resolve([]);
    });
};

/**
 * Configure mock API responses for a server that does not
 * belong to any cluster.
 */
const setupMocksWithoutCluster = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url.includes('/connections/')) {
            return Promise.resolve(
                makeClusterInfoResponse(null, null),
            );
        }
        if (url === '/api/v1/clusters') {
            return Promise.resolve([]);
        }
        return Promise.resolve([]);
    });
};

describe('ClusterFields topology diagram', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders topology diagram when server is assigned to a cluster', async () => {
        setupMocksWithCluster();

        renderWithTheme(
            <ClusterFields mode="edit" serverId={10} />,
        );

        await waitFor(() => {
            expect(
                screen.getByTestId('topology-diagram'),
            ).toBeInTheDocument();
        });
    });

    it('does not render topology diagram when no cluster is assigned', async () => {
        setupMocksWithoutCluster();

        renderWithTheme(
            <ClusterFields mode="edit" serverId={10} />,
        );

        // Wait for the component to finish loading
        await waitFor(() => {
            expect(
                screen.queryByRole('progressbar'),
            ).not.toBeInTheDocument();
        });

        expect(
            screen.queryByTestId('topology-diagram'),
        ).not.toBeInTheDocument();
    });

    it('highlights the current server in the topology diagram', async () => {
        setupMocksWithCluster();

        renderWithTheme(
            <ClusterFields mode="edit" serverId={10} />,
        );

        await waitFor(() => {
            expect(
                screen.getByTestId('topology-diagram'),
            ).toBeInTheDocument();
        });

        // The highlighted node (serverId=10) renders with an
        // aria-label containing the server name.
        const highlightedNode = screen.getByRole('button', {
            name: /select server primary-server/i,
        });
        expect(highlightedNode).toBeInTheDocument();

        // The non-highlighted node should also be present.
        const otherNode = screen.getByRole('button', {
            name: /select server standby-server/i,
        });
        expect(otherNode).toBeInTheDocument();
    });

    it('does not render topology diagram in create mode', async () => {
        mockApiGet.mockResolvedValue([
            {
                id: 1,
                name: 'Test Cluster',
                replication_type: 'binary',
                auto_cluster_key: null,
            },
        ]);

        renderWithTheme(
            <ClusterFields
                mode="create"
                value={{
                    clusterId: 1,
                    role: 'binary_primary',
                    clusterOverride: true,
                }}
                onChange={vi.fn()}
            />,
        );

        // Wait for loading to complete
        await waitFor(() => {
            expect(
                screen.queryByRole('progressbar'),
            ).not.toBeInTheDocument();
        });

        // Topology diagram should not appear in create mode
        expect(
            screen.queryByTestId('topology-diagram'),
        ).not.toBeInTheDocument();
    });
});
