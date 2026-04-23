/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Types and constants for the token management components.
 */

// ---------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------

/** A single admin permission entry. */
export interface AdminPermissionEntry {
    id: string;
    label: string;
}

/** MCP privilege from the API. */
export interface McpPrivilege {
    id: number;
    identifier: string;
}

/** MCP privilege option including the "All" sentinel. */
export interface McpPrivilegeOption extends McpPrivilege {
    _isAll?: boolean;
}

/** Admin permission option including the "All" sentinel. */
export interface AdminPermissionOption {
    id: string;
    label: string;
    _isAll?: boolean;
}

/** A connection with an associated access level in a token scope. */
export interface ScopedConnection {
    id: number;
    name: string;
    access_level: string;
}

/** A connection from the API. */
export interface Connection {
    id: number;
    name: string;
}

/** A connection scope entry within a token scope. */
export interface TokenScopeConnection {
    connection_id: number;
    access_level: string;
}

/** The scope definition for a token. */
export interface TokenScope {
    scoped: boolean;
    connections?: TokenScopeConnection[];
    mcp_privileges?: number[];
    admin_permissions?: string[];
}

/** A token from the API. */
export interface Token {
    id: number;
    name?: string;
    token_prefix?: string;
    username?: string;
    user_id?: number;
    is_service_account?: boolean;
    is_superuser?: boolean;
    expires_at?: string | null;
    scope?: TokenScope;
}

/** A user from the API. */
export interface User {
    id: number;
    username: string;
}

/** Response from the create token endpoint. */
export interface CreateTokenResponse {
    id: number;
    token: string;
}

/** Response from the user privileges endpoint. */
export interface UserPrivilegesResponse {
    is_superuser: boolean;
    connection_privileges?: Record<string, string>;
    mcp_privileges?: string[];
    admin_permissions?: string[];
}

// ---------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------

/** Token expiry duration options. */
export const EXPIRY_OPTIONS = [
    { label: '30 days', value: '30d' },
    { label: '90 days', value: '90d' },
    { label: '1 year', value: '1y' },
    { label: 'Never', value: 'never' },
];

/** All available admin permissions. */
export const ADMIN_PERMISSIONS: AdminPermissionEntry[] = [
    { id: 'manage_connections', label: 'Manage Connections' },
    { id: 'manage_groups', label: 'Manage Groups' },
    { id: 'manage_permissions', label: 'Manage Permissions' },
    { id: 'manage_users', label: 'Manage Users' },
    { id: 'manage_token_scopes', label: 'Manage Token Scopes' },
    { id: 'manage_blackouts', label: 'Manage Blackouts' },
    { id: 'manage_probes', label: 'Manage Probes' },
    { id: 'manage_alert_rules', label: 'Manage Alert Rules' },
    { id: 'manage_notification_channels', label: 'Manage Notification Channels' },
];

/** Sentinel option for selecting all MCP privileges. */
export const ALL_MCP_OPTION: McpPrivilegeOption = {
    id: -1,
    identifier: '*',
    _isAll: true,
};

/** Sentinel option for selecting all admin permissions. */
export const ALL_ADMIN_OPTION: AdminPermissionOption = {
    id: '*',
    label: 'All Admin Permissions',
    _isAll: true,
};

// ---------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------

/**
 * Filter MCP privileges to only those the user is allowed to grant.
 * If allowedIdentifiers includes '*', all privileges are allowed.
 */
export const filterMcpPrivileges = (
    allPrivileges: McpPrivilege[],
    allowedIdentifiers: string[],
): McpPrivilege[] => {
    if (allowedIdentifiers.includes('*')) {
        return allPrivileges;
    }
    return allPrivileges.filter((p) => allowedIdentifiers.includes(p.identifier));
};

/**
 * Filter admin permissions to only those the user is allowed to grant.
 * If allowedPermissionIds includes '*', all permissions are allowed.
 */
export const filterAdminPermissions = (
    allowedPermissionIds: string[],
): AdminPermissionEntry[] => {
    if (allowedPermissionIds.includes('*')) {
        return ADMIN_PERMISSIONS;
    }
    return ADMIN_PERMISSIONS.filter((p) => allowedPermissionIds.includes(p.id));
};
