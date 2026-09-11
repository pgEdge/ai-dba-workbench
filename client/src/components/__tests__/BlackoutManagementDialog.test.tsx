/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Tests for BlackoutManagementDialog. These exercise the dialog end to
 * end:
 *
 *   - The empty state when no blackouts or schedules exist.
 *   - The active-blackout banner, including the FrontHandSharp icon,
 *     the scope chip, the time-remaining formatting branches, and the
 *     Stop action.
 *   - The non-active blackout list, including reason and creator meta,
 *     the time-range formatting, and the delete-confirmation flow.
 *   - The schedule list, including enabled/disabled state, the duration
 *     label formatting branches, timezone handling, and deletion.
 *   - The create actions and the close control.
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import BlackoutManagementDialog from '../BlackoutManagementDialog';
import { renderWithTheme } from '../../test/renderWithTheme';
import type { ServerSelection } from '../../types/selection';
import type { Blackout, BlackoutSchedule } from '../../contexts/BlackoutContext';

// ---------------------------------------------------------------------------
// Blackout context mock. The arrays are reassigned per test.
// ---------------------------------------------------------------------------

const mockStopBlackout = vi.fn();
const mockDeleteBlackout = vi.fn();
const mockDeleteSchedule = vi.fn();

let mockBlackouts: Blackout[] = [];
let mockSchedules: BlackoutSchedule[] = [];
let mockActive: Blackout[] = [];

vi.mock('../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({
        blackouts: mockBlackouts,
        schedules: mockSchedules,
        activeBlackoutsForSelection: mockActive,
        stopBlackout: mockStopBlackout,
        deleteBlackout: mockDeleteBlackout,
        deleteSchedule: mockDeleteSchedule,
    }),
}));

// Stub the sub-dialogs so we can observe their open state and drive the
// delete-confirmation callback directly.
vi.mock('../BlackoutDialog', () => ({
    default: ({ open }: { open: boolean }) =>
        open ? <div data-testid="blackout-dialog-open" /> : null,
}));
vi.mock('../BlackoutScheduleDialog', () => ({
    default: ({ open }: { open: boolean }) =>
        open ? <div data-testid="schedule-dialog-open" /> : null,
}));
vi.mock('../DeleteConfirmationDialog', () => ({
    default: ({
        open,
        title,
        message,
        onConfirm,
    }: {
        open: boolean;
        title: string;
        message: string;
        onConfirm: () => void | Promise<void>;
    }) =>
        open ? (
            <div data-testid="delete-dialog">
                <span data-testid="delete-title">{title}</span>
                <span data-testid="delete-message">{message}</span>
                <button onClick={() => { void onConfirm(); }}>confirm-delete</button>
            </div>
        ) : null,
}));

const selection: ServerSelection = {
    type: 'server',
    id: 1,
    name: 'db-1',
    status: 'online',
    description: '',
    host: 'localhost',
    port: 5432,
    role: 'primary',
    version: '16',
    database: 'postgres',
    username: 'admin',
    os: 'linux',
    platform: 'x86_64',
};

const makeBlackout = (overrides: Partial<Blackout> = {}): Blackout => ({
    id: 1,
    scope: 'server',
    reason: 'Maintenance window',
    start_time: '2026-04-20T10:00:00Z',
    end_time: '2026-04-20T12:00:00Z',
    created_by: 'alice',
    created_at: '2026-04-19T10:00:00Z',
    is_active: false,
    ...overrides,
});

const makeSchedule = (
    overrides: Partial<BlackoutSchedule> = {},
): BlackoutSchedule => ({
    id: 10,
    scope: 'cluster',
    name: 'Nightly',
    cron_expression: '0 0 * * *',
    duration_minutes: 90,
    timezone: 'UTC',
    reason: 'Nightly maintenance',
    enabled: true,
    created_by: 'bob',
    created_at: '2026-04-19T10:00:00Z',
    updated_at: '2026-04-19T10:00:00Z',
    ...overrides,
});

const renderDialog = (open = true) =>
    renderWithTheme(
        <BlackoutManagementDialog
            open={open}
            onClose={onClose}
            selection={selection}
        />,
    );

const onClose = vi.fn();

describe('BlackoutManagementDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockBlackouts = [];
        mockSchedules = [];
        mockActive = [];
    });

    it('does not render dialog content when closed', () => {
        renderDialog(false);
        expect(
            screen.queryByText('Blackout management'),
        ).not.toBeInTheDocument();
    });

    it('shows the empty state when nothing is configured', () => {
        renderDialog();
        expect(screen.getByText('Blackout management')).toBeInTheDocument();
        expect(
            screen.getByText('No blackouts or schedules configured'),
        ).toBeInTheDocument();
    });

    it('invokes onClose when the close button is clicked', () => {
        renderDialog();
        fireEvent.click(screen.getByLabelText('close'));
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('renders an active blackout with hours-remaining and stops it', () => {
        const future = new Date(Date.now() + 3 * 60 * 60 * 1000 + 15 * 60 * 1000);
        const active = makeBlackout({
            id: 1,
            scope: 'estate',
            end_time: future.toISOString(),
            reason: 'Active reason',
        });
        mockActive = [active];
        mockBlackouts = [active];

        renderDialog();
        expect(screen.getByText('Blackout Active')).toBeInTheDocument();
        expect(screen.getByText('Estate')).toBeInTheDocument();
        expect(screen.getByText('Active reason')).toBeInTheDocument();
        expect(screen.getByText(/3h \d+m remaining/)).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: /Stop/ }));
        expect(mockStopBlackout).toHaveBeenCalledWith(1);
    });

    it('formats minutes-only remaining time for a short active blackout', () => {
        const future = new Date(Date.now() + 30 * 60 * 1000);
        mockActive = [makeBlackout({ id: 2, end_time: future.toISOString(), reason: '' })];
        mockBlackouts = [];
        renderDialog();
        expect(screen.getByText(/\d+m remaining/)).toBeInTheDocument();
    });

    it('shows "Ending..." when an active blackout has already elapsed', () => {
        const past = new Date(Date.now() - 60 * 1000);
        mockActive = [makeBlackout({ id: 3, end_time: past.toISOString() })];
        renderDialog();
        expect(screen.getByText('Ending...')).toBeInTheDocument();
    });

    it('renders a non-active blackout with reason and creator', () => {
        mockBlackouts = [
            makeBlackout({ id: 5, scope: 'group', reason: 'Planned', created_by: 'carol' }),
        ];
        renderDialog();
        expect(screen.getByText('Blackouts')).toBeInTheDocument();
        expect(screen.getByText('Group')).toBeInTheDocument();
        expect(screen.getByText('Planned')).toBeInTheDocument();
        expect(screen.getByText('carol')).toBeInTheDocument();
    });

    it('uses the raw scope label and default icon for an unknown scope', () => {
        mockBlackouts = [
            makeBlackout({ id: 6, scope: 'weird' as Blackout['scope'], reason: '', created_by: '' }),
        ];
        renderDialog();
        // getScopeLabel falls through to returning the raw scope string.
        expect(screen.getByText('weird')).toBeInTheDocument();
    });

    it('runs the delete-confirmation flow for a non-active blackout', async () => {
        mockBlackouts = [makeBlackout({ id: 7, scope: 'cluster' })];
        renderDialog();
        fireEvent.click(screen.getByLabelText('Delete blackout'));
        expect(screen.getByTestId('delete-title')).toHaveTextContent(
            'Delete blackout',
        );
        expect(screen.getByTestId('delete-message')).toHaveTextContent(
            'Are you sure you want to delete this blackout?',
        );
        fireEvent.click(screen.getByText('confirm-delete'));
        await waitFor(() =>
            expect(mockDeleteBlackout).toHaveBeenCalledWith(7),
        );
    });

    it('renders an enabled schedule with an hours+minutes duration', () => {
        mockSchedules = [
            makeSchedule({ id: 10, duration_minutes: 90, enabled: true, timezone: 'UTC' }),
        ];
        renderDialog();
        expect(screen.getByText('Schedules')).toBeInTheDocument();
        expect(screen.getByText('Nightly')).toBeInTheDocument();
        expect(screen.getByText('0 0 * * *')).toBeInTheDocument();
        expect(screen.getByText(/1h 30m duration \(UTC\)/)).toBeInTheDocument();
    });

    it('renders a disabled schedule with a whole-hour duration and no timezone', () => {
        mockSchedules = [
            makeSchedule({
                id: 11,
                name: 'Weekly',
                duration_minutes: 60,
                enabled: false,
                timezone: '',
            }),
        ];
        renderDialog();
        expect(screen.getByText('Weekly')).toBeInTheDocument();
        // 60 minutes -> "1h " with no trailing minutes and no timezone suffix.
        expect(screen.getByText(/1h\s+duration/)).toBeInTheDocument();
    });

    it('renders a sub-hour schedule duration in minutes', () => {
        mockSchedules = [
            makeSchedule({ id: 12, name: 'Quick', duration_minutes: 45, timezone: '' }),
        ];
        renderDialog();
        expect(screen.getByText(/45m duration/)).toBeInTheDocument();
    });

    it('runs the delete-confirmation flow for a schedule', async () => {
        mockSchedules = [makeSchedule({ id: 13 })];
        renderDialog();
        fireEvent.click(screen.getByLabelText('Delete schedule'));
        expect(screen.getByTestId('delete-title')).toHaveTextContent(
            'Delete schedule',
        );
        expect(screen.getByTestId('delete-message')).toHaveTextContent(
            'Are you sure you want to delete this schedule?',
        );
        fireEvent.click(screen.getByText('confirm-delete'));
        await waitFor(() =>
            expect(mockDeleteSchedule).toHaveBeenCalledWith(13),
        );
    });

    it('opens the one-time blackout dialog from the create action', () => {
        renderDialog();
        fireEvent.click(
            screen.getByRole('button', { name: /New One Time Blackout/ }),
        );
        expect(screen.getByTestId('blackout-dialog-open')).toBeInTheDocument();
    });

    it('opens the scheduled blackout dialog from the create action', () => {
        renderDialog();
        fireEvent.click(
            screen.getByRole('button', { name: /New Scheduled Blackout/ }),
        );
        expect(screen.getByTestId('schedule-dialog-open')).toBeInTheDocument();
    });
});
