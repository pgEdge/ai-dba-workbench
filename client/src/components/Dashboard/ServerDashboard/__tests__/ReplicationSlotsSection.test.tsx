/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ReplicationSlotsSection from '../ReplicationSlotsSection';
import type { ClusterGroup } from '../../../../contexts/ClusterDataContext';
import type { UseReplicationSlotsReturn } from '../../../../hooks/useReplicationSlots';
import type { ReplicationSlotRow } from '../replicationSlots';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

let mockSlotsState: UseReplicationSlotsReturn = {
    slots: [],
    totalCount: 0,
    loading: false,
    error: null,
};
const mockUseReplicationSlots = vi.fn(
    (_connectionId: number) => mockSlotsState,
);
vi.mock('../../../../hooks/useReplicationSlots', () => ({
    useReplicationSlots: (id: number) => mockUseReplicationSlots(id),
}));

let mockClusterData: ClusterGroup[] = [];
vi.mock('../../../../contexts/useClusterData', () => ({
    useClusterData: () => ({ clusterData: mockClusterData }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const makeSlot = (
    overrides: Partial<ReplicationSlotRow> = {},
): ReplicationSlotRow => ({
    slot_name: 'standby_1',
    slot_type: 'physical',
    active: true,
    wal_status: 'reserved',
    safe_wal_size: 1024,
    retained_bytes: 2048,
    ...overrides,
});

const setSlots = (
    slots: ReplicationSlotRow[],
    totalCount = slots.length,
): void => {
    mockSlotsState = { slots, totalCount, loading: false, error: null };
};

const renderSection = (connectionId = 1) => render(
    <ReplicationSlotsSection
        connectionId={connectionId}
        connectionName="Test Server"
    />,
);

/** Cells of the data row for the named slot. */
const cellsOf = (slotName: string): HTMLElement[] => {
    const row = screen.getByTitle(slotName).closest('[role="row"]');
    if (!row) { throw new Error(`no row for ${slotName}`); }
    return within(row as HTMLElement).getAllByRole('cell');
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ReplicationSlotsSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockClusterData = [];
        setSlots([]);
        localStorage.clear();
    });

    it('renders the section title and queries its connection', () => {
        renderSection(9);

        expect(screen.getByText('Replication Slots')).toBeInTheDocument();
        expect(mockUseReplicationSlots).toHaveBeenCalledWith(9);
    });

    it('shows a spinner whilst loading', () => {
        mockSlotsState = { ...mockSlotsState, loading: true };

        renderSection();

        expect(screen.getByLabelText('Loading replication slots'))
            .toBeInTheDocument();
        expect(screen.queryByRole('table')).not.toBeInTheDocument();
        expect(screen.queryByText(/No replication slots/))
            .not.toBeInTheDocument();
    });

    it('shows an error', () => {
        mockSlotsState = { ...mockSlotsState, error: 'Request failed' };

        renderSection();

        expect(screen.getByText('Request failed')).toBeInTheDocument();
        expect(screen.queryByRole('table')).not.toBeInTheDocument();
    });

    it('shows an empty state when no slots were collected', () => {
        renderSection();

        expect(screen.getByText(/No replication slots were collected/))
            .toBeInTheDocument();
    });

    it('lists each slot with its type, status and WAL figures', () => {
        setSlots([
            makeSlot(),
            makeSlot({
                slot_name: 'sub_orders',
                slot_type: 'logical',
                active: false,
                wal_status: 'extended',
                safe_wal_size: null,
                retained_bytes: 5 * 1024 * 1024,
            }),
        ]);

        renderSection();

        expect(screen.getByRole('table', { name: 'Replication slots' }))
            .toBeInTheDocument();
        expect(screen.getAllByRole('columnheader').map((h) => h.textContent))
            .toEqual([
                'Slot', 'Type', 'Status', 'WAL status',
                'Retained WAL', 'Safe WAL size',
            ]);

        const physical = cellsOf('standby_1');
        expect(physical[1]).toHaveTextContent('Physical');
        expect(physical[2]).toHaveTextContent('Active');
        expect(physical[3]).toHaveTextContent('Reserved');
        expect(physical[4]).toHaveTextContent('2.0 KB');
        expect(physical[5]).toHaveTextContent('1.0 KB');

        const logical = cellsOf('sub_orders');
        expect(logical[1]).toHaveTextContent('Logical');
        expect(logical[2]).toHaveTextContent('Inactive');
        expect(logical[3]).toHaveTextContent('Extended');
        expect(logical[4]).toHaveTextContent('5.0 MB');
        expect(logical[5]).toHaveTextContent('Unlimited');
    });

    it('colours the chips by health', () => {
        setSlots([
            makeSlot({ slot_name: 'ok' }),
            makeSlot({
                slot_name: 'bad', active: false, wal_status: 'lost',
            }),
            makeSlot({
                slot_name: 'old', active: null, wal_status: null,
            }),
        ]);

        renderSection();

        const chipOf = (cell: HTMLElement): HTMLElement => {
            const chip = cell.querySelector('.MuiChip-root');
            if (!chip) { throw new Error('no chip'); }
            return chip as HTMLElement;
        };
        const ok = cellsOf('ok');
        expect(chipOf(ok[2]).className).toContain('MuiChip-colorSuccess');
        expect(chipOf(ok[3]).className).toContain('MuiChip-colorSuccess');

        const bad = cellsOf('bad');
        expect(chipOf(bad[2]).className).toContain('MuiChip-colorWarning');
        expect(chipOf(bad[3]).className).toContain('MuiChip-colorError');

        const old = cellsOf('old');
        expect(old[2]).toHaveTextContent('Unknown');
        expect(chipOf(old[2]).className).toContain('MuiChip-outlined');
        expect(old[3]).toHaveTextContent('Not reported');
        expect(chipOf(old[3]).className).toContain('MuiChip-outlined');
    });

    it('shows -- for a lost slot and Exceeded for an overdrawn one', () => {
        setSlots([
            makeSlot({
                slot_name: 'gone',
                active: false,
                wal_status: 'lost',
                safe_wal_size: null,
            }),
            makeSlot({
                slot_name: 'over',
                active: false,
                wal_status: 'unreserved',
                safe_wal_size: -38_482_739,
            }),
        ]);

        renderSection();

        const gone = cellsOf('gone');
        expect(gone[3]).toHaveTextContent('Lost');
        expect(gone[5]).toHaveTextContent('--');
        expect(gone[5]).not.toHaveTextContent('Unlimited');

        const over = cellsOf('over');
        expect(over[3]).toHaveTextContent('Unreserved');
        expect(over[5]).toHaveTextContent('Exceeded');
        expect(over[5]).not.toHaveTextContent('-');
    });

    it('describes each status in a tooltip', () => {
        setSlots([makeSlot({ wal_status: 'unreserved' })]);

        renderSection();

        expect(screen.getByLabelText(
            'Required WAL will be removed at the next checkpoint',
        )).toBeInTheDocument();
        expect(screen.getByLabelText('A consumer is connected to this slot'))
            .toBeInTheDocument();
    });

    it('shows placeholders for missing type and sizes', () => {
        setSlots([
            makeSlot({
                slot_name: 'bare',
                slot_type: null,
                wal_status: null,
                safe_wal_size: null,
                retained_bytes: null,
            }),
            makeSlot({ slot_name: 'odd', slot_type: 'custom' }),
        ]);

        renderSection();

        const bare = cellsOf('bare');
        expect(bare[1]).toHaveTextContent('--');
        expect(bare[4]).toHaveTextContent('--');
        expect(bare[5]).toHaveTextContent('--');
        expect(cellsOf('odd')[1]).toHaveTextContent('custom');
    });

    it('summarises the slots', () => {
        setSlots([
            makeSlot(),
            makeSlot({ slot_name: 'b', active: false, wal_status: 'lost' }),
        ]);

        renderSection();

        expect(screen.getByText(
            '2 slots, 1 active, 1 inactive, 1 with WAL at risk',
        )).toBeInTheDocument();
    });

    it('uses the singular and omits the risk count when healthy', () => {
        setSlots([makeSlot()]);

        renderSection();

        expect(screen.getByText('1 slot, 1 active, 0 inactive'))
            .toBeInTheDocument();
    });

    it('notes when only the first slots are shown', () => {
        setSlots([makeSlot()], 140);

        renderSection();

        expect(screen.getByText('Showing the first 1 of 140 slots by name'))
            .toBeInTheDocument();
    });

    it('omits the truncation note when every slot is shown', () => {
        setSlots([makeSlot()]);

        renderSection();

        expect(screen.queryByText(/Showing the first/)).not.toBeInTheDocument();
    });

    it('omits the replication context when there is none', () => {
        renderSection();

        expect(screen.queryByRole('group', { name: 'Replication context' }))
            .not.toBeInTheDocument();
    });

    it('shows the upstream and downstream servers', () => {
        const rel = (id: number, name: string, type: string) => ({
            target_server_id: id,
            target_server_name: name,
            relationship_type: type,
            is_auto_detected: true,
        });
        mockClusterData = [{
            id: 'g',
            name: 'Group',
            clusters: [{
                id: 'c',
                name: 'Cluster',
                servers: [
                    { id: 1, name: 'origin', relationships: [] },
                    {
                        id: 2,
                        name: 'middle',
                        relationships: [rel(1, 'origin', 'streams_from')],
                    },
                    {
                        id: 3,
                        name: 'leaf-a',
                        relationships: [rel(2, 'middle', 'streams_from')],
                    },
                    {
                        id: 4,
                        name: 'leaf-b',
                        relationships: [rel(2, 'middle', 'subscribes_to')],
                    },
                ],
            }],
        }];

        renderSection(2);

        const context = screen.getByRole('group', {
            name: 'Replication context',
        });
        expect(context).toHaveTextContent('Upstream');
        expect(context).toHaveTextContent('Streams from origin');
        expect(context).toHaveTextContent('Downstream');
        expect(context).toHaveTextContent('Standby leaf-a;');
        expect(context).toHaveTextContent('Subscriber leaf-b');
        expect(within(context).getByText('origin').tagName).toBe('BDI');
    });

    it('shows only the downstream line for a primary', () => {
        mockClusterData = [{
            id: 'g',
            name: 'Group',
            clusters: [{
                id: 'c',
                name: 'Cluster',
                servers: [
                    { id: 1, name: 'primary' },
                    {
                        id: 2,
                        name: 'replica',
                        relationships: [{
                            target_server_id: 1,
                            target_server_name: 'primary',
                            relationship_type: 'streams_from',
                            is_auto_detected: true,
                        }],
                    },
                ],
            }],
        }];

        renderSection(1);

        const context = screen.getByRole('group', {
            name: 'Replication context',
        });
        expect(context).not.toHaveTextContent('Upstream');
        expect(context).toHaveTextContent('Standby replica');
    });
});
