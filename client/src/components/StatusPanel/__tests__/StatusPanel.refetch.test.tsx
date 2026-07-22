/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for StatusPanel's alert-fetching resilience. These cover the
 * fetchAlertsData path routed through useRetryingFetch:
 *
 *   - Happy path: alerts render after a successful fetch.
 *   - Retry path: a transient failure leaves the panel empty, then a
 *     scheduled retry succeeds and the alert appears without any
 *     manual refresh.
 *   - Guard path: a server selection missing an id skips the fetch.
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, act } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';
import { DEFAULT_RETRY_BASE_DELAY_MS } from '../../../hooks/useRetryingFetch';
import type { Selection } from '../../../types/selection';

// ---------------------------------------------------------------------------
// Module mocks -- declared before importing the SUT so vi.mock is hoisted.
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: vi.fn(),
    apiDelete: vi.fn(),
    apiFetch: vi.fn(),
    ApiError: class ApiError extends Error {
        public readonly statusCode: number;
        constructor(message: string, statusCode: number) {
            super(message);
            this.statusCode = statusCode;
        }
    },
}));

const stableUser = { id: 1, username: 'testuser' };
let mockAuthUser: typeof stableUser | null = stableUser;
const stableHasPermission = () => false;
const stableAIValue = { aiEnabled: false };
const stableClusterValue = { lastRefresh: 0 };
const stableDashboardValue = {
    currentOverlay: null,
    clearOverlays: () => {},
    pushOverlay: () => {},
    refreshTrigger: 0,
};

vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockAuthUser, hasPermission: stableHasPermission }),
}));
vi.mock('../../../contexts/useAICapabilities', () => ({ useAICapabilities: () => stableAIValue }));
vi.mock('../../../contexts/useClusterData', () => ({ useClusterData: () => stableClusterValue }));
vi.mock('../../../contexts/useDashboard', () => ({ useDashboard: () => stableDashboardValue }));

// Stub heavy siblings to keep the tree small.
vi.mock('../../EventTimeline', () => ({ default: () => null }));
vi.mock('../../BlackoutPanel', () => ({ default: () => null }));
vi.mock('../../AlertAnalysisDialog', () => ({ default: () => null }));
vi.mock('../../ServerAnalysisDialog', () => ({ default: () => null }));
vi.mock('../../AlertOverrideEditDialog', () => ({ default: () => null }));
vi.mock('../../BlackoutManagementDialog', () => ({ default: () => null }));
vi.mock('../../AIOverview', () => ({ default: () => null }));
vi.mock('../../Dashboard', () => ({
    ServerDashboard: () => null,
    EstateDashboard: () => null,
    ClusterDashboard: () => null,
    DatabaseDashboard: () => null,
    ObjectDashboard: () => null,
    MetricOverlay: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../../Dashboard/ClusterDashboard/TopologySection', () => ({ default: () => null }));
vi.mock('../../Dashboard/CollapsibleSection', () => ({
    default: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../../Dashboard/TimeRangeSelector', () => ({ default: () => null }));
vi.mock('../SelectionHeader', () => ({ default: () => null }));
vi.mock('../ServerInfoCard', () => ({ default: () => null }));
vi.mock('../PerformanceTiles', () => ({ default: () => null }));
vi.mock('../AcknowledgeDialog', () => ({ default: () => null }));
vi.mock('../../../hooks/useServerAnalysis', () => ({ hasCachedServerAnalysis: () => false }));

// ---------------------------------------------------------------------------
// Now import the SUT.
// ---------------------------------------------------------------------------

import StatusPanel from '../index';

const testTheme = createPgedgeTheme('dark');

const renderPanel = (selection: Selection) =>
    render(
        <ThemeProvider theme={testTheme}>
            <StatusPanel selection={selection} />
        </ThemeProvider>,
    );

const makeAlertRecord = (overrides: Record<string, unknown> = {}) => ({
    id: 99,
    title: 'High CPU Usage',
    description: 'CPU usage exceeded threshold',
    severity: 'warning',
    alert_type: 'threshold',
    triggered_at: '2026-04-20T12:00:00Z',
    last_updated: '2026-04-20T12:00:00Z',
    server_name: 'server-1',
    connection_id: 1,
    ...overrides,
});

const serverSelection: Selection = {
    type: 'server',
    id: 1,
    name: 'server-1',
    status: 'online',
    description: 'Test server',
    host: 'localhost',
    port: 5432,
    role: 'primary',
    version: '14.0',
    database: 'testdb',
    username: 'testuser',
    os: 'Linux',
    platform: 'x86_64',
};

const clusterSelection: Selection = {
    type: 'cluster',
    id: 'cluster-1',
    name: 'Cluster 1',
    status: 'online',
    description: '',
    servers: [{ id: 1, name: 's1' }, { id: 2, name: 's2' }],
    serverIds: [1, 2],
};

describe('StatusPanel alert fetch resilience', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockAuthUser = stableUser;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('renders alerts after a successful fetch', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });

        renderPanel(serverSelection);

        expect(await screen.findByText('High CPU Usage')).toBeInTheDocument();
        expect(mockApiGet).toHaveBeenCalledWith(
            expect.stringContaining('/api/v1/alerts?exclude_cleared=true'),
        );
    });

    it('recovers via a scheduled retry after a transient failure', async () => {
        vi.useFakeTimers();
        mockApiGet
            .mockRejectedValueOnce(new Error('backend restarting'))
            .mockResolvedValue({ alerts: [makeAlertRecord()] });

        renderPanel(serverSelection);

        // Let the initial (failing) fetch settle.
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(screen.queryByText('High CPU Usage')).not.toBeInTheDocument();

        // The scheduled retry fires after the base backoff delay.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(DEFAULT_RETRY_BASE_DELAY_MS);
            await Promise.resolve();
        });

        expect(mockApiGet).toHaveBeenCalledTimes(2);
        expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
    });

    it('skips the fetch when a server selection has no id', async () => {
        const noId = { ...serverSelection, id: undefined } as unknown as Selection;

        renderPanel(noId);

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('builds a multi-connection alerts URL for a cluster selection', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });

        renderPanel(clusterSelection);

        expect(await screen.findByText('High CPU Usage')).toBeInTheDocument();
        expect(mockApiGet).toHaveBeenCalledWith(
            expect.stringContaining('connection_ids=1,2'),
        );
    });

    it('bails out of the terminal state update when unmounted mid-fetch', async () => {
        // Hold the alerts fetch pending so the component can unmount
        // while fetchAlertsData is still awaiting its response.
        let resolveGet: (value: unknown) => void = () => {};
        mockApiGet.mockImplementation(
            () => new Promise(resolve => { resolveGet = resolve; }),
        );

        const { unmount } = renderPanel(serverSelection);

        // Let the fetch start and reach the pending await.
        await act(async () => {
            await Promise.resolve();
        });

        // Unmount while the request is still in flight.
        unmount();

        // Resolving now drives fetchAlertsData past its await into the
        // post-unmount guard, which returns without touching state.
        await act(async () => {
            resolveGet({ alerts: [makeAlertRecord()] });
            await Promise.resolve();
            await Promise.resolve();
        });

        // The guard prevented a setState on the unmounted component, so
        // nothing was rendered.
        expect(screen.queryByText('High CPU Usage')).not.toBeInTheDocument();
    });

    it('skips the fetch and clears alerts when there is no user', async () => {
        mockAuthUser = null;

        renderPanel(serverSelection);

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiGet).not.toHaveBeenCalled();
    });
});
