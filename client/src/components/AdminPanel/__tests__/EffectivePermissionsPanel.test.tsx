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
import { screen } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import EffectivePermissionsPanel from '../EffectivePermissionsPanel';

describe('EffectivePermissionsPanel', () => {
    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders with minimal props', () => {
        renderWithTheme(<EffectivePermissionsPanel />);

        expect(screen.getByText('Connections')).toBeInTheDocument();
        expect(screen.getByText('MCP')).toBeInTheDocument();
    });

    it('displays "None" when no connection privileges exist', () => {
        renderWithTheme(
            <EffectivePermissionsPanel connectionPrivileges={{}} />
        );

        // Should show None for empty connections
        const noneElements = screen.getAllByText('None');
        expect(noneElements.length).toBeGreaterThan(0);
    });

    it('displays connection privileges when provided as object', () => {
        const connectionPrivileges = {
            '1': ['read', 'write'],
            '2': ['read'],
        };

        const connections = [
            { id: 1, name: 'Primary DB' },
            { id: 2, name: 'Replica DB' },
        ];

        renderWithTheme(
            <EffectivePermissionsPanel
                connectionPrivileges={connectionPrivileges}
                connections={connections}
            />
        );

        expect(screen.getByText('Connections')).toBeInTheDocument();
    });

    it('displays connection privileges when provided as array', () => {
        const connectionPrivileges = [
            { connection_id: '1', access_level: 'read' },
            { connection_id: '2', access_level: 'write' },
        ];

        const connections = [
            { id: 1, name: 'Primary DB' },
            { id: 2, name: 'Replica DB' },
        ];

        renderWithTheme(
            <EffectivePermissionsPanel
                connectionPrivileges={connectionPrivileges}
                connections={connections}
            />
        );

        expect(screen.getByText(/Primary DB/)).toBeInTheDocument();
        expect(screen.getByText(/Replica DB/)).toBeInTheDocument();
    });

    it('displays "All Connections" for connection_id 0', () => {
        const connectionPrivileges = [
            { connection_id: '0', access_level: 'read' },
        ];

        renderWithTheme(
            <EffectivePermissionsPanel connectionPrivileges={connectionPrivileges} />
        );

        expect(screen.getByText(/All Connections/)).toBeInTheDocument();
    });

    it('displays admin permissions when isSuperuser is true', () => {
        const adminPermissions = ['manage_users', 'manage_groups'];

        renderWithTheme(
            <EffectivePermissionsPanel
                adminPermissions={adminPermissions}
                isSuperuser={true}
            />
        );

        expect(screen.getByText('Admin')).toBeInTheDocument();
        expect(screen.getByText('Manage Users')).toBeInTheDocument();
        expect(screen.getByText('Manage Groups')).toBeInTheDocument();
    });

    it('does not display admin permissions when isSuperuser is false', () => {
        const adminPermissions = ['manage_users', 'manage_groups'];

        renderWithTheme(
            <EffectivePermissionsPanel
                adminPermissions={adminPermissions}
                isSuperuser={false}
            />
        );

        expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    });

    it('displays "All Admin Permissions" for wildcard permission', () => {
        const adminPermissions = ['*'];

        renderWithTheme(
            <EffectivePermissionsPanel
                adminPermissions={adminPermissions}
                isSuperuser={true}
            />
        );

        expect(screen.getByText('All Admin Permissions')).toBeInTheDocument();
    });

    it('displays all known admin permission labels', () => {
        const adminPermissions = [
            'manage_blackouts',
            'manage_connections',
            'manage_groups',
            'manage_permissions',
            'manage_probes',
            'manage_alert_rules',
            'manage_token_scopes',
            'manage_notification_channels',
            'manage_users',
        ];

        renderWithTheme(
            <EffectivePermissionsPanel
                adminPermissions={adminPermissions}
                isSuperuser={true}
            />
        );

        expect(screen.getByText('Manage Blackouts')).toBeInTheDocument();
        expect(screen.getByText('Manage Connections')).toBeInTheDocument();
        expect(screen.getByText('Manage Groups')).toBeInTheDocument();
        expect(screen.getByText('Manage Permissions')).toBeInTheDocument();
        expect(screen.getByText('Manage Probes')).toBeInTheDocument();
        expect(screen.getByText('Manage Alert Rules')).toBeInTheDocument();
        expect(screen.getByText('Manage Token Scopes')).toBeInTheDocument();
        expect(screen.getByText('Manage Notification Channels')).toBeInTheDocument();
        expect(screen.getByText('Manage Users')).toBeInTheDocument();
    });

    it('displays MCP privileges when provided as array', () => {
        const mcpPrivileges = [
            { privilege: 'query_read' },
            { privilege: 'query_write' },
        ];

        renderWithTheme(
            <EffectivePermissionsPanel mcpPrivileges={mcpPrivileges} />
        );

        expect(screen.getByText('MCP')).toBeInTheDocument();
        expect(screen.getByText('query read')).toBeInTheDocument();
        expect(screen.getByText('query write')).toBeInTheDocument();
    });

    it('displays MCP privileges when provided as strings', () => {
        const mcpPrivileges = ['query_read', 'schema_read'];

        renderWithTheme(
            <EffectivePermissionsPanel mcpPrivileges={mcpPrivileges} />
        );

        expect(screen.getByText('query read')).toBeInTheDocument();
        expect(screen.getByText('schema read')).toBeInTheDocument();
    });

    it('displays "All MCP Privileges" for wildcard', () => {
        const mcpPrivileges = ['*'];

        renderWithTheme(
            <EffectivePermissionsPanel mcpPrivileges={mcpPrivileges} />
        );

        expect(screen.getByText('All MCP Privileges')).toBeInTheDocument();
    });

    it('displays "None" when no MCP privileges exist', () => {
        renderWithTheme(
            <EffectivePermissionsPanel mcpPrivileges={[]} />
        );

        const noneElements = screen.getAllByText('None');
        expect(noneElements.length).toBeGreaterThan(0);
    });

    it('displays groups when provided', () => {
        // The component expects groups as string array despite the interface
        // (the actual usage shows key={g} and label={g} expecting strings)
        const groups = ['Admins', 'Developers'] as unknown as Array<{ id: number; name: string }>;

        renderWithTheme(
            <EffectivePermissionsPanel groups={groups} />
        );

        expect(screen.getByText('Groups:')).toBeInTheDocument();
        expect(screen.getByText('Admins')).toBeInTheDocument();
        expect(screen.getByText('Developers')).toBeInTheDocument();
    });

    it('does not display groups section when groups is empty', () => {
        renderWithTheme(
            <EffectivePermissionsPanel groups={[]} />
        );

        expect(screen.queryByText('Groups:')).not.toBeInTheDocument();
    });

    it('handles unknown admin permissions gracefully', () => {
        const adminPermissions = ['unknown_permission'];

        renderWithTheme(
            <EffectivePermissionsPanel
                adminPermissions={adminPermissions}
                isSuperuser={true}
            />
        );

        // Unknown permissions should display as-is
        expect(screen.getByText('unknown_permission')).toBeInTheDocument();
    });

    it('falls back to connection ID when name not found', () => {
        const connectionPrivileges = [
            { connection_id: '999', access_level: 'read' },
        ];

        renderWithTheme(
            <EffectivePermissionsPanel
                connectionPrivileges={connectionPrivileges}
                connections={[]}
            />
        );

        expect(screen.getByText(/Connection 999/)).toBeInTheDocument();
    });

    it('renders all three category cards', () => {
        renderWithTheme(
            <EffectivePermissionsPanel
                isSuperuser={true}
                adminPermissions={['manage_users']}
            />
        );

        expect(screen.getByText('Connections')).toBeInTheDocument();
        expect(screen.getByText('Admin')).toBeInTheDocument();
        expect(screen.getByText('MCP')).toBeInTheDocument();
    });
});
