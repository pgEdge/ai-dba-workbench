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
import ServerDashboard from '../index';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../SystemResourcesSection', () => ({
    default: ({ connectionId, connectionName }: {
        connectionId: number;
        connectionName?: string;
    }) => (
        <div
            data-testid="system-resources-section"
            data-connection-id={connectionId}
            data-connection-name={connectionName}
        >
            System Resources Content
        </div>
    ),
}));

vi.mock('../PostgresOverviewSection', () => ({
    default: ({ connectionId }: { connectionId: number }) => (
        <div data-testid="postgres-overview-section" data-connection-id={connectionId}>
            PostgreSQL Overview Content
        </div>
    ),
}));

vi.mock('../WalReplicationSection', () => ({
    default: ({ connectionId }: { connectionId: number }) => (
        <div data-testid="wal-replication-section" data-connection-id={connectionId}>
            WAL Replication Content
        </div>
    ),
}));

vi.mock('../DatabaseSummariesSection', () => ({
    default: ({ connectionId }: { connectionId: number }) => (
        <div data-testid="database-summaries-section" data-connection-id={connectionId}>
            Database Summaries Content
        </div>
    ),
}));

vi.mock('../TopQueriesSection', () => ({
    default: ({ connectionId }: { connectionId: number }) => (
        <div data-testid="top-queries-section" data-connection-id={connectionId}>
            Top Queries Content
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const createSelection = (id: number = 1, name?: string) => ({
    id,
    name,
});

const renderServerDashboard = (selection: Record<string, unknown> = createSelection()) => {
    return render(
        <ThemeProvider theme={theme}>
            <ServerDashboard selection={selection} />
        </ThemeProvider>,
    );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ServerDashboard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders all sections when valid selection is provided', () => {
        renderServerDashboard(createSelection(1, 'Test Server'));

        expect(screen.getByTestId('system-resources-section')).toBeInTheDocument();
        expect(screen.getByTestId('postgres-overview-section')).toBeInTheDocument();
        expect(screen.getByTestId('wal-replication-section')).toBeInTheDocument();
        expect(screen.getByTestId('database-summaries-section')).toBeInTheDocument();
        expect(screen.getByTestId('top-queries-section')).toBeInTheDocument();
    });

    it('passes connectionId to all sections', () => {
        renderServerDashboard(createSelection(42, 'Test Server'));

        expect(screen.getByTestId('system-resources-section')).toHaveAttribute(
            'data-connection-id',
            '42',
        );
        expect(screen.getByTestId('postgres-overview-section')).toHaveAttribute(
            'data-connection-id',
            '42',
        );
        expect(screen.getByTestId('wal-replication-section')).toHaveAttribute(
            'data-connection-id',
            '42',
        );
        expect(screen.getByTestId('database-summaries-section')).toHaveAttribute(
            'data-connection-id',
            '42',
        );
        expect(screen.getByTestId('top-queries-section')).toHaveAttribute(
            'data-connection-id',
            '42',
        );
    });

    it('passes connectionName to SystemResourcesSection', () => {
        renderServerDashboard(createSelection(1, 'Production Server'));

        expect(screen.getByTestId('system-resources-section')).toHaveAttribute(
            'data-connection-name',
            'Production Server',
        );
    });

    it('shows "No server selected" when id is missing', () => {
        const selection = { name: 'Server without ID' };
        renderServerDashboard(selection);

        expect(screen.getByText('No server selected')).toBeInTheDocument();
        expect(screen.queryByTestId('system-resources-section')).not.toBeInTheDocument();
    });

    it('shows "No server selected" when id is undefined', () => {
        const selection = { id: undefined, name: 'Test' };
        renderServerDashboard(selection);

        expect(screen.getByText('No server selected')).toBeInTheDocument();
    });

    it('renders with connectionId of 0 (falsy but valid)', () => {
        const selection = { id: 0, name: 'Server Zero' };
        renderServerDashboard(selection);

        expect(screen.getByTestId('system-resources-section')).toBeInTheDocument();
        expect(screen.getByTestId('system-resources-section')).toHaveAttribute(
            'data-connection-id',
            '0',
        );
    });

    it('handles missing connectionName gracefully', () => {
        renderServerDashboard(createSelection(1));

        // SystemResourcesSection should still render
        expect(screen.getByTestId('system-resources-section')).toBeInTheDocument();
    });
});
