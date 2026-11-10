/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ClusterCardsSection from '../ClusterCardsSection';
import type { ClusterEntry } from '../../../../contexts/ClusterDataContext';
import type { EstateSelection } from '../../../../types/selection';

const mockSelectCluster = vi.fn();
vi.mock('../../../../contexts/useClusterSelection', () => ({
    useClusterSelection: () => ({ selectCluster: mockSelectCluster }),
}));

const theme = createTheme();

const autoCluster: ClusterEntry = {
    id: 'server-1',
    name: 'Auto Cluster',
    description: 'Detected streaming cluster',
    db_cluster_id: 21,
    servers: [
        {
            id: 1,
            name: 'primary',
            status: 'online',
            role: 'binary_primary',
            children: [
                {
                    id: 2,
                    name: 'standby',
                    status: 'online',
                    role: 'binary_standby',
                    active_alert_count: 3,
                },
                {
                    id: 3,
                    name: 'standby-2',
                    status: 'offline',
                    primary_role: 'binary_standby',
                },
            ],
        },
    ],
};

const singleCluster: ClusterEntry = {
    id: 'cluster-9',
    name: 'Single',
    servers: [{ id: 9, name: 'lonely' }],
};

const makeSelection = (clusters: ClusterEntry[] | null): EstateSelection => ({
    type: 'estate',
    name: 'All Servers',
    status: 'online',
    groups: [
        { id: 'group-1', name: 'Default', clusters },
        { id: 'group-2', name: 'Empty', clusters: null },
    ],
});

const renderSection = (selection: EstateSelection) => render(
    <ThemeProvider theme={theme}>
        <ClusterCardsSection selection={selection} />
    </ThemeProvider>,
);

describe('ClusterCardsSection', () => {
    beforeEach(() => {
        mockSelectCluster.mockReset();
    });

    it('shows an empty message when the estate has no clusters', () => {
        renderSection(makeSelection(null));
        expect(screen.getByText('No clusters found in the estate.')).toBeInTheDocument();
    });

    it('summarises each cluster on its card', () => {
        renderSection(makeSelection([autoCluster, singleCluster]));

        expect(screen.getByText('Auto Cluster')).toBeInTheDocument();
        expect(screen.getByText('3 servers')).toBeInTheDocument();
        expect(screen.getByText('1 server')).toBeInTheDocument();
        expect(screen.getByText('1 warning')).toBeInTheDocument();
        expect(screen.getByText('1 offline')).toBeInTheDocument();
        expect(screen.getByText('3')).toBeInTheDocument();
        expect(screen.getByText('binary_standby: 2')).toBeInTheDocument();
        expect(screen.getByText('unknown: 1')).toBeInTheDocument();
    });

    it('selects the topology entry itself on click, keeping db_cluster_id', () => {
        renderSection(makeSelection([autoCluster]));

        fireEvent.click(screen.getByRole('button', { name: 'Select cluster Auto Cluster' }));

        // The selection must be the topology's own entry, so the blackout
        // dialogs and banner see its database row (issue #809).
        expect(mockSelectCluster).toHaveBeenCalledWith(autoCluster);
        expect(mockSelectCluster.mock.calls[0][0]).toBe(autoCluster);
        expect(mockSelectCluster.mock.calls[0][0].db_cluster_id).toBe(21);
    });

    it('selects a cluster from the keyboard with Enter or Space only', () => {
        renderSection(makeSelection([autoCluster]));
        const card = screen.getByRole('button', { name: 'Select cluster Auto Cluster' });

        fireEvent.keyDown(card, { key: 'Tab' });
        expect(mockSelectCluster).not.toHaveBeenCalled();

        fireEvent.keyDown(card, { key: 'Enter' });
        fireEvent.keyDown(card, { key: ' ' });
        expect(mockSelectCluster).toHaveBeenCalledTimes(2);
        expect(mockSelectCluster.mock.calls[1][0]).toBe(autoCluster);
    });
});
