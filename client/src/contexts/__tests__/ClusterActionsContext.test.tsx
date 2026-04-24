/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ClusterActionsContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ClusterActionsProvider } from '../ClusterActionsContext';
import { useClusterActions } from '../useClusterActions';

vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    apiPost: vi.fn(),
    apiPut: vi.fn(),
    apiDelete: vi.fn(),
}));

vi.mock('../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

let mockUser: { username: string } | null = { username: 'testuser' };
const mockFetchClusterData = vi.fn(async () => {});
const mockClearSelection = vi.fn(async () => {});
let mockSelectedServer: { id: number } | null = null;

vi.mock('../useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));
vi.mock('../useClusterData', () => ({
    useClusterData: () => ({ fetchClusterData: mockFetchClusterData }),
}));
vi.mock('../useClusterSelection', () => ({
    useClusterSelection: () => ({
        selectedServer: mockSelectedServer,
        clearSelection: mockClearSelection,
    }),
}));

import {
    apiGet,
    apiPost,
    apiPut,
    apiDelete,
} from '../../utils/apiClient';

const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;
const mockApiPost = apiPost as unknown as ReturnType<typeof vi.fn>;
const mockApiPut = apiPut as unknown as ReturnType<typeof vi.fn>;
const mockApiDelete = apiDelete as unknown as ReturnType<typeof vi.fn>;

describe('ClusterActionsContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <ClusterActionsProvider>{children}</ClusterActionsProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
        mockUser = { username: 'testuser' };
        mockSelectedServer = null;
        mockApiGet.mockResolvedValue({});
        mockApiPost.mockResolvedValue({});
        mockApiPut.mockResolvedValue({});
        mockApiDelete.mockResolvedValue({});
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('auth gating', () => {
        it.each([
            ['updateGroupName', ['group-1', 'new']],
            ['updateClusterName', ['cluster-1', 'x', 'group-1']],
            ['updateServerName', [1, 'x']],
            ['getServer', [1]],
            ['createServer', [{ name: 'x' }]],
            ['updateServer', [1, { name: 'x' }]],
            ['deleteServer', [1]],
            ['deleteCluster', ['cluster-1']],
            ['createGroup', [{ name: 'x' }]],
            ['deleteGroup', ['group-1']],
            ['moveClusterToGroup', ['cluster-1', 'group-2']],
        ] as const)('%s throws when user is null', async (fn, args) => {
            mockUser = null;
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                (result.current as any)[fn](...args),
            ).rejects.toThrow('Not authenticated');
        });
    });

    describe('updateGroupName', () => {
        it('uses group id as-is for auto-detected groups', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateGroupName('group-auto', 'My Auto');
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/cluster-groups/group-auto',
                { name: 'My Auto' },
            );
            expect(mockFetchClusterData).toHaveBeenCalled();
        });

        it('passes through auto-detected ids with a key suffix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateGroupName(
                    'group-auto-some-key',
                    'Auto Prod',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/cluster-groups/group-auto-some-key',
                { name: 'Auto Prod' },
            );
        });

        it('uses numeric id for database-backed groups', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateGroupName('group-42', 'Prod');
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/cluster-groups/42',
                { name: 'Prod' },
            );
        });

        it('rejects ids without the "group-" prefix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.updateGroupName('42', 'Bad'),
            ).rejects.toThrow('Invalid group ID');
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it('rejects ids with a non-numeric, non-auto suffix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.updateGroupName('group-named', 'Bad'),
            ).rejects.toThrow('Invalid group ID');
            expect(mockApiPut).not.toHaveBeenCalled();
        });

        it.each([
            ['group-autobad'],
            ['group-automatic'],
            ['group-auto_foo'],
            ['group-auto/evil'],
            ['group-auto-'],
            ['group-auto-bad id'],
        ])(
            'rejects malformed auto-prefixed id %j and never forwards it',
            async (malformed) => {
                const { result } = renderHook(() => useClusterActions(), {
                    wrapper,
                });

                await expect(
                    result.current.updateGroupName(malformed, 'X'),
                ).rejects.toThrow('Invalid group ID');
                expect(mockApiPut).not.toHaveBeenCalled();
            },
        );

        it('accepts the bare "group-auto" bucket id', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateGroupName('group-auto', 'Auto');
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/cluster-groups/group-auto',
                { name: 'Auto' },
            );
        });

        it('accepts "group-auto-<token>" with url-safe suffix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateGroupName(
                    'group-auto-some_key-123',
                    'X',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/cluster-groups/group-auto-some_key-123',
                { name: 'X' },
            );
        });
    });

    describe('updateClusterName', () => {
        it('sends auto_cluster_key for auto-detected clusters', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateClusterName(
                    'server-5',
                    'my cluster',
                    'group-1',
                    'auto-key',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/clusters/server-5',
                { name: 'my cluster', auto_cluster_key: 'auto-key' },
            );
        });

        it('omits auto_cluster_key when not provided for auto clusters', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateClusterName(
                    'cluster-spock-abc',
                    'pg-cluster',
                    'group-1',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/clusters/cluster-spock-abc',
                { name: 'pg-cluster' },
            );
        });

        it('updates database-backed clusters with numeric ids', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateClusterName(
                    'cluster-7',
                    'db cluster',
                    'group-3',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/clusters/7',
                { name: 'db cluster', group_id: 3 },
            );
        });

        it('throws when cluster id is not numeric and not auto-detected', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.updateClusterName(
                    'cluster-not-a-number',
                    'x',
                    'group-1',
                ),
            ).rejects.toThrow('Invalid cluster ID');
        });

        it('throws when group id is not numeric', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.updateClusterName(
                    'cluster-1',
                    'x',
                    'group-named',
                ),
            ).rejects.toThrow('Invalid group ID');
        });
    });

    describe('updateServerName', () => {
        it('PUTs /connections and refetches', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateServerName(1, 'New');
            });

            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/connections/1',
                { name: 'New' },
            );
            expect(mockFetchClusterData).toHaveBeenCalled();
        });
    });

    describe('getServer / createServer / updateServer / deleteServer', () => {
        it('getServer GETs the connection and returns the payload', async () => {
            mockApiGet.mockResolvedValueOnce({ id: 1, name: 'x' });
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            let payload: unknown;
            await act(async () => {
                payload = await result.current.getServer(1);
            });

            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/connections/1');
            expect(payload).toEqual({ id: 1, name: 'x' });
        });

        it('createServer POSTs and refetches', async () => {
            mockApiPost.mockResolvedValueOnce({ id: 99 });
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            let created: unknown;
            await act(async () => {
                created = await result.current.createServer({ name: 'new' });
            });

            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/connections', {
                name: 'new',
            });
            expect(mockFetchClusterData).toHaveBeenCalled();
            expect(created).toEqual({ id: 99 });
        });

        it('updateServer PUTs and refetches', async () => {
            mockApiPut.mockResolvedValueOnce({ id: 1 });
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.updateServer(1, { name: 'edited' });
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/connections/1', {
                name: 'edited',
            });
            expect(mockFetchClusterData).toHaveBeenCalled();
        });

        it('deleteServer calls clearSelection when selected server is deleted', async () => {
            mockSelectedServer = { id: 5 };
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.deleteServer(5);
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/connections/5');
            expect(mockClearSelection).toHaveBeenCalled();
        });

        it('deleteServer leaves selection alone when deleting a different server', async () => {
            mockSelectedServer = { id: 5 };
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.deleteServer(6);
            });

            expect(mockClearSelection).not.toHaveBeenCalled();
        });
    });

    describe('deleteCluster', () => {
        it('DELETEs the cluster and refetches', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.deleteCluster('cluster-1');
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/clusters/cluster-1');
            expect(mockFetchClusterData).toHaveBeenCalled();
        });
    });

    describe('createGroup and deleteGroup', () => {
        it('createGroup POSTs and refetches', async () => {
            mockApiPost.mockResolvedValueOnce({ id: 10 });
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            let created: unknown;
            await act(async () => {
                created = await result.current.createGroup({ name: 'new-group' });
            });

            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/cluster-groups', {
                name: 'new-group',
            });
            expect(created).toEqual({ id: 10 });
        });

        it('deleteGroup extracts numeric id from group- prefix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.deleteGroup('group-42');
            });

            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/cluster-groups/42');
            expect(mockFetchClusterData).toHaveBeenCalled();
        });

        it('deleteGroup rejects ids without a numeric suffix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.deleteGroup('group-auto'),
            ).rejects.toThrow('Invalid group ID');
            expect(mockApiDelete).not.toHaveBeenCalled();
        });

        it('deleteGroup rejects ids without the "group-" prefix', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await expect(
                result.current.deleteGroup('42'),
            ).rejects.toThrow('Invalid group ID');
            expect(mockApiDelete).not.toHaveBeenCalled();
        });
    });

    describe('moveClusterToGroup', () => {
        it('sends numeric group_id extracted from group-NN format', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup('cluster-1', 'group-42');
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/cluster-1', {
                group_id: 42,
            });
        });

        it('sends group_id = null when targetGroupId is null', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup('cluster-1', null);
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/cluster-1', {
                group_id: null,
            });
        });

        it('includes auto_cluster_key and name when provided', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup(
                    'server-3',
                    'group-1',
                    'auto-key-abc',
                    'My Cluster',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/server-3', {
                group_id: 1,
                auto_cluster_key: 'auto-key-abc',
                name: 'My Cluster',
            });
        });

        it('sends group_id = null when dropping on the bare "group-auto" bucket', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup(
                    'server-3',
                    'group-auto',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/server-3', {
                group_id: null,
            });
        });

        it('sends group_id = null when dropping on a "group-auto-<key>" bucket', async () => {
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup(
                    'server-3',
                    'group-auto-foo',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/server-3', {
                group_id: null,
            });
        });

        it('forwards auto_cluster_key when dropping on an auto-group', async () => {
            // The caller (handleDragEnd) passes the dragged cluster's own
            // auto_cluster_key. Verify moveClusterToGroup forwards it
            // verbatim so the server-side auto-group path receives it.
            const { result } = renderHook(() => useClusterActions(), { wrapper });

            await act(async () => {
                await result.current.moveClusterToGroup(
                    'server-3',
                    'group-auto-foo',
                    'caller-provided-key',
                );
            });

            expect(mockApiPut).toHaveBeenCalledWith('/api/v1/clusters/server-3', {
                group_id: null,
                auto_cluster_key: 'caller-provided-key',
            });
        });

        it.each([
            ['group-foo'],
            ['group-1a'],
            ['group-'],
            ['group-auto_bad'],
            ['group-autobad'],
            ['group-auto-'],
            ['not-a-group'],
            ['42'],
            [''],
        ])(
            'throws "Invalid group ID" and does not call fetch for %j',
            async (malformed) => {
                const { result } = renderHook(() => useClusterActions(), {
                    wrapper,
                });

                await expect(
                    result.current.moveClusterToGroup('cluster-1', malformed),
                ).rejects.toThrow('Invalid group ID');
                expect(mockApiPut).not.toHaveBeenCalled();
                expect(mockFetchClusterData).not.toHaveBeenCalled();
            },
        );
    });

    describe('hook outside provider', () => {
        it('throws when used outside provider', () => {
            expect(() => {
                renderHook(() => useClusterActions());
            }).toThrow(
                'useClusterActions must be used within a ClusterActionsProvider',
            );
        });
    });
});
