/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach, Mock } from 'vitest';
import renderWithTheme from '../../test/renderWithTheme';
import TopologyPanel from '../TopologyPanel';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import type {
    NodeRelationship,
    ClusterServerInfo,
} from '../ServerDialog/ServerDialog.types';

// Mock the API client
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    apiPost: vi.fn(),
    apiPut: vi.fn(),
    apiDelete: vi.fn(),
}));

// Mock the TopologyDiagram component since it is complex SVG rendering
vi.mock('../Dashboard/ClusterDashboard/topology/TopologyDiagram', () => ({
    default: ({ servers }: { servers: unknown[] }) => (
        <div data-testid="topology-diagram">
            Topology Diagram with {servers.length} servers
        </div>
    ),
}));

const mockApiGet = apiGet as Mock;
const mockApiPost = apiPost as Mock;
const mockApiPut = apiPut as Mock;
const mockApiDelete = apiDelete as Mock;

const createMockServer = (
    overrides: Partial<ClusterServerInfo> = {},
): ClusterServerInfo => ({
    id: 1,
    name: 'server-1',
    host: 'host1.example.com',
    port: 5432,
    status: 'online',
    role: 'binary_primary',
    ...overrides,
});

const createMockRelationship = (
    overrides: Partial<NodeRelationship> = {},
): NodeRelationship => ({
    id: 10,
    cluster_id: 1,
    source_connection_id: 1,
    target_connection_id: 2,
    source_name: 'server-1',
    target_name: 'server-2',
    relationship_type: 'streams_from',
    is_auto_detected: false,
    ...overrides,
});

interface MockUnassigned {
    id: number;
    name: string;
    host: string;
    port: number;
    cluster_id?: number | null;
    membership_source?: string;
}

const createMockUnassigned = (
    overrides: Partial<MockUnassigned> = {},
): MockUnassigned => ({
    id: 100,
    name: 'unassigned-server',
    host: 'unassigned.example.com',
    port: 5432,
    cluster_id: null,
    ...overrides,
});

describe('TopologyPanel', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        // Default mock implementations
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
    });

    afterEach(() => {
        vi.resetAllMocks();
    });

    it('displays loading spinner while fetching data', async () => {
        // Make the API call hang to show loading state
        mockApiGet.mockImplementation(
            () => new Promise(() => {}), // Never resolves
        );

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('displays error alert and allows dismissal', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.reject(new Error('Failed to connect'));
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Failed to connect')).toBeInTheDocument();
        });

        const closeButton = screen.getByRole('button', { name: /close/i });
        fireEvent.click(closeButton);

        await waitFor(() => {
            expect(
                screen.queryByText('Failed to connect'),
            ).not.toBeInTheDocument();
        });
    });

    it('displays fallback error message when non-Error is thrown', async () => {
        // Reject with a non-Error value to test the fallback message
        mockApiGet.mockImplementation((url: string) => {
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.reject('Some string error');
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Failed to load cluster topology'),
            ).toBeInTheDocument();
        });
    });

    it('displays error when fetchUnassigned fails', async () => {
        // Make servers/relationships succeed but connections fail
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.reject(new Error('Connection fetch failed'));
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Connection fetch failed'),
            ).toBeInTheDocument();
        });
    });

    it('displays fallback error for fetchUnassigned non-Error', async () => {
        // Make servers/relationships succeed but connections fail with non-Error
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.reject('string error');
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText('Failed to load available servers'),
            ).toBeInTheDocument();
        });
    });

    it('displays empty state when no servers in cluster', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText(/No servers in this cluster/),
            ).toBeInTheDocument();
        });
    });

    it('displays topology diagram when servers exist', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByTestId('topology-diagram')).toBeInTheDocument();
        });
    });

    it('displays server list in management section', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-alpha', role: 'binary_primary' }),
            createMockServer({ id: 2, name: 'server-beta', role: 'binary_standby' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-alpha')).toBeInTheDocument();
            expect(screen.getByText('server-beta')).toBeInTheDocument();
        });
    });

    it('adds server to cluster successfully', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
            host: 'new.example.com',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });
        mockApiPost.mockResolvedValue({});

        const onMembershipChange = vi.fn();

        // Use null replicationType to avoid role dropdown (one less combobox)
        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
                onMembershipChange={onMembershipChange}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        // Open autocomplete - get by placeholder which is unique to autocomplete
        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText(/new-server/));

        // Click Add button
        const addButton = screen.getByRole('button', { name: /Add/i });
        fireEvent.click(addButton);

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith(
                '/api/v1/clusters/1/servers',
                expect.objectContaining({ connection_id: 100 }),
            );
        });

        await waitFor(() => {
            expect(onMembershipChange).toHaveBeenCalled();
        });
    });

    it('displays success message after adding server', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });
        mockApiPost.mockResolvedValue({});

        // Use null replicationType to avoid role dropdown
        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        // Select and add server - use placeholder to find the autocomplete input
        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText(/new-server/));

        fireEvent.click(screen.getByRole('button', { name: /Add/i }));

        await waitFor(() => {
            expect(
                screen.getByText(/new-server added to test-cluster/i),
            ).toBeInTheDocument();
        });
    });

    it('displays error when adding server fails', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });
        mockApiPost.mockRejectedValue(new Error('Server already in cluster'));

        // Use null replicationType to avoid role dropdown
        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText(/new-server/));

        fireEvent.click(screen.getByRole('button', { name: /Add/i }));

        await waitFor(() => {
            expect(
                screen.getByText('Server already in cluster'),
            ).toBeInTheDocument();
        });
    });

    it('displays fallback error when adding server fails with non-Error', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });
        mockApiPost.mockRejectedValue('string error');

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText(/new-server/));

        fireEvent.click(screen.getByRole('button', { name: /Add/i }));

        await waitFor(() => {
            expect(
                screen.getByText('Failed to add server to cluster'),
            ).toBeInTheDocument();
        });
    });

    it('displays error when removing server fails', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-to-remove' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockRejectedValue(new Error('Cannot remove server'));

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-to-remove')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-to-remove from cluster',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Remove' }),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

        await waitFor(() => {
            expect(screen.getByText('Cannot remove server')).toBeInTheDocument();
        });
    });

    it('displays fallback error when removing server fails with non-Error', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-to-remove' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockRejectedValue('string error');

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-to-remove')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-to-remove from cluster',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Remove' }),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

        await waitFor(() => {
            expect(
                screen.getByText('Failed to remove server from cluster'),
            ).toBeInTheDocument();
        });
    });

    it('opens remove confirmation dialog when remove button clicked', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-to-remove' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-to-remove')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-to-remove from cluster',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText('Remove server from cluster'),
            ).toBeInTheDocument();
        });
    });

    it('removes server after confirmation', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-to-remove' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockResolvedValue({});

        const onMembershipChange = vi.fn();

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
                onMembershipChange={onMembershipChange}
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-to-remove')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-to-remove from cluster',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Remove' }),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                '/api/v1/clusters/1/servers/1',
            );
        });

        await waitFor(() => {
            expect(onMembershipChange).toHaveBeenCalled();
        });
    });

    it('cancels server removal when Cancel is clicked', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-1' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-1')).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-1 from cluster',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Cancel' }),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        await waitFor(() => {
            expect(mockApiDelete).not.toHaveBeenCalled();
        });
    });

    it('hides relationship section when fewer than 2 servers', async () => {
        const servers = [createMockServer({ id: 1, name: 'server-1' })];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('server-1')).toBeInTheDocument();
        });

        expect(screen.queryByText('Relationships')).not.toBeInTheDocument();
    });

    it('shows relationship section when 2 or more servers', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });
    });

    it('displays existing relationships', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'db-primary' }),
            createMockServer({ id: 2, name: 'db-standby' }),
        ];
        const relationships = [
            createMockRelationship({
                id: 10,
                source_name: 'db-primary',
                target_name: 'db-standby',
                relationship_type: 'streams_from',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve(relationships);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            // Scope assertion to the Relationships section by finding
            // the relationship-specific delete button aria-label
            expect(
                screen.getByRole('button', {
                    name: 'Remove relationship between db-primary and db-standby',
                }),
            ).toBeInTheDocument();
        });
    });

    it('renders relationship controls when 2+ servers exist', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-source' }),
            createMockServer({ id: 2, name: 'server-target' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Verify relationship controls are present
        expect(screen.getByLabelText('Source')).toBeInTheDocument();
        expect(screen.getByLabelText('Target')).toBeInTheDocument();
        expect(screen.getByLabelText('Type')).toBeInTheDocument();
    });

    it('deletes relationship successfully', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const relationships = [
            createMockRelationship({
                id: 42,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve(relationships);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockResolvedValue({});

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', {
                    name: 'Remove relationship between server-1 and server-2',
                }),
            ).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove relationship between server-1 and server-2',
            }),
        );

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith(
                '/api/v1/clusters/1/relationships/42',
            );
        });
    });

    it('displays error when delete relationship fails', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const relationships = [
            createMockRelationship({
                id: 42,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve(relationships);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockRejectedValue(new Error('Cannot delete relationship'));

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', {
                    name: 'Remove relationship between server-1 and server-2',
                }),
            ).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove relationship between server-1 and server-2',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText('Cannot delete relationship'),
            ).toBeInTheDocument();
        });
    });

    it('displays fallback error when delete relationship fails with non-Error', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const relationships = [
            createMockRelationship({
                id: 42,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve(relationships);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });
        mockApiDelete.mockRejectedValue('string error');

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByRole('button', {
                    name: 'Remove relationship between server-1 and server-2',
                }),
            ).toBeInTheDocument();
        });

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove relationship between server-1 and server-2',
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText('Failed to remove relationship'),
            ).toBeInTheDocument();
        });
    });

    it('displays "All members already have this relationship type" when fully meshed', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const relationships = [
            createMockRelationship({
                id: 10,
                source_connection_id: 1,
                target_connection_id: 2,
                source_name: 'server-1',
                target_name: 'server-2',
            }),
            createMockRelationship({
                id: 11,
                source_connection_id: 2,
                target_connection_id: 1,
                source_name: 'server-2',
                target_name: 'server-1',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve(relationships);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(
                screen.getByText(
                    'All members already have this relationship type.',
                ),
            ).toBeInTheDocument();
        });
    });

    it('displays role dropdown for binary replication type', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([createMockUnassigned()]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Role')).toBeInTheDocument();
        });
    });

    it('derives binary replication type from autoClusterKey with sysid prefix', async () => {
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([createMockUnassigned()]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
                autoClusterKey="sysid:123456789"
            />,
        );

        await waitFor(() => {
            // Role dropdown should be present when replication type is derived
            expect(screen.getByLabelText('Role')).toBeInTheDocument();
        });
    });

    it('renders role dropdown for binary replication type', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        // Verify role dropdown is rendered for binary replication type
        expect(screen.getByLabelText('Role')).toBeInTheDocument();
        expect(screen.getByLabelText('Role')).toHaveAttribute(
            'aria-haspopup',
            'listbox',
        );
    });

    it('clears success message when close button is clicked', async () => {
        const unassigned = createMockUnassigned({
            id: 100,
            name: 'new-server',
        });

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([unassigned]);
            }
            return Promise.resolve([]);
        });
        mockApiPost.mockResolvedValue({});

        // Use null replicationType to avoid role dropdown
        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        // Select and add server - use placeholder to find autocomplete
        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText(/new-server/));

        fireEvent.click(screen.getByRole('button', { name: /Add/i }));

        await waitFor(() => {
            expect(
                screen.getByText(/new-server added to test-cluster/i),
            ).toBeInTheDocument();
        });

        // Close success message
        const closeButtons = screen.getAllByRole('button', { name: /close/i });
        fireEvent.click(closeButtons[0]);

        await waitFor(() => {
            expect(
                screen.queryByText(/new-server added to test-cluster/i),
            ).not.toBeInTheDocument();
        });
    });

    it('filters out connections already assigned to other clusters', async () => {
        const connections = [
            createMockUnassigned({
                id: 1,
                name: 'free-server',
                cluster_id: null,
            }),
            createMockUnassigned({
                id: 2,
                name: 'manual-assigned-server',
                cluster_id: 99,
                membership_source: 'manual',
            }),
            createMockUnassigned({
                id: 3,
                name: 'auto-assigned-server',
                cluster_id: 99,
                membership_source: 'auto',
            }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve([]);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve(connections);
            }
            return Promise.resolve([]);
        });

        // Use null replicationType to avoid role dropdown
        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType={null}
            />,
        );

        await waitFor(() => {
            expect(screen.getByLabelText('Server')).toBeInTheDocument();
        });

        const serverInput = screen.getByPlaceholderText('Search unassigned servers...');
        fireEvent.mouseDown(serverInput);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const listbox = within(screen.getByRole('listbox'));

        // Unassigned server should be available
        expect(listbox.getByText(/free-server/)).toBeInTheDocument();

        // Auto-assigned to another cluster should be available
        expect(listbox.getByText(/auto-assigned-server/)).toBeInTheDocument();

        // Manually assigned to another cluster should NOT be available
        expect(listbox.queryByText(/manual-assigned-server/)).not.toBeInTheDocument();
    });

    it('calls handleSourceChange when source select is changed', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-alpha' }),
            createMockServer({ id: 2, name: 'server-beta' }),
            createMockServer({ id: 3, name: 'server-gamma' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        // Wait for the relationship section to appear
        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Open the Source dropdown and select a server
        const sourceSelect = screen.getByLabelText('Source');
        fireEvent.mouseDown(sourceSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        // Click on the first server option
        const listbox = within(screen.getByRole('listbox'));
        fireEvent.click(listbox.getByText('server-alpha'));

        // After selecting source, verify Target dropdown is now enabled
        // (the handler sets selectedSourceId and resets selectedTargetId)
        await waitFor(() => {
            const targetSelect = screen.getByLabelText('Target');
            expect(targetSelect).not.toBeDisabled();
        });
    });

    it('calls handleRelTypeChange when type select is changed', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-one' }),
            createMockServer({ id: 2, name: 'server-two' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        // Wait for the relationship section to appear
        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // First, select a source to enable target selection
        const sourceSelect = screen.getByLabelText('Source');
        fireEvent.mouseDown(sourceSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        fireEvent.click(within(screen.getByRole('listbox')).getByText('server-one'));

        // Wait for listbox to close and source to be selected
        await waitFor(() => {
            expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
        });

        await waitFor(() => {
            const targetSelect = screen.getByLabelText('Target');
            expect(targetSelect).not.toBeDisabled();
        });

        // Now change the relationship type - this should reset the target
        // (even though no target is selected yet, the handler is still called)
        const typeSelect = screen.getByLabelText('Type');
        fireEvent.mouseDown(typeSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        // Select "Subscribes to (logical)" which is different from default
        fireEvent.click(
            within(screen.getByRole('listbox')).getByText('Subscribes to (logical)'),
        );

        // Wait for listbox to close
        await waitFor(() => {
            expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
        });

        // The Type select should now show the selected value
        // This confirms handleRelTypeChange was called
        await waitFor(() => {
            expect(screen.getByLabelText('Type')).toHaveTextContent(
                'Subscribes to (logical)',
            );
        });
    });

    it('resets target when source changes to a different server', async () => {
        const servers = [
            createMockServer({ id: 1, name: 'primary-server' }),
            createMockServer({ id: 2, name: 'standby-server' }),
            createMockServer({ id: 3, name: 'third-server' }),
        ];

        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/servers')) {
                return Promise.resolve(servers);
            }
            if (url.includes('/relationships')) {
                return Promise.resolve([]);
            }
            if (url === '/api/v1/connections') {
                return Promise.resolve([]);
            }
            return Promise.resolve([]);
        });

        renderWithTheme(
            <TopologyPanel
                clusterId={1}
                clusterName="test-cluster"
                replicationType="binary"
            />,
        );

        await waitFor(() => {
            expect(screen.getByText('Relationships')).toBeInTheDocument();
        });

        // Select first source
        const sourceSelect = screen.getByLabelText('Source');
        fireEvent.mouseDown(sourceSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        fireEvent.click(
            within(screen.getByRole('listbox')).getByText('primary-server'),
        );

        // Wait for listbox to close
        await waitFor(() => {
            expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
        });

        // Wait for target to be enabled
        await waitFor(() => {
            expect(screen.getByLabelText('Target')).not.toBeDisabled();
        });

        // Select a target
        const targetSelect = screen.getByLabelText('Target');
        fireEvent.mouseDown(targetSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        fireEvent.click(
            within(screen.getByRole('listbox')).getByText('standby-server'),
        );

        // Wait for listbox to close
        await waitFor(() => {
            expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
        });

        // Verify at least one Add button is enabled (there are two: one for
        // server management, one for relationships)
        await waitFor(() => {
            const addButtons = screen.getAllByRole('button', { name: /Add/i });
            // Relationship Add button should be enabled
            expect(addButtons.some((btn) => !btn.hasAttribute('disabled'))).toBe(
                true,
            );
        });

        // Now change source to a different server
        fireEvent.mouseDown(sourceSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        fireEvent.click(
            within(screen.getByRole('listbox')).getByText('third-server'),
        );

        // Wait for listbox to close
        await waitFor(() => {
            expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
        });

        // After changing source, target should be reset, so we need to verify
        // by checking that the Target select still has no value selected
        // (the Add button in relationships section should be disabled)
        await waitFor(() => {
            expect(
                screen.getByRole('button', { name: 'Add relationship' }),
            ).toBeDisabled();
        });
    });
});
