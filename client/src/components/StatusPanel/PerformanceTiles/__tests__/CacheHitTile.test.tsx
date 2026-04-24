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
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CacheHitTile from '../CacheHitTile';
import type { ConnectionPerformance, DatabaseCacheHitData } from '../types';

// Mock the Chart component to avoid ECharts complexity in tests
vi.mock('../../../Chart/Chart', () => ({
    Chart: ({ data }: { data: { series: Array<{ name: string }> } }) => (
        <div data-testid="mock-chart">
            {data.series.map(s => (
                <span key={s.name} data-testid={`series-${s.name}`}>{s.name}</span>
            ))}
        </div>
    ),
}));

// Mock ChartAnalysisDialog
vi.mock('../../../ChartAnalysisDialog', () => ({
    ChartAnalysisDialog: () => null,
}));

// Mock AI capabilities context
vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: false }),
}));

// Mock hasCachedAnalysis
vi.mock('../../../../hooks/useChartAnalysis', () => ({
    hasCachedAnalysis: () => false,
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
    { time: '2024-01-01T10:00:00Z', value: 95.5 },
    { time: '2024-01-01T11:00:00Z', value: 96.0 },
    { time: '2024-01-01T12:00:00Z', value: 94.8 },
];

const createConnectionPerformance = (
    id: number,
    name: string,
    cacheHitCurrent: number,
    timeSeries = mockTimeSeries
): ConnectionPerformance => ({
    connection_id: id,
    connection_name: name,
    xid_age: [],
    cache_hit_ratio: {
        current: cacheHitCurrent,
        time_series: timeSeries,
    },
    transactions: {
        commits_per_sec: 100,
        rollback_percent: 0.5,
        time_series: [],
    },
    checkpoints: {
        time_series: [],
    },
});

const createDatabaseCacheHitData = (
    dbName: string,
    current: number,
    timeSeries = mockTimeSeries
): DatabaseCacheHitData => ({
    database_name: dbName,
    cache_hit_ratio: {
        current,
        time_series: timeSeries,
    },
});

describe('CacheHitTile', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('loading state', () => {
        it('shows loading skeleton when loading', () => {
            renderWithTheme(
                <CacheHitTile
                    connections={[]}
                    loading={true}
                    isMultiServer={false}
                />
            );

            // TileContainer uses MUI Skeleton for loading state
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });
    });

    describe('no data state', () => {
        it('shows no data message when connections is empty', () => {
            renderWithTheme(
                <CacheHitTile
                    connections={[]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });

        it('shows no data message when connection has no cache hit data', () => {
            const conn: ConnectionPerformance = {
                connection_id: 1,
                connection_name: 'test',
                xid_age: [],
                cache_hit_ratio: {
                    current: undefined as unknown as number,
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
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });

    describe('single server view without database data', () => {
        it('displays single connection cache hit ratio', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            expect(screen.getByText('97.5')).toBeInTheDocument();
            expect(screen.getByText('%')).toBeInTheDocument();
        });

        it('renders chart with single series', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
            expect(screen.getByTestId('series-Cache Hit %')).toBeInTheDocument();
        });

        it('does not show database name label with no database data', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            // No label should be shown
            expect(screen.queryByText('(Server 1)')).not.toBeInTheDocument();
            expect(screen.queryByText('(worst)')).not.toBeInTheDocument();
        });
    });

    describe('single server view with database data', () => {
        it('displays worst database cache hit ratio', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9),
                createDatabaseCacheHitData('ecommerce', 48.2),
                createDatabaseCacheHitData('analytics', 85.3),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            // Should show worst (lowest) database value
            expect(screen.getByText('48.2')).toBeInTheDocument();
        });

        it('displays database name for worst ratio', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9),
                createDatabaseCacheHitData('ecommerce', 48.2),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            expect(screen.getByText('(ecommerce)')).toBeInTheDocument();
        });

        it('does not show database label with only one database', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            // With only one database, no label is needed
            expect(screen.queryByText('(postgres)')).not.toBeInTheDocument();
        });

        it('renders chart with per-database series', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9),
                createDatabaseCacheHitData('ecommerce', 48.2),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
            expect(screen.getByTestId('series-postgres')).toBeInTheDocument();
            expect(screen.getByTestId('series-ecommerce')).toBeInTheDocument();
        });

        it('handles database with missing time series', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9, mockTimeSeries),
                {
                    database_name: 'empty_db',
                    cache_hit_ratio: {
                        current: 50.0,
                        time_series: [],
                    },
                },
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            // Should only show the database with time series
            expect(screen.getByTestId('series-postgres')).toBeInTheDocument();
            expect(screen.queryByTestId('series-empty_db')).not.toBeInTheDocument();
        });
    });

    describe('multi-server view', () => {
        it('displays worst server cache hit ratio with label', () => {
            const connections = [
                createConnectionPerformance(1, 'Primary', 99.5),
                createConnectionPerformance(2, 'Replica 1', 85.2),
                createConnectionPerformance(3, 'Replica 2', 92.1),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={connections}
                    loading={false}
                    isMultiServer={true}
                />
            );

            // Should show worst server value
            expect(screen.getByText('85.2')).toBeInTheDocument();
            expect(screen.getByText('(worst)')).toBeInTheDocument();
        });

        it('ignores databaseData in multi-server view', () => {
            const connections = [
                createConnectionPerformance(1, 'Primary', 99.5),
                createConnectionPerformance(2, 'Replica 1', 85.2),
            ];
            const databaseData: DatabaseCacheHitData[] = [
                createDatabaseCacheHitData('postgres', 99.9),
                createDatabaseCacheHitData('ecommerce', 48.2),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={connections}
                    loading={false}
                    isMultiServer={true}
                    databaseData={databaseData}
                />
            );

            // Should show server-level data, not database data
            expect(screen.getByText('85.2')).toBeInTheDocument();
            expect(screen.getByText('(worst)')).toBeInTheDocument();
            expect(screen.queryByText('48.2')).not.toBeInTheDocument();
        });

        it('renders chart with per-server series', () => {
            const connections = [
                createConnectionPerformance(1, 'Primary', 99.5),
                createConnectionPerformance(2, 'Replica 1', 85.2),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={connections}
                    loading={false}
                    isMultiServer={true}
                />
            );

            expect(screen.getByTestId('mock-chart')).toBeInTheDocument();
            expect(screen.getByTestId('series-Primary')).toBeInTheDocument();
            expect(screen.getByTestId('series-Replica 1')).toBeInTheDocument();
        });

        it('uses connection_id as fallback for server name', () => {
            const connections = [
                createConnectionPerformance(1, '', 99.5),
                createConnectionPerformance(2, '', 85.2),
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={connections}
                    loading={false}
                    isMultiServer={true}
                />
            );

            expect(screen.getByTestId('series-Server 1')).toBeInTheDocument();
            expect(screen.getByTestId('series-Server 2')).toBeInTheDocument();
        });
    });

    describe('color coding', () => {
        it('applies green color for high cache hit ratio (>= 95%)', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 98.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            const valueElement = screen.getByText('98.5');
            expect(valueElement).toHaveStyle({ color: '#4caf50' });
        });

        it('applies orange color for medium cache hit ratio (90-95%)', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 92.0);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            const valueElement = screen.getByText('92.0');
            expect(valueElement).toHaveStyle({ color: '#ff9800' });
        });

        it('applies red color for low cache hit ratio (< 90%)', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 75.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            const valueElement = screen.getByText('75.5');
            expect(valueElement).toHaveStyle({ color: '#f44336' });
        });
    });

    describe('title and structure', () => {
        it('displays Cache Hit Ratio title', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            expect(screen.getByText('Cache Hit Ratio')).toBeInTheDocument();
        });
    });

    describe('edge cases', () => {
        it('handles empty databaseData array', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={[]}
                />
            );

            // Should fallback to connection data
            expect(screen.getByText('97.5')).toBeInTheDocument();
        });

        it('handles databaseData with all empty time series', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                {
                    database_name: 'db1',
                    cache_hit_ratio: {
                        current: 80.0,
                        time_series: [],
                    },
                },
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            // Headline should show the database value even without chart
            expect(screen.getByText('80.0')).toBeInTheDocument();
        });

        it('handles connection with empty time series', () => {
            const conn: ConnectionPerformance = {
                ...createConnectionPerformance(1, 'Server 1', 97.5),
                cache_hit_ratio: {
                    current: 97.5,
                    time_series: [],
                },
            };

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                />
            );

            // Should still show headline value even without chart
            expect(screen.getByText('97.5')).toBeInTheDocument();
        });

        it('handles undefined cache_hit_ratio in databaseData', () => {
            const conn = createConnectionPerformance(1, 'Server 1', 97.5);
            const databaseData: DatabaseCacheHitData[] = [
                {
                    database_name: 'db1',
                    cache_hit_ratio: undefined as unknown as { current: number; time_series: Array<{ time: string; value: number }> },
                },
            ];

            renderWithTheme(
                <CacheHitTile
                    connections={[conn]}
                    loading={false}
                    isMultiServer={false}
                    databaseData={databaseData}
                />
            );

            // When database data has entries but none have valid data,
            // findWorstDatabase returns null, so headline is null and shows "No data"
            expect(screen.getByText('No data')).toBeInTheDocument();
        });
    });
});
