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
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';

// Mock the apiClient module before importing the component
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

import ClusterFields from '../ServerDialog/ClusterFields';

/**
 * Helper that sets up the mock API responses for edit mode with a
 * cluster assignment already in place and the relationships data.
 */
function setupEditModeWithCluster(options?: {
    relationships?: Array<{
        id: number;
        cluster_id: number;
        source_connection_id: number;
        target_connection_id: number;
        source_name: string;
        target_name: string;
        relationship_type: string;
        is_auto_detected: boolean;
    }>;
    servers?: Array<{
        id: number;
        name: string;
        host: string;
        port: number;
        status: string;
        role?: string;
    }>;
    replicationType?: string;
}) {
    const repType = options?.replicationType ?? 'spock';

    // Derive a suitable role for the given replication type
    let defaultRole = 'spock_node';
    if (repType === 'binary') {
        defaultRole = 'binary_standby';
    } else if (repType === 'logical') {
        defaultRole = 'logical_subscriber';
    }

    const relationships = options?.relationships ?? [];
    const servers = options?.servers ?? [
        {
            id: 10,
            name: 'Node A',
            host: 'node-a.example.com',
            port: 5432,
            status: 'ok',
            role: defaultRole,
        },
        {
            id: 20,
            name: 'Node B',
            host: 'node-b.example.com',
            port: 5432,
            status: 'ok',
            role: defaultRole,
        },
        {
            id: 30,
            name: 'Node C',
            host: 'node-c.example.com',
            port: 5432,
            status: 'ok',
            role: defaultRole,
        },
    ];

    mockApiGet.mockImplementation((url: string) => {
        if (url.includes('/connections/') && url.endsWith('/cluster')) {
            return Promise.resolve({
                info: {
                    cluster_id: 1,
                    role: defaultRole,
                    cluster_override: true,
                    cluster_name: 'Test Cluster',
                    replication_type: repType,
                    auto_cluster_key: null,
                },
                clusters: [
                    {
                        id: 1,
                        name: 'Test Cluster',
                        replication_type: repType,
                        auto_cluster_key: null,
                    },
                ],
            });
        }
        if (url.includes('/clusters/') && url.endsWith('/relationships')) {
            return Promise.resolve(relationships);
        }
        if (url.includes('/clusters/') && url.endsWith('/servers')) {
            return Promise.resolve(servers);
        }
        return Promise.resolve([]);
    });
}

describe('ClusterFields - Relationships', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('shows the relationships section when cluster and role are set in edit mode', async () => {
        setupEditModeWithCluster();

        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });
    });

    it('does not show the relationships section in create mode', async () => {
        mockApiGet.mockResolvedValue([]);

        render(
            <ClusterFields
                mode="create"
                value={{
                    clusterId: 1,
                    role: 'spock_node',
                    clusterOverride: true,
                }}
                onChange={vi.fn()}
            />,
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        expect(screen.queryByText('Relationships')).not.toBeInTheDocument();
    });

    it('does not show the relationships section when no cluster is assigned', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/') && url.endsWith('/cluster')) {
                return Promise.resolve({
                    info: {
                        cluster_id: null,
                        role: null,
                        cluster_override: false,
                        cluster_name: null,
                        replication_type: null,
                        auto_cluster_key: null,
                    },
                    clusters: [
                        {
                            id: 1,
                            name: 'Test Cluster',
                            replication_type: 'spock',
                            auto_cluster_key: null,
                        },
                    ],
                });
            }
            return Promise.resolve([]);
        });

        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        expect(screen.queryByText('Relationships')).not.toBeInTheDocument();
    });

    it('displays existing relationships with auto-detected badges', async () => {
        setupEditModeWithCluster({
            relationships: [
                {
                    id: 100,
                    cluster_id: 1,
                    source_connection_id: 10,
                    target_connection_id: 20,
                    source_name: 'Node A',
                    target_name: 'Node B',
                    relationship_type: 'replicates_with',
                    is_auto_detected: true,
                },
                {
                    id: 101,
                    cluster_id: 1,
                    source_connection_id: 10,
                    target_connection_id: 30,
                    source_name: 'Node A',
                    target_name: 'Node C',
                    relationship_type: 'replicates_with',
                    is_auto_detected: false,
                },
            ],
        });

        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Both relationships should be visible
        expect(screen.getByText('Node B')).toBeInTheDocument();
        expect(screen.getByText('Node C')).toBeInTheDocument();

        // Only the auto-detected one should have an "Auto" badge
        const autoBadges = screen.getAllByText('Auto');
        expect(autoBadges).toHaveLength(1);
    });

    it('shows "No relationships defined" when there are none', async () => {
        setupEditModeWithCluster({ relationships: [] });

        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(
                screen.getByText('No relationships defined.'),
            ).toBeInTheDocument();
        });
    });

    it('shows available cluster members in the add dropdown', async () => {
        // Server 10 is the current server; servers 20 and 30 should
        // appear as available targets
        setupEditModeWithCluster({ relationships: [] });

        const user = userEvent.setup({ delay: null });
        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Open the target server dropdown using its data-testid
        const serverSelectControl = screen.getByTestId('relationship-server-select');
        const targetSelect = within(serverSelectControl).getByRole('combobox');
        await user.click(targetSelect);

        // The available targets should be listed (excluding current
        // server with id=10)
        await waitFor(() => {
            const listbox = screen.getByRole('listbox');
            expect(within(listbox).getByText('Node B')).toBeInTheDocument();
            expect(within(listbox).getByText('Node C')).toBeInTheDocument();
            expect(
                within(listbox).queryByText('Node A'),
            ).not.toBeInTheDocument();
        });
    });

    it('excludes servers that already have a relationship from the dropdown', async () => {
        setupEditModeWithCluster({
            relationships: [
                {
                    id: 100,
                    cluster_id: 1,
                    source_connection_id: 10,
                    target_connection_id: 20,
                    source_name: 'Node A',
                    target_name: 'Node B',
                    relationship_type: 'replicates_with',
                    is_auto_detected: false,
                },
            ],
        });

        const user = userEvent.setup({ delay: null });
        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Open the dropdown using its data-testid
        const serverSelectControl = screen.getByTestId('relationship-server-select');
        const targetSelect = within(serverSelectControl).getByRole('combobox');
        await user.click(targetSelect);

        // Node B already has a relationship, so it should not appear
        await waitFor(() => {
            const listbox = screen.getByRole('listbox');
            expect(
                within(listbox).queryByText('Node B'),
            ).not.toBeInTheDocument();
            expect(within(listbox).getByText('Node C')).toBeInTheDocument();
        });
    });

    it('displays relationship type labels correctly for binary clusters', async () => {
        setupEditModeWithCluster({
            replicationType: 'binary',
            relationships: [
                {
                    id: 100,
                    cluster_id: 1,
                    source_connection_id: 10,
                    target_connection_id: 20,
                    source_name: 'Node A',
                    target_name: 'Node B',
                    relationship_type: 'streams_from',
                    is_auto_detected: false,
                },
            ],
            servers: [
                {
                    id: 10,
                    name: 'Node A',
                    host: 'a.example.com',
                    port: 5432,
                    status: 'ok',
                    role: 'binary_standby',
                },
                {
                    id: 20,
                    name: 'Node B',
                    host: 'b.example.com',
                    port: 5432,
                    status: 'ok',
                    role: 'binary_primary',
                },
            ],
        });

        render(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText(/Streams from/)).toBeInTheDocument();
        });
    });
});
