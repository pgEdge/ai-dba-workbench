/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback } from 'react';
import {
    Box,
    Typography,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Paper,
    Button,
    IconButton,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Chip,
    Autocomplete,
    TextField,
    MenuItem,
    Collapse,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Close as CloseIcon,
    ContentCopy as CopyIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import EffectivePermissionsPanel from './EffectivePermissionsPanel';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    loadingContainerSx,
    pageHeadingSx,
    subsectionLabelSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
} from './styles';

const API_BASE_URL = '/api/v1';

const EXPIRY_OPTIONS = [
    { label: '30 days', value: '30d' },
    { label: '90 days', value: '90d' },
    { label: '1 year', value: '1y' },
    { label: 'Never', value: 'never' },
];

const ADMIN_PERMISSIONS = [
    { id: 'manage_connections', label: 'Manage Connections' },
    { id: 'manage_groups', label: 'Manage Groups' },
    { id: 'manage_permissions', label: 'Manage Permissions' },
    { id: 'manage_users', label: 'Manage Users' },
    { id: 'manage_token_scopes', label: 'Manage Token Scopes' },
    { id: 'manage_blackouts', label: 'Manage Blackouts' },
    { id: 'manage_probes', label: 'Manage Probes' },
    { id: 'manage_alert_rules', label: 'Manage Alert Rules' },
];

const ALL_MCP_OPTION = { id: -1, identifier: '*', _isAll: true };
const ALL_ADMIN_OPTION = { id: '*', label: 'All Admin Permissions', _isAll: true };

interface ScopedConnection {
    id: number;
    name: string;
    access_level: string;
}

interface AdminTokenScopesProps {
    mode: string;
}

const AdminTokenScopes: React.FC<AdminTokenScopesProps> = ({ mode }) => {
    const theme = useTheme();
    const [tokens, setTokens] = useState<any[]>([]);
    const [connections, setConnections] = useState<any[]>([]);
    const [mcpPrivileges, setMcpPrivileges] = useState<any[]>([]);
    const [users, setUsers] = useState<any[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedToken, setExpandedToken] = useState<number | null>(null);

    // Create token dialog
    const [createOpen, setCreateOpen] = useState(false);
    const [createOwner, setCreateOwner] = useState<any>(null);
    const [createAnnotation, setCreateAnnotation] = useState('');
    const [createExpiry, setCreateExpiry] = useState('90d');
    const [createLoading, setCreateLoading] = useState(false);
    const [createError, setCreateError] = useState<string | null>(null);
    const [createConnections, setCreateConnections] = useState<any[]>([]);
    const [createMcpPrivileges, setCreateMcpPrivileges] = useState<any[]>([]);
    const [createAdminPermissions, setCreateAdminPermissions] = useState<any[]>([]);

    // Token created success dialog
    const [createdToken, setCreatedToken] = useState<string | null>(null);
    const [createdDialogOpen, setCreatedDialogOpen] = useState(false);

    // Create dialog - owner privilege filtering
    const [ownerConnections, setOwnerConnections] = useState<any[]>([]);
    const [ownerConnectionLevels, setOwnerConnectionLevels] = useState<Record<number, string>>({});
    const [ownerMcpPrivileges, setOwnerMcpPrivileges] = useState<any[]>([]);
    const [ownerAdminPermissions, setOwnerAdminPermissions] = useState<any[]>([]);
    const [ownerIsSuperuser, setOwnerIsSuperuser] = useState(false);

    // Edit scope dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editToken, setEditToken] = useState<any>(null);
    const [editConnections, setEditConnections] = useState<any[]>([]);
    const [editMcpPrivileges, setEditMcpPrivileges] = useState<any[]>([]);
    const [editAdminPermissions, setEditAdminPermissions] = useState<any[]>([]);
    const [editLoading, setEditLoading] = useState(false);
    const [editError, setEditError] = useState<string | null>(null);
    const [editAvailableConnections, setEditAvailableConnections] = useState<any[]>([]);
    const [editOwnerConnectionLevels, setEditOwnerConnectionLevels] = useState<Record<number, string>>({});
    const [editOwnerIsSuperuser, setEditOwnerIsSuperuser] = useState(false);
    const [editAvailableMcpPrivileges, setEditAvailableMcpPrivileges] = useState<any[]>([]);
    const [editAvailableAdminPermissions, setEditAvailableAdminPermissions] = useState<any[]>([]);

    // Delete confirmation
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deleteToken, setDeleteToken] = useState<any>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    const getConnectionName = useCallback((id: number) => {
        if (id === 0) return 'All Connections';
        const conn = connections.find((c: any) => c.id === id);
        return conn ? conn.name : `Connection ${id}`;
    }, [connections]);

    const getMcpPrivilegeName = useCallback((id: number) => {
        if (id === -1) return 'All MCP Privileges';
        const priv = mcpPrivileges.find((p: any) => p.id === id);
        return priv ? priv.identifier : `Privilege ${id}`;
    }, [mcpPrivileges]);

    const getAdminPermissionLabel = useCallback((id: string) => {
        if (id === '*') return 'All Admin Permissions';
        const perm = ADMIN_PERMISSIONS.find(p => p.id === id);
        return perm ? perm.label : id;
    }, []);

    const fetchData = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const [tokRes, connRes, mcpRes, usersRes] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/tokens`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/connections`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/rbac/privileges/mcp`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/rbac/users`, { credentials: 'include' }),
            ]);
            if (!tokRes.ok) throw new Error('Failed to fetch tokens');
            const tokData = await tokRes.json();
            setTokens(tokData.tokens || []);
            if (connRes.ok) {
                const connData = await connRes.json();
                setConnections(connData.connections || connData || []);
            }
            if (mcpRes.ok) {
                const mcpData = await mcpRes.json();
                setMcpPrivileges(mcpData || []);
            }
            if (usersRes.ok) {
                const usersData = await usersRes.json();
                setUsers(usersData.users || []);
            }
        } catch (err: any) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchData();
    }, [fetchData]);

    const handleTokenRowClick = (token: any) => {
        if (expandedToken === token.id) {
            setExpandedToken(null);
        } else {
            setExpandedToken(token.id);
        }
    };

    // Handle owner change in create dialog - fetch privileges
    const handleOwnerChange = async (owner: any) => {
        setCreateOwner(owner);
        // Clear current scope selections when owner changes
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
            const res = await fetch(
                `${API_BASE_URL}/rbac/users/${owner.id}/privileges`,
                { credentials: 'include' }
            );
            if (res.ok) {
                const data = await res.json();
                if (data.is_superuser) {
                    // Superusers can access everything
                    setOwnerIsSuperuser(true);
                    setOwnerConnections(connections);
                    const levels: Record<number, string> = {};
                    connections.forEach((c: any) => { levels[c.id] = 'read_write'; });
                    setOwnerConnectionLevels(levels);
                    setOwnerMcpPrivileges(mcpPrivileges);
                    setOwnerAdminPermissions(ADMIN_PERMISSIONS);
                } else {
                    setOwnerIsSuperuser(false);
                    // Filter connections to those the user has access to
                    const connPrivs = data.connection_privileges || {};
                    const allowedConnIds = Object.keys(connPrivs).map(Number);
                    setOwnerConnectionLevels(
                        Object.fromEntries(Object.entries(connPrivs).map(([k, v]) => [Number(k), v as string]))
                    );
                    // Check for wildcard (0 means all connections)
                    if (allowedConnIds.includes(0)) {
                        setOwnerConnections(connections);
                    } else {
                        setOwnerConnections(
                            connections.filter((c: any) =>
                                allowedConnIds.includes(c.id)
                            )
                        );
                    }
                    // Filter MCP privileges to those the user has
                    const allowedMcp = data.mcp_privileges || [];
                    setOwnerMcpPrivileges(
                        mcpPrivileges.filter((p: any) =>
                            allowedMcp.includes(p.identifier)
                        )
                    );
                    // Filter admin permissions to those the user has
                    const allowedAdmin = data.admin_permissions || [];
                    setOwnerAdminPermissions(
                        ADMIN_PERMISSIONS.filter(p =>
                            allowedAdmin.includes(p.id)
                        )
                    );
                }
            }
        } catch {
            // If privilege fetch fails, show all options as fallback
            setOwnerConnections(connections);
            setOwnerMcpPrivileges(mcpPrivileges);
            setOwnerAdminPermissions(ADMIN_PERMISSIONS);
        }
    };

    // Create token
    const handleCreateToken = async () => {
        if (!createOwner || !createAnnotation.trim()) return;
        try {
            setCreateLoading(true);
            setCreateError(null);
            const body: any = {
                owner_username: createOwner.username,
                annotation: createAnnotation.trim(),
                expires_in: createExpiry,
            };
            const response = await fetch(`${API_BASE_URL}/rbac/tokens`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to create token');
            }
            const data = await response.json();
            // Set scope if specified
            if (createConnections.length > 0 || createMcpPrivileges.length > 0 || createAdminPermissions.length > 0) {
                await fetch(`${API_BASE_URL}/rbac/tokens/${data.id}/scope`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        connections: createConnections.map((c: ScopedConnection) => ({
                            connection_id: c.id,
                            access_level: c.access_level,
                        })),
                        mcp_privileges: createMcpPrivileges.some((p: any) => p._isAll)
                            ? ['*']
                            : createMcpPrivileges.map((p: any) => p.identifier),
                        admin_permissions: createAdminPermissions.some((p: any) => p._isAll)
                            ? ['*']
                            : createAdminPermissions.map((p: any) => p.id),
                    }),
                });
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
        } catch (err: any) {
            setCreateError(err.message);
        } finally {
            setCreateLoading(false);
        }
    };

    // Edit scope
    const handleOpenEdit = async (token: any) => {
        setEditToken(token);
        const scopeConns = token.scope?.connections || [];
        setEditConnections(scopeConns.map((sc: any) => {
            const conn = connections.find((c: any) => c.id === sc.connection_id);
            return {
                id: sc.connection_id,
                name: conn ? conn.name : `Connection ${sc.connection_id}`,
                access_level: sc.access_level || 'read_write',
            };
        }));
        const scopeMcpIds = token.scope?.mcp_privileges || [];
        const mcpNames = scopeMcpIds.map((id: number) => getMcpPrivilegeName(id));
        if (mcpNames.includes('*')) {
            setEditMcpPrivileges([ALL_MCP_OPTION]);
        } else {
            setEditMcpPrivileges(mcpPrivileges.filter((p: any) => scopeMcpIds.includes(p.id)));
        }
        const scopeAdminPerms = token.scope?.admin_permissions || [];
        if (scopeAdminPerms.includes('*')) {
            setEditAdminPermissions([ALL_ADMIN_OPTION]);
        } else {
            setEditAdminPermissions(ADMIN_PERMISSIONS.filter((p: any) => scopeAdminPerms.includes(p.id)));
        }
        setEditError(null);
        setEditOpen(true);

        // Fetch owner's privileges for filtering scope options
        if (token.user_id) {
            try {
                const res = await fetch(
                    `${API_BASE_URL}/rbac/users/${token.user_id}/privileges`,
                    { credentials: 'include' }
                );
                if (res.ok) {
                    const data = await res.json();
                    if (data.is_superuser) {
                        setEditOwnerIsSuperuser(true);
                        setEditAvailableConnections(connections);
                        const levels: Record<number, string> = {};
                        connections.forEach((c: any) => { levels[c.id] = 'read_write'; });
                        setEditOwnerConnectionLevels(levels);
                        setEditAvailableMcpPrivileges(mcpPrivileges);
                        setEditAvailableAdminPermissions(ADMIN_PERMISSIONS);
                    } else {
                        setEditOwnerIsSuperuser(false);
                        const connPrivs = data.connection_privileges || {};
                        const allowedConnIds = Object.keys(connPrivs).map(Number);
                        setEditOwnerConnectionLevels(
                            Object.fromEntries(Object.entries(connPrivs).map(([k, v]) => [Number(k), v as string]))
                        );
                        if (allowedConnIds.includes(0)) {
                            setEditAvailableConnections(connections);
                        } else {
                            setEditAvailableConnections(
                                connections.filter((c: any) =>
                                    allowedConnIds.includes(c.id)
                                )
                            );
                        }
                        const allowedMcp = data.mcp_privileges || [];
                        setEditAvailableMcpPrivileges(
                            mcpPrivileges.filter((p: any) =>
                                allowedMcp.includes(p.identifier)
                            )
                        );
                        const allowedAdmin = data.admin_permissions || [];
                        setEditAvailableAdminPermissions(
                            ADMIN_PERMISSIONS.filter(p =>
                                allowedAdmin.includes(p.id)
                            )
                        );
                    }
                } else {
                    setEditOwnerIsSuperuser(false);
                    setEditAvailableConnections(connections);
                    setEditOwnerConnectionLevels({});
                    setEditAvailableMcpPrivileges(mcpPrivileges);
                    setEditAvailableAdminPermissions(ADMIN_PERMISSIONS);
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
        if (!editToken) return;
        try {
            setEditLoading(true);
            setEditError(null);
            const response = await fetch(
                `${API_BASE_URL}/rbac/tokens/${editToken.id}/scope`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        connections: editConnections.map((c: ScopedConnection) => ({
                            connection_id: c.id,
                            access_level: c.access_level,
                        })),
                        mcp_privileges: editMcpPrivileges.some((p: any) => p._isAll)
                            ? ['*']
                            : editMcpPrivileges.map((p: any) => p.identifier),
                        admin_permissions: editAdminPermissions.some((p: any) => p._isAll)
                            ? ['*']
                            : editAdminPermissions.map((p: any) => p.id),
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update scope');
            }
            setEditOpen(false);
            fetchData();
        } catch (err: any) {
            setEditError(err.message);
        } finally {
            setEditLoading(false);
        }
    };

    // Delete token
    const handleOpenDelete = (token: any) => {
        setDeleteToken(token);
        setDeleteOpen(true);
    };

    const handleDeleteToken = async () => {
        if (!deleteToken) return;
        try {
            setDeleteLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/rbac/tokens/${deleteToken.id}`,
                { method: 'DELETE', credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to delete token');
            }
            setDeleteOpen(false);
            setDeleteToken(null);
            fetchData();
        } catch (err: any) {
            setError(err.message);
        } finally {
            setDeleteLoading(false);
        }
    };

    // Copy token to clipboard
    const handleCopyToken = async () => {
        if (createdToken) {
            await navigator.clipboard.writeText(createdToken);
        }
    };

    // Format expiry date
    const formatExpiry = (expiresAt: string | null | undefined) => {
        if (!expiresAt) return 'Never';
        return new Date(expiresAt).toLocaleDateString();
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Tokens
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={() => {
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
                    }}
                    sx={containedButtonSx}
                >
                    Create Token
                </Button>
            </Box>

            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}

            <TableContainer
                component={Paper}
                elevation={0}
                sx={tableContainerSx}
            >
                <Table>
                    <TableHead>
                        <TableRow>
                            <TableCell sx={{ ...tableHeaderCellSx, width: 40 }} />
                            <TableCell sx={tableHeaderCellSx}>Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Owner</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Expires</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {tokens.length > 0 ? (
                            tokens.map((token: any) => {
                                const hasScope = token.scope?.scoped;
                                return (
                                    <React.Fragment key={token.id}>
                                        <TableRow
                                            hover
                                            onClick={() => handleTokenRowClick(token)}
                                            sx={{ cursor: 'pointer' }}
                                        >
                                            <TableCell sx={{ px: 1 }}>
                                                {expandedToken === token.id
                                                    ? <ExpandLessIcon sx={{ color: 'text.secondary' }} />
                                                    : <ExpandMoreIcon sx={{ color: 'text.secondary' }} />
                                                }
                                            </TableCell>
                                            <TableCell>
                                                {token.name || token.token_prefix || `Token #${token.id}`}
                                            </TableCell>
                                            <TableCell>
                                                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
                                                    {token.username || '-'}
                                                    {token.is_service_account && (
                                                        <Chip
                                                            label="Service Account"
                                                            size="small"
                                                            sx={{
                                                                bgcolor: alpha(theme.palette.info.main, 0.15),
                                                                color: theme.palette.info.main,
                                                                fontSize: '0.75rem',
                                                            }}
                                                        />
                                                    )}
                                                    {token.is_superuser && (
                                                        <Chip
                                                            label="Superuser"
                                                            size="small"
                                                            sx={{
                                                                bgcolor: alpha(theme.palette.warning.main, 0.15),
                                                                color: theme.palette.warning.main,
                                                                fontSize: '0.75rem',
                                                            }}
                                                        />
                                                    )}
                                                </Box>
                                            </TableCell>
                                            <TableCell>
                                                {formatExpiry(token.expires_at)}
                                            </TableCell>
                                            <TableCell align="right">
                                                <IconButton
                                                    size="small"
                                                    onClick={(e) => { e.stopPropagation(); handleOpenEdit(token); }}
                                                    aria-label="edit token"
                                                >
                                                    <EditIcon fontSize="small" />
                                                </IconButton>
                                                <IconButton
                                                    size="small"
                                                    onClick={(e) => { e.stopPropagation(); handleOpenDelete(token); }}
                                                    sx={deleteIconSx}
                                                    aria-label="delete token"
                                                >
                                                    <DeleteIcon fontSize="small" />
                                                </IconButton>
                                            </TableCell>
                                        </TableRow>
                                        <TableRow>
                                            <TableCell colSpan={5} sx={{ py: 0, borderBottom: expandedToken === token.id ? undefined : 'none' }}>
                                                <Collapse in={expandedToken === token.id} timeout="auto" unmountOnExit>
                                                    <Box sx={{ py: 2, px: 2 }}>
                                                        <Typography variant="subtitle2" sx={{ ...subsectionLabelSx, mb: 1 }}>
                                                            Token Scope
                                                        </Typography>
                                                        {hasScope ? (
                                                            <EffectivePermissionsPanel
                                                                connectionPrivileges={token.scope.connections?.map((sc: any) => ({
                                                                    connection_id: sc.connection_id,
                                                                    access_level: sc.access_level,
                                                                }))}
                                                                mcpPrivileges={
                                                                    token.scope.mcp_privileges?.some((id: number) => getMcpPrivilegeName(id) === '*')
                                                                        ? ['All MCP Privileges']
                                                                        : token.scope.mcp_privileges?.map((id: number) => getMcpPrivilegeName(id))
                                                                }
                                                                adminPermissions={
                                                                    token.scope.admin_permissions?.includes('*')
                                                                        ? ['All Admin Permissions']
                                                                        : token.scope.admin_permissions
                                                                }
                                                                isSuperuser={true}
                                                                isDark={mode === 'dark'}
                                                                connections={connections}
                                                            />
                                                        ) : (
                                                            <Typography color="text.secondary" sx={{ fontSize: '0.875rem' }}>
                                                                Unrestricted - this token has access to all permissions granted to its owner.
                                                            </Typography>
                                                        )}
                                                    </Box>
                                                </Collapse>
                                            </TableCell>
                                        </TableRow>
                                    </React.Fragment>
                                );
                            })
                        ) : (
                            <TableRow>
                                <TableCell colSpan={5} align="center" sx={{ py: 4 }}>
                                    <Typography color="text.secondary">No tokens found.</Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            <Box sx={{ mt: 3 }}>
                <Typography variant="subtitle2" sx={{ ...subsectionLabelSx, mb: 1 }}>
                    API Usage Examples
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                    Use tokens with the Authorization header to access the API.
                </Typography>
                <Box
                    component="pre"
                    sx={{
                        bgcolor: alpha(theme.palette.text.primary, 0.05),
                        border: '1px solid',
                        borderColor: theme.palette.divider,
                        borderRadius: 1,
                        p: 2,
                        fontSize: '0.8rem',
                        fontFamily: 'monospace',
                        overflowX: 'auto',
                        whiteSpace: 'pre',
                        lineHeight: 1.6,
                        color: 'text.secondary',
                    }}
                >
{`# List connections
curl -s -H "Authorization: Bearer <token>" \\
  http://localhost:8080/api/v1/connections

# Get connection details
curl -s -H "Authorization: Bearer <token>" \\
  http://localhost:8080/api/v1/connections/1

# Create a connection
curl -s -X POST -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"name": "mydb", "host": "localhost", "port": 5432, "database": "mydb", "username": "postgres", "password": "secret"}' \\
  http://localhost:8080/api/v1/connections

# Delete a connection
curl -s -X DELETE -H "Authorization: Bearer <token>" \\
  http://localhost:8080/api/v1/connections/1

# Chat with the AI assistant
curl -s -X POST -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"messages": [{"role": "user", "content": "What tables exist in the database?"}]}' \\
  http://localhost:8080/api/v1/llm/chat`}
                </Box>
            </Box>

            {/* Create Token Dialog */}
            <Dialog open={createOpen} onClose={() => !createLoading && setCreateOpen(false)} maxWidth="xs" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Create Token</DialogTitle>
                <DialogContent>
                    {createError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{createError}</Alert>
                    )}
                    <TextField
                        fullWidth
                        label="Name"
                        value={createAnnotation}
                        onChange={(e) => setCreateAnnotation(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                        required
                    />
                    <Autocomplete
                        options={users}
                        getOptionLabel={(option: any) => option.username || ''}
                        isOptionEqualToValue={(option: any, value: any) => option.id === value.id}
                        value={createOwner}
                        onChange={(_e, value) => handleOwnerChange(value)}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Owner"
                                margin="dense"
                                required
                            />
                        )}
                        disabled={createLoading}
                    />
                    <TextField
                        fullWidth
                        select
                        label="Expiry"
                        value={createExpiry}
                        onChange={(e) => setCreateExpiry(e.target.value)}
                        disabled={createLoading}
                        margin="dense"
                    >
                        {EXPIRY_OPTIONS.map((option) => (
                            <MenuItem key={option.value} value={option.value}>
                                {option.label}
                            </MenuItem>
                        ))}
                    </TextField>
                    <Typography
                        variant="subtitle2"
                        sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                    >
                        Scope (Optional)
                    </Typography>
                    <Autocomplete
                        options={ownerConnections.filter((c: any) => !createConnections.some((sc: ScopedConnection) => sc.id === c.id))}
                        getOptionLabel={(option: any) => option.name || ''}
                        value={null}
                        onChange={(_e, value) => {
                            if (value) {
                                const maxLevel = ownerConnectionLevels[value.id] || 'read_write';
                                setCreateConnections([...createConnections, { id: value.id, name: value.name, access_level: maxLevel }]);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Add Connection"
                                margin="dense"
                                placeholder="Select a connection to add..."
                            />
                        )}
                        disabled={createLoading}
                    />
                    {createConnections.length > 0 && (
                        <Table size="small" sx={{ mt: 1 }}>
                            <TableHead>
                                <TableRow>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>Connection</TableCell>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>Access Level</TableCell>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }} align="right"></TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {createConnections.map((sc: ScopedConnection) => (
                                    <TableRow key={sc.id}>
                                        <TableCell sx={{ py: 0.5 }}>{sc.name}</TableCell>
                                        <TableCell sx={{ py: 0.5 }}>
                                            <TextField
                                                select
                                                size="small"
                                                value={sc.access_level}
                                                onChange={(e) => {
                                                    setCreateConnections(createConnections.map((c: ScopedConnection) =>
                                                        c.id === sc.id ? { ...c, access_level: e.target.value } : c
                                                    ));
                                                }}
                                                sx={{ minWidth: 130 }}
                                            >
                                                <MenuItem value="read">Read Only</MenuItem>
                                                {(ownerConnectionLevels[sc.id] === 'read_write' || ownerIsSuperuser) && (
                                                    <MenuItem value="read_write">Read/Write</MenuItem>
                                                )}
                                            </TextField>
                                        </TableCell>
                                        <TableCell align="right" sx={{ py: 0.5 }}>
                                            <IconButton
                                                size="small"
                                                onClick={() => setCreateConnections(createConnections.filter((c: ScopedConnection) => c.id !== sc.id))}
                                                sx={deleteIconSx}
                                            >
                                                <DeleteIcon fontSize="small" />
                                            </IconButton>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    )}
                    <Autocomplete
                        multiple
                        options={[
                            ...(ownerMcpPrivileges.length > 0 ? [ALL_MCP_OPTION] : []),
                            ...ownerMcpPrivileges
                        ].filter((p: any) => {
                            if (createMcpPrivileges.some((s: any) => s._isAll)) return p._isAll;
                            if (createMcpPrivileges.length > 0 && p._isAll) return false;
                            return true;
                        })}
                        getOptionLabel={(option: any) => option._isAll ? 'All MCP Privileges' : (option.identifier || '')}
                        isOptionEqualToValue={(option: any, value: any) => option.id === value.id}
                        value={createMcpPrivileges}
                        onChange={(_e, value) => {
                            const hasAll = value.some((v: any) => v._isAll);
                            const hadAll = createMcpPrivileges.some((v: any) => v._isAll);
                            if (hasAll && !hadAll) {
                                setCreateMcpPrivileges([ALL_MCP_OPTION]);
                            } else if (!hasAll && hadAll) {
                                setCreateMcpPrivileges(value.filter((v: any) => !v._isAll));
                            } else {
                                setCreateMcpPrivileges(value);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed MCP Privileges"
                                margin="dense"
                            />
                        )}
                        disabled={createLoading}
                    />
                    <Autocomplete
                        multiple
                        options={[
                            ...(ownerAdminPermissions.length > 0 ? [ALL_ADMIN_OPTION] : []),
                            ...ownerAdminPermissions
                        ].filter((p: any) => {
                            if (createAdminPermissions.some((s: any) => s._isAll)) return p._isAll;
                            if (createAdminPermissions.length > 0 && p._isAll) return false;
                            return true;
                        })}
                        getOptionLabel={(option: any) => option._isAll ? 'All Admin Permissions' : (option.label || option.id || '')}
                        isOptionEqualToValue={(option: any, value: any) => option.id === value.id}
                        value={createAdminPermissions}
                        onChange={(_e, value) => {
                            const hasAll = value.some((v: any) => v._isAll);
                            const hadAll = createAdminPermissions.some((v: any) => v._isAll);
                            if (hasAll && !hadAll) {
                                setCreateAdminPermissions([ALL_ADMIN_OPTION]);
                            } else if (!hasAll && hadAll) {
                                setCreateAdminPermissions(value.filter((v: any) => !v._isAll));
                            } else {
                                setCreateAdminPermissions(value);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed Admin Permissions"
                                margin="dense"
                            />
                        )}
                        disabled={createLoading}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setCreateOpen(false)} disabled={createLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleCreateToken}
                        variant="contained"
                        disabled={createLoading || !createOwner || !createAnnotation.trim()}
                        sx={containedButtonSx}
                    >
                        {createLoading ? <CircularProgress size={20} color="inherit" /> : 'Create'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Token Created Success Dialog */}
            <Dialog
                open={createdDialogOpen}
                onClose={() => setCreatedDialogOpen(false)}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>Token Created</DialogTitle>
                <DialogContent>
                    <Alert severity="warning" sx={{ mb: 2, borderRadius: 1 }}>
                        Save this token securely. It will not be shown again.
                    </Alert>
                    <Box
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 1,
                            fontFamily: 'monospace',
                            fontSize: '0.875rem',
                            bgcolor: alpha(theme.palette.text.primary, 0.05),
                            border: '1px solid',
                            borderColor: theme.palette.divider,
                            borderRadius: 1,
                            p: 2,
                            wordBreak: 'break-all',
                        }}
                    >
                        <Box sx={{ flex: 1 }}>
                            {createdToken}
                        </Box>
                        <IconButton
                            onClick={handleCopyToken}
                            size="small"
                            aria-label="copy token"
                        >
                            <CopyIcon fontSize="small" />
                        </IconButton>
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setCreatedDialogOpen(false)}>
                        Close
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Edit Token Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle sx={{ ...dialogTitleSx, display: 'flex', alignItems: 'center' }}>
                    <Box sx={{ flex: 1 }}>
                        Edit Token: {editToken?.name || editToken?.token_prefix || 'Token'}
                    </Box>
                    <IconButton onClick={() => setEditOpen(false)} size="small" disabled={editLoading}>
                        <CloseIcon />
                    </IconButton>
                </DialogTitle>
                <DialogContent>
                    {editError && (
                        <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>{editError}</Alert>
                    )}
                    <Typography
                        variant="subtitle2"
                        sx={{ ...subsectionLabelSx, mb: 1, mt: 1 }}
                    >
                        Connections
                    </Typography>
                    <Autocomplete
                        options={editAvailableConnections.filter((c: any) => !editConnections.some((sc: ScopedConnection) => sc.id === c.id))}
                        getOptionLabel={(option: any) => option.name || ''}
                        value={null}
                        onChange={(_e, value) => {
                            if (value) {
                                const maxLevel = editOwnerConnectionLevels[value.id] || 'read_write';
                                setEditConnections([...editConnections, { id: value.id, name: value.name, access_level: maxLevel }]);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Add Connection"
                                margin="dense"
                                placeholder="Select a connection to add..."
                            />
                        )}
                        disabled={editLoading}
                    />
                    {editConnections.length > 0 && (
                        <Table size="small" sx={{ mt: 1 }}>
                            <TableHead>
                                <TableRow>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>Connection</TableCell>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>Access Level</TableCell>
                                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }} align="right"></TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {editConnections.map((sc: ScopedConnection) => (
                                    <TableRow key={sc.id}>
                                        <TableCell sx={{ py: 0.5 }}>{sc.name}</TableCell>
                                        <TableCell sx={{ py: 0.5 }}>
                                            <TextField
                                                select
                                                size="small"
                                                value={sc.access_level}
                                                onChange={(e) => {
                                                    setEditConnections(editConnections.map((c: ScopedConnection) =>
                                                        c.id === sc.id ? { ...c, access_level: e.target.value } : c
                                                    ));
                                                }}
                                                sx={{ minWidth: 130 }}
                                            >
                                                <MenuItem value="read">Read Only</MenuItem>
                                                {(editOwnerConnectionLevels[sc.id] === 'read_write' || editOwnerIsSuperuser) && (
                                                    <MenuItem value="read_write">Read/Write</MenuItem>
                                                )}
                                            </TextField>
                                        </TableCell>
                                        <TableCell align="right" sx={{ py: 0.5 }}>
                                            <IconButton
                                                size="small"
                                                onClick={() => setEditConnections(editConnections.filter((c: ScopedConnection) => c.id !== sc.id))}
                                                sx={deleteIconSx}
                                            >
                                                <DeleteIcon fontSize="small" />
                                            </IconButton>
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    )}
                    <Typography
                        variant="subtitle2"
                        sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                    >
                        MCP Privileges
                    </Typography>
                    <Autocomplete
                        multiple
                        options={[
                            ...(editAvailableMcpPrivileges.length > 0 ? [ALL_MCP_OPTION] : []),
                            ...editAvailableMcpPrivileges
                        ].filter((p: any) => {
                            if (editMcpPrivileges.some((s: any) => s._isAll)) return p._isAll;
                            if (editMcpPrivileges.length > 0 && p._isAll) return false;
                            return true;
                        })}
                        getOptionLabel={(option: any) => option._isAll ? 'All MCP Privileges' : (option.identifier || '')}
                        isOptionEqualToValue={(option: any, value: any) => option.id === value.id}
                        value={editMcpPrivileges}
                        onChange={(_e, value) => {
                            const hasAll = value.some((v: any) => v._isAll);
                            const hadAll = editMcpPrivileges.some((v: any) => v._isAll);
                            if (hasAll && !hadAll) {
                                setEditMcpPrivileges([ALL_MCP_OPTION]);
                            } else if (!hasAll && hadAll) {
                                setEditMcpPrivileges(value.filter((v: any) => !v._isAll));
                            } else {
                                setEditMcpPrivileges(value);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed MCP Privileges"
                                margin="dense"
                            />
                        )}
                        disabled={editLoading}
                    />
                    <Typography
                        variant="subtitle2"
                        sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                    >
                        Admin Permissions
                    </Typography>
                    <Autocomplete
                        multiple
                        options={[
                            ...(editAvailableAdminPermissions.length > 0 ? [ALL_ADMIN_OPTION] : []),
                            ...editAvailableAdminPermissions
                        ].filter((p: any) => {
                            if (editAdminPermissions.some((s: any) => s._isAll)) return p._isAll;
                            if (editAdminPermissions.length > 0 && p._isAll) return false;
                            return true;
                        })}
                        getOptionLabel={(option: any) => option._isAll ? 'All Admin Permissions' : (option.label || option.id || '')}
                        isOptionEqualToValue={(option: any, value: any) => option.id === value.id}
                        value={editAdminPermissions}
                        onChange={(_e, value) => {
                            const hasAll = value.some((v: any) => v._isAll);
                            const hadAll = editAdminPermissions.some((v: any) => v._isAll);
                            if (hasAll && !hadAll) {
                                setEditAdminPermissions([ALL_ADMIN_OPTION]);
                            } else if (!hasAll && hadAll) {
                                setEditAdminPermissions(value.filter((v: any) => !v._isAll));
                            } else {
                                setEditAdminPermissions(value);
                            }
                        }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed Admin Permissions"
                                margin="dense"
                            />
                        )}
                        disabled={editLoading}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setEditOpen(false)} disabled={editLoading}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveScope}
                        variant="contained"
                        disabled={editLoading}
                        sx={containedButtonSx}
                    >
                        {editLoading ? <CircularProgress size={20} color="inherit" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => { setDeleteOpen(false); setDeleteToken(null); }}
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
