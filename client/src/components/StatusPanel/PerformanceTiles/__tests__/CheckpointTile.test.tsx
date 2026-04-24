/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CheckpointTile from '../CheckpointTile';
import type { ConnectionPerformance } from '../types';

// Mock the Chart component to avoid ECharts complexity in tests
vi.mock('../../../Chart/Chart', () => ({
    Chart: ({
        data,
        showLegend,
        stacked,
        areaFill,
        smooth,
        echartsOptions,
    }: {
        data: { series: Array<{ name: string }> };
        showLegend?: boolean;
        stacked?: boolean;
        areaFill?: boolean;
        smooth?: boolean;
        echartsOptions?: Record<string, unknown>;
    }) => (
        <div data-testid="mock-chart">
            {data.series.map(s => (
                <span key={s.name} data-testid={`series-${s.name}`}>{s.name}</span>
            ))}
            <span data-testid="show-legend">{String(showLegend)}</span>
            <span data-testid="stacked">{String(stacked)}</span>
            <span data-testid="area-fill">{String(areaFill)}</span>
            <span data-testid="smooth">{String(smooth)}</span>
            {echartsOptions && (
                <span data-testid="echarts-options">
                    {JSON.stringify(echartsOptions)}
                </span>
            )}
        </div>
    ),
}));

// Mock ChartAnalysisDialog
vi.mock('../../../ChartAnalysisDialog', () => ({
    ChartAnalysisDialog: ({
        open,
        onClose,
    }: {
        open: boolean;
        onClose: () => void;
    }) => (
        open ? (
            <div data-testid="chart-analysis-dialog">
                <button onClick={onClose} data-testid="close-dialog">Close</button>
            </div>
        ) : null
    ),
}));

// Mock AI capabilities context
const mockAIEnabled = vi.fn(() => true);
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: mockAIEnabled() }),
}));

// Mock hasCachedAnalysis
const mockHasCachedAnalysis = vi.fn(() => false);
vi.mock('../../../../hooks/useChartAnalysis', () => ({
    hasCachedAnalysis: () => mockHasCachedAnalysis(),
}));

const theme = createTheme();

const renderWithTheme = (ui: React.ReactElement) => {
    return render(
        <ThemeProvider theme={theme}>
            {ui}
        </ThemeProvider>
    );
};

const mockTimeSeries = [
    { time: '2024-01-01T10:00:00Z', write_time_ms: 50, sync_time_ms: 20 },
    { time: '2024-01-01T11:00:00Z', write_time_ms: 60, sync_time_ms: 25 },
    { time: '2024-01-01T12:00:00Z', write_time_ms: 45, sync_time_ms: 18 },
];

const createConnectionPerformance = (
    id: number,
    name: string,
    timeSeries = mockTimeSeries
): ConnectionPerformance => ({
    connection_id: id,
    connection_name: name,
    xid_age: [],
    cache_hit_ratio: {
        current: 99.5,
        time_series: [],
    },
    transactions: {
        commits_per_sec: 100,
        rollback_percent: 0.5,
        time_series: [],
    },
    checkpoints: {
        time_series: timeSeries,
    },
});

describe('CheckpointTile', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockAIEnabled.mockReturnValue(true);
        mockHasCachedAnalysis.mockReturnValue(false);
    });

    describe('loading state', () => {
        it('shows loading skeleton when loading', () => {
            renderWithTheme(
                <CheckpointTile
                    connections={[]}
                    loading={true}
                />
            );

            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });
    });

    describe('no data state', () => {
        it('shows no data message when connections is empty', () => {
            renderWithTheme(
                <CheckpointTile
                    connections={[]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });

        it('shows no data message when connection has no checkpoints data', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: 99.5,
                    time_series: [],
                },
                transactions: {
                    commits_per_sec: 100,
                    rollback_percent: 0.5,
                    time_series: [],
                },
                checkpoints: {
                    time_series: [],
                },
            };

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });

    describe('with data', () => {
        it('displays Checkpoints title', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('Checkpoints')).toBeInTheDocument();
        });

        it('renders chart with two series', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
            expect(screen.getByTestId('series-Write Time (ms)')).toBeInTheDocument();
            expect(screen.getByTestId('series-Sync Time (ms)')).toBeInTheDocument();
        });

        it('passes showLegend={false} to Chart component', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('show-legend')).toHaveTextContent('false');
        });

        it('passes stacked={true} to Chart component', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('stacked')).toHaveTextContent('true');
        });

        it('passes areaFill={true} to Chart component', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('area-fill')).toHaveTextContent('true');
        });

        it('passes smooth={true} to Chart component', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('smooth')).toHaveTextContent('true');
        });

        it('passes correct echartsOptions without legend config', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            // Should have grid configuration without legend space
            expect(options.grid).toBeDefined();
            expect(options.grid.bottom).toBe(20);

            // Should NOT have legend configuration
            expect(options.legend).toBeUndefined();

            // Should have xAxis and yAxis
            expect(options.xAxis).toBeDefined();
            expect(options.yAxis).toBeDefined();
        });

        it('sorts time series data chronologically', () => {
            const unsortedTimeSeries = [
                { time: '2024-01-01T12:00:00Z', write_time_ms: 45, sync_time_ms: 18 },
                { time: '2024-01-01T10:00:00Z', write_time_ms: 50, sync_time_ms: 20 },
                { time: '2024-01-01T11:00:00Z', write_time_ms: 60, sync_time_ms: 25 },
            ];
            const conn = createConnectionPerformance(1, 'Server 1', unsortedTimeSeries);

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            // The chart should render with sorted data
            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
        });

        it('combines data from multiple connections', () => {
            const conn1 = createConnectionPerformance(1, 'Server 1', [
                { time: '2024-01-01T10:00:00Z', write_time_ms: 50, sync_time_ms: 20 },
            ]);
            const conn2 = createConnectionPerformance(2, 'Server 2', [
                { time: '2024-01-01T11:00:00Z', write_time_ms: 60, sync_time_ms: 25 },
            ]);

            renderWithTheme(
                <CheckpointTile
                    connections={[conn1, conn2]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
        });
    });

    describe('AI analysis', () => {
        it('shows AI analysis button when AI is enabled and data is present', () => {
            mockAIEnabled.mockReturnValue(true);
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            expect(button).toBeInTheDocument();
        });

        it('hides AI analysis button when AI is disabled', () => {
            mockAIEnabled.mockReturnValue(false);
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.queryByRole('button', { name: /ai analysis/i })).not.toBeInTheDocument();
        });

        it('opens analysis dialog when AI button is clicked', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            fireEvent.click(button);

            expect(screen.getByTestId('chart-analysis-dialog')).toBeInTheDocument();
        });

        it('closes analysis dialog when close button is clicked', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            fireEvent.click(button);

            expect(screen.getByTestId('chart-analysis-dialog')).toBeInTheDocument();

            const closeButton = screen.getByTestId('close-dialog');
            fireEvent.click(closeButton);

            expect(screen.queryByTestId('chart-analysis-dialog')).not.toBeInTheDocument();
        });

        it('uses warning color when analysis is cached', () => {
            mockHasCachedAnalysis.mockReturnValue(true);
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            expect(button).toHaveClass('MuiIconButton-colorWarning');
        });

        it('uses secondary color when analysis is not cached', () => {
            mockHasCachedAnalysis.mockReturnValue(false);
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            expect(button).toHaveClass('MuiIconButton-colorSecondary');
        });
    });

    describe('xAxis configuration', () => {
        it('configures xAxis with time formatting', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            expect(options.xAxis.boundaryGap).toBe(false);
            expect(options.xAxis.axisLabel).toBeDefined();
            expect(options.xAxis.axisLabel.hideOverlap).toBe(true);
        });

        it('configures yAxis with splitNumber', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            expect(options.yAxis.splitNumber).toBe(3);
        });
    });

    describe('edge cases', () => {
        it('handles connection with undefined checkpoints', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: 99.5,
                    time_series: [],
                },
                transactions: {
                    commits_per_sec: 100,
                    rollback_percent: 0.5,
                    time_series: [],
                },
                checkpoints: undefined as unknown as ConnectionPerformance['checkpoints'],
            };

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });

        it('handles connection with null time_series', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: 99.5,
                    time_series: [],
                },
                transactions: {
                    commits_per_sec: 100,
                    rollback_percent: 0.5,
                    time_series: [],
                },
                checkpoints: {
                    time_series: null as unknown as ConnectionPerformance['checkpoints']['time_series'],
                },
            };

            renderWithTheme(
                <CheckpointTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });
});
