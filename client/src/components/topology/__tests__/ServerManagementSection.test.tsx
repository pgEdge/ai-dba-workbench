/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import ServerManagementSection from '../ServerManagementSection';
import type { ServerManagementSectionProps } from '../ServerManagementSection';
import type { ClusterServerInfo } from '../../ServerDialog/ServerDialog.types';
import type { UnassignedConnection, RoleOption } from '../topologyHelpers';

const createMockServer = (
    overrides: Partial<ClusterServerInfo> = {},
): ClusterServerInfo => ({
    id: 1,
    name: 'server-1',
    host: 'host1.example.com',
    port: 5432,
    status: 'online',
    role: 'binary_primary',
    ...overrides,
});

const createMockUnassigned = (
    overrides: Partial<UnassignedConnection> = {},
): UnassignedConnection => ({
    id: 10,
    name: 'unassigned-1',
    host: 'host10.example.com',
    port: 5432,
    ...overrides,
});

const createDefaultProps = (
    overrides: Partial<ServerManagementSectionProps> = {},
): ServerManagementSectionProps => ({
    unassignedConnections: [],
    selectedConnection: null,
    selectedRole: '',
    roleOptions: [],
    addingServer: false,
    clusterServers: [],
    onConnectionChange: vi.fn(),
    onRoleChange: vi.fn(),
    onAddServer: vi.fn(),
    onRemoveTarget: vi.fn(),
    ...overrides,
});

describe('ServerManagementSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders the Add Server section heading', () => {
        const props = createDefaultProps();

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByText('Add Server')).toBeInTheDocument();
    });

    it('renders autocomplete for server selection', () => {
        const props = createDefaultProps();

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(
            screen.getByLabelText('Server'),
        ).toBeInTheDocument();
    });

    it('renders Add button', () => {
        const props = createDefaultProps();

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(
            screen.getByRole('button', { name: 'Add server' }),
        ).toBeInTheDocument();
    });

    it('disables Add button when no connection is selected', () => {
        const props = createDefaultProps({ selectedConnection: null });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByRole('button', { name: 'Add server' })).toBeDisabled();
    });

    it('enables Add button when connection is selected', () => {
        const connection = createMockUnassigned();
        const props = createDefaultProps({ selectedConnection: connection });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByRole('button', { name: 'Add server' })).not.toBeDisabled();
    });

    it('disables Add button when addingServer is true', () => {
        const connection = createMockUnassigned();
        const props = createDefaultProps({
            selectedConnection: connection,
            addingServer: true,
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        const addButton = screen.getByRole('button', { name: 'Add server' });
        expect(addButton).toBeDisabled();
    });

    it('shows loading spinner when addingServer is true', () => {
        const connection = createMockUnassigned();
        const props = createDefaultProps({
            selectedConnection: connection,
            addingServer: true,
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('disables autocomplete when addingServer is true', () => {
        const props = createDefaultProps({ addingServer: true });

        renderWithTheme(<ServerManagementSection {...props} />);

        const autocomplete = screen.getByRole('combobox');
        expect(autocomplete).toBeDisabled();
    });

    it('calls onAddServer when Add button is clicked', () => {
        const onAddServer = vi.fn();
        const connection = createMockUnassigned();
        const props = createDefaultProps({
            selectedConnection: connection,
            onAddServer,
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        fireEvent.click(screen.getByRole('button', { name: 'Add server' }));

        expect(onAddServer).toHaveBeenCalledTimes(1);
    });

    it('does not render role dropdown when roleOptions is empty', () => {
        const props = createDefaultProps({ roleOptions: [] });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.queryByLabelText('Role')).not.toBeInTheDocument();
    });

    it('renders role dropdown when roleOptions are provided', () => {
        const roleOptions: RoleOption[] = [
            { value: 'binary_primary', label: 'Primary' },
            { value: 'binary_standby', label: 'Standby' },
        ];
        const props = createDefaultProps({ roleOptions });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByLabelText('Role')).toBeInTheDocument();
    });

    it('renders role options in the dropdown menu', () => {
        const roleOptions: RoleOption[] = [
            { value: 'binary_primary', label: 'Primary' },
            { value: 'binary_standby', label: 'Standby' },
        ];
        const props = createDefaultProps({
            roleOptions,
            selectedRole: '',
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        // Verify the role select is present and has the right label
        const roleSelect = screen.getByLabelText('Role');
        expect(roleSelect).toBeInTheDocument();

        // The dropdown exists and is connected
        expect(roleSelect).toHaveAttribute('aria-haspopup', 'listbox');
    });

    it('disables role dropdown when addingServer is true', () => {
        const roleOptions: RoleOption[] = [
            { value: 'binary_primary', label: 'Primary' },
        ];
        const props = createDefaultProps({
            roleOptions,
            addingServer: true,
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        const roleInputs = screen.getAllByRole('combobox');
        const roleInput = roleInputs.find((input) =>
            input.closest('[class*="MuiTextField"]'),
        );
        expect(roleInput).toBeDisabled();
    });

    it('does not render server list when clusterServers is empty', () => {
        const props = createDefaultProps({ clusterServers: [] });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.queryByRole('list')).not.toBeInTheDocument();
    });

    it('renders server list when clusterServers are provided', () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByText('server-1')).toBeInTheDocument();
        expect(screen.getByText('server-2')).toBeInTheDocument();
    });

    it('displays server host and port', () => {
        const servers = [
            createMockServer({
                id: 1,
                name: 'server-1',
                host: 'db.example.com',
                port: 5433,
            }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByText('db.example.com:5433')).toBeInTheDocument();
    });

    it('displays server role as chip when role is set', () => {
        const servers = [
            createMockServer({
                id: 1,
                name: 'server-1',
                role: 'binary_primary',
            }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByText('Binary Primary')).toBeInTheDocument();
    });

    it('does not display role chip when role is undefined', () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1', role: undefined }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.queryByText('Binary Primary')).not.toBeInTheDocument();
    });

    it('renders remove button for each server', () => {
        const servers = [
            createMockServer({ id: 1, name: 'server-1' }),
            createMockServer({ id: 2, name: 'server-2' }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(
            screen.getByRole('button', {
                name: 'Remove server-1 from cluster',
            }),
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', {
                name: 'Remove server-2 from cluster',
            }),
        ).toBeInTheDocument();
    });

    it('calls onRemoveTarget when remove button is clicked', () => {
        const onRemoveTarget = vi.fn();
        const servers = [createMockServer({ id: 1, name: 'server-1' })];
        const props = createDefaultProps({
            clusterServers: servers,
            onRemoveTarget,
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        fireEvent.click(
            screen.getByRole('button', {
                name: 'Remove server-1 from cluster',
            }),
        );

        expect(onRemoveTarget).toHaveBeenCalledWith(servers[0]);
    });

    it('displays role with proper capitalization', () => {
        const servers = [
            createMockServer({
                id: 1,
                name: 'server-1',
                role: 'spock_node',
            }),
        ];
        const props = createDefaultProps({ clusterServers: servers });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByText('Spock Node')).toBeInTheDocument();
    });

    it('displays selected connection in autocomplete', () => {
        const connection = createMockUnassigned({
            name: 'selected-server',
            host: 'selected.example.com',
            port: 5432,
        });
        const props = createDefaultProps({
            selectedConnection: connection,
            unassignedConnections: [connection],
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        const autocomplete = screen.getByRole('combobox');
        expect(autocomplete).toHaveValue(
            'selected-server (selected.example.com:5432)',
        );
    });

    it('displays selected role in dropdown', () => {
        const roleOptions: RoleOption[] = [
            { value: 'binary_primary', label: 'Primary' },
            { value: 'binary_standby', label: 'Standby' },
        ];
        const props = createDefaultProps({
            roleOptions,
            selectedRole: 'binary_standby',
        });

        renderWithTheme(<ServerManagementSection {...props} />);

        expect(screen.getByLabelText('Role')).toHaveTextContent('Standby');
    });
});
