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

/**
 * The id the server stores, and returns in a token's scope, for the
 * all-MCP-privileges wildcard (privilege_identifier_id = 0).
 */
export const MCP_WILDCARD_ID = 0;

/**
 * Reports whether an MCP privilege id in a token's scope is the
 * wildcard, whether it came from the server (0) or from the dialog's
 * own sentinel option (-1).
 */
export const isMcpWildcardId = (id: number): boolean =>
    id === MCP_WILDCARD_ID || id === ALL_MCP_OPTION.id;

/**
 * The connection id the server stores, and returns in a token's scope,
 * for the "all connections" entry.
 */
export const ALL_CONNECTIONS_ID = 0;

/** The label for the "all connections" connection scope entry. */
export const ALL_CONNECTIONS_LABEL = "All the owner's connections";

/** Option for scoping a token to every connection its owner can reach. */
export const ALL_CONNECTIONS_OPTION: Connection = {
    id: ALL_CONNECTIONS_ID,
    name: ALL_CONNECTIONS_LABEL,
};

/** Sentinel option for selecting all admin permissions. */
export const ALL_ADMIN_OPTION: AdminPermissionOption = {
    id: '*',
    label: "All the owner's admin permissions",
    _isAll: true,
};

// A scope category with no entries places no restriction on the token,
// which the server enforces the same way; these explain that under an
// empty category so it is not read as "restricted to nothing".

/** Helper text shown when a token's connection scope is empty. */
export const NO_CONNECTION_RESTRICTION_TEXT =
    'No restriction: the token may use every connection its owner can reach';

/** Helper text shown when a token's MCP privilege scope is empty. */
export const NO_MCP_RESTRICTION_TEXT =
    'No restriction: the token may use every MCP privilege its owner holds';

/** Helper text shown when a token's admin permission scope is empty. */
export const NO_ADMIN_RESTRICTION_TEXT =
    'No restriction: the token may use every admin permission its owner holds';

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

// ---------------------------------------------------------------------
// Connection scope rules
// ---------------------------------------------------------------------

// The server refuses a connection scope that names a connection twice,
// or that mixes the "all connections" entry with entries for particular
// connections (ErrInvalidConnectionScope in
// server/src/internal/auth/token_scope.go). The editor keeps to the
// same rules, so that it never builds a scope the server would refuse.

/**
 * Adds an entry to a connection scope without breaking the server's
 * rules: an entry already present is not added again, the "all
 * connections" entry replaces every other entry, and an entry for a
 * particular connection replaces the "all connections" entry.
 */
export const addScopedConnection = (
    scoped: ScopedConnection[],
    entry: ScopedConnection,
): ScopedConnection[] => {
    if (scoped.some((c) => c.id === entry.id)) {
        return scoped;
    }
    if (entry.id === ALL_CONNECTIONS_ID) {
        return [entry];
    }
    return [...scoped.filter((c) => c.id !== ALL_CONNECTIONS_ID), entry];
};

/**
 * Explains why a connection scope cannot be saved as it stands, or
 * returns null when the server would accept it. A scope stored before
 * the server enforced these rules may still break them.
 */
export const connectionScopeProblem = (
    scoped: ScopedConnection[],
): string | null => {
    const seen = new Set<number>();
    for (const c of scoped) {
        if (seen.has(c.id)) {
            return `The connection scope names "${c.name}" more than ` +
                'once. Remove the extra entries before saving.';
        }
        seen.add(c.id);
    }
    if (seen.has(ALL_CONNECTIONS_ID) && scoped.length > 1) {
        return `The connection scope combines "${ALL_CONNECTIONS_LABEL}" ` +
            'with entries for particular connections, which cannot be ' +
            `saved. Remove either "${ALL_CONNECTIONS_LABEL}" or the ` +
            'particular connections before saving.';
    }
    return null;
};
