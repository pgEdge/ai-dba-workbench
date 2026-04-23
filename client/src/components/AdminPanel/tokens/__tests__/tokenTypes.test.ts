/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import {
    filterMcpPrivileges,
    filterAdminPermissions,
    ADMIN_PERMISSIONS,
    ALL_MCP_OPTION,
    ALL_ADMIN_OPTION,
    EXPIRY_OPTIONS,
} from '../tokenTypes';
import type { McpPrivilege } from '../tokenTypes';

describe('tokenTypes', () => {
    describe('constants', () => {
        it('exports EXPIRY_OPTIONS with correct values', () => {
            expect(EXPIRY_OPTIONS).toHaveLength(4);
            expect(EXPIRY_OPTIONS[0]).toEqual({ label: '30 days', value: '30d' });
            expect(EXPIRY_OPTIONS[1]).toEqual({ label: '90 days', value: '90d' });
            expect(EXPIRY_OPTIONS[2]).toEqual({ label: '1 year', value: '1y' });
            expect(EXPIRY_OPTIONS[3]).toEqual({ label: 'Never', value: 'never' });
        });

        it('exports ADMIN_PERMISSIONS with all expected permissions', () => {
            expect(ADMIN_PERMISSIONS).toHaveLength(9);
            const ids = ADMIN_PERMISSIONS.map((p) => p.id);
            expect(ids).toContain('manage_connections');
            expect(ids).toContain('manage_groups');
            expect(ids).toContain('manage_permissions');
            expect(ids).toContain('manage_users');
            expect(ids).toContain('manage_token_scopes');
            expect(ids).toContain('manage_blackouts');
            expect(ids).toContain('manage_probes');
            expect(ids).toContain('manage_alert_rules');
            expect(ids).toContain('manage_notification_channels');
        });

        it('exports ALL_MCP_OPTION with correct sentinel values', () => {
            expect(ALL_MCP_OPTION.id).toBe(-1);
            expect(ALL_MCP_OPTION.identifier).toBe('*');
            expect(ALL_MCP_OPTION._isAll).toBe(true);
        });

        it('exports ALL_ADMIN_OPTION with correct sentinel values', () => {
            expect(ALL_ADMIN_OPTION.id).toBe('*');
            expect(ALL_ADMIN_OPTION.label).toBe('All Admin Permissions');
            expect(ALL_ADMIN_OPTION._isAll).toBe(true);
        });
    });

    describe('filterMcpPrivileges', () => {
        const allPrivileges: McpPrivilege[] = [
            { id: 1, identifier: 'query_read' },
            { id: 2, identifier: 'query_write' },
            { id: 3, identifier: 'schema_read' },
        ];

        it('returns all privileges when allowedIdentifiers includes wildcard', () => {
            const result = filterMcpPrivileges(allPrivileges, ['*']);
            expect(result).toEqual(allPrivileges);
        });

        it('returns all privileges when wildcard is among other identifiers', () => {
            const result = filterMcpPrivileges(allPrivileges, ['query_read', '*']);
            expect(result).toEqual(allPrivileges);
        });

        it('filters to only allowed identifiers', () => {
            const result = filterMcpPrivileges(allPrivileges, ['query_read', 'schema_read']);
            expect(result).toHaveLength(2);
            expect(result[0].identifier).toBe('query_read');
            expect(result[1].identifier).toBe('schema_read');
        });

        it('returns empty array when no identifiers match', () => {
            const result = filterMcpPrivileges(allPrivileges, ['nonexistent']);
            expect(result).toHaveLength(0);
        });

        it('returns empty array when allowedIdentifiers is empty', () => {
            const result = filterMcpPrivileges(allPrivileges, []);
            expect(result).toHaveLength(0);
        });
    });

    describe('filterAdminPermissions', () => {
        it('returns all permissions when allowedPermissionIds includes wildcard', () => {
            const result = filterAdminPermissions(['*']);
            expect(result).toEqual(ADMIN_PERMISSIONS);
        });

        it('returns all permissions when wildcard is among other ids', () => {
            const result = filterAdminPermissions(['manage_users', '*']);
            expect(result).toEqual(ADMIN_PERMISSIONS);
        });

        it('filters to only allowed permission ids', () => {
            const result = filterAdminPermissions(['manage_users', 'manage_groups']);
            expect(result).toHaveLength(2);
            const ids = result.map((p) => p.id);
            expect(ids).toContain('manage_users');
            expect(ids).toContain('manage_groups');
        });

        it('returns empty array when no ids match', () => {
            const result = filterAdminPermissions(['nonexistent']);
            expect(result).toHaveLength(0);
        });

        it('returns empty array when allowedPermissionIds is empty', () => {
            const result = filterAdminPermissions([]);
            expect(result).toHaveLength(0);
        });
    });
});
