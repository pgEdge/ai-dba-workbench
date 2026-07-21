/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for StatusPanel's acknowledgement-refresh handlers. These
 * confirm that both the single and group acknowledgement flows route
 * their follow-up refresh through the retry-managed refetchAlerts path
 * (which re-issues the alerts fetch) rather than an unmanaged direct
 * call.
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, act, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';
import type { Selection } from '../../../types/selection';

// ---------------------------------------------------------------------------
// Module mocks -- declared before importing the SUT so vi.mock is hoisted.
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
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
    useAuth: () => ({ user: stableUser, hasPermission: stableHasPermission }),
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

// Expose the acknowledgement callbacks as buttons so the test can drive
// the confirm handlers without the real dialog UI.
vi.mock('../AcknowledgeDialog', () => ({
    default: ({
        onConfirm,
        onConfirmMultiple,
    }: {
        onConfirm: (id: number, message: string, fp?: boolean) => void;
        onConfirmMultiple: (ids: number[], message: string, fp?: boolean) => void;
    }) => (
        <div>
            <button
                type="button"
                data-testid="confirm-ack"
                onClick={() => onConfirm(99, 'ack message', false)}
            >
                confirm
            </button>
            <button
                type="button"
                data-testid="confirm-group-ack"
                onClick={() => onConfirmMultiple([99, 100], 'group message', false)}
            >
                confirm group
            </button>
        </div>
    ),
}));

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

describe('StatusPanel acknowledgement refresh', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });
        mockApiPost.mockResolvedValue({});
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('routes the single-ack refresh through the retry-managed path', async () => {
        renderPanel(serverSelection);

        // Wait for the initial fetch to settle.
        expect(await screen.findByText('High CPU Usage')).toBeInTheDocument();
        const initialGetCalls = mockApiGet.mock.calls.length;

        await act(async () => {
            screen.getByTestId('confirm-ack').click();
        });

        // The acknowledgement was posted and a managed refetch followed.
        expect(mockApiPost).toHaveBeenCalledWith(
            '/api/v1/alerts/acknowledge',
            expect.objectContaining({ alert_id: 99, message: 'ack message' }),
        );
        await waitFor(() => {
            expect(mockApiGet.mock.calls.length).toBeGreaterThan(initialGetCalls);
        });
    });

    it('routes the group-ack refresh through the retry-managed path', async () => {
        renderPanel(serverSelection);

        expect(await screen.findByText('High CPU Usage')).toBeInTheDocument();
        const initialGetCalls = mockApiGet.mock.calls.length;

        await act(async () => {
            screen.getByTestId('confirm-group-ack').click();
        });

        // Each alert id is acknowledged, then a managed refetch follows.
        expect(mockApiPost).toHaveBeenCalledWith(
            '/api/v1/alerts/acknowledge',
            expect.objectContaining({ alert_id: 99, message: 'group message' }),
        );
        expect(mockApiPost).toHaveBeenCalledWith(
            '/api/v1/alerts/acknowledge',
            expect.objectContaining({ alert_id: 100, message: 'group message' }),
        );
        await waitFor(() => {
            expect(mockApiGet.mock.calls.length).toBeGreaterThan(initialGetCalls);
        });
    });
});
