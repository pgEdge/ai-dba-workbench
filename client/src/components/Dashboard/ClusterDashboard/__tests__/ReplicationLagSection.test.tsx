/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ReplicationLagSection from '../ReplicationLagSection';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries } from '../../types';
import type { ClusterSelection } from '../../../../types/selection';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockUseMetrics = vi.fn<(p: MetricQueryParams | null) => UseMetricsReturn>();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => mockUseMetrics(params),
}));

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: '1h' },
    }),
}));

vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: false }),
}));

vi.mock('../../../Chart', () => ({
    Chart: ({ title, data, echartsOptions }: {
        title: string;
        data: {
            categories?: string[];
            series: { name: string; data: (number | null)[] }[];
        };
        echartsOptions?: unknown;
    }) => (
        <div
            data-testid="chart"
            data-title={title}
            data-categories={(data.categories ?? []).join(',')}
            data-axis-label={String(
                (echartsOptions as {
                    yAxis: { axisLabel: { formatter: (v: number) => string } };
                }).yAxis.axisLabel.formatter(90),
            )}
            data-tooltip-value={String(
                (echartsOptions as {
                    tooltip: { valueFormatter: (v: number) => string };
                }).tooltip.valueFormatter(1.5),
            )}
        >
            {data.series.map((s) => (
                <span
                    key={s.name}
                    data-testid="chart-series"
                    data-series={s.name}
                    data-values={JSON.stringify(s.data)}
                />
            ))}
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a MetricSeries for the given metric and values. */
const series = (metric: string, values: (number | null)[]): MetricSeries => ({
    name: metric,
    metric,
    data: values.map((value, idx) => ({
        time: `2026-01-01T00:0${idx}:00Z`,
        value,
    })),
});

const ready = (data: MetricSeries[] | null): UseMetricsReturn => ({
    data,
    window: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

const loading = (): UseMetricsReturn => ({
    data: null,
    window: null,
    loading: true,
    error: null,
    refetch: vi.fn(),
});

const failed = (message: string): UseMetricsReturn => ({
    data: null,
    window: null,
    loading: false,
    error: message,
    refetch: vi.fn(),
});

const selectionWith = (
    servers: ClusterSelection['servers'],
): ClusterSelection => ({
    type: 'cluster',
    id: 'c1',
    name: 'Cluster',
    description: '',
    status: 'ok',
    servers,
    serverIds: servers.map(s => s.id),
});

const PRIMARY_SELECTION = selectionWith([
    { id: 7, name: 'replica', role: 'binary_standby' },
    {
        id: 9,
        name: 'group',
        children: [{ id: 3, name: 'primary', primary_role: 'binary_primary' }],
    },
] as ClusterSelection['servers']);

const renderSection = (selection = PRIMARY_SELECTION) => render(
    <ReplicationLagSection selection={selection} serverIds={[3, 7]} />,
);

const seriesValues = (name: string): string =>
    screen.getAllByTestId('chart-series')
        .filter(el => el.getAttribute('data-series') === name)
        .map(el => el.getAttribute('data-values') ?? '')[0];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ReplicationLagSection', () => {
    beforeEach(() => {
        mockUseMetrics.mockReset();
        mockUseMetrics.mockImplementation(() => ready([
            series('replay_lag', [0.5, 2, 45]),
            series('write_lag', [0.1, 0.2, 10]),
            series('flush_lag', [0.1, 0.2, 0.3]),
            series('sent_bytes', [1, 2, 3]),
        ]));
    });

    it('queries the primary server found in nested children', () => {
        renderSection();
        expect(mockUseMetrics).toHaveBeenCalledWith(expect.objectContaining({
            probeName: 'pg_stat_replication',
            connectionId: 3,
        }));
    });

    it('reports when no primary server is present', () => {
        mockUseMetrics.mockImplementation(() => ready(null));
        renderSection(selectionWith([
            { id: 7, name: 'replica', role: 'binary_standby' },
        ] as ClusterSelection['servers']));
        expect(mockUseMetrics).toHaveBeenCalledWith(null);
        expect(screen.getByText('No primary server detected in this cluster.'))
            .toBeInTheDocument();
    });

    it('shows a spinner while loading and the error when it fails', () => {
        mockUseMetrics.mockImplementation(() => loading());
        const { unmount } = renderSection();
        expect(screen.getByLabelText('Loading replication data'))
            .toBeInTheDocument();
        unmount();

        mockUseMetrics.mockImplementation(() => failed('boom'));
        renderSection();
        expect(screen.getByText('boom')).toBeInTheDocument();
    });

    it('explains when no lag series are returned', () => {
        mockUseMetrics.mockImplementation(() => ready([
            series('sent_bytes', [1]),
        ]));
        renderSection();
        expect(screen.getByText(/No replication lag data available/))
            .toBeInTheDocument();
    });

    it('renders a tile per lag series with the latest value and status', () => {
        renderSection();
        expect(screen.getByText('Replay Lag')).toBeInTheDocument();
        expect(screen.getByText('45.0 s')).toBeInTheDocument();
        expect(screen.getByText('10.0 s')).toBeInTheDocument();
        expect(screen.getByText('300 ms')).toBeInTheDocument();
        expect(screen.queryByText('Sent Bytes')).not.toBeInTheDocument();
        expect(screen.getByTestId('chart'))
            .toHaveAttribute('data-title', 'Replication Lag Over Time');
    });

    it('uses the latest non-null reading and passes gaps to the chart', () => {
        mockUseMetrics.mockImplementation(() => ready([
            series('replay_lag', [null, 2, null]),
        ]));
        renderSection();
        expect(screen.getByText('2.0 s')).toBeInTheDocument();
        expect(seriesValues('Replay Lag')).toBe('[null,2,null]');
    });

    it('shows the placeholder for an all-null lag series', () => {
        mockUseMetrics.mockImplementation(() => ready([
            series('replay_lag', [null, null]),
        ]));
        renderSection();
        expect(screen.getByText('Replay Lag')).toBeInTheDocument();
        expect(screen.getByText('--')).toBeInTheDocument();
    });
    it('formats the axis and tooltip values as lag durations', () => {
        renderSection();
        const chart = screen.getByTestId('chart');
        expect(chart).toHaveAttribute('data-axis-label', '1.5 min');
        expect(chart).toHaveAttribute('data-tooltip-value', '1.5 s');
    });

    it('anchors the axis to the queried window (issue #430)', () => {
        mockUseMetrics.mockImplementation(() => ({
            data: [{
                name: 'replay_lag',
                metric: 'replay_lag',
                data: [
                    { time: '2026-01-01T00:30:00.000Z', value: 4 },
                    {
                        time: '2026-01-01T00:45:00.000Z',
                        value: 4,
                        filled: true,
                    },
                ],
            }],
            window: {
                start: '2026-01-01T00:00:00.000Z',
                end: '2026-01-01T01:00:00.000Z',
                bucketSeconds: 900,
            },
            loading: false,
            error: null,
            refetch: vi.fn(),
        }));
        renderSection();

        expect(screen.getByTestId('chart'))
            .toHaveAttribute(
                'data-categories',
                '2026-01-01T00:00:00.000Z,2026-01-01T00:15:00.000Z,'
                + '2026-01-01T00:30:00.000Z,2026-01-01T00:45:00.000Z',
            );
        expect(seriesValues('Replay Lag')).toBe('[null,null,4,4]');
    });
});
