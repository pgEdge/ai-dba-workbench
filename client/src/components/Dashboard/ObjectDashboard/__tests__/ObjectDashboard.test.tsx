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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ObjectDashboard from '../index';
import { ObjectType, OverlayEntry } from '../../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

let mockCurrentOverlay: OverlayEntry | null = null;

vi.mock('../../../../contexts/DashboardContext', () => ({
    useDashboard: () => ({
        currentOverlay: mockCurrentOverlay,
    }),
}));

vi.mock('../TableDetail', () => ({
    default: ({ connectionId, databaseName, schemaName, objectName }: {
        connectionId: number;
        databaseName: string;
        schemaName?: string;
        objectName: string;
    }) => (
        <div
            data-testid="table-detail"
            data-connection-id={connectionId}
            data-database-name={databaseName}
            data-schema-name={schemaName}
            data-object-name={objectName}
        >
            Table Detail Content
        </div>
    ),
}));

vi.mock('../IndexDetail', () => ({
    default: ({ connectionId, databaseName, schemaName, objectName }: {
        connectionId: number;
        databaseName: string;
        schemaName?: string;
        objectName: string;
    }) => (
        <div
            data-testid="index-detail"
            data-connection-id={connectionId}
            data-database-name={databaseName}
            data-schema-name={schemaName}
            data-object-name={objectName}
        >
            Index Detail Content
        </div>
    ),
}));

vi.mock('../QueryDetail', () => ({
    default: ({ connectionId, databaseName, objectName }: {
        connectionId: number;
        databaseName: string;
        objectName: string;
    }) => (
        <div
            data-testid="query-detail"
            data-connection-id={connectionId}
            data-database-name={databaseName}
            data-object-name={objectName}
        >
            Query Detail Content
        </div>
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

interface ObjectDashboardProps {
    connectionId: number;
    databaseName: string;
    objectType: ObjectType;
    schemaName?: string;
    objectName: string;
}

const defaultProps: ObjectDashboardProps = {
    connectionId: 1,
    databaseName: 'testdb',
    objectType: 'table',
    schemaName: 'public',
    objectName: 'users',
};

const renderObjectDashboard = (props: Partial<ObjectDashboardProps> = {}) => {
    return render(
        <ThemeProvider theme={theme}>
            <ObjectDashboard {...defaultProps} {...props} />
        </ThemeProvider>,
    );
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ObjectDashboard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockCurrentOverlay = null;
    });

    describe('header rendering', () => {
        it('renders type badge for table', () => {
            renderObjectDashboard({ objectType: 'table' });

            expect(screen.getByText('Table')).toBeInTheDocument();
        });

        it('renders type badge for index', () => {
            renderObjectDashboard({ objectType: 'index' });

            expect(screen.getByText('Index')).toBeInTheDocument();
        });

        it('renders type badge for query', () => {
            renderObjectDashboard({ objectType: 'query' });

            expect(screen.getByText('Query')).toBeInTheDocument();
        });

        it('renders database name in context label', () => {
            renderObjectDashboard({ databaseName: 'production' });

            expect(screen.getByText(/production/)).toBeInTheDocument();
        });

        it('renders connection name from overlay when available', () => {
            mockCurrentOverlay = {
                level: 'object',
                title: 'Table: users',
                entityId: 'users',
                entityName: 'users',
                connectionName: 'Production Server',
            };
            renderObjectDashboard();

            expect(screen.getByText(/Production Server/)).toBeInTheDocument();
        });

        it('renders fallback connection label when no connectionName', () => {
            mockCurrentOverlay = null;
            renderObjectDashboard({ connectionId: 42 });

            expect(screen.getByText(/Connection 42/)).toBeInTheDocument();
        });
    });

    describe('table object type', () => {
        it('renders TableDetail component', () => {
            renderObjectDashboard({ objectType: 'table' });

            expect(screen.getByTestId('table-detail')).toBeInTheDocument();
            expect(screen.queryByTestId('index-detail')).not.toBeInTheDocument();
            expect(screen.queryByTestId('query-detail')).not.toBeInTheDocument();
        });

        it('passes correct props to TableDetail', () => {
            renderObjectDashboard({
                objectType: 'table',
                connectionId: 5,
                databaseName: 'mydb',
                schemaName: 'custom',
                objectName: 'orders',
            });

            const tableDetail = screen.getByTestId('table-detail');
            expect(tableDetail).toHaveAttribute('data-connection-id', '5');
            expect(tableDetail).toHaveAttribute('data-database-name', 'mydb');
            expect(tableDetail).toHaveAttribute('data-schema-name', 'custom');
            expect(tableDetail).toHaveAttribute('data-object-name', 'orders');
        });
    });

    describe('index object type', () => {
        it('renders IndexDetail component', () => {
            renderObjectDashboard({ objectType: 'index' });

            expect(screen.getByTestId('index-detail')).toBeInTheDocument();
            expect(screen.queryByTestId('table-detail')).not.toBeInTheDocument();
            expect(screen.queryByTestId('query-detail')).not.toBeInTheDocument();
        });

        it('passes correct props to IndexDetail', () => {
            renderObjectDashboard({
                objectType: 'index',
                connectionId: 3,
                databaseName: 'analytics',
                schemaName: 'reports',
                objectName: 'idx_user_email',
            });

            const indexDetail = screen.getByTestId('index-detail');
            expect(indexDetail).toHaveAttribute('data-connection-id', '3');
            expect(indexDetail).toHaveAttribute('data-database-name', 'analytics');
            expect(indexDetail).toHaveAttribute('data-schema-name', 'reports');
            expect(indexDetail).toHaveAttribute('data-object-name', 'idx_user_email');
        });
    });

    describe('query object type', () => {
        it('renders QueryDetail component', () => {
            renderObjectDashboard({ objectType: 'query' });

            expect(screen.getByTestId('query-detail')).toBeInTheDocument();
            expect(screen.queryByTestId('table-detail')).not.toBeInTheDocument();
            expect(screen.queryByTestId('index-detail')).not.toBeInTheDocument();
        });

        it('passes correct props to QueryDetail', () => {
            renderObjectDashboard({
                objectType: 'query',
                connectionId: 7,
                databaseName: 'logs',
                objectName: 'abc123queryid',
            });

            const queryDetail = screen.getByTestId('query-detail');
            expect(queryDetail).toHaveAttribute('data-connection-id', '7');
            expect(queryDetail).toHaveAttribute('data-database-name', 'logs');
            expect(queryDetail).toHaveAttribute('data-object-name', 'abc123queryid');
        });
    });

    describe('empty state', () => {
        it('shows "No object selected" when objectName is empty', () => {
            renderObjectDashboard({ objectName: '' });

            expect(screen.getByText('No object selected')).toBeInTheDocument();
            expect(screen.queryByTestId('table-detail')).not.toBeInTheDocument();
        });

        it('does not show type badge when no object selected', () => {
            renderObjectDashboard({ objectName: '' });

            expect(screen.queryByText('Table')).not.toBeInTheDocument();
        });
    });
});
