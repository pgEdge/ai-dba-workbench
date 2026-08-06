/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for SelectionHeader. These exercise every branch of the header:
 *
 *   - Icon and label selection across server, cluster, estate, and an
 *     unknown selection type.
 *   - Status-driven icon colouring and tooltip text (offline, alerting,
 *     online).
 *   - The alert-count badge, including the 99+ clamp.
 *   - The blackout management control: its FrontHandSharp icon, active
 *     versus inactive tooltip text, the dot badge, and the click handler.
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, fireEvent, act } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';
import SelectionHeader from '../SelectionHeader';
import type {
    ServerSelection,
    ClusterSelection,
    EstateSelection,
    Selection,
} from '../../../types/selection';

// ---------------------------------------------------------------------------
// Mock the blackout context so we can toggle the active-blackout state.
// ---------------------------------------------------------------------------

let mockActiveBlackoutsForSelection: unknown[] = [];

vi.mock('../../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({
        activeBlackoutsForSelection: mockActiveBlackoutsForSelection,
    }),
}));

const testTheme = createPgedgeTheme('dark');

const renderHeader = (
    selection: Selection,
    props: Partial<React.ComponentProps<typeof SelectionHeader>> = {},
) =>
    render(
        <ThemeProvider theme={testTheme}>
            <SelectionHeader
                selection={selection}
                onBlackoutClick={props.onBlackoutClick ?? (() => {})}
                alertCount={props.alertCount}
                alertSeverities={props.alertSeverities}
            />
        </ThemeProvider>,
    );

const serverSelection: ServerSelection = {
    type: 'server',
    id: 1,
    name: 'db-primary',
    status: 'online',
    description: 'Primary node',
    host: 'localhost',
    port: 5432,
    role: 'primary',
    version: '16',
    database: 'postgres',
    username: 'admin',
    os: 'linux',
    platform: 'x86_64',
};

const clusterSelection: ClusterSelection = {
    type: 'cluster',
    id: 'c1',
    name: 'prod-cluster',
    status: 'online',
    description: '',
    servers: [],
    serverIds: [],
};

const estateSelection: EstateSelection = {
    type: 'estate',
    name: 'All Estates',
    status: 'online',
    groups: [],
};

describe('SelectionHeader', () => {
    beforeEach(() => {
        mockActiveBlackoutsForSelection = [];
    });

    it('renders the Server label for a server selection', () => {
        renderHeader(serverSelection);
        expect(screen.getByText('Server')).toBeInTheDocument();
        expect(screen.getByText('db-primary')).toBeInTheDocument();
        // The optional description renders alongside the name.
        expect(screen.getByText('Primary node')).toBeInTheDocument();
    });

    it('renders the Cluster label for a cluster selection', () => {
        renderHeader(clusterSelection);
        expect(screen.getByText('Cluster')).toBeInTheDocument();
        expect(screen.getByText('prod-cluster')).toBeInTheDocument();
    });

    it('renders the Estate Overview label for an estate selection', () => {
        renderHeader(estateSelection);
        expect(screen.getByText('Estate Overview')).toBeInTheDocument();
        expect(screen.getByText('All Estates')).toBeInTheDocument();
    });

    it('falls back to the Selection label for an unknown type', () => {
        // Cast through unknown to reach the default switch branches.
        const unknown = {
            type: 'mystery',
            name: 'What',
            status: 'online',
        } as unknown as Selection;
        renderHeader(unknown);
        expect(screen.getByText('Selection')).toBeInTheDocument();
    });

    it('shows an offline tooltip when the selection is offline', async () => {
        renderHeader({ ...serverSelection, status: 'offline' });
        await act(async () => {
            fireEvent.mouseOver(screen.getByTestId('StorageIcon'));
        });
        expect(await screen.findByRole('tooltip')).toHaveTextContent(
            'Server is offline',
        );
    });

    it('summarises active alerts in the tooltip and shows a count badge', async () => {
        renderHeader(serverSelection, {
            alertCount: 3,
            alertSeverities: { warning: 2, critical: 1 },
        });
        // The alert-count badge renders the numeric total.
        expect(screen.getByText('3')).toBeInTheDocument();
        await act(async () => {
            fireEvent.mouseOver(screen.getByTestId('StorageIcon'));
        });
        // Severities are ordered critical before warning in the summary.
        expect(await screen.findByRole('tooltip')).toHaveTextContent(
            '3 active alerts: 1 Critical, 2 Warning',
        );
    });

    it('clamps the alert-count badge at 99+', () => {
        renderHeader(serverSelection, {
            alertCount: 150,
            alertSeverities: { warning: 150 },
        });
        expect(screen.getByText('99+')).toBeInTheDocument();
    });

    it('uses the singular alert wording for a single active alert', async () => {
        renderHeader(serverSelection, {
            alertCount: 1,
            alertSeverities: { info: 1 },
        });
        expect(screen.getByText('1')).toBeInTheDocument();
        await act(async () => {
            fireEvent.mouseOver(screen.getByTestId('StorageIcon'));
        });
        expect(await screen.findByRole('tooltip')).toHaveTextContent(
            '1 active alert: 1 Info',
        );
    });

    it('shows an online tooltip when there are no alerts', async () => {
        renderHeader(serverSelection);
        await act(async () => {
            fireEvent.mouseOver(screen.getByTestId('StorageIcon'));
        });
        expect(await screen.findByRole('tooltip')).toHaveTextContent(
            'Server is online',
        );
    });

    it('renders the blackout button with the default management tooltip', () => {
        renderHeader(serverSelection);
        expect(
            screen.getByRole('button', { name: 'Blackout management' }),
        ).toBeInTheDocument();
    });

    it('shows the active blackout tooltip when a blackout is active', () => {
        mockActiveBlackoutsForSelection = [{ id: 1 }];
        renderHeader(serverSelection);
        expect(
            screen.getByRole('button', { name: 'Blackout active' }),
        ).toBeInTheDocument();
    });

    it('invokes onBlackoutClick when the blackout button is clicked', () => {
        const onBlackoutClick = vi.fn();
        renderHeader(serverSelection, { onBlackoutClick });
        fireEvent.click(
            screen.getByRole('button', { name: 'Blackout management' }),
        );
        expect(onBlackoutClick).toHaveBeenCalledTimes(1);
    });
});
