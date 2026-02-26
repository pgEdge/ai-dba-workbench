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
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}));

import MembersPanel from '../MembersPanel';

const CLUSTER_ID = 5;
const CLUSTER_NAME = 'Test Cluster';

const mockMembers = [
    {
        id: 10,
        name: 'Primary',
        host: 'primary.example.com',
        port: 5432,
        status: 'online',
        role: 'binary_primary',
        membership_source: 'auto',
    },
    {
        id: 20,
        name: 'Standby',
        host: 'standby.example.com',
        port: 5432,
        status: 'warning',
        role: 'binary_standby',
        membership_source: 'manual',
    },
];

describe('MembersPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('displays cluster members in a table after loading', async () => {
        mockApiGet.mockResolvedValue(mockMembers);

        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
            expect(screen.getByText('Standby')).toBeInTheDocument();
        });

        // Check that the host:port info is displayed
        expect(
            screen.getByText('primary.example.com:5432'),
        ).toBeInTheDocument();

        // Check membership source chips
        expect(screen.getByText('Auto')).toBeInTheDocument();
        expect(screen.getByText('Manual')).toBeInTheDocument();
    });

    it('shows empty state when no members exist', async () => {
        mockApiGet.mockResolvedValue([]);

        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('No servers in this cluster.'),
            ).toBeInTheDocument();
        });
    });

    it('shows error when API call fails', async () => {
        mockApiGet.mockRejectedValue(
            new Error('Network error'),
        );

        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Network error'),
            ).toBeInTheDocument();
        });
    });

    it('opens add server form when button is clicked', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(mockMembers);
            }
            // Connections list
            return Promise.resolve([
                {
                    id: 30,
                    name: 'Unassigned Server',
                    host: 'standalone.example.com',
                    port: 5432,
                    cluster_name: null,
                },
                {
                    id: 10,
                    name: 'Primary',
                    host: 'primary.example.com',
                    port: 5432,
                    cluster_name: 'Test Cluster',
                },
            ]);
        });

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        await user.click(screen.getByText('Add server'));

        expect(
            screen.getByText('Add a server to this cluster'),
        ).toBeInTheDocument();
    });

    it('opens remove confirmation dialog when remove button is clicked', async () => {
        mockApiGet.mockResolvedValue(mockMembers);

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        // Click the remove button for the first member
        const removeButtons = screen.getAllByLabelText(/Remove .* from cluster/);
        await user.click(removeButtons[0]);

        expect(
            screen.getByText('Remove server from cluster'),
        ).toBeInTheDocument();
        expect(
            screen.getByText(/will become standalone/),
        ).toBeInTheDocument();
    });

    it('calls delete API and refreshes when removal is confirmed', async () => {
        mockApiGet.mockResolvedValue(mockMembers);
        mockApiDelete.mockResolvedValue(undefined);

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        // Click remove for 'Primary'
        const removeButtons = screen.getAllByLabelText(/Remove .* from cluster/);
        await user.click(removeButtons[0]);

        // Confirm removal
        await user.click(screen.getByText('Remove'));

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                `/api/v1/clusters/${CLUSTER_ID}/servers/10`,
            );
        });
    });

    it('calls onMembershipChange after successful add', async () => {
        const onMembershipChange = vi.fn();
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(mockMembers);
            }
            return Promise.resolve([
                {
                    id: 30,
                    name: 'Unassigned Server',
                    host: 'standalone.example.com',
                    port: 5432,
                    cluster_name: null,
                },
            ]);
        });
        mockApiPost.mockResolvedValue({
            connection_id: 30,
            role: null,
        });

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
                onMembershipChange={onMembershipChange}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        // Open add form
        await user.click(screen.getByText('Add server'));

        // Type in the autocomplete
        const serverInput = screen.getByPlaceholderText(
            'Search unassigned servers...',
        );
        await user.click(serverInput);
        await user.type(serverInput, 'Unassigned');

        // Select the option from the dropdown
        await waitFor(() => {
            expect(
                screen.getByText(/Unassigned Server/),
            ).toBeInTheDocument();
        });

        const option = screen.getByText(/Unassigned Server/);
        await user.click(option);

        // Click Add button
        const addButton = screen.getByRole('button', { name: 'Add' });
        await user.click(addButton);

        await waitFor(() => {
            expect(onMembershipChange).toHaveBeenCalled();
        });
    });

    it('calls onMembershipChange after successful removal', async () => {
        const onMembershipChange = vi.fn();
        mockApiGet.mockResolvedValue(mockMembers);
        mockApiDelete.mockResolvedValue(undefined);

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
                onMembershipChange={onMembershipChange}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        // Click remove for 'Primary'
        const removeButtons = screen.getAllByLabelText(
            /Remove .* from cluster/,
        );
        await user.click(removeButtons[0]);

        // Confirm removal
        await user.click(screen.getByText('Remove'));

        await waitFor(() => {
            expect(onMembershipChange).toHaveBeenCalled();
        });
    });

    it('calls add API when a server is added', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(mockMembers);
            }
            return Promise.resolve([
                {
                    id: 30,
                    name: 'Unassigned Server',
                    host: 'standalone.example.com',
                    port: 5432,
                    cluster_name: null,
                },
            ]);
        });
        mockApiPost.mockResolvedValue({
            connection_id: 30,
            role: null,
        });

        const user = userEvent.setup({ delay: null });
        render(
            <MembersPanel
                clusterId={CLUSTER_ID}
                clusterName={CLUSTER_NAME}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Primary')).toBeInTheDocument();
        });

        // Open add form
        await user.click(screen.getByText('Add server'));

        // Type in the autocomplete
        const serverInput = screen.getByPlaceholderText(
            'Search unassigned servers...',
        );
        await user.click(serverInput);
        await user.type(serverInput, 'Unassigned');

        // Select the option from the dropdown
        await waitFor(() => {
            expect(
                screen.getByText(
                    /Unassigned Server/,
                ),
            ).toBeInTheDocument();
        });

        const option = screen.getByText(
            /Unassigned Server/,
        );
        await user.click(option);

        // Click Add button
        const addButton = screen.getByRole('button', { name: 'Add' });
        await user.click(addButton);

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith(
                `/api/v1/clusters/${CLUSTER_ID}/servers`,
                expect.objectContaining({
                    connection_id: 30,
                }),
            );
        });
    });
});
