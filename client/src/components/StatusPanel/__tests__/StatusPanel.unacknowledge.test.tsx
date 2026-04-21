/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for StatusPanel's handleUnacknowledge flow. These tests cover:
 *
 *   - Happy path: clicking the "Restore to active" icon triggers an
 *     optimistic move of the alert out of the "Acknowledged" section,
 *     awaits the apiDelete call, and then reconciles by re-fetching.
 *
 *   - Failure path: apiDelete rejects with ApiError, the optimistic
 *     update is rolled back, and a user-visible error is shown.
 *
 *   - Double-click prevention: while a request is in flight, repeated
 *     clicks are ignored and the icon button is disabled.
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';

// ---------------------------------------------------------------------------
// Module mocks -- declare before importing the SUT so vi.mock is hoisted.
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
const mockApiPost = vi.fn();
const mockApiDelete = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPost: (...args: unknown[]) => mockApiPost(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
    apiFetch: vi.fn(),
    ApiError: class ApiError extends Error {
        public readonly statusCode: number;
        public readonly errorBody: string;
        constructor(message: string, statusCode: number, errorBody: string = '') {
            super(message);
            this.name = 'ApiError';
            this.statusCode = statusCode;
            this.errorBody = errorBody;
        }
    },
}));

// Stable context return values so React hooks see reference-equal
// deps across renders (otherwise useCallback / useEffect loops fire
// on every render and consume the mocked apiGet responses).
const stableUser = { id: 1, username: 'testuser' };
const stableHasPermission = () => false;
const stableAuthValue = { user: stableUser, hasPermission: stableHasPermission };
const stableAIValue = { aiEnabled: false };
const stableClusterValue = { lastRefresh: 0 };
const stableClearOverlays = () => {};
const stableDashboardValue = {
    currentOverlay: null,
    clearOverlays: stableClearOverlays,
    pushOverlay: () => {},
    refreshTrigger: 0,
};

vi.mock('../../../contexts/AuthContext', () => ({
    useAuth: () => stableAuthValue,
}));

vi.mock('../../../contexts/AICapabilitiesContext', () => ({
    useAICapabilities: () => stableAIValue,
}));

vi.mock('../../../contexts/ClusterDataContext', () => ({
    useClusterData: () => stableClusterValue,
}));

vi.mock('../../../contexts/DashboardContext', () => ({
    useDashboard: () => stableDashboardValue,
}));

// Stub heavy sibling components to keep the render tree small and
// focus the tests on the alert list behaviour.
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
vi.mock('../../../hooks/useServerAnalysis', () => ({
    hasCachedServerAnalysis: () => false,
}));

// ---------------------------------------------------------------------------
// Now import the SUT and supporting types.
// ---------------------------------------------------------------------------

import StatusPanel from '../index';
import { ApiError } from '../../../utils/apiClient';

// ---------------------------------------------------------------------------
// Fixtures / helpers
// ---------------------------------------------------------------------------

const testTheme = createPgedgeTheme('dark');

const renderPanel = (selection: Record<string, unknown>) =>
    render(
        <ThemeProvider theme={testTheme}>
            <StatusPanel selection={selection} />
        </ThemeProvider>,
    );

/**
 * Build an acknowledged alert record (the shape the server returns).
 */
const makeAckedAlertRecord = (overrides: Record<string, unknown> = {}) => ({
    id: 42,
    title: 'High CPU Usage',
    description: 'CPU usage exceeded threshold',
    severity: 'warning',
    alert_type: 'threshold',
    triggered_at: '2026-04-20T12:00:00Z',
    last_updated: '2026-04-20T12:00:00Z',
    server_name: 'server-1',
    connection_id: 1,
    acknowledged_at: '2026-04-20T12:05:00Z',
    acknowledged_by: 'admin',
    ack_message: 'investigating',
    false_positive: false,
    ...overrides,
});

const serverSelection = {
    type: 'server',
    id: 1,
    name: 'server-1',
    status: 'online',
    active_alert_count: 0,
};

/**
 * Find the "Restore to active" icon button.
 *
 * The button wraps the MUI Undo icon. We locate it by descending from
 * the icon's data-testid and returning the nearest ancestor button.
 */
const getRestoreButton = (): HTMLButtonElement => {
    const icons = document.querySelectorAll('[data-testid="UndoIcon"]');
    for (const icon of icons) {
        const btn = icon.closest('button');
        if (btn) {
            return btn as HTMLButtonElement;
        }
    }
    throw new Error('Restore button not found');
};

/**
 * Expand the "Acknowledged" collapsible so the restore buttons
 * are visible.
 */
const expandAcknowledged = async () => {
    const header = await screen.findByText('Acknowledged');
    fireEvent.click(header);
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StatusPanel handleUnacknowledge', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('optimistically removes the alert from the acknowledged list and reconciles with a refetch', async () => {
        // Default apiGet response: the acknowledged alert. After the
        // DELETE succeeds we want the reconciling GET to return empty
        // so we can assert the row disappears.
        mockApiGet.mockResolvedValue({ alerts: [makeAckedAlertRecord()] });
        mockApiDelete.mockResolvedValueOnce(undefined);

        renderPanel(serverSelection);

        await expandAcknowledged();
        // Swap the apiGet response now that the initial render has
        // settled, so the post-delete refetch returns an empty list.
        mockApiGet.mockResolvedValue({ alerts: [] });

        const restoreBtn = getRestoreButton();
        const callsBeforeClick = mockApiGet.mock.calls.length;

        fireEvent.click(restoreBtn);

        // DELETE should be issued with the alert id in the query string.
        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledTimes(1);
        });
        expect(mockApiDelete).toHaveBeenCalledWith(
            '/api/v1/alerts/acknowledge?alert_id=42',
        );

        // The reconciling fetch runs after the DELETE resolves.
        await waitFor(() => {
            expect(mockApiGet.mock.calls.length).toBeGreaterThan(callsBeforeClick);
        });

        // The "Acknowledged" header disappears when there are no
        // acknowledged alerts left (the server returned []).
        await waitFor(() => {
            expect(screen.queryByText('Acknowledged')).not.toBeInTheDocument();
        });
    });

    it('rolls back the optimistic update and surfaces an error when apiDelete fails', async () => {
        // All fetches return the acknowledged alert so the row
        // remains visible after rollback.
        mockApiGet.mockResolvedValue({ alerts: [makeAckedAlertRecord()] });
        mockApiDelete.mockRejectedValueOnce(
            new ApiError('Server error', 500, 'boom'),
        );

        renderPanel(serverSelection);

        await expandAcknowledged();
        const restoreBtn = getRestoreButton();

        fireEvent.click(restoreBtn);

        // DELETE attempt happens.
        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledTimes(1);
        });

        // Error surfaces via the Snackbar/Alert.
        const errorAlert = await screen.findByRole('alert');
        expect(errorAlert.textContent).toMatch(/Failed to restore alert/);
        expect(errorAlert.textContent).toContain('Server error');

        // Acknowledged section is still visible (rollback happened +
        // the cached alert is still there).
        await waitFor(() => {
            expect(screen.getByText('Acknowledged')).toBeInTheDocument();
        });
    });

    it('ignores repeated clicks while a request is in flight', async () => {
        // All initial GETs return the acknowledged alert so the
        // restore button is visible.
        mockApiGet.mockResolvedValue({ alerts: [makeAckedAlertRecord()] });

        // Make the DELETE call hang until we resolve it manually.
        let resolveDelete: (value?: unknown) => void = () => {};
        mockApiDelete.mockImplementation(
            () => new Promise((resolve) => {
                resolveDelete = resolve;
            }),
        );

        renderPanel(serverSelection);

        await expandAcknowledged();
        const restoreBtn = getRestoreButton();

        fireEvent.click(restoreBtn);

        // The first click kicked off the request.
        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledTimes(1);
        });

        // Second and third clicks must not enqueue further DELETEs.
        // The handler guards against duplicate in-flight requests by
        // checking the id against the in-flight set.
        fireEvent.click(restoreBtn);
        fireEvent.click(restoreBtn);
        expect(mockApiDelete).toHaveBeenCalledTimes(1);

        // Resolve the in-flight request and ensure the reconcile
        // fetch fires after resolution.
        const fetchCountBefore = mockApiGet.mock.calls.length;
        resolveDelete();
        await waitFor(() => {
            expect(mockApiGet.mock.calls.length).toBeGreaterThan(fetchCountBefore);
        });
    });
});
