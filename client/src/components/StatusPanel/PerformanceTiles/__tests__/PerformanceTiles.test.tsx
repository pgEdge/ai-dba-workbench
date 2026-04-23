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
import { ThemeProvider, createTheme } from '@mui/material/styles';
import PerformanceTiles from '../index';
import type { Selection, ServerSelection, ClusterSelection, EstateSelection } from '../../../../types/selection';

// Mock usePerformanceSummary hook
vi.mock('../usePerformanceSummary', () => ({
    usePerformanceSummary: vi.fn(),
    default: vi.fn(),
}));

// Mock useDatabaseCacheHit hook
vi.mock('../useDatabaseCacheHit', () => ({
    useDatabaseCacheHit: vi.fn(),
    default: vi.fn(),
}));

// Mock the tile components to simplify testing
vi.mock('../DatabaseAgeTile', () => ({
    default: () => <div data-testid="database-age-tile">DatabaseAgeTile</div>,
}));

vi.mock('../CacheHitTile', () => ({
    default: ({ databaseData }: { databaseData?: unknown[] }) => (
        <div data-testid="cache-hit-tile">
            CacheHitTile
            {databaseData && (
                <span data-testid="has-database-data">
                    databases: {databaseData.length}
                </span>
            )}
        </div>
    ),
}));

vi.mock('../TransactionTile', () => ({
    default: () => <div data-testid="transaction-tile">TransactionTile</div>,
}));

vi.mock('../CheckpointTile', () => ({
    default: () => <div data-testid="checkpoint-tile">CheckpointTile</div>,
}));

import { usePerformanceSummary } from '../usePerformanceSummary';
import { useDatabaseCacheHit } from '../useDatabaseCacheHit';

const mockUsePerformanceSummary = vi.mocked(usePerformanceSummary);
const mockUseDatabaseCacheHit = vi.mocked(useDatabaseCacheHit);

const theme = createTheme();

const renderWithTheme = (ui: React.ReactElement) => {
    return render(
        <ThemeProvider theme={theme}>
            {ui}
        </ThemeProvider>
    );
};

const makeServerSelection = (id: number = 1): ServerSelection => ({
    type: 'server',
    id,
    name: `Server ${id}`,
    status: 'online',
    description: '',
    host: 'localhost',
    port: 5432,
    role: 'primary',
    version: '16',
    database: 'postgres',
    username: 'postgres',
    os: 'linux',
    platform: 'x86_64',
});

const makeClusterSelection = (serverIds: number[] = [1, 2, 3]): ClusterSelection => ({
    type: 'cluster',
    id: 'cluster-1',
    name: 'Test Cluster',
    status: 'online',
    description: '',
    servers: serverIds.map(id => ({ id, name: `Server ${id}` })),
    serverIds,
});

const makeEstateSelection = (): EstateSelection => ({
    type: 'estate',
    name: 'Estate',
    status: 'online',
    groups: [],
});

describe('PerformanceTiles', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUsePerformanceSummary.mockReturnValue({
            data: {
                time_range: '24h',
                connections: [],
            },
            loading: false,
            error: null,
        });
        mockUseDatabaseCacheHit.mockReturnValue({
            databases: [],
            loading: false,
            error: null,
        });
    });

    it('renders all four tiles', () => {
        renderWithTheme(
            <PerformanceTiles selection={makeServerSelection(1)} />
        );

        expect(screen.getByTestId('database-age-tile')).toBeInTheDocument();
        expect(screen.getByTestId('cache-hit-tile')).toBeInTheDocument();
        expect(screen.getByTestId('transaction-tile')).toBeInTheDocument();
        expect(screen.getByTestId('checkpoint-tile')).toBeInTheDocument();
    });

    it('calls useDatabaseCacheHit with connectionId for single-server view', () => {
        renderWithTheme(
            <PerformanceTiles selection={makeServerSelection(123)} />
        );

        expect(mockUseDatabaseCacheHit).toHaveBeenCalledWith(123);
    });

    it('calls useDatabaseCacheHit with null for cluster view', () => {
        renderWithTheme(
            <PerformanceTiles
                selection={makeClusterSelection([1, 2, 3])}
            />
        );

        expect(mockUseDatabaseCacheHit).toHaveBeenCalledWith(null);
    });

    it('calls useDatabaseCacheHit with null for estate view', () => {
        renderWithTheme(
            <PerformanceTiles
                selection={makeEstateSelection()}
            />
        );

        expect(mockUseDatabaseCacheHit).toHaveBeenCalledWith(null);
    });

    it('passes database data to CacheHitTile in single-server view', () => {
        mockUseDatabaseCacheHit.mockReturnValue({
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [],
                    },
                },
                {
                    database_name: 'ecommerce',
                    cache_hit_ratio: {
                        current: 85.0,
                        time_series: [],
                    },
                },
            ],
            loading: false,
            error: null,
        });

        renderWithTheme(
            <PerformanceTiles selection={makeServerSelection(1)} />
        );

        expect(screen.getByTestId('has-database-data')).toBeInTheDocument();
        expect(screen.getByText('databases: 2')).toBeInTheDocument();
    });

    it('does not pass database data to CacheHitTile in multi-server view', () => {
        mockUseDatabaseCacheHit.mockReturnValue({
            databases: [
                {
                    database_name: 'postgres',
                    cache_hit_ratio: {
                        current: 99.5,
                        time_series: [],
                    },
                },
            ],
            loading: false,
            error: null,
        });

        renderWithTheme(
            <PerformanceTiles
                selection={makeClusterSelection([1, 2])}
            />
        );

        expect(screen.queryByTestId('has-database-data')).not.toBeInTheDocument();
    });

    it('handles selection with non-numeric id', () => {
        renderWithTheme(
            <PerformanceTiles
                selection={{ type: 'server', id: 'invalid' } as unknown as Selection}
            />
        );

        // Should call useDatabaseCacheHit with the id (type narrowing
        // makes it always a number for 'server', but runtime data may differ)
        expect(mockUseDatabaseCacheHit).toHaveBeenCalled();
    });

    it('handles selection with undefined id', () => {
        renderWithTheme(
            <PerformanceTiles
                selection={{ type: 'server' } as unknown as Selection}
            />
        );

        // Should call useDatabaseCacheHit with undefined since id is missing
        expect(mockUseDatabaseCacheHit).toHaveBeenCalled();
    });
});
