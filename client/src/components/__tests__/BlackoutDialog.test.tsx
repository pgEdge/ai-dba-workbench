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
import BlackoutDialog from '../BlackoutDialog';
import { renderWithTheme } from '../../test/renderWithTheme';
import type {
    ClusterSelection,
    EstateSelection,
    ServerSelection,
} from '../../types/selection';

const mockCreateBlackout = vi.fn();

vi.mock('../../contexts/useBlackouts', () => ({
    useBlackouts: () => ({ createBlackout: mockCreateBlackout }),
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

const estate: EstateSelection = {
    type: 'estate',
    name: 'All Servers',
    status: 'online',
    groups: [],
};

const radio = (name: string) =>
    screen.getByRole('radio', { name }) as HTMLInputElement;

const createButton = () =>
    screen.getByRole('button', { name: 'Create Blackout' });

describe('BlackoutDialog', () => {
    beforeEach(() => {
        mockCreateBlackout.mockReset();
        mockCreateBlackout.mockResolvedValue(undefined);
    });

    describe('scope availability', () => {
        it('enables Cluster for a server in an auto-detected cluster (issue #809)', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({
                        clusterId: 'server-7',
                        clusterName: 'Auto cluster',
                        dbClusterId: 42,
                    })}
                />,
            );
            expect(radio('Cluster').disabled).toBe(false);
            expect(radio('Server').checked).toBe(true);
            expect(screen.getByText('pg-7')).toBeInTheDocument();
        });

        it('disables Cluster for a standalone server', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({
                        dbClusterId: 42,
                        isStandalone: true,
                    })}
                />,
            );
            expect(radio('Cluster').disabled).toBe(true);
            expect(radio('Group').disabled).toBe(true);
            expect(radio('Estate').disabled).toBe(false);
        });

        it('selects Cluster for an auto-detected cluster selection', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeClusterSelection({ dbClusterId: 42 })}
                />,
            );
            expect(radio('Cluster').checked).toBe(true);
            expect(radio('Server').disabled).toBe(true);
            expect(screen.getByText('Auto cluster')).toBeInTheDocument();
        });

        it('falls back to the first available scope', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeClusterSelection({
                        groupId: 'group-3',
                        groupName: 'Production',
                    })}
                />,
            );
            expect(radio('Cluster').disabled).toBe(true);
            expect(radio('Group').checked).toBe(true);
            expect(screen.getByText('Production')).toBeInTheDocument();
        });

        it('falls back to Estate when nothing else is available', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeClusterSelection()}
                />,
            );
            expect(radio('Estate').checked).toBe(true);
        });

        it('uses Estate when there is no selection', () => {
            renderWithTheme(
                <BlackoutDialog open onClose={vi.fn()} selection={null} />,
            );
            expect(radio('Estate').checked).toBe(true);
            expect(radio('Server').disabled).toBe(true);
        });

        it('selects Estate for an estate selection and shows its label', () => {
            renderWithTheme(
                <BlackoutDialog open onClose={vi.fn()} selection={estate} />,
            );
            expect(radio('Estate').checked).toBe(true);
            expect(screen.getByText('All Servers')).toBeInTheDocument();
        });

        it('changes scope when another radio is chosen', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(radio('Estate'));
            expect(radio('Estate').checked).toBe(true);
            expect(screen.getByText('All Servers')).toBeInTheDocument();
        });
    });

    describe('duration', () => {
        it('shows the end time for the default preset', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            expect(screen.getByText(/^Ends at /)).toBeInTheDocument();
            expect(createButton()).toBeEnabled();
        });

        it('disables Create for a zero custom duration', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.change(screen.getByLabelText('Hours'), {
                target: { value: '0' },
            });
            expect(createButton()).toBeDisabled();
            expect(screen.queryByText(/^Ends at /)).not.toBeInTheDocument();
        });

        it('accepts custom hours and minutes', async () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.change(screen.getByLabelText('Hours'), {
                target: { value: '1' },
            });
            fireEvent.change(screen.getByLabelText('Minutes'), {
                target: { value: '30' },
            });
            expect(createButton()).toBeEnabled();

            const before = Date.now();
            fireEvent.click(createButton());
            await waitFor(() => {
                expect(mockCreateBlackout).toHaveBeenCalledTimes(1);
            });
            const payload = mockCreateBlackout.mock.calls[0][0];
            const minutes = (Date.parse(payload.end_time) - before) / 60000;
            expect(minutes).toBeGreaterThanOrEqual(89);
            expect(minutes).toBeLessThanOrEqual(91);
        });

        it('clears custom values when a preset is chosen', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            const hours = screen.getByLabelText('Hours') as HTMLInputElement;
            fireEvent.change(hours, { target: { value: '0' } });
            expect(createButton()).toBeDisabled();
            fireEvent.click(screen.getByText('2h'));
            expect(hours.value).toBe('');
            expect(createButton()).toBeEnabled();
        });
    });

    describe('future mode', () => {
        it('requires both dates and submits them', async () => {
            const onClose = vi.fn();
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={onClose}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(
                screen.getByRole('button', { name: 'Schedule Future' }),
            );
            expect(screen.queryByLabelText('Hours')).not.toBeInTheDocument();
            expect(createButton()).toBeDisabled();

            fireEvent.change(screen.getByLabelText('Start Date/Time'), {
                target: { value: '2030-01-01T10:00' },
            });
            expect(createButton()).toBeDisabled();
            fireEvent.change(screen.getByLabelText('End Date/Time'), {
                target: { value: '2030-01-01T12:00' },
            });
            expect(createButton()).toBeEnabled();

            fireEvent.click(createButton());
            await waitFor(() => {
                expect(onClose).toHaveBeenCalled();
            });
            const payload = mockCreateBlackout.mock.calls[0][0];
            expect(payload.start_time).toBe(
                new Date('2030-01-01T10:00').toISOString(),
            );
            expect(payload.end_time).toBe(
                new Date('2030-01-01T12:00').toISOString(),
            );
        });

        it('ignores deselecting the active mode', () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(screen.getByRole('button', { name: 'Start Now' }));
            expect(screen.getByLabelText('Hours')).toBeInTheDocument();
        });
    });

    describe('submission', () => {
        it('sends cluster_id from dbClusterId for an auto-detected cluster', async () => {
            const onClose = vi.fn();
            const onSuccess = vi.fn();
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={onClose}
                    onSuccess={onSuccess}
                    selection={makeClusterSelection({ dbClusterId: 42 })}
                />,
            );
            fireEvent.change(screen.getByLabelText('Reason'), {
                target: { value: '  Patching  ' },
            });
            fireEvent.click(createButton());
            await waitFor(() => {
                expect(onSuccess).toHaveBeenCalled();
            });
            expect(onClose).toHaveBeenCalled();
            const payload = mockCreateBlackout.mock.calls[0][0];
            expect(payload).toMatchObject({
                scope: 'cluster',
                cluster_id: 42,
                reason: 'Patching',
            });
            expect(payload).not.toHaveProperty('connection_id');
            expect(payload).not.toHaveProperty('group_id');
        });

        it('sends connection_id for a server', async () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(createButton());
            await waitFor(() => {
                expect(mockCreateBlackout).toHaveBeenCalled();
            });
            expect(mockCreateBlackout.mock.calls[0][0]).toMatchObject({
                scope: 'server',
                connection_id: 7,
            });
        });

        it('sends group_id for a group scope', async () => {
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection({
                        groupId: 'group-3',
                        groupName: 'Production',
                    })}
                />,
            );
            fireEvent.click(radio('Group'));
            fireEvent.click(createButton());
            await waitFor(() => {
                expect(mockCreateBlackout).toHaveBeenCalled();
            });
            expect(mockCreateBlackout.mock.calls[0][0]).toMatchObject({
                scope: 'group',
                group_id: 3,
            });
        });

        it('shows the saving state and blocks closing', async () => {
            let resolveCreate: () => void = () => undefined;
            mockCreateBlackout.mockReturnValue(
                new Promise<void>((resolve) => { resolveCreate = resolve; }),
            );
            const onClose = vi.fn();
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={onClose}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(createButton());
            expect(await screen.findByLabelText('Saving')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
            fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
            expect(onClose).not.toHaveBeenCalled();

            resolveCreate();
            await waitFor(() => {
                expect(onClose).toHaveBeenCalled();
            });
        });

        it('shows an error message and lets it be dismissed', async () => {
            mockCreateBlackout.mockRejectedValue(new Error('Access denied'));
            const onClose = vi.fn();
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={onClose}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(createButton());
            expect(await screen.findByText('Access denied')).toBeInTheDocument();
            expect(onClose).not.toHaveBeenCalled();

            fireEvent.click(screen.getByRole('button', { name: 'Close' }));
            expect(screen.queryByText('Access denied')).not.toBeInTheDocument();
        });

        it('shows a generic message for a non-Error rejection', async () => {
            mockCreateBlackout.mockRejectedValue('nope');
            renderWithTheme(
                <BlackoutDialog
                    open
                    onClose={vi.fn()}
                    selection={makeServerSelection()}
                />,
            );
            fireEvent.click(createButton());
            expect(
                await screen.findByText('Failed to create blackout'),
            ).toBeInTheDocument();
        });
    });

    it('calls onClose from Cancel', () => {
        const onClose = vi.fn();
        renderWithTheme(
            <BlackoutDialog
                open
                onClose={onClose}
                selection={makeServerSelection()}
            />,
        );
        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        expect(onClose).toHaveBeenCalled();
    });

    it('resets the form when reopened', () => {
        const selection = makeServerSelection();
        const { rerender } = renderWithTheme(
            <BlackoutDialog open onClose={vi.fn()} selection={selection} />,
        );
        fireEvent.change(screen.getByLabelText('Reason'), {
            target: { value: 'Old reason' },
        });
        rerender(
            <BlackoutDialog open={false} onClose={vi.fn()} selection={selection} />,
        );
        rerender(
            <BlackoutDialog open onClose={vi.fn()} selection={selection} />,
        );
        expect(
            (screen.getByLabelText('Reason') as HTMLInputElement).value,
        ).toBe('');
    });
});
