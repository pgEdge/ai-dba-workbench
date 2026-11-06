/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import BlackoutScheduleDialog from '../BlackoutScheduleDialog';
import { renderWithTheme } from '../../test/renderWithTheme';
import type {
    ClusterSelection,
    ServerSelection,
} from '../../types/selection';

const mockCreateSchedule = vi.fn();
const mockUpdateSchedule = vi.fn();

vi.mock('../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({
        createSchedule: mockCreateSchedule,
        updateSchedule: mockUpdateSchedule,
    }),
}));

const makeServerSelection = (
    overrides: Partial<ServerSelection> = {},
): ServerSelection => ({
    type: 'server',
    id: 7,
    name: 'pg-7',
    status: 'online',
    description: '',
    host: '192.0.2.7',
    port: 5432,
    role: 'primary',
    version: '17.0',
    database: 'postgres',
    username: 'postgres',
    os: 'linux',
    platform: 'amd64',
    ...overrides,
});

const makeClusterSelection = (
    overrides: Partial<ClusterSelection> = {},
): ClusterSelection => ({
    type: 'cluster',
    id: 'server-7',
    name: 'Auto cluster',
    status: 'online',
    description: '',
    servers: [],
    serverIds: [],
    ...overrides,
});

const radio = (name: string) =>
    screen.getByRole('radio', { name }) as HTMLInputElement;

const input = (label: string) =>
    screen.getByLabelText(label, { exact: label !== 'Name' }) as HTMLInputElement;

const saveButton = (name = 'Create blackout schedule') =>
    screen.getByRole('button', { name });

const setName = (value: string) => {
    fireEvent.change(input('Name'), { target: { value } });
};

describe('BlackoutScheduleDialog', () => {
    beforeEach(() => {
        mockCreateSchedule.mockReset();
        mockCreateSchedule.mockResolvedValue(undefined);
        mockUpdateSchedule.mockReset();
        mockUpdateSchedule.mockResolvedValue(undefined);
    });

    describe('scope availability', () => {
        it('enables Cluster for a server in an auto-detected cluster (issue #809)', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({
                        clusterName: 'Auto cluster',
                        dbClusterId: 42,
                    })}
                />,
            );
            expect(radio('Cluster').disabled).toBe(false);
            expect(radio('Server').checked).toBe(true);
            fireEvent.click(radio('Cluster'));
            expect(radio('Cluster').checked).toBe(true);
            expect(screen.getByText('Auto cluster')).toBeInTheDocument();
        });

        it('disables Cluster for a standalone server', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({
                        dbClusterId: 42,
                        isStandalone: true,
                    })}
                />,
            );
            expect(radio('Cluster').disabled).toBe(true);
        });

        it('falls back to the first available scope', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeClusterSelection({ groupId: 'group-3' })}
                />,
            );
            expect(radio('Group').checked).toBe(true);
        });

        it('falls back to Estate without a selection', () => {
            renderWithTheme(
                <BlackoutScheduleDialog open onClose={vi.fn()} />,
            );
            expect(radio('Estate').checked).toBe(true);
        });
    });

    describe('recurrence', () => {
        it('builds a daily cron from the hour and minute fields', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            expect(input('Cron Expression').value).toBe('0 2 * * *');
            expect(screen.getByText('Daily at 2:00 AM')).toBeInTheDocument();

            fireEvent.change(input('Hour'), { target: { value: '14' } });
            fireEvent.change(input('Minute'), { target: { value: '30' } });
            expect(input('Cron Expression').value).toBe('30 14 * * *');
            expect(screen.getByText('Daily at 2:30 PM')).toBeInTheDocument();

            fireEvent.change(input('Hour'), { target: { value: '0' } });
            expect(screen.getByText('Daily at 12:30 AM')).toBeInTheDocument();
        });

        it.each([
            ['Weekdays', '0 2 * * 1-5', 'Weekdays at 2:00 AM'],
            ['Weekends', '0 2 * * 0,6', 'Weekends at 2:00 AM'],
            ['Weekly', '0 2 * * 0', 'Every Sunday at 2:00 AM'],
            ['Monthly', '0 2 1 * *', 'First of each month at 2:00 AM'],
        ])('describes the %s preset', (label, cron, description) => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(screen.getByText(label));
            expect(input('Cron Expression').value).toBe(cron);
            expect(screen.getByText(description)).toBeInTheDocument();
        });

        it('describes custom, unusual and invalid expressions', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(screen.getByText('Custom'));
            expect(screen.queryByLabelText('Hour')).not.toBeInTheDocument();
            const cron = input('Cron Expression');
            expect(cron.value).toBe('0 2 * * *');

            fireEvent.change(cron, { target: { value: '*/5 * * * *' } });
            expect(screen.getByText('Daily at *:*/5')).toBeInTheDocument();

            fireEvent.change(cron, { target: { value: '0 3 15 * 2' } });
            expect(
                screen.getByText('0 3 15 * 2 (at 3:00 AM)'),
            ).toBeInTheDocument();

            fireEvent.change(cron, { target: { value: '0 3 *' } });
            expect(
                screen.getByText('Invalid cron expression'),
            ).toBeInTheDocument();

            fireEvent.change(cron, { target: { value: '' } });
            expect(
                screen.queryByText('Invalid cron expression'),
            ).not.toBeInTheDocument();
            setName('Nightly');
            expect(saveButton()).toBeDisabled();

            fireEvent.click(screen.getByText('Daily'));
            expect(input('Cron Expression').value).toBe('0 2 * * *');
            expect(saveButton()).toBeEnabled();
        });

        it('keeps an existing custom cron when Custom is chosen again', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(screen.getByText('Custom'));
            fireEvent.change(input('Cron Expression'), {
                target: { value: '15 4 * * *' },
            });
            fireEvent.click(screen.getByText('Custom'));
            expect(input('Cron Expression').value).toBe('15 4 * * *');
        });

        it('ignores edits to the read-only preset expression', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.change(input('Cron Expression'), {
                target: { value: '1 1 1 1 1' },
            });
            expect(input('Cron Expression').value).toBe('0 2 * * *');
        });
    });

    describe('duration and validation', () => {
        it('requires a name and a positive duration', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            expect(saveButton()).toBeDisabled();
            setName('Nightly');
            expect(saveButton()).toBeEnabled();

            fireEvent.change(input('Custom (minutes)'), {
                target: { value: '0' },
            });
            expect(saveButton()).toBeDisabled();
            fireEvent.change(input('Custom (minutes)'), {
                target: { value: '90' },
            });
            expect(saveButton()).toBeEnabled();

            fireEvent.click(screen.getByText('4h'));
            expect(input('Custom (minutes)').value).toBe('');
        });
    });

    describe('submission', () => {
        it('creates a cluster schedule using dbClusterId', async () => {
            const onClose = vi.fn();
            const onSuccess = vi.fn();
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={onClose}
                    onSuccess={onSuccess}
                    selection={makeClusterSelection({ dbClusterId: 42 })}
                />,
            );
            expect(radio('Cluster').checked).toBe(true);
            setName('  Nightly  ');
            fireEvent.click(screen.getByText('2h'));
            fireEvent.change(input('Timezone'), {
                target: { value: ' UTC ' },
            });
            fireEvent.change(input('Reason'), {
                target: { value: ' Backups ' },
            });
            fireEvent.click(screen.getByRole('checkbox'));
            expect(screen.getByText('Disabled')).toBeInTheDocument();

            fireEvent.click(saveButton());
            await waitFor(() => {
                expect(onSuccess).toHaveBeenCalled();
            });
            expect(onClose).toHaveBeenCalled();
            const payload = mockCreateSchedule.mock.calls[0][0];
            expect(payload).toEqual({
                scope: 'cluster',
                name: 'Nightly',
                cron_expression: '0 2 * * *',
                duration_minutes: 120,
                timezone: 'UTC',
                reason: 'Backups',
                enabled: false,
                cluster_id: 42,
            });
        });

        it('creates server and group schedules with their IDs', async () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({ groupId: 'group-3' })}
                />,
            );
            setName('Nightly');
            fireEvent.click(saveButton());
            await waitFor(() => {
                expect(mockCreateSchedule).toHaveBeenCalledTimes(1);
            });
            expect(mockCreateSchedule.mock.calls[0][0]).toMatchObject({
                scope: 'server',
                connection_id: 7,
            });

            fireEvent.click(radio('Group'));
            fireEvent.click(saveButton());
            await waitFor(() => {
                expect(mockCreateSchedule).toHaveBeenCalledTimes(2);
            });
            expect(mockCreateSchedule.mock.calls[1][0]).toMatchObject({
                scope: 'group',
                group_id: 3,
            });
        });

        it('updates an existing schedule', async () => {
            const onClose = vi.fn();
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={onClose}
                    selection={makeClusterSelection({ dbClusterId: 42 })}
                    schedule={{
                        id: 5,
                        scope: 'cluster',
                        name: 'Weekly patching',
                        cron_expression: '30 3 * * 0',
                        duration_minutes: 90,
                        timezone: 'Europe/London',
                        reason: 'Patching',
                        enabled: false,
                    }}
                />,
            );
            expect(
                screen.getByText('Edit blackout schedule'),
            ).toBeInTheDocument();
            expect(input('Name').value).toBe('Weekly patching');
            expect(input('Cron Expression').value).toBe('30 3 * * 0');
            expect(input('Timezone').value).toBe('Europe/London');
            expect(screen.getByText('Disabled')).toBeInTheDocument();

            fireEvent.click(saveButton('Update blackout schedule'));
            await waitFor(() => {
                expect(onClose).toHaveBeenCalled();
            });
            expect(mockUpdateSchedule).toHaveBeenCalledWith(
                5,
                expect.objectContaining({
                    scope: 'cluster',
                    cluster_id: 42,
                    cron_expression: '30 3 * * 0',
                    duration_minutes: 90,
                }),
            );
            expect(mockCreateSchedule).not.toHaveBeenCalled();
        });

        it('fills defaults for a sparse existing schedule', () => {
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    schedule={{ id: 6, scope: 'bogus' }}
                />,
            );
            expect(input('Name').value).toBe('');
            expect(radio('Server').checked).toBe(true);
            expect(screen.getByText('Enabled')).toBeInTheDocument();
            expect(input('Timezone').value).not.toBe('');
        });

        it('shows the saving state and blocks closing', async () => {
            let resolveSave: () => void = () => undefined;
            mockCreateSchedule.mockReturnValue(
                new Promise<void>((resolve) => { resolveSave = resolve; }),
            );
            const onClose = vi.fn();
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={onClose}
                    selection={makeServerSelection()}
                />,
            );
            setName('Nightly');
            fireEvent.click(saveButton());
            expect(await screen.findByLabelText('Saving')).toBeInTheDocument();
            fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
            fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
            expect(onClose).not.toHaveBeenCalled();

            resolveSave();
            await waitFor(() => {
                expect(onClose).toHaveBeenCalled();
            });
        });

        it('shows an error and lets it be dismissed', async () => {
            mockCreateSchedule.mockRejectedValue(new Error('Access denied'));
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            setName('Nightly');
            fireEvent.click(saveButton());
            expect(await screen.findByText('Access denied')).toBeInTheDocument();
            fireEvent.click(screen.getByRole('button', { name: 'Close' }));
            expect(screen.queryByText('Access denied')).not.toBeInTheDocument();
        });

        it('shows a generic message for a non-Error rejection', async () => {
            mockCreateSchedule.mockRejectedValue('nope');
            renderWithTheme(
                <BlackoutScheduleDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            setName('Nightly');
            fireEvent.click(saveButton());
            expect(
                await screen.findByText('Failed to save schedule'),
            ).toBeInTheDocument();
        });
    });

    it('calls onClose from Cancel', () => {
        const onClose = vi.fn();
        renderWithTheme(
            <BlackoutScheduleDialog
                open
                onClose={onClose}
                selection={makeServerSelection()}
            />,
        );
        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        expect(onClose).toHaveBeenCalled();
    });
});
