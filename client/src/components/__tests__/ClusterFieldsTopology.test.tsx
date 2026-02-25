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
 * Render helper that wraps the component with the pgEdge theme.
 */
const renderWithTheme = (ui: React.ReactElement) =>
    render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);

/**
 * Configure mock API responses for a server that does not
 * belong to any cluster.
 */
const setupMocksWithoutCluster = () => {
    mockApiGet.mockImplementation((url: string) => {
        if (url.includes('/connections/')) {
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
};

describe('ClusterFields edit mode (read-only)', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('does not render topology diagram in edit mode (now read-only)', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/connections/')) {
                return Promise.resolve({
                    info: {
                        cluster_id: 1,
                        role: 'binary_primary',
                        membership_source: 'manual',
                        cluster_name: 'Test Cluster',
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
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <ClusterFields mode="edit" serverId={10} />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Test Cluster'),
            ).toBeInTheDocument();
        });

        // Topology diagram is no longer rendered in edit mode;
        // it has moved to ClusterConfigDialog
        expect(
            screen.queryByTestId('topology-diagram'),
        ).not.toBeInTheDocument();
    });

    it('does not render topology diagram when no cluster is assigned', async () => {
        setupMocksWithoutCluster();

        renderWithTheme(
            <ClusterFields mode="edit" serverId={10} />,
        );

        await waitFor(() => {
            expect(
                screen.queryByRole('progressbar'),
            ).not.toBeInTheDocument();
        });

        expect(
            screen.queryByTestId('topology-diagram'),
        ).not.toBeInTheDocument();
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
                    membershipSource: 'manual',
                }}
                onChange={vi.fn()}
            />,
        );

        await waitFor(() => {
            expect(
                screen.queryByRole('progressbar'),
            ).not.toBeInTheDocument();
        });

        expect(
            screen.queryByTestId('topology-diagram'),
        ).not.toBeInTheDocument();
    });
});
