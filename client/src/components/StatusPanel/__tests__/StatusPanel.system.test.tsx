/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for StatusPanel's handling of system alerts (alerts about the
 * Workbench itself, with a null connection_id) and for the panel's
 * wiring of its dialogs, metrics and overlays. The dialogs and heavy
 * siblings are replaced by small stubs that expose their callbacks as
 * buttons, so the tests can drive the panel's handlers directly.
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, act, fireEvent, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';
import type { Selection } from '../../../types/selection';

// ---------------------------------------------------------------------------
// Module mocks
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
let mockCanManageRules = false;
const hasPermission = () => mockCanManageRules;
const aiValue = { aiEnabled: false };
const stableClusterValue = { lastRefresh: 0 };
const dashboardValue: {
    currentOverlay: Record<string, unknown> | null;
    clearOverlays: () => void;
    pushOverlay: () => void;
    refreshTrigger: number;
} = {
    currentOverlay: null,
    clearOverlays: () => {},
    pushOverlay: () => {},
    refreshTrigger: 0,
};

vi.mock('../../../contexts/useAuth', () => ({
    useAuth: () => ({ user: stableUser, hasPermission }),
}));
vi.mock('../../../contexts/useAICapabilities', () => ({ useAICapabilities: () => aiValue }));
vi.mock('../../../contexts/useClusterData', () => ({ useClusterData: () => stableClusterValue }));
vi.mock('../../../contexts/useDashboard', () => ({ useDashboard: () => dashboardValue }));

type StubProps = Record<string, unknown>;
const click = (props: StubProps, name: string, ...args: unknown[]) => () => {
    (props[name] as (...a: unknown[]) => void)(...args);
};

vi.mock('../../EventTimeline', () => ({ default: () => null }));
vi.mock('../../BlackoutPanel', () => ({ default: () => null }));
vi.mock('../../AlertAnalysisDialog', () => ({
    default: (props: StubProps) => (props.open ? (
        <div data-testid="analysis-dialog">
            {String((props.alert as { title?: string } | null)?.title)}
            <button type="button" onClick={click(props, 'onAnalysisComplete', 99, 'fresh analysis')}>
                complete analysis
            </button>
            <button type="button" onClick={click(props, 'onClose')}>close analysis</button>
        </div>
    ) : null),
}));
vi.mock('../../ServerAnalysisDialog', () => ({
    default: (props: StubProps) => (props.open ? (
        <button type="button" onClick={click(props, 'onClose')}>close server analysis</button>
    ) : null),
}));
vi.mock('../../AlertOverrideEditDialog', () => ({
    default: (props: StubProps) => (props.open ? (
        <button type="button" onClick={click(props, 'onClose')}>close override</button>
    ) : null),
}));
vi.mock('../../BlackoutManagementDialog', () => ({
    default: (props: StubProps) => (props.open ? (
        <button type="button" onClick={click(props, 'onClose')}>close blackouts</button>
    ) : null),
}));
vi.mock('../../AIOverview', () => ({
    default: (props: StubProps) => (
        <div data-testid="ai-overview">
            {typeof props.onAnalyze === 'function' && (
                <button type="button" onClick={click(props, 'onAnalyze')}>analyse server</button>
            )}
        </div>
    ),
}));
vi.mock('../../Dashboard', () => ({
    ServerDashboard: () => <div>server dashboard</div>,
    EstateDashboard: () => <div>estate dashboard</div>,
    ClusterDashboard: () => <div>cluster dashboard</div>,
    DatabaseDashboard: (props: StubProps) => (
        <div>database dashboard {String(props.connectionId)} {String(props.databaseName)}</div>
    ),
    ObjectDashboard: (props: StubProps) => (
        <div>object dashboard {String(props.schemaName)}.{String(props.objectName)}</div>
    ),
    MetricOverlay: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../../Dashboard/ClusterDashboard/TopologySection', () => ({ default: () => <div>topology</div> }));
vi.mock('../../Dashboard/CollapsibleSection', () => ({
    default: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../../Dashboard/TimeRangeSelector', () => ({ default: () => null }));
vi.mock('../SelectionHeader', () => ({
    default: (props: StubProps) => (
        <button type="button" onClick={click(props, 'onBlackoutClick')}>open blackouts</button>
    ),
}));
vi.mock('../ServerInfoCard', () => ({ default: () => null }));
vi.mock('../PerformanceTiles', () => ({ default: () => null }));
vi.mock('../AcknowledgeDialog', () => ({
    default: (props: StubProps) => (props.open ? (
        <div data-testid="ack-dialog">
            <button type="button" onClick={click(props, 'onConfirm', 99, 'noted', true)}>confirm ack</button>
            <button type="button" onClick={click(props, 'onConfirmMultiple', [99, 98], '')}>confirm group ack</button>
            <button type="button" onClick={click(props, 'onClose')}>close ack</button>
        </div>
    ) : null),
}));
vi.mock('../../../hooks/useServerAnalysis', () => ({ hasCachedServerAnalysis: () => false }));

import StatusPanel from '../index';

const renderPanel = (selection: Selection | null, mode: 'light' | 'dark' = 'dark') =>
    render(
        <ThemeProvider theme={createPgedgeTheme(mode)}>
            <StatusPanel selection={selection} />
        </ThemeProvider>,
    );

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

const makeAlertRecord = (overrides: Record<string, unknown> = {}) => ({
    id: 99,
    title: 'High CPU Usage',
    description: 'CPU usage exceeded threshold',
    severity: 'warning',
    alert_type: 'threshold',
    rule_id: 4,
    triggered_at: ago(5 * MINUTE),
    last_updated: ago(5 * MINUTE),
    server_name: 'server-1',
    connection_id: 1,
    ...overrides,
});

// A system alert as the server sends it: no connection, and no
// server_name at all, because the field is omitted when empty.
const systemAlertRecord = {
    id: 98,
    title: 'Anomaly detection degraded: Tier 2 embedding provider ollama failing',
    description: 'Provider ollama has failed 3 consecutive times.',
    severity: 'warning',
    alert_type: 'system',
    triggered_at: ago(2 * HOUR),
    connection_id: null,
    object_name: 'ollama/nomic-embed-text',
};

const serverSelection: Selection = {
    type: 'server',
    id: 1,
    name: 'server-1',
    status: 'online',
    description: 'Test server',
    host: 'db.example.com',
    port: 5432,
    role: 'primary',
    version: '17.0',
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
    servers: [
        { id: 1, name: 's1', status: 'online' },
        { id: 2, name: 's2', status: 'offline' },
        { id: 3, name: 's3', status: 'online', active_alert_count: 2 },
    ] as never,
    serverIds: [1, 2, 3],
};

const estateSelection: Selection = {
    type: 'estate',
    name: 'Estate',
    status: 'online',
    groups: [
        {
            id: 'g1',
            name: 'Group 1',
            clusters: [
                {
                    id: 'c1',
                    name: 'Cluster 1',
                    servers: [
                        {
                            id: 1,
                            name: 's1',
                            status: 'online',
                            children: [{ id: 2, name: 's2', status: 'offline' }],
                        },
                    ],
                },
                { id: 'c2', name: 'Cluster 2', servers: null },
            ],
        },
        { id: 'g2', name: 'Group 2', clusters: null },
    ] as never,
};

const metricValue = (label: string) =>
    screen.getByText(label).closest('.MuiPaper-root')?.textContent ?? '';

// getFriendlyTitle title-cases the prefix of the system alert title.
const SYSTEM_TITLE = 'Anomaly Detection Degraded: Tier 2 embedding provider ollama failing';

describe('StatusPanel system alerts', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        aiValue.aiEnabled = true;
        mockCanManageRules = true;
        dashboardValue.currentOverlay = null;
    });

    it('labels an estate system alert as the Workbench and offers no analysis', async () => {
        mockApiGet.mockResolvedValue({ alerts: [systemAlertRecord] });

        renderPanel(estateSelection);

        expect(await screen.findByText(SYSTEM_TITLE)).toBeInTheDocument();
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/alerts?exclude_cleared=true');
        expect(screen.getByText('AI DBA Workbench')).toBeInTheDocument();
        expect(screen.getByText('System')).toBeInTheDocument();
        expect(screen.getByText('2 hours ago')).toBeInTheDocument();
        expect(screen.queryByLabelText('Analyze with AI')).not.toBeInTheDocument();
        expect(screen.queryByLabelText('Edit alert override')).not.toBeInTheDocument();
    });

    it('treats a null connection as a system alert whatever its type', async () => {
        mockApiGet.mockResolvedValue({
            alerts: [{ ...systemAlertRecord, alert_type: 'threshold' }],
        });

        renderPanel(estateSelection, 'light');

        expect(await screen.findByText('AI DBA Workbench')).toBeInTheDocument();
        expect(screen.queryByLabelText('Analyze with AI')).not.toBeInTheDocument();
    });

    it('does not fetch for a cluster with no servers', async () => {
        renderPanel({ ...clusterSelection, servers: [], serverIds: [] } as Selection);

        await act(async () => {
            await Promise.resolve();
        });

        expect(mockApiGet).not.toHaveBeenCalled();
    });

    it('hides acknowledge on a system alert from a user without manage_alert_rules', async () => {
        mockCanManageRules = false;
        mockApiGet.mockResolvedValue({ alerts: [systemAlertRecord, makeAlertRecord()] });

        renderPanel(estateSelection);

        await screen.findByText(SYSTEM_TITLE);
        // Only the connection alert keeps its acknowledge action.
        expect(screen.getAllByLabelText('Acknowledge')).toHaveLength(1);
    });

    it('lets a permitted user acknowledge a system alert', async () => {
        mockApiGet.mockResolvedValue({ alerts: [{ ...systemAlertRecord, id: 99 }] });
        mockApiPost.mockResolvedValue({});

        renderPanel(estateSelection);

        await screen.findByText(SYSTEM_TITLE);
        fireEvent.click(screen.getByLabelText('Acknowledge').querySelector('button') as HTMLElement);
        fireEvent.click(await screen.findByText('confirm ack'));

        await waitFor(() => {
            expect(mockApiPost).toHaveBeenCalledWith('/api/v1/alerts/acknowledge', {
                alert_id: 99,
                message: 'noted',
                false_positive: true,
            });
        });
        await waitFor(() => {
            expect(screen.queryByTestId('ack-dialog')).not.toBeInTheDocument();
        });
    });
});

describe('StatusPanel wiring', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        aiValue.aiEnabled = true;
        mockCanManageRules = true;
        dashboardValue.currentOverlay = null;
    });

    it('shows the welcome state with AI enabled and disabled', () => {
        const { unmount } = renderPanel(null);
        expect(screen.getByTestId('ai-overview')).toBeInTheDocument();
        expect(screen.getByText(/AI can make mistakes/)).toBeInTheDocument();
        unmount();

        aiValue.aiEnabled = false;
        renderPanel(null, 'light');
        expect(screen.getByText('Welcome to AI DBA Workbench')).toBeInTheDocument();
        expect(screen.queryByText(/AI can make mistakes/)).not.toBeInTheDocument();
    });

    it('computes cluster metrics and renders the topology', async () => {
        mockApiGet.mockResolvedValue({ alerts: [] });

        renderPanel(clusterSelection);

        await waitFor(() => { expect(mockApiGet).toHaveBeenCalled(); });
        expect(metricValue('OK')).toBe('OK1');
        expect(metricValue('Warning')).toBe('Warning1');
        expect(metricValue('Offline')).toBe('Offline1');
        expect(screen.getByText('topology')).toBeInTheDocument();
        expect(screen.getByText('cluster dashboard')).toBeInTheDocument();
    });

    it('computes estate metrics across groups and nested servers', async () => {
        mockApiGet.mockResolvedValue({ alerts: [] });

        renderPanel(estateSelection, 'light');

        await waitFor(() => { expect(mockApiGet).toHaveBeenCalled(); });
        expect(metricValue('Clusters')).toBe('Clusters2');
        expect(metricValue('Groups')).toBe('Groups2');
        expect(metricValue('OK')).toBe('OK1');
        expect(metricValue('Offline')).toBe('Offline1');
        expect(screen.getByText('estate dashboard')).toBeInTheDocument();
    });

    it('formats relative times across ranges', async () => {
        mockApiGet.mockResolvedValue({
            alerts: [
                makeAlertRecord({ id: 1, title: 'A', triggered_at: ago(10 * 1000), last_updated: undefined }),
                makeAlertRecord({ id: 2, title: 'B', triggered_at: ago(1 * HOUR + MINUTE), severity: 'critical' }),
                makeAlertRecord({ id: 3, title: 'C', triggered_at: ago(1 * DAY + HOUR), severity: 'info' }),
                makeAlertRecord({ id: 4, title: 'D', triggered_at: ago(3 * DAY), severity: undefined }),
                makeAlertRecord({ id: 5, title: 'E', triggered_at: ago(30 * DAY) }),
                makeAlertRecord({ id: 6, title: 'F', triggered_at: undefined }),
            ],
        });

        renderPanel(serverSelection);

        expect(await screen.findByText('just now')).toBeInTheDocument();
        expect(screen.getByText('1 hour ago')).toBeInTheDocument();
        expect(screen.getByText('1 day ago')).toBeInTheDocument();
        expect(screen.getByText('3 days ago')).toBeInTheDocument();
    });

    it('opens and closes the analysis dialog and keeps the fresh analysis', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });

        renderPanel(serverSelection);

        await screen.findByText('High CPU Usage');
        fireEvent.click(screen.getByLabelText('Analyze with AI'));
        expect(screen.getByTestId('analysis-dialog')).toHaveTextContent('High CPU Usage');

        fireEvent.click(screen.getByText('complete analysis'));
        expect(await screen.findByLabelText('View cached analysis')).toBeInTheDocument();

        fireEvent.click(screen.getByText('close analysis'));
        expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();
    });

    it('keeps a locally cached analysis until the server has it', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });

        const { rerender } = renderPanel(serverSelection);

        await screen.findByText('High CPU Usage');
        fireEvent.click(screen.getByLabelText('Analyze with AI'));
        fireEvent.click(screen.getByText('complete analysis'));

        // Refetch without the analysis on the server: the local copy stays.
        rerender(
            <ThemeProvider theme={createPgedgeTheme('dark')}>
                <StatusPanel selection={{ ...serverSelection, id: 1, name: 'renamed' }} />
            </ThemeProvider>,
        );
        await waitFor(() => { expect(mockApiGet.mock.calls.length).toBeGreaterThan(1); });
        expect(await screen.findByLabelText('View cached analysis')).toBeInTheDocument();

        // Once the server returns it, the local entry is dropped.
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord({ ai_analysis: 'server copy' })] });
        rerender(
            <ThemeProvider theme={createPgedgeTheme('dark')}>
                <StatusPanel selection={{ ...serverSelection, id: 1, name: 'renamed again' }} />
            </ThemeProvider>,
        );
        await waitFor(() => { expect(mockApiGet.mock.calls.length).toBeGreaterThan(2); });
        expect(await screen.findByLabelText('View cached analysis')).toBeInTheDocument();
    });

    it('opens and closes the override, blackout and server analysis dialogs', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord()] });

        renderPanel(serverSelection);

        await screen.findByText('High CPU Usage');
        fireEvent.click(screen.getByLabelText('Edit alert override'));
        fireEvent.click(screen.getByText('close override'));
        expect(screen.queryByText('close override')).not.toBeInTheDocument();

        fireEvent.click(screen.getByText('open blackouts'));
        fireEvent.click(screen.getByText('close blackouts'));
        expect(screen.queryByText('close blackouts')).not.toBeInTheDocument();

        fireEvent.click(screen.getByText('analyse server'));
        fireEvent.click(screen.getByText('close server analysis'));
        expect(screen.queryByText('close server analysis')).not.toBeInTheDocument();
    });

    it('acknowledges a group of alerts and closes the dialog', async () => {
        mockApiGet.mockResolvedValue({
            alerts: [makeAlertRecord(), makeAlertRecord({ id: 98, server_name: 'server-2' })],
        });
        mockApiPost.mockResolvedValue({});

        renderPanel(clusterSelection);

        await screen.findByText('2 instances');
        fireEvent.click(screen.getByLabelText('Acknowledge all in group'));
        fireEvent.click(await screen.findByText('confirm group ack'));

        await waitFor(() => { expect(mockApiPost).toHaveBeenCalledTimes(2); });
        expect(mockApiPost).toHaveBeenCalledWith('/api/v1/alerts/acknowledge', {
            alert_id: 98,
            message: '',
            false_positive: false,
        });
        await waitFor(() => {
            expect(screen.queryByTestId('ack-dialog')).not.toBeInTheDocument();
        });
    });

    it('closes the acknowledge dialog even when the request fails', async () => {
        mockApiGet.mockResolvedValue({ alerts: [makeAlertRecord(), makeAlertRecord({ id: 98 })] });
        mockApiPost.mockRejectedValue(new Error('nope'));

        renderPanel(clusterSelection);

        await screen.findByText('2 instances');
        fireEvent.click(screen.getByLabelText('Acknowledge all in group'));
        fireEvent.click(await screen.findByText('confirm group ack'));
        await waitFor(() => {
            expect(screen.queryByTestId('ack-dialog')).not.toBeInTheDocument();
        });

        fireEvent.click(screen.getAllByLabelText('Acknowledge')[0].querySelector('button') as HTMLElement);
        fireEvent.click(await screen.findByText('confirm ack'));
        await waitFor(() => {
            expect(screen.queryByTestId('ack-dialog')).not.toBeInTheDocument();
        });

        fireEvent.click(screen.getAllByLabelText('Acknowledge')[0].querySelector('button') as HTMLElement);
        fireEvent.click(await screen.findByText('close ack'));
        expect(screen.queryByTestId('ack-dialog')).not.toBeInTheDocument();
    });

    it('renders the database and object overlays', async () => {
        mockApiGet.mockResolvedValue({ alerts: [] });
        dashboardValue.currentOverlay = {
            level: 'database',
            entityName: 'appdb',
            connectionId: 1,
            connectionName: 'server-1',
        };

        const { unmount } = renderPanel(serverSelection);
        expect(screen.getByText('database dashboard 1 appdb')).toBeInTheDocument();
        unmount();

        dashboardValue.currentOverlay = {
            level: 'object',
            objectType: 'table',
            entityName: 'orders',
        };
        const second = renderPanel(serverSelection);
        expect(screen.getByText('object dashboard public.orders')).toBeInTheDocument();
        second.unmount();

        dashboardValue.currentOverlay = { level: 'database', entityName: 'fallback' };
        const third = renderPanel(serverSelection);
        expect(screen.getByText('database dashboard 0 fallback')).toBeInTheDocument();
        third.unmount();

        dashboardValue.currentOverlay = { level: 'object', entityName: 'no type' };
        renderPanel(serverSelection);
        expect(screen.queryByText(/object dashboard/)).not.toBeInTheDocument();
        await waitFor(() => { expect(mockApiGet).toHaveBeenCalled(); });
    });
});
