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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import TransactionTile from '../TransactionTile';
import { ConnectionPerformance } from '../types';

// Mock the Chart component to avoid ECharts complexity in tests
vi.mock('../../../Chart/Chart', () => ({
    Chart: ({
        data,
        showLegend,
        echartsOptions,
    }: {
        data: { series: Array<{ name: string }> };
        showLegend?: boolean;
        echartsOptions?: Record<string, unknown>;
    }) => (
        <div data-testid="mock-chart">
            {data.series.map(s => (
                <span key={s.name} data-testid={`series-${s.name}`}>{s.name}</span>
            ))}
            <span data-testid="show-legend">{String(showLegend)}</span>
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
    { time: '2024-01-01T10:00:00Z', commits_per_sec: 100, rollback_percent: 0.5 },
    { time: '2024-01-01T11:00:00Z', commits_per_sec: 150, rollback_percent: 0.3 },
    { time: '2024-01-01T12:00:00Z', commits_per_sec: 120, rollback_percent: 0.8 },
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
        commits_per_sec: timeSeries[0]?.commits_per_sec ?? 100,
        rollback_percent: timeSeries[0]?.rollback_percent ?? 0.5,
        time_series: timeSeries,
    },
    checkpoints: {
        time_series: [],
    },
});

describe('TransactionTile', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockAIEnabled.mockReturnValue(true);
        mockHasCachedAnalysis.mockReturnValue(false);
    });

    describe('loading state', () => {
        it('shows loading skeleton when loading', () => {
            renderWithTheme(
                <TransactionTile
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
                <TransactionTile
                    connections={[]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });

        it('shows no data message when connection has no transactions data', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: 99.5,
                    time_series: [],
                },
                transactions: {
                    commits_per_sec: 0,
                    rollback_percent: 0,
                    time_series: [],
                },
                checkpoints: {
                    time_series: [],
                },
            };

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });

    describe('with data', () => {
        it('displays Transactions title', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('Transactions')).toBeInTheDocument();
        });

        it('renders chart with two series', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
            expect(screen.getByTestId('series-Commits/sec')).toBeInTheDocument();
            expect(screen.getByTestId('series-Rollback %')).toBeInTheDocument();
        });

        it('passes showLegend={false} to Chart component', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByTestId('show-legend')).toHaveTextContent('false');
        });

        it('passes correct echartsOptions without legend config', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
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
            expect(Array.isArray(options.yAxis)).toBe(true);
            expect(options.yAxis.length).toBe(2);
        });

        it('sorts time series data chronologically', () => {
            const unsortedTimeSeries = [
                { time: '2024-01-01T12:00:00Z', commits_per_sec: 120, rollback_percent: 0.8 },
                { time: '2024-01-01T10:00:00Z', commits_per_sec: 100, rollback_percent: 0.5 },
                { time: '2024-01-01T11:00:00Z', commits_per_sec: 150, rollback_percent: 0.3 },
            ];
            const conn = createConnectionPerformance(1, 'Server 1', unsortedTimeSeries);

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            // The series data should be sorted by time
            expect(options.series[0].data[0]).toBe(100); // 10:00
            expect(options.series[0].data[1]).toBe(150); // 11:00
            expect(options.series[0].data[2]).toBe(120); // 12:00
        });

        it('combines data from multiple connections', () => {
            const conn1 = createConnectionPerformance(1, 'Server 1', [
                { time: '2024-01-01T10:00:00Z', commits_per_sec: 100, rollback_percent: 0.5 },
            ]);
            const conn2 = createConnectionPerformance(2, 'Server 2', [
                { time: '2024-01-01T11:00:00Z', commits_per_sec: 200, rollback_percent: 1.0 },
            ]);

            renderWithTheme(
                <TransactionTile
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
                <TransactionTile
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
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.queryByRole('button', { name: /ai analysis/i })).not.toBeInTheDocument();
        });

        it('opens analysis dialog when AI button is clicked', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
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
                <TransactionTile
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
                <TransactionTile
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
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const button = screen.getByRole('button', { name: /ai analysis/i });
            expect(button).toHaveClass('MuiIconButton-colorSecondary');
        });
    });

    describe('dual axis configuration', () => {
        it('configures first y-axis for commits/sec', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            expect(options.yAxis[0].type).toBe('value');
            expect(options.yAxis[0].splitLine.show).toBe(true);
        });

        it('configures second y-axis for rollback percentage with max 100', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            expect(options.yAxis[1].type).toBe('value');
            expect(options.yAxis[1].max).toBe(100);
            expect(options.yAxis[1].splitLine.show).toBe(false);
        });

        it('configures commits/sec series with area style', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            const commitsSeries = options.series.find(
                (s: { name: string }) => s.name === 'Commits/sec'
            );
            expect(commitsSeries.yAxisIndex).toBe(0);
            expect(commitsSeries.areaStyle).toBeDefined();
        });

        it('configures rollback series with dashed line style', () => {
            const conn = createConnectionPerformance(1, 'Server 1');

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            const optionsElement = screen.getByTestId('echarts-options');
            const options = JSON.parse(optionsElement.textContent || '{}');

            const rollbackSeries = options.series.find(
                (s: { name: string }) => s.name === 'Rollback %'
            );
            expect(rollbackSeries.yAxisIndex).toBe(1);
            expect(rollbackSeries.lineStyle.type).toBe('dashed');
        });
    });

    describe('edge cases', () => {
        it('handles connection with undefined transactions', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: 99.5,
                    time_series: [],
                },
                transactions: undefined as unknown as ConnectionPerformance['transactions'],
                checkpoints: {
                    time_series: [],
                },
            };

            renderWithTheme(
                <TransactionTile
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
                    time_series: null as unknown as ConnectionPerformance['transactions']['time_series'],
                },
                checkpoints: {
                    time_series: [],
                },
            };

            renderWithTheme(
                <TransactionTile
                    connections={[conn]}
                    loading={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });
});
