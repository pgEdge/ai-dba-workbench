/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterNavigator handler tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// These tests render the real ClusterNavigator with its child tree and
// dialogs replaced by stubs that record their props, so that each
// navigator handler (resize, search, blackout inheritance, add, edit,
// configure, delete and drag) can be driven directly.

import type React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { ThemeProvider } from '@mui/material/styles';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterNavigator from '../ClusterNavigator';
import type { ClusterNavigatorProps } from '../ClusterNavigator/index';
import { STORAGE_KEYS } from '../ClusterNavigator/constants';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

type Props = Record<string, unknown>;
type Fn = (...args: unknown[]) => unknown;

const {
    captured,
    clusterState,
    blackoutState,
    mockGetServer,
    mockCreateServer,
    mockUpdateServer,
    mockDeleteServer,
    mockDeleteCluster,
    mockDeleteGroup,
    mockCreateGroup,
    mockUpdateGroup,
    mockMoveClusterToGroup,
    mockFetchClusterData,
    mockSetAutoRefreshEnabled,
    mockApiFetch,
    mockApiGet,
} = vi.hoisted(() => ({
    captured: {} as Record<string, Record<string, unknown>>,
    clusterState: { lastRefresh: null as Date | null },
    blackoutState: {
        list: [] as Array<Record<string, unknown>>,
    },
    mockGetServer: vi.fn(),
    mockCreateServer: vi.fn(),
    mockUpdateServer: vi.fn(),
    mockDeleteServer: vi.fn(),
    mockDeleteCluster: vi.fn(),
    mockDeleteGroup: vi.fn(),
    mockCreateGroup: vi.fn(),
    mockUpdateGroup: vi.fn(),
    mockMoveClusterToGroup: vi.fn(),
    mockFetchClusterData: vi.fn(),
    mockSetAutoRefreshEnabled: vi.fn(),
    mockApiFetch: vi.fn(),
    mockApiGet: vi.fn(),
}));

// Dialog and menu stubs record their props and render a marker while
// open, so tests can assert on visibility and call the handlers.
const { recordingStub } = vi.hoisted(() => ({
    recordingStub: (name: string) => ({
        default: (p: Record<string, unknown>) => {
            captured[name] = p;
            return p.open ? <div data-testid={name}>{String(p.title ?? '')}</div> : null;
        },
    }),
}));

/** Invoke a recorded prop callback inside act(). */
const call = async (component: string, prop: string, ...args: unknown[]) => {
    let result: unknown;
    await act(async () => {
        result = await (captured[component][prop] as Fn)(...args);
    });
    return result;
};

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: { isSuperuser: true }, hasPermission: () => true }),
}));

vi.mock('../../contexts/useCluster', () => ({
    useCluster: () => ({
        updateGroupName: vi.fn(),
        updateGroup: mockUpdateGroup,
        updateClusterName: vi.fn(),
        updateServerName: vi.fn(),
        getServer: mockGetServer,
        createServer: mockCreateServer,
        updateServer: mockUpdateServer,
        deleteServer: mockDeleteServer,
        deleteCluster: mockDeleteCluster,
        createGroup: mockCreateGroup,
        deleteGroup: mockDeleteGroup,
        moveClusterToGroup: mockMoveClusterToGroup,
        fetchClusterData: mockFetchClusterData,
        autoRefreshEnabled: false,
        setAutoRefreshEnabled: mockSetAutoRefreshEnabled,
        lastRefresh: clusterState.lastRefresh,
    }),
}));

vi.mock('../../contexts/useAlerts', () => ({
    useAlerts: () => ({
        getServerAlertCount: () => 0,
        getTotalAlertCount: () => 0,
    }),
}));

vi.mock('../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({ blackouts: blackoutState.list }),
}));

vi.mock('@dnd-kit/core', async (importOriginal) => ({
    ...(await importOriginal<typeof import('@dnd-kit/core')>()),
    DndContext: (props: Props & { children: React.ReactNode }) => {
        captured.dnd = props;
        return <>{props.children}</>;
    },
    DragOverlay: ({ children }: { children: React.ReactNode }) => (
        <div data-testid="drag-overlay">{children}</div>
    ),
}));

vi.mock('../ClusterNavigator/DragDropComponents', () => ({
    DragOverlayContent: ({ cluster }: { cluster: { name: string } }) => (
        <div>Dragging {cluster.name}</div>
    ),
}));

interface StubServer { id: number; name: string; children?: StubServer[] }
interface StubCluster { id: string; name: string; servers?: StubServer[] }
interface StubGroup { id: string; name: string; clusters?: StubCluster[] }

vi.mock('../ClusterNavigator/GroupItem', () => {
    const status = (s: { active: boolean; inherited: boolean }) => {
        if (!s.active) {return 'none';}
        return s.inherited ? 'inherited' : 'direct';
    };
    const ServerStub = ({ server, p }: { server: StubServer; p: Props }) => {
        const getStatus = p.getServerBlackoutStatus as (
            id: number,
        ) => { active: boolean; inherited: boolean };
        const expanded = (p.expandedServers as Set<number>).has(server.id);
        return (
            <div data-testid={`server-${server.id}`} data-expanded={String(expanded)}>
                {server.name}: {status(getStatus(server.id))}
                <button onClick={() => (p.onToggleServer as Fn)(server.id)}>
                    toggle server {server.id}
                </button>
                <button onClick={() => (p.onEditServer as Fn)(server)}>
                    edit server {server.id}
                </button>
                <button onClick={() => (p.onDeleteServer as Fn)(server)}>
                    delete server {server.id}
                </button>
                {server.children?.map((child) => (
                    <ServerStub key={child.id} server={child} p={p} />
                ))}
            </div>
        );
    };
    return {
        default: (p: Props) => {
            const group = p.group as StubGroup;
            captured[`group:${group.id}`] = p;
            const getClusterStatus = p.getClusterBlackoutStatus as (
                id: string,
            ) => { active: boolean; inherited: boolean };
            const expandedClusters = p.expandedClusters as Set<string>;
            return (
                <div data-testid={`group-${group.id}`} data-expanded={String(p.isExpanded)}>
                    {group.name}
                    <button onClick={p.onToggle as Fn}>toggle group {group.id}</button>
                    <button onClick={() => (p.onDeleteGroup as Fn)(group)}>
                        delete group {group.id}
                    </button>
                    {group.clusters?.map((cluster) => (
                        <div
                            key={cluster.id}
                            data-testid={`cluster-${cluster.id}`}
                            data-expanded={String(expandedClusters.has(cluster.id))}
                        >
                            {cluster.name}: {status(getClusterStatus(cluster.id))}
                            <button onClick={() => (p.onToggleCluster as Fn)(cluster.id)}>
                                toggle cluster {cluster.id}
                            </button>
                            <button onClick={() => (p.onConfigureCluster as Fn)(cluster)}>
                                configure cluster {cluster.id}
                            </button>
                            <button onClick={() => (p.onDeleteCluster as Fn)(cluster)}>
                                delete cluster {cluster.id}
                            </button>
                            {cluster.servers?.map((server) => (
                                <ServerStub key={server.id} server={server} p={p} />
                            ))}
                        </div>
                    ))}
                </div>
            );
        },
    };
});

vi.mock('../ServerDialog', () => recordingStub('serverDialog'));
vi.mock('../GroupDialog', () => recordingStub('groupDialog'));
vi.mock('../DeleteConfirmationDialog', () => recordingStub('deleteDialog'));
vi.mock('../AddMenu', () => recordingStub('addMenu'));
vi.mock('../ClusterConfigDialog', () => recordingStub('clusterConfig'));

const theme = createPgedgeTheme('light');

const data = [
    {
        id: 'group-1',
        name: 'Production',
        clusters: [
            {
                id: 'cluster-5',
                name: 'Manual cluster',
                description: 'Hand built',
                replication_type: 'binary',
                servers: [
                    {
                        id: 1,
                        name: 'pg-primary',
                        host: '192.0.2.1',
                        port: 5432,
                        status: 'online',
                        children: [
                            { id: 2, name: 'pg-standby', host: '192.0.2.2', port: 5432, status: 'online' },
                        ],
                    },
                ],
            },
            {
                id: 'server-10',
                name: 'Auto cluster',
                db_cluster_id: 42,
                auto_cluster_key: 'binary:10',
                replication_type: 'binary',
                servers: [
                    { id: 10, name: 'pg-auto', host: '192.0.2.10', port: 5432, status: 'offline' },
                ],
            },
        ],
    },
    {
        id: 'group-auto',
        name: 'Ungrouped',
        clusters: [
            {
                id: 'cluster-spock-x',
                name: 'Spock cluster',
                servers: [
                    { id: 20, name: 'pg-spock', host: '192.0.2.20', port: 5432, status: 'warning' },
                ],
            },
        ],
    },
] as unknown as NonNullable<ClusterNavigatorProps['data']>;

const renderNavigator = (props: Partial<ClusterNavigatorProps> = {}) => {
    const allProps: ClusterNavigatorProps = {
        data,
        onSelectServer: vi.fn(),
        ...props,
    };
    const result = render(
        <ThemeProvider theme={theme}>
            <ClusterNavigator {...allProps} />
        </ThemeProvider>,
    );
    const rerender = (next: Partial<ClusterNavigatorProps>) => {
        result.rerender(
            <ThemeProvider theme={theme}>
                <ClusterNavigator {...allProps} {...next} />
            </ThemeProvider>,
        );
    };
    return { ...result, rerender };
};

const click = (name: string) => {
    fireEvent.click(screen.getByRole('button', { name }));
};

const okResponse = (body: unknown) => ({
    ok: true,
    json: async () => body,
});

const errorResponse = (body: unknown) => ({
    ok: false,
    json: async () => body,
});

describe('ClusterNavigator handlers', () => {
    beforeEach(() => {
        for (const key of Object.keys(captured)) {delete captured[key];}
        clusterState.lastRefresh = null;
        blackoutState.list = [];
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        vi.mocked(localStorage.setItem).mockClear();
        for (const mock of [
            mockCreateServer, mockUpdateServer, mockDeleteServer,
            mockDeleteCluster, mockDeleteGroup, mockCreateGroup,
            mockUpdateGroup, mockMoveClusterToGroup, mockFetchClusterData,
            mockSetAutoRefreshEnabled, mockApiFetch, mockApiGet,
        ]) {
            mock.mockReset().mockResolvedValue(undefined);
        }
        mockGetServer.mockReset().mockResolvedValue({ id: 1, name: 'pg-primary', username: 'admin' });
    });

    describe('blackout indicators', () => {
        it('marks a cluster directly and its nested servers by inheritance', () => {
            blackoutState.list = [
                { scope: 'cluster', cluster_id: 5, is_active: true },
                { scope: 'server', connection_id: 20, is_active: true },
                { scope: 'estate', is_active: false },
            ];
            renderNavigator();
            expect(screen.getByTestId('cluster-cluster-5')).toHaveTextContent('Manual cluster: direct');
            expect(screen.getByTestId('server-2')).toHaveTextContent('pg-standby: inherited');
            expect(screen.getByTestId('server-20')).toHaveTextContent('pg-spock: direct');
            expect(screen.getByTestId('server-10')).toHaveTextContent('pg-auto: none');
            expect(screen.getByTestId('cluster-cluster-spock-x')).toHaveTextContent('Spock cluster: none');
        });

        it('cascades an estate blackout to clusters and servers', () => {
            blackoutState.list = [{ scope: 'estate', is_active: true }];
            renderNavigator();
            expect(screen.getByTestId('cluster-server-10')).toHaveTextContent('Auto cluster: inherited');
            expect(screen.getByTestId('server-10')).toHaveTextContent('pg-auto: inherited');
            const getStatus = captured['group:group-1'].getServerBlackoutStatus as Fn;
            expect(getStatus(999)).toEqual({ active: true, inherited: true });
        });
    });

    describe('header', () => {
        it('wires the add, auto-refresh and estate controls', () => {
            const onSelectEstate = vi.fn();
            renderNavigator({ onSelectEstate });
            expect(captured.addMenu.open).toBe(false);
            click('Add server or group');
            expect(captured.addMenu.open).toBe(true);
            act(() => { (captured.addMenu.onClose as Fn)(); });
            expect(captured.addMenu.open).toBe(false);

            click('Auto-refresh disabled');
            expect(mockSetAutoRefreshEnabled).toHaveBeenCalledWith(true);

            fireEvent.click(screen.getByText('3 online of 4 servers'));
            expect(onSelectEstate).toHaveBeenCalled();
        });

        it('shows the last refresh time in the footer', () => {
            clusterState.lastRefresh = new Date();
            renderNavigator();
            expect(screen.getByText('Updated just now')).toBeInTheDocument();
            expect(screen.getByText('2 groups • 3 clusters')).toBeInTheDocument();
        });

        it('resizes the panel within its limits and stores the width', () => {
            renderNavigator();
            const panel = screen.getByTestId('cluster-navigator');
            const handle = panel.firstElementChild as HTMLElement;
            fireEvent.mouseDown(handle);
            expect(document.body.style.cursor).toBe('col-resize');
            fireEvent.mouseMove(document, { clientX: 900 });
            fireEvent.mouseUp(document);
            expect(document.body.style.cursor).toBe('');
            expect(localStorage.setItem).toHaveBeenCalledWith(
                STORAGE_KEYS.WIDTH, '500',
            );
        });
    });

    describe('tree state', () => {
        it('expands groups and parent servers by default and toggles them', () => {
            renderNavigator();
            const group = screen.getByTestId('group-group-1');
            expect(group).toHaveAttribute('data-expanded', 'true');
            click('toggle group group-1');
            expect(group).toHaveAttribute('data-expanded', 'false');
            click('toggle group group-1');
            expect(group).toHaveAttribute('data-expanded', 'true');

            const server = screen.getByTestId('server-1');
            expect(server).toHaveAttribute('data-expanded', 'true');
            click('toggle server 1');
            expect(server).toHaveAttribute('data-expanded', 'false');
            click('toggle server 1');
            expect(server).toHaveAttribute('data-expanded', 'true');

            const cluster = screen.getByTestId('cluster-cluster-5');
            expect(cluster).toHaveAttribute('data-expanded', 'false');
            click('toggle cluster cluster-5');
            expect(cluster).toHaveAttribute('data-expanded', 'true');
            expect(localStorage.setItem).toHaveBeenCalledWith(
                STORAGE_KEYS.EXPANDED_CLUSTERS, JSON.stringify(['cluster-5']),
            );
            click('toggle cluster cluster-5');
            expect(cluster).toHaveAttribute('data-expanded', 'false');
        });

        it('filters by server, cluster and group name', () => {
            renderNavigator();
            const search = screen.getByPlaceholderText('Search servers...');

            fireEvent.change(search, { target: { value: 'standby' } });
            expect(screen.getByTestId('server-2')).toBeInTheDocument();
            expect(screen.queryByTestId('server-10')).not.toBeInTheDocument();
            expect(screen.queryByTestId('group-group-auto')).not.toBeInTheDocument();

            fireEvent.change(search, { target: { value: 'spock cluster' } });
            expect(screen.getByTestId('server-20')).toBeInTheDocument();
            expect(screen.queryByTestId('group-group-1')).not.toBeInTheDocument();

            fireEvent.change(search, { target: { value: 'ungrouped' } });
            expect(screen.getByTestId('group-group-auto')).toBeInTheDocument();

            fireEvent.change(search, { target: { value: 'nothing-here' } });
            expect(screen.getByText('No servers found')).toBeInTheDocument();
        });

        it('shows the empty and loading states', () => {
            const { rerender } = renderNavigator({ data: [] });
            expect(screen.getByText('No servers configured')).toBeInTheDocument();
            rerender({ loading: true });
            expect(screen.queryByText('No servers configured')).not.toBeInTheDocument();
        });

        it('restores the scroll position after a data update', () => {
            const { rerender } = renderNavigator();
            const container = screen.getByTestId('group-group-1').parentElement as HTMLElement;
            container.scrollTop = 120;
            fireEvent.scroll(container);
            container.scrollTop = 0;
            rerender({ data: [...data] });
            expect(container.scrollTop).toBe(120);
        });
    });

    describe('servers', () => {
        it('creates a server from the add menu', async () => {
            mockCreateServer.mockResolvedValue({ id: 30 });
            renderNavigator();
            await call('addMenu', 'onAddServer');
            expect(captured.serverDialog).toMatchObject({ open: true, mode: 'create', server: null });
            expect(await call('serverDialog', 'onSave', { name: 'new' })).toEqual({ id: 30 });
            expect(mockCreateServer).toHaveBeenCalledWith({ name: 'new' });
            expect(captured.serverDialog.open).toBe(false);
        });

        it('edits a server using its full details', async () => {
            renderNavigator();
            click('edit server 1');
            await act(async () => { await Promise.resolve(); });
            expect(mockGetServer).toHaveBeenCalledWith(1);
            expect(captured.serverDialog).toMatchObject({
                open: true,
                mode: 'edit',
                server: { id: 1, username: 'admin' },
            });
            await call('serverDialog', 'onSave', { name: 'renamed' });
            expect(mockUpdateServer).toHaveBeenCalledWith(1, { name: 'renamed' });

            await call('serverDialog', 'onClose');
            expect(captured.serverDialog.open).toBe(false);
        });

        it('falls back to the tree data when the detail fetch fails', async () => {
            mockGetServer.mockRejectedValue(new Error('boom'));
            renderNavigator();
            click('edit server 10');
            await act(async () => { await Promise.resolve(); });
            expect(captured.serverDialog).toMatchObject({
                open: true,
                mode: 'edit',
                server: { id: 10, name: 'pg-auto' },
            });
        });

        it('opens cluster configuration from the server dialog', async () => {
            renderNavigator();
            await call('serverDialog', 'onOpenClusterConfig', 5, 'Manual cluster');
            expect(captured.clusterConfig).toMatchObject({
                open: true,
                mode: 'edit',
                clusterId: 'cluster-5',
                numericClusterId: 5,
                clusterDescription: 'Hand built',
                replicationType: 'binary',
            });

            await call('serverDialog', 'onOpenClusterConfig', 77, 'Elsewhere');
            expect(captured.clusterConfig).toMatchObject({
                clusterId: 'cluster-77',
                clusterDescription: undefined,
            });
        });
    });

    describe('clusters', () => {
        it('creates a cluster from the add menu', async () => {
            mockApiFetch.mockResolvedValue(okResponse({ id: 9 }));
            renderNavigator();
            await call('addMenu', 'onAddCluster');
            expect(captured.clusterConfig).toMatchObject({ open: true, mode: 'create' });

            const result = await call('clusterConfig', 'onCreate', {
                name: 'New', description: 'D', replication_type: 'spock',
            });
            expect(result).toEqual({ id: 9 });
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/clusters', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ name: 'New', description: 'D', replication_type: 'spock' }),
            }));
            expect(mockFetchClusterData).toHaveBeenCalled();

            await call('clusterConfig', 'onClose');
            expect(screen.queryByTestId('clusterConfig')).not.toBeInTheDocument();
        });

        it('reports cluster creation errors', async () => {
            renderNavigator();
            await call('addMenu', 'onAddCluster');
            const create = captured.clusterConfig.onCreate as Fn;
            mockApiFetch.mockResolvedValueOnce(errorResponse({ error: 'Duplicate name' }));
            await expect(create({ name: 'x', description: '', replication_type: '' }))
                .rejects.toThrow('Duplicate name');
            mockApiFetch.mockResolvedValueOnce(errorResponse({}));
            await expect(create({ name: 'x', description: '', replication_type: '' }))
                .rejects.toThrow('Failed to create cluster');
        });

        it('saves a database-backed cluster', async () => {
            mockApiFetch.mockResolvedValue(okResponse({}));
            renderNavigator();
            click('configure cluster cluster-5');
            expect(captured.clusterConfig).toMatchObject({
                open: true, numericClusterId: 5, autoClusterKey: undefined,
            });
            await call('clusterConfig', 'onSave', {
                name: 'Renamed', description: 'New text', replication_type: 'spock',
            });
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/clusters/cluster-5', expect.objectContaining({
                method: 'PUT',
                body: JSON.stringify({ name: 'Renamed', description: 'New text', replication_type: 'spock' }),
            }));
            expect(captured.clusterConfig).toMatchObject({
                clusterName: 'Renamed', replicationType: 'spock',
            });
            await call('clusterConfig', 'onSave', { name: 'Again', description: '' });
            expect(captured.clusterConfig).toMatchObject({
                clusterName: 'Again', replicationType: 'spock',
            });
            expect(mockFetchClusterData).toHaveBeenCalledTimes(2);
        });

        it('saves an auto-detected cluster and adopts its new ID', async () => {
            mockApiFetch.mockResolvedValue(okResponse({ id: 42 }));
            renderNavigator();
            click('configure cluster server-10');
            expect(captured.clusterConfig).toMatchObject({
                numericClusterId: undefined, autoClusterKey: 'binary:10',
            });
            await call('clusterConfig', 'onSave', { name: 'Claimed', description: '' });
            expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/clusters/server-10', expect.objectContaining({
                body: JSON.stringify({ name: 'Claimed', description: '', auto_cluster_key: 'binary:10' }),
            }));
            expect(captured.clusterConfig).toMatchObject({
                numericClusterId: 42, clusterName: 'Claimed', replicationType: 'binary',
            });
        });

        it('leaves an auto-detected cluster unchanged when no ID comes back', async () => {
            mockApiFetch.mockResolvedValue(okResponse({}));
            renderNavigator();
            click('configure cluster server-10');
            await call('clusterConfig', 'onSave', { name: 'Claimed', description: '' });
            expect(captured.clusterConfig).toMatchObject({
                numericClusterId: undefined, clusterName: 'Auto cluster',
            });
        });

        it('reports cluster save errors', async () => {
            renderNavigator();
            click('configure cluster cluster-5');
            const save = captured.clusterConfig.onSave as Fn;
            mockApiFetch.mockResolvedValueOnce(errorResponse({ error: 'Forbidden' }));
            await expect(save({ name: 'x', description: '' })).rejects.toThrow('Forbidden');
            mockApiFetch.mockResolvedValueOnce(errorResponse({}));
            await expect(save({ name: 'x', description: '' })).rejects.toThrow('Failed to update cluster');
        });
    });

    describe('groups', () => {
        it('creates a group from the add menu and edits an existing one', async () => {
            renderNavigator();
            await call('addMenu', 'onAddGroup');
            expect(captured.groupDialog).toMatchObject({ open: true, mode: 'create', group: null });
            await call('groupDialog', 'onSave', { name: 'Staging' });
            expect(mockCreateGroup).toHaveBeenCalledWith({ name: 'Staging' });
            expect(captured.groupDialog.open).toBe(false);

            await call('group:group-auto', 'onConfigureGroup', data[1]);
            expect(mockApiGet).not.toHaveBeenCalled();
            expect(captured.groupDialog).toMatchObject({ open: true, mode: 'edit' });
            await call('groupDialog', 'onSave', { name: 'Renamed', description: 'd', is_shared: true });
            expect(mockUpdateGroup).toHaveBeenCalledWith('group-auto', {
                name: 'Renamed', description: 'd', is_shared: true,
            });

            await call('group:group-1', 'onConfigureGroup', data[0]);
            await call('groupDialog', 'onClose');
            expect(captured.groupDialog.open).toBe(false);
        });
    });

    describe('deletion', () => {
        it.each([
            ['delete server 1', 'Delete Server', () => mockDeleteServer, 1],
            ['delete cluster cluster-5', 'Delete Cluster', () => mockDeleteCluster, 'cluster-5'],
            ['delete group group-1', 'Delete Cluster Group', () => mockDeleteGroup, 'group-1'],
        ])('confirms "%s"', async (button, title, getMock, id) => {
            renderNavigator();
            click(button);
            expect(screen.getByTestId('deleteDialog')).toHaveTextContent(title);
            await call('deleteDialog', 'onConfirm');
            expect(getMock()).toHaveBeenCalledWith(id);
            expect(screen.queryByTestId('deleteDialog')).not.toBeInTheDocument();
        });

        it('keeps the dialog open when deletion fails, and closes on cancel', async () => {
            mockDeleteServer.mockRejectedValue(new Error('in use'));
            renderNavigator();
            await call('deleteDialog', 'onConfirm');
            expect(mockDeleteServer).not.toHaveBeenCalled();

            click('delete server 10');
            await call('deleteDialog', 'onConfirm');
            expect(screen.getByTestId('deleteDialog')).toBeInTheDocument();
            expect(captured.deleteDialog.loading).toBe(false);

            await call('deleteDialog', 'onClose');
            expect(screen.queryByTestId('deleteDialog')).not.toBeInTheDocument();
        });
    });

    describe('drag and drop', () => {
        const cluster = { id: 'server-10', name: 'Auto cluster', auto_cluster_key: 'binary:10' };
        const drag = (over: unknown, type = 'cluster') => ({
            active: { data: { current: { type, cluster, groupId: 'group-1' } } },
            over,
        });
        const overGroup = (groupId: string) => ({
            data: { current: { type: 'group', groupId } },
        });

        it('shows an overlay and moves the cluster to another group', async () => {
            renderNavigator();
            await call('dnd', 'onDragStart', drag(null));
            expect(screen.getByText('Dragging Auto cluster')).toBeInTheDocument();
            await call('dnd', 'onDragEnd', drag(overGroup('group-auto')));
            expect(screen.queryByText('Dragging Auto cluster')).not.toBeInTheDocument();
            expect(mockMoveClusterToGroup).toHaveBeenCalledWith(
                'server-10', 'group-auto', 'binary:10', 'Auto cluster',
            );
        });

        it('ignores drops that are not a move to another group', async () => {
            renderNavigator();
            await call('dnd', 'onDragStart', drag(null, 'server'));
            expect(screen.queryByText(/^Dragging/)).not.toBeInTheDocument();
            await call('dnd', 'onDragEnd', drag(null));
            await call('dnd', 'onDragEnd', drag(overGroup('group-1')));
            await call('dnd', 'onDragEnd', drag({ data: { current: { type: 'cluster' } } }));
            await call('dnd', 'onDragEnd', { active: { data: {} }, over: overGroup('group-auto') });
            expect(mockMoveClusterToGroup).not.toHaveBeenCalled();
        });

        it('survives a failed move', async () => {
            mockMoveClusterToGroup.mockRejectedValue(new Error('denied'));
            renderNavigator();
            await call('dnd', 'onDragEnd', drag(overGroup('group-auto')));
            expect(mockMoveClusterToGroup).toHaveBeenCalled();
        });
    });
});
