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
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import DatabaseDashboard from '../index';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../TimeRangeSelector', () => ({
    default: () => <div data-testid="time-range-selector">Time Range Selector</div>,
}));

vi.mock('../PerformanceSection', () => ({
    default: ({ connectionId, databaseName }: {
        connectionId: number;
        databaseName: string;
    }) => (
        <div
            data-testid="performance-section"
            data-connection-id={connectionId}
            data-database-name={databaseName}
        >
            Performance Section Content
        </div>
    ),
}));

vi.mock('../TableLeaderboardSection', () => ({
    default: ({ connectionId, databaseName }: {
        connectionId: number;
        databaseName: string;
    }) => (
        <div
            data-testid="table-leaderboard-section"
            data-connection-id={connectionId}
            data-database-name={databaseName}
        >
            Table Leaderboard Content
        </div>
    ),
}));

vi.mock('../IndexLeaderboardSection', () => ({
    default: ({ connectionId, databaseName }: {
        connectionId: number;
        databaseName: string;
    }) => (
        <div
            data-testid="index-leaderboard-section"
            data-connection-id={connectionId}
            data-database-name={databaseName}
        >
            Index Leaderboard Content
        </div>
    ),
}));

vi.mock('../VacuumStatusSection', () => ({
    default: ({ connectionId, databaseName }: {
        connectionId: number;
        databaseName: string;
    }) => (
        <div
            data-testid="vacuum-status-section"
            data-connection-id={connectionId}
            data-database-name={databaseName}
        >
            Vacuum Status Content
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

interface DatabaseDashboardProps {
    connectionId: number;
    connectionName?: string;
    databaseName: string;
}

const defaultProps: DatabaseDashboardProps = {
    connectionId: 1,
    connectionName: 'Test Server',
    databaseName: 'testdb',
};

const renderDatabaseDashboard = (props: Partial<DatabaseDashboardProps> = {}) => {
    return render(
        <ThemeProvider theme={theme}>
            <DatabaseDashboard {...defaultProps} {...props} />
        </ThemeProvider>,
    );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DatabaseDashboard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders database name in header', () => {
        renderDatabaseDashboard({ databaseName: 'production_db' });

        expect(screen.getByText('production_db')).toBeInTheDocument();
    });

    it('renders connection name in header', () => {
        renderDatabaseDashboard({ connectionName: 'Production Server' });

        expect(screen.getByText('Production Server')).toBeInTheDocument();
    });

    it('renders fallback connection label when connectionName is missing', () => {
        renderDatabaseDashboard({ connectionId: 42, connectionName: undefined });

        expect(screen.getByText('Connection 42')).toBeInTheDocument();
    });

    it('renders TimeRangeSelector', () => {
        renderDatabaseDashboard();

        expect(screen.getByTestId('time-range-selector')).toBeInTheDocument();
    });

    it('renders all sections', () => {
        renderDatabaseDashboard();

        expect(screen.getByTestId('performance-section')).toBeInTheDocument();
        expect(screen.getByTestId('table-leaderboard-section')).toBeInTheDocument();
        expect(screen.getByTestId('index-leaderboard-section')).toBeInTheDocument();
        expect(screen.getByTestId('vacuum-status-section')).toBeInTheDocument();
    });

    it('passes connectionId and databaseName to all sections', () => {
        renderDatabaseDashboard({
            connectionId: 5,
            databaseName: 'analytics',
        });

        expect(screen.getByTestId('performance-section')).toHaveAttribute(
            'data-connection-id',
            '5',
        );
        expect(screen.getByTestId('performance-section')).toHaveAttribute(
            'data-database-name',
            'analytics',
        );

        expect(screen.getByTestId('table-leaderboard-section')).toHaveAttribute(
            'data-connection-id',
            '5',
        );
        expect(screen.getByTestId('table-leaderboard-section')).toHaveAttribute(
            'data-database-name',
            'analytics',
        );

        expect(screen.getByTestId('index-leaderboard-section')).toHaveAttribute(
            'data-connection-id',
            '5',
        );
        expect(screen.getByTestId('index-leaderboard-section')).toHaveAttribute(
            'data-database-name',
            'analytics',
        );

        expect(screen.getByTestId('vacuum-status-section')).toHaveAttribute(
            'data-connection-id',
            '5',
        );
        expect(screen.getByTestId('vacuum-status-section')).toHaveAttribute(
            'data-database-name',
            'analytics',
        );
    });

    it('shows "No database selected" when databaseName is empty', () => {
        renderDatabaseDashboard({ databaseName: '' });

        expect(screen.getByText('No database selected')).toBeInTheDocument();
        expect(screen.queryByTestId('performance-section')).not.toBeInTheDocument();
    });

    it('does not render TimeRangeSelector when no database selected', () => {
        renderDatabaseDashboard({ databaseName: '' });

        expect(screen.queryByTestId('time-range-selector')).not.toBeInTheDocument();
    });
});
