/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useEffect, useCallback, useRef } from 'react';
import { Box, Typography, Button, CircularProgress, Alert } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Add as AddIcon } from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { apiGet, apiPost, apiPut, apiDelete } from '../../utils/apiClient';
import { copyToClipboard } from '../../utils/clipboard';
import { loadingContainerSx, pageHeadingSx, getContainedButtonSx } from './styles';
import { extractErrorMessage } from './_shared';
import {
    TokensTable,
    CreateTokenDialog,
    EditTokenDialog,
    CreatedTokenDialog,
    ApiUsageExample,
    ADMIN_PERMISSIONS,
    ALL_MCP_OPTION,
    ALL_ADMIN_OPTION,
    isMcpWildcardId,
    filterMcpPrivileges,
    filterAdminPermissions,
} from './tokens';
import type {
    Token,
    User,
    Connection,
    McpPrivilege,
    McpPrivilegeOption,
    AdminPermissionEntry,
    AdminPermissionOption,
    ScopedConnection,
    TokenScopeConnection,
    CreateTokenResponse,
    UserPrivilegesResponse,
} from './tokens';

/** The body of PUT /api/v1/rbac/tokens/{id}/scope. */
interface TokenScopeBody {
    connections?: { connection_id: number; access_level: string }[];
    mcp_privileges?: string[];
    admin_permissions?: (string | number)[];
}

/**
 * Builds the scope PUT body from the selected categories, leaving out
 * every empty one. A category with no entries places no restriction on
 * the token, and the server refuses an empty array rather than guess
 * whether it means "no restriction" or "restricted to nothing", so an
 * empty category is simply omitted.
 */
const buildScopeBody = (
    connections: ScopedConnection[],
    mcpPrivileges: McpPrivilegeOption[],
    adminPermissions: AdminPermissionOption[],
): TokenScopeBody => {
    const body: TokenScopeBody = {};
    if (connections.length > 0) {
        body.connections = connections.map((c) => ({
            connection_id: c.id,
            access_level: c.access_level,
        }));
    }
    if (mcpPrivileges.length > 0) {
        body.mcp_privileges = mcpPrivileges.some((p) => p._isAll)
            ? ['*']
            : mcpPrivileges.map((p) => p.identifier);
    }
    if (adminPermissions.length > 0) {
        body.admin_permissions = adminPermissions.some((p) => p._isAll)
            ? ['*']
            : adminPermissions.map((p) => p.id);
    }
    return body;
};

/**
 * Explains why a scope edit cannot be saved as it stands: a category the
 * token was restricted in has been emptied whilst others stay
 * restricted. Omitting that category would leave its old restriction in
 * place, and the API has no call that lifts one category alone.
 */
const liftOneCategoryError = (categories: string[]): string =>
    `The ${categories.join(' and ')} restriction cannot be lifted on its own ` +
    'whilst other categories stay restricted. For MCP privileges or admin ' +
    `permissions, choose "All the owner's MCP privileges" or "All the ` +
    `owner's admin permissions" instead. The connections restriction can ` +
    'be lifted only together with every other one, so to keep the others, ' +
    'create a new token with the scope it needs and then delete this one.';

/**
 * Explains why a scope edit cannot be saved at all: the stored scope
 * holds entries the dialog could not show, most often because the MCP
 * privilege list failed to load, so saving what the dialog holds would
 * drop them and widen the token.
 */
const unshownScopeError = (categories: string[]): string =>
    `This token's stored ${categories.join(' and ')} could not all be ` +
    'shown, so saving now could widen its scope. Reload the page and try ' +
    'again.';

const AdminTokenScopes: React.FC = () => {
    const theme = useTheme();
    const [tokens, setTokens] = useState<Token[]>([]);
    const [connections, setConnections] = useState<Connection[]>([]);
    const [mcpPrivileges, setMcpPrivileges] = useState<McpPrivilege[]>([]);
    const [users, setUsers] = useState<User[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedToken, setExpandedToken] = useState<number | null>(null);

    // Create token dialog
    const [createOpen, setCreateOpen] = useState(false);
    const [createOwner, setCreateOwner] = useState<User | null>(null);
    const [createAnnotation, setCreateAnnotation] = useState('');
    const [createExpiry, setCreateExpiry] = useState('90d');
    const [createLoading, setCreateLoading] = useState(false);
    const [createError, setCreateError] = useState<string | null>(null);
    const [createConnections, setCreateConnections] = useState<ScopedConnection[]>([]);
    const [createMcpPrivileges, setCreateMcpPrivileges] = useState<McpPrivilegeOption[]>([]);
    const [createAdminPermissions, setCreateAdminPermissions] = useState<AdminPermissionOption[]>([]);

    // Token created success dialog
    const [createdToken, setCreatedToken] = useState<string | null>(null);
    const [createdDialogOpen, setCreatedDialogOpen] = useState(false);
    const [tokenCopied, setTokenCopied] = useState(false);
    const copyResetTimerRef = useRef<number | null>(null);
    const createdDialogContentRef = useRef<HTMLDivElement>(null);

    // Create dialog - owner privilege filtering
    const [ownerConnections, setOwnerConnections] = useState<Connection[]>([]);
    const [ownerConnectionLevels, setOwnerConnectionLevels] = useState<Record<number, string>>({});
    const [ownerMcpPrivileges, setOwnerMcpPrivileges] = useState<McpPrivilege[]>([]);
    const [ownerAdminPermissions, setOwnerAdminPermissions] = useState<AdminPermissionEntry[]>([]);
    const [ownerIsSuperuser, setOwnerIsSuperuser] = useState(false);

    // Edit scope dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editToken, setEditToken] = useState<Token | null>(null);
    const [editConnections, setEditConnections] = useState<ScopedConnection[]>([]);
    const [editMcpPrivileges, setEditMcpPrivileges] = useState<McpPrivilegeOption[]>([]);
    const [editAdminPermissions, setEditAdminPermissions] = useState<AdminPermissionOption[]>([]);
    const [editLoading, setEditLoading] = useState(false);
    const [editError, setEditError] = useState<string | null>(null);
    const [editUnshownError, setEditUnshownError] = useState<string | null>(null);
    const [editAvailableConnections, setEditAvailableConnections] = useState<Connection[]>([]);
    const [editOwnerConnectionLevels, setEditOwnerConnectionLevels] = useState<Record<number, string>>({});
    const [editOwnerIsSuperuser, setEditOwnerIsSuperuser] = useState(false);
    const [editAvailableMcpPrivileges, setEditAvailableMcpPrivileges] = useState<McpPrivilege[]>([]);
    const [editAvailableAdminPermissions, setEditAvailableAdminPermissions] = useState<AdminPermissionEntry[]>([]);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteToken, setDeleteToken] = useState<Token | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    const getMcpPrivilegeName = useCallback((id: number) => {
        if (isMcpWildcardId(id)) {
            return "All the owner's MCP privileges";
        }
        const priv = mcpPrivileges.find((p) => p.id === id);
        return priv ? priv.identifier : `Privilege ${id}`;
    }, [mcpPrivileges]);

    const fetchData = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const [tokData, connResult, mcpResult, usersResult] = await Promise.all([
                apiGet<{ tokens?: Token[] }>('/api/v1/rbac/tokens'),
                apiGet<{ connections?: Connection[] }>('/api/v1/connections').catch(() => null),
                apiGet<McpPrivilege[]>('/api/v1/rbac/privileges/mcp').catch(() => null),
                apiGet<{ users?: User[] }>('/api/v1/rbac/users').catch(() => null),
            ]);
            setTokens(tokData.tokens ?? []);
            if (connResult) {
                setConnections((connResult.connections ?? (connResult as unknown as Connection[])) || []);
            }
            if (mcpResult) {
                setMcpPrivileges(mcpResult || []);
            }
            if (usersResult) {
                setUsers(usersResult.users ?? []);
            }
        } catch (err: unknown) {
            setError(extractErrorMessage(err));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void fetchData();
    }, [fetchData]);

    const handleTokenRowClick = (token: Token) => {
        setExpandedToken(expandedToken === token.id ? null : token.id);
    };

    // Handle owner change in create dialog - fetch privileges
    const handleOwnerChange = async (owner: User | null) => {
        setCreateOwner(owner);
        setCreateConnections([]);
        setCreateMcpPrivileges([]);
        setCreateAdminPermissions([]);

        if (!owner) {
            setOwnerConnections([]);
            setOwnerConnectionLevels({});
            setOwnerMcpPrivileges([]);
            setOwnerAdminPermissions([]);
            setOwnerIsSuperuser(false);
            return;
        }

        try {
            const data = await apiGet<UserPrivilegesResponse>(
                `/api/v1/rbac/users/${owner.id}/privileges`
            );
            if (data.is_superuser) {
                setOwnerIsSuperuser(true);
                setOwnerConnections(connections);
                const levels: Record<number, string> = {};
                connections.forEach((c) => {
                    levels[c.id] = 'read_write';
                });
                setOwnerConnectionLevels(levels);
                setOwnerMcpPrivileges(mcpPrivileges);
                setOwnerAdminPermissions(ADMIN_PERMISSIONS);
            } else {
                setOwnerIsSuperuser(false);
                const connPrivs = data.connection_privileges ?? {};
                const allowedConnIds = Object.keys(connPrivs).map(Number);
                setOwnerConnectionLevels(
                    Object.fromEntries(Object.entries(connPrivs).map(([k, v]) => [Number(k), v]))
                );
                if (allowedConnIds.includes(0)) {
                    setOwnerConnections(connections);
                } else {
                    setOwnerConnections(connections.filter((c) => allowedConnIds.includes(c.id)));
                }
                setOwnerMcpPrivileges(filterMcpPrivileges(mcpPrivileges, data.mcp_privileges ?? []));
                setOwnerAdminPermissions(filterAdminPermissions(data.admin_permissions ?? []));
            }
        } catch {
            setOwnerConnections(connections);
            setOwnerMcpPrivileges(mcpPrivileges);
            setOwnerAdminPermissions(ADMIN_PERMISSIONS);
        }
    };

    // Create token
    const handleCreateToken = async () => {
        if (!createOwner || !createAnnotation.trim()) {
            return;
        }
        try {
            setCreateLoading(true);
            setCreateError(null);
            const body = {
                owner_username: createOwner.username,
                annotation: createAnnotation.trim(),
                expires_in: createExpiry,
            };
            const data = await apiPost<CreateTokenResponse>('/api/v1/rbac/tokens', body);

            const scopeBody = buildScopeBody(
                createConnections,
                createMcpPrivileges,
                createAdminPermissions,
            );
            if (Object.keys(scopeBody).length > 0) {
                await apiPut(`/api/v1/rbac/tokens/${data.id}/scope`, scopeBody);
            }

            setCreateOpen(false);
            setCreateOwner(null);
            setCreateAnnotation('');
            setCreateExpiry('90d');
            setCreateConnections([]);
            setCreateMcpPrivileges([]);
            setCreateAdminPermissions([]);
            setOwnerAdminPermissions([]);
            setCreatedToken(data.token);
            setCreatedDialogOpen(true);
            fetchData();
        } catch (err: unknown) {
            setCreateError(extractErrorMessage(err));
        } finally {
            setCreateLoading(false);
        }
    };

    // Open create dialog
    const handleOpenCreate = () => {
        setCreateError(null);
        setCreateOwner(null);
        setCreateAnnotation('');
        setCreateExpiry('90d');
        setCreateConnections([]);
        setCreateMcpPrivileges([]);
        setCreateAdminPermissions([]);
        setOwnerConnections([]);
        setOwnerConnectionLevels({});
        setOwnerMcpPrivileges([]);
        setOwnerAdminPermissions([]);
        setOwnerIsSuperuser(false);
        setCreateOpen(true);
    };

    // Edit scope
    const handleOpenEdit = async (token: Token) => {
        setEditToken(token);
        const scopeConns = token.scope?.connections ?? [];
        setEditConnections(scopeConns.map((sc: TokenScopeConnection) => {
            const conn = connections.find((c) => c.id === sc.connection_id);
            return {
                id: sc.connection_id,
                name: conn ? conn.name : `Connection ${sc.connection_id}`,
                access_level: sc.access_level || 'read_write',
            };
        }));

        const scopeMcpIds = token.scope?.mcp_privileges ?? [];
        const mcpWildcard = scopeMcpIds.some(isMcpWildcardId);
        if (mcpWildcard) {
            setEditMcpPrivileges([ALL_MCP_OPTION]);
        } else {
            setEditMcpPrivileges(mcpPrivileges.filter((p) => scopeMcpIds.includes(p.id)));
        }

        const scopeAdminPerms = token.scope?.admin_permissions ?? [];
        if (scopeAdminPerms.includes('*')) {
            setEditAdminPermissions([ALL_ADMIN_OPTION]);
        } else {
            setEditAdminPermissions(ADMIN_PERMISSIONS.filter((p) => scopeAdminPerms.includes(p.id)));
        }

        // A stored entry the dialog cannot show would be dropped by a
        // save, lifting a restriction nobody chose to lift.
        const unshown: string[] = [];
        if (!mcpWildcard && scopeMcpIds.some(
            (id: number) => !mcpPrivileges.some((p) => p.id === id),
        )) {
            unshown.push('MCP privileges');
        }
        if (!scopeAdminPerms.includes('*') && scopeAdminPerms.some(
            (perm: string) => !ADMIN_PERMISSIONS.some((p) => p.id === perm),
        )) {
            unshown.push('admin permissions');
        }
        const unshownError = unshown.length > 0 ? unshownScopeError(unshown) : null;
        setEditUnshownError(unshownError);

        setEditError(unshownError);
        setEditOpen(true);

        if (token.user_id) {
            try {
                const data = await apiGet<UserPrivilegesResponse>(
                    `/api/v1/rbac/users/${token.user_id}/privileges`
                );
                if (data.is_superuser) {
                    setEditOwnerIsSuperuser(true);
                    setEditAvailableConnections(connections);
                    const levels: Record<number, string> = {};
                    connections.forEach((c) => {
                        levels[c.id] = 'read_write';
                    });
                    setEditOwnerConnectionLevels(levels);
                    setEditAvailableMcpPrivileges(mcpPrivileges);
                    setEditAvailableAdminPermissions(ADMIN_PERMISSIONS);
                } else {
                    setEditOwnerIsSuperuser(false);
                    const connPrivs = data.connection_privileges ?? {};
                    const allowedConnIds = Object.keys(connPrivs).map(Number);
                    setEditOwnerConnectionLevels(
                        Object.fromEntries(Object.entries(connPrivs).map(([k, v]) => [Number(k), v]))
                    );
                    if (allowedConnIds.includes(0)) {
                        setEditAvailableConnections(connections);
                    } else {
                        setEditAvailableConnections(connections.filter((c) => allowedConnIds.includes(c.id)));
                    }
                    setEditAvailableMcpPrivileges(filterMcpPrivileges(mcpPrivileges, data.mcp_privileges ?? []));
                    setEditAvailableAdminPermissions(filterAdminPermissions(data.admin_permissions ?? []));
                }
            } catch {
                setEditOwnerIsSuperuser(false);
                setEditAvailableConnections(connections);
                setEditOwnerConnectionLevels({});
                setEditAvailableMcpPrivileges(mcpPrivileges);
                setEditAvailableAdminPermissions(ADMIN_PERMISSIONS);
            }
        } else {
            setEditOwnerIsSuperuser(false);
            setEditAvailableConnections(connections);
            setEditOwnerConnectionLevels({});
            setEditAvailableMcpPrivileges(mcpPrivileges);
            setEditAvailableAdminPermissions(ADMIN_PERMISSIONS);
        }
    };

    const handleSaveScope = async () => {
        if (!editToken) {
            return;
        }
        if (editUnshownError) {
            setEditError(editUnshownError);
            return;
        }
        const body = buildScopeBody(
            editConnections,
            editMcpPrivileges,
            editAdminPermissions,
        );

        // Which categories the token is restricted in today, taken from
        // the stored scope rather than the dialog, so that a restriction
        // the dialog could not display still counts.
        const scope = editToken.scope;
        const wasRestricted = {
            connections: (scope?.connections?.length ?? 0) > 0,
            mcp_privileges: (scope?.mcp_privileges?.length ?? 0) > 0,
            admin_permissions: (scope?.admin_permissions?.length ?? 0) > 0,
        };
        const anyWasRestricted = Object.values(wasRestricted).some(Boolean);
        const lifted: string[] = [];
        if (wasRestricted.connections && !body.connections) {
            lifted.push('connections');
        }
        if (wasRestricted.mcp_privileges && !body.mcp_privileges) {
            lifted.push('MCP privileges');
        }
        if (wasRestricted.admin_permissions && !body.admin_permissions) {
            lifted.push('admin permissions');
        }

        const clearAll = Object.keys(body).length === 0;
        if (!clearAll && lifted.length > 0) {
            setEditError(liftOneCategoryError(lifted));
            return;
        }

        try {
            setEditLoading(true);
            setEditError(null);
            if (clearAll) {
                // Every category is now empty, so the token is to have
                // no restriction at all; one DELETE does that atomically.
                if (anyWasRestricted) {
                    await apiDelete(`/api/v1/rbac/tokens/${editToken.id}/scope`);
                }
            } else {
                await apiPut(`/api/v1/rbac/tokens/${editToken.id}/scope`, body);
            }
            setEditOpen(false);
            fetchData();
        } catch (err: unknown) {
            setEditError(extractErrorMessage(err));
        } finally {
            setEditLoading(false);
        }
    };

    // Delete token
    const handleOpenDelete = (token: Token) => {
        setDeleteToken(token);
        setDeleteOpen(true);
    };

    const handleDeleteToken = async () => {
        if (!deleteToken) {
            return;
        }
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/rbac/tokens/${deleteToken.id}`);
            setDeleteOpen(false);
            setDeleteToken(null);
            fetchData();
        } catch (err: unknown) {
            setError(extractErrorMessage(err));
        } finally {
            setDeleteLoading(false);
        }
    };

    // Copy token to clipboard
    const handleCopyToken = useCallback(async () => {
        if (!createdToken) {
            return;
        }
        try {
            await copyToClipboard(
                createdToken,
                createdDialogContentRef.current ?? undefined
            );
            setError(null);
            setTokenCopied(true);
            if (copyResetTimerRef.current !== null) {
                window.clearTimeout(copyResetTimerRef.current);
            }
            copyResetTimerRef.current = window.setTimeout(() => {
                setTokenCopied(false);
                copyResetTimerRef.current = null;
            }, 2000);
        } catch (err: unknown) {
            setError(
                err instanceof Error
                    ? `Failed to copy token: ${err.message}`
                    : 'Failed to copy token to clipboard.'
            );
        }
    }, [createdToken]);

    // Close the "token created" dialog
    const handleCloseCreatedDialog = useCallback(() => {
        setCreatedDialogOpen(false);
        setTokenCopied(false);
        if (copyResetTimerRef.current !== null) {
            window.clearTimeout(copyResetTimerRef.current);
            copyResetTimerRef.current = null;
        }
    }, []);

    // Clean up timer on unmount
    useEffect(() => {
        return () => {
            if (copyResetTimerRef.current !== null) {
                window.clearTimeout(copyResetTimerRef.current);
                copyResetTimerRef.current = null;
            }
        };
    }, []);

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading tokens" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Tokens
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={handleOpenCreate}
                    sx={containedButtonSx}
                >
                    Create Token
                </Button>
            </Box>

            {error && (
                <Alert
                    severity="error"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => { setError(null); }}
                >
                    {error}
                </Alert>
            )}

            <TokensTable
                tokens={tokens}
                connections={connections}
                expandedToken={expandedToken}
                onRowClick={handleTokenRowClick}
                onEdit={handleOpenEdit}
                onDelete={handleOpenDelete}
                getMcpPrivilegeName={getMcpPrivilegeName}
            />

            <ApiUsageExample />

            <CreateTokenDialog
                open={createOpen}
                onClose={() => { setCreateOpen(false); }}
                onSubmit={handleCreateToken}
                loading={createLoading}
                error={createError}
                annotation={createAnnotation}
                onAnnotationChange={setCreateAnnotation}
                owner={createOwner}
                onOwnerChange={handleOwnerChange}
                users={users}
                expiry={createExpiry}
                onExpiryChange={setCreateExpiry}
                availableConnections={ownerConnections}
                scopedConnections={createConnections}
                onScopedConnectionsChange={setCreateConnections}
                ownerConnectionLevels={ownerConnectionLevels}
                ownerIsSuperuser={ownerIsSuperuser}
                availableMcpPrivileges={ownerMcpPrivileges}
                selectedMcpPrivileges={createMcpPrivileges}
                onMcpPrivilegesChange={setCreateMcpPrivileges}
                availableAdminPermissions={ownerAdminPermissions}
                selectedAdminPermissions={createAdminPermissions}
                onAdminPermissionsChange={setCreateAdminPermissions}
            />

            <CreatedTokenDialog
                ref={createdDialogContentRef}
                open={createdDialogOpen}
                onClose={handleCloseCreatedDialog}
                token={createdToken}
                onCopy={handleCopyToken}
                copied={tokenCopied}
            />

            <EditTokenDialog
                open={editOpen}
                onClose={() => { setEditOpen(false); }}
                onSubmit={handleSaveScope}
                loading={editLoading}
                error={editError}
                token={editToken}
                availableConnections={editAvailableConnections}
                scopedConnections={editConnections}
                onScopedConnectionsChange={setEditConnections}
                ownerConnectionLevels={editOwnerConnectionLevels}
                ownerIsSuperuser={editOwnerIsSuperuser}
                availableMcpPrivileges={editAvailableMcpPrivileges}
                selectedMcpPrivileges={editMcpPrivileges}
                onMcpPrivilegesChange={setEditMcpPrivileges}
                availableAdminPermissions={editAvailableAdminPermissions}
                selectedAdminPermissions={editAdminPermissions}
                onAdminPermissionsChange={setEditAdminPermissions}
            />

            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => {
                    setDeleteOpen(false);
                    setDeleteToken(null);
                }}
                onConfirm={handleDeleteToken}
                title="Delete Token"
                message="Are you sure you want to delete the token"
                itemName={deleteToken?.name ? `"${deleteToken.name}"?` : '?'}
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminTokenScopes;
