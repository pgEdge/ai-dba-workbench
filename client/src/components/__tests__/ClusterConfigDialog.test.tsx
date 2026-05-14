/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ClusterConfigDialog from '../ClusterConfigDialog';
import { renderWithTheme } from '../../test/renderWithTheme';

/*
 * Mock the heavyweight child panels so the dialog can render without
 * hitting the network or pulling in unrelated components.
 */
vi.mock('../AlertOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="alert-overrides-panel">
            AlertOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ProbeOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="probe-overrides-panel">
            ProbeOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../ChannelOverridesPanel', () => ({
    default: ({ scope, scopeId }: { scope: string; scopeId: number }) => (
        <div data-testid="channel-overrides-panel">
            ChannelOverridesPanel: {scope} {scopeId}
        </div>
    ),
}));

vi.mock('../TopologyPanel', () => ({
    default: ({
        clusterId,
        clusterName,
        replicationType,
        autoClusterKey,
    }: {
        clusterId: number;
        clusterName: string;
        replicationType: string | null;
        autoClusterKey?: string | null;
    }) => (
        <div data-testid="topology-panel">
            TopologyPanel: id={clusterId} name={clusterName}{' '}
            replicationType={String(replicationType)}{' '}
            autoClusterKey={String(autoClusterKey)}
        </div>
    ),
}));

/**
 * Returns the rendered replication-type select element.
 */
const getReplicationTypeSelect = () =>
    screen.getByLabelText('Replication Type');

describe('ClusterConfigDialog', () => {
    const baseProps = {
        open: true,
        onClose: vi.fn(),
        onSave: vi.fn(),
        onCreate: vi.fn(),
        onMembershipChange: vi.fn(),
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('replication type derivation (issue #235)', () => {
        it('shows the explicit replication type in edit mode', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-1"
                    numericClusterId={1}
                    clusterName="Manual Cluster"
                    clusterDescription=""
                    replicationType="spock"
                    autoClusterKey={null}
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent('Spock');
        });

        it('derives Spock from autoClusterKey when replicationType is null', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-2"
                    numericClusterId={2}
                    clusterName="Auto Spock Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent('Spock');
        });

        it('derives Binary from autoClusterKey with sysid prefix', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-3"
                    numericClusterId={3}
                    clusterName="Auto Binary Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="sysid:123"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent(
                'Binary (Physical)',
            );
        });

        it('derives Logical from autoClusterKey with logical prefix', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-4"
                    numericClusterId={4}
                    clusterName="Auto Logical Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="logical:xyz"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent('Logical');
        });

        it('leaves the select blank when both replicationType and autoClusterKey are null', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-5"
                    numericClusterId={5}
                    clusterName="Bare Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const select = getReplicationTypeSelect();
            // Assert that no replication-type label is shown when the
            // value is empty.
            expect(select).not.toHaveTextContent('Spock');
            expect(select).not.toHaveTextContent('Binary (Physical)');
            expect(select).not.toHaveTextContent('Logical');
            expect(select).not.toHaveTextContent(/^Other$/);
        });

        it('leaves the select blank in create mode', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const select = getReplicationTypeSelect();
            expect(select).not.toHaveTextContent('Spock');
            expect(select).not.toHaveTextContent('Binary (Physical)');
            expect(select).not.toHaveTextContent('Logical');
        });

        it('prefers explicit replicationType over autoClusterKey when both are set', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-6"
                    numericClusterId={6}
                    clusterName="Mixed Cluster"
                    clusterDescription=""
                    replicationType="logical"
                    autoClusterKey="spock:abc"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent('Logical');
        });

        it('re-derives replication type when the dialog is reopened with a different cluster', () => {
            const { rerender } = renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-a"
                    numericClusterId={1}
                    clusterName="Cluster A"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent('Spock');

            // Close the dialog.
            rerender(
                <ClusterConfigDialog
                    {...baseProps}
                    open={false}
                    mode="edit"
                    clusterId="cluster-a"
                    numericClusterId={1}
                    clusterName="Cluster A"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            // Reopen with a different cluster that should derive Binary.
            rerender(
                <ClusterConfigDialog
                    {...baseProps}
                    open={true}
                    mode="edit"
                    clusterId="cluster-b"
                    numericClusterId={2}
                    clusterName="Cluster B"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="sysid:42"
                />,
            );

            expect(getReplicationTypeSelect()).toHaveTextContent(
                'Binary (Physical)',
            );
        });
    });

    describe('topology panel wiring', () => {
        it('passes the derived replication type to the TopologyPanel', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-7"
                    numericClusterId={7}
                    clusterName="Auto Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            await user.click(screen.getByRole('tab', { name: /topology/i }));

            await waitFor(() => {
                expect(
                    screen.getByTestId('topology-panel'),
                ).toBeInTheDocument();
            });

            expect(screen.getByTestId('topology-panel')).toHaveTextContent(
                'replicationType=spock',
            );
            expect(screen.getByTestId('topology-panel')).toHaveTextContent(
                'autoClusterKey=spock:abc',
            );
        });

        it('lets the user override the derived replication type', async () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-8"
                    numericClusterId={8}
                    clusterName="Override Cluster"
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            const select = getReplicationTypeSelect();
            expect(select).toHaveTextContent('Spock');

            // Change the selection to Logical.
            fireEvent.mouseDown(select);

            await waitFor(() => {
                expect(screen.getByRole('listbox')).toBeInTheDocument();
            });

            fireEvent.click(
                within(screen.getByRole('listbox')).getByText('Logical'),
            );

            await waitFor(() => {
                expect(getReplicationTypeSelect()).toHaveTextContent(
                    'Logical',
                );
            });
        });
    });

    describe('save flow', () => {
        it('calls onSave with the derived replication type in edit mode', async () => {
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-9"
                    numericClusterId={9}
                    clusterName="Save Cluster"
                    clusterDescription="desc"
                    replicationType={null}
                    autoClusterKey="spock:abc"
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(onSave).toHaveBeenCalledWith({
                    name: 'Save Cluster',
                    description: 'desc',
                    replication_type: 'spock',
                });
            });
        });

        it('requires a replication type before creating a cluster', async () => {
            const onCreate = vi.fn();
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onCreate={onCreate}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            // Type a name so the name validator passes.
            const nameField = screen.getByRole('textbox', {
                name: /^name/i,
            });
            fireEvent.change(nameField, {
                target: { value: 'Brand New Cluster' },
            });

            fireEvent.click(screen.getByRole('button', { name: /create/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Replication type is required'),
                ).toBeInTheDocument();
            });
            expect(onCreate).not.toHaveBeenCalled();
        });

        it('requires a name before saving', async () => {
            const onSave = vi.fn();
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-10"
                    numericClusterId={10}
                    clusterName=""
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Name is required'),
                ).toBeInTheDocument();
            });
            expect(onSave).not.toHaveBeenCalled();
        });

        it('creates a cluster with the chosen replication type', async () => {
            const onCreate = vi.fn().mockResolvedValue({ id: 99 });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onCreate={onCreate}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const nameField = screen.getByRole('textbox', {
                name: /^name/i,
            });
            fireEvent.change(nameField, {
                target: { value: 'Fresh Cluster' },
            });

            // Choose Binary in the replication type select.
            fireEvent.mouseDown(getReplicationTypeSelect());

            await waitFor(() => {
                expect(screen.getByRole('listbox')).toBeInTheDocument();
            });

            fireEvent.click(
                within(screen.getByRole('listbox')).getByText(
                    'Binary (Physical)',
                ),
            );

            fireEvent.click(screen.getByRole('button', { name: /create/i }));

            await waitFor(() => {
                expect(onCreate).toHaveBeenCalledWith({
                    name: 'Fresh Cluster',
                    description: '',
                    replication_type: 'binary',
                });
            });
        });

        it('surfaces an error message when save fails', async () => {
            const onSave = vi
                .fn()
                .mockRejectedValue(new Error('Save exploded'));
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-11"
                    numericClusterId={11}
                    clusterName="Failing Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Save exploded'),
                ).toBeInTheDocument();
            });
        });

        it('surfaces a fallback error when save rejects with a non-Error', async () => {
            const onSave = vi.fn().mockRejectedValue('boom');
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-12"
                    numericClusterId={12}
                    clusterName="Failing Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Failed to save'),
                ).toBeInTheDocument();
            });
        });

        it('surfaces an error message when create fails', async () => {
            const onCreate = vi
                .fn()
                .mockRejectedValue(new Error('Create exploded'));
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onCreate={onCreate}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const nameField = screen.getByRole('textbox', {
                name: /^name/i,
            });
            fireEvent.change(nameField, {
                target: { value: 'Doomed Cluster' },
            });

            fireEvent.mouseDown(getReplicationTypeSelect());

            await waitFor(() => {
                expect(screen.getByRole('listbox')).toBeInTheDocument();
            });

            fireEvent.click(
                within(screen.getByRole('listbox')).getByText('Spock'),
            );

            fireEvent.click(screen.getByRole('button', { name: /create/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Create exploded'),
                ).toBeInTheDocument();
            });
        });

        it('surfaces a fallback error when create rejects with a non-Error', async () => {
            const onCreate = vi.fn().mockRejectedValue('boom');
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onCreate={onCreate}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const nameField = screen.getByRole('textbox', {
                name: /^name/i,
            });
            fireEvent.change(nameField, {
                target: { value: 'Doomed Cluster' },
            });

            fireEvent.mouseDown(getReplicationTypeSelect());

            await waitFor(() => {
                expect(screen.getByRole('listbox')).toBeInTheDocument();
            });

            fireEvent.click(
                within(screen.getByRole('listbox')).getByText('Spock'),
            );

            fireEvent.click(screen.getByRole('button', { name: /create/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Failed to create cluster'),
                ).toBeInTheDocument();
            });
        });

        it('shows the success banner after a successful create', async () => {
            const onCreate = vi.fn().mockResolvedValue({ id: 7 });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onCreate={onCreate}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            const nameField = screen.getByRole('textbox', {
                name: /^name/i,
            });
            fireEvent.change(nameField, {
                target: { value: 'Brand New Cluster' },
            });

            fireEvent.mouseDown(getReplicationTypeSelect());

            await waitFor(() => {
                expect(screen.getByRole('listbox')).toBeInTheDocument();
            });

            fireEvent.click(
                within(screen.getByRole('listbox')).getByText('Spock'),
            );

            fireEvent.click(screen.getByRole('button', { name: /create/i }));

            await waitFor(() => {
                expect(
                    screen.getByText(/Cluster created successfully/i),
                ).toBeInTheDocument();
            });

            // After creation, the Topology tab becomes available.
            const topologyTab = screen.getByRole('tab', {
                name: /topology/i,
            });
            expect(topologyTab).not.toHaveAttribute('aria-disabled', 'true');
        });
    });

    describe('dialog lifecycle', () => {
        it('renders the create-mode title', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            expect(screen.getByText('Create Cluster')).toBeInTheDocument();
        });

        it('renders the edit-mode title with the cluster name', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-x"
                    numericClusterId={42}
                    clusterName="Production"
                    clusterDescription=""
                    replicationType="spock"
                    autoClusterKey={null}
                />,
            );

            expect(
                screen.getByText('Cluster Settings: Production'),
            ).toBeInTheDocument();
        });

        it('invokes onClose when the close icon is clicked', () => {
            const onClose = vi.fn();
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onClose={onClose}
                    mode="edit"
                    clusterId="cluster-y"
                    numericClusterId={1}
                    clusterName="Closeable Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(
                screen.getByRole('button', {
                    name: /close cluster settings/i,
                }),
            );
            expect(onClose).toHaveBeenCalled();
        });

        it('invokes onClose when the Cancel button is clicked', () => {
            const onClose = vi.fn();
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onClose={onClose}
                    mode="edit"
                    clusterId="cluster-z"
                    numericClusterId={1}
                    clusterName="Cancel Me"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
            expect(onClose).toHaveBeenCalled();
        });

        it('disables the secondary tabs in create mode until the cluster is created', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="create"
                    clusterName=""
                    clusterDescription=""
                    replicationType={null}
                    autoClusterKey={null}
                />,
            );

            // The Topology, Alert overrides, Probe configuration, and
            // Notification channels tabs are all disabled in create
            // mode until the cluster is created.
            for (const tabName of [
                /topology/i,
                /alert overrides/i,
                /probe configuration/i,
                /notification channels/i,
            ]) {
                const tab = screen.getByRole('tab', { name: tabName });
                expect(tab).toBeDisabled();
            }

            // Cancel remains enabled.
            expect(
                screen.getByRole('button', { name: /cancel/i }),
            ).toBeEnabled();
        });

        it('shows the AlertOverridesPanel when the Alert overrides tab is opened', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-w"
                    numericClusterId={55}
                    clusterName="Tabbed Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            await user.click(
                screen.getByRole('tab', { name: /alert overrides/i }),
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('alert-overrides-panel'),
                ).toHaveTextContent('AlertOverridesPanel: cluster 55');
            });
        });

        it('shows the ProbeOverridesPanel when the Probe configuration tab is opened', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-w"
                    numericClusterId={56}
                    clusterName="Tabbed Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            await user.click(
                screen.getByRole('tab', { name: /probe configuration/i }),
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('probe-overrides-panel'),
                ).toHaveTextContent('ProbeOverridesPanel: cluster 56');
            });
        });

        it('shows the ChannelOverridesPanel when the Notification channels tab is opened', async () => {
            const user = userEvent.setup({ delay: null });
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-w"
                    numericClusterId={57}
                    clusterName="Tabbed Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            await user.click(
                screen.getByRole('tab', { name: /notification channels/i }),
            );

            await waitFor(() => {
                expect(
                    screen.getByTestId('channel-overrides-panel'),
                ).toHaveTextContent('ChannelOverridesPanel: cluster 57');
            });
        });

        it('dismisses the save error alert when the close icon is clicked', async () => {
            const onSave = vi
                .fn()
                .mockRejectedValue(new Error('Dismissable error'));
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-err"
                    numericClusterId={1}
                    clusterName="Erroring Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Dismissable error'),
                ).toBeInTheDocument();
            });

            const alert = screen
                .getByText('Dismissable error')
                .closest('.MuiAlert-root');
            expect(alert).not.toBeNull();
            const closeButton = within(alert as HTMLElement).getByRole(
                'button',
                { name: /close/i },
            );
            fireEvent.click(closeButton);

            await waitFor(() => {
                expect(
                    screen.queryByText('Dismissable error'),
                ).not.toBeInTheDocument();
            });
        });

        it('dismisses the success alert when the close icon is clicked', async () => {
            const onSave = vi.fn().mockResolvedValue(undefined);
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    onSave={onSave}
                    mode="edit"
                    clusterId="cluster-ok"
                    numericClusterId={1}
                    clusterName="Good Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText(/Cluster settings saved successfully/i),
                ).toBeInTheDocument();
            });

            const alert = screen
                .getByText(/Cluster settings saved successfully/i)
                .closest('.MuiAlert-root');
            const closeButton = within(alert as HTMLElement).getByRole(
                'button',
                { name: /close/i },
            );
            fireEvent.click(closeButton);

            await waitFor(() => {
                expect(
                    screen.queryByText(
                        /Cluster settings saved successfully/i,
                    ),
                ).not.toBeInTheDocument();
            });
        });

        it('clears the name field error after the user edits the name', async () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-name"
                    numericClusterId={1}
                    clusterName=""
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

            await waitFor(() => {
                expect(
                    screen.getByText('Name is required'),
                ).toBeInTheDocument();
            });

            fireEvent.change(
                screen.getByRole('textbox', { name: /^name/i }),
                { target: { value: 'Now Has A Name' } },
            );

            await waitFor(() => {
                expect(
                    screen.queryByText('Name is required'),
                ).not.toBeInTheDocument();
            });
        });

        it('updates the description field as the user types', () => {
            renderWithTheme(
                <ClusterConfigDialog
                    {...baseProps}
                    mode="edit"
                    clusterId="cluster-desc"
                    numericClusterId={1}
                    clusterName="Desc Cluster"
                    clusterDescription=""
                    replicationType="binary"
                    autoClusterKey={null}
                />,
            );

            const descField = screen.getByRole('textbox', {
                name: /description/i,
            });
            fireEvent.change(descField, {
                target: { value: 'Updated description' },
            });
            expect(descField).toHaveValue('Updated description');
        });
    });
});
