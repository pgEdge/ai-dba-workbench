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
import { vi, describe, it, expect, beforeEach } from 'vitest';
import { ThemeProvider } from '@mui/material/styles';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

const theme = createPgedgeTheme('light');

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
 * Render helper that wraps the component with the pgEdge theme.
 */
const renderWithTheme = (ui: React.ReactElement) =>
    render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);

describe('ClusterFields - Edit Mode (Read-Only)', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('shows cluster info when server is assigned to a cluster', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/') && url.endsWith('/cluster')) {
                return Promise.resolve({
                    info: {
                        cluster_id: 1,
                        role: 'spock_node',
                        membership_source: 'manual',
                        cluster_name: 'Test Cluster',
                        replication_type: 'spock',
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

        renderWithTheme(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Test Cluster')).toBeInTheDocument();
        });

        expect(screen.getByText('Node')).toBeInTheDocument();
        expect(screen.getAllByText('Manual').length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText('Configure Cluster')).toBeInTheDocument();
    });

    it('shows unassigned message when server has no cluster', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/') && url.endsWith('/cluster')) {
                return Promise.resolve({
                    info: {
                        cluster_id: null,
                        role: null,
                        membership_source: 'auto',
                        cluster_name: null,
                        replication_type: null,
                        auto_cluster_key: null,
                    },
                    clusters: [],
                });
            }
            return Promise.resolve([]);
        });

        renderWithTheme(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(
                screen.getByText(
                    'This server is not assigned to a cluster.',
                ),
            ).toBeInTheDocument();
        });
    });

    it('does not show relationships section in edit mode', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/') && url.endsWith('/cluster')) {
                return Promise.resolve({
                    info: {
                        cluster_id: 1,
                        role: 'spock_node',
                        membership_source: 'manual',
                        cluster_name: 'Test Cluster',
                        replication_type: 'spock',
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

        renderWithTheme(<ClusterFields mode="edit" serverId={10} />);

        await waitFor(() => {
            expect(screen.getByText('Test Cluster')).toBeInTheDocument();
        });

        expect(
            screen.queryByText('Relationships'),
        ).not.toBeInTheDocument();
    });

    it('calls onOpenClusterConfig when Configure Cluster is clicked', async () => {
        const onOpenClusterConfig = vi.fn();

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/') && url.endsWith('/cluster')) {
                return Promise.resolve({
                    info: {
                        cluster_id: 1,
                        role: 'spock_node',
                        membership_source: 'manual',
                        cluster_name: 'Test Cluster',
                        replication_type: 'spock',
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

        renderWithTheme(
            <ClusterFields
                mode="edit"
                serverId={10}
                onOpenClusterConfig={onOpenClusterConfig}
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Configure Cluster'),
            ).toBeInTheDocument();
        });

        screen.getByText('Configure Cluster').click();
        expect(onOpenClusterConfig).toHaveBeenCalledWith(
            1,
            'Test Cluster',
        );
    });
});

describe('ClusterFields - Create Mode', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('does not show the relationships section in create mode', async () => {
        mockApiGet.mockResolvedValue([]);

        renderWithTheme(
            <ClusterFields
                mode="create"
                value={{
                    clusterId: 1,
                    role: 'spock_node',
                    membershipSource: 'manual',
                }}
                onChange={vi.fn()}
            />,
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        expect(
            screen.queryByText('Relationships'),
        ).not.toBeInTheDocument();
    });
});
