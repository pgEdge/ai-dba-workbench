/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
    Delete as DeleteIcon,
    Close as CloseIcon,
} from '@mui/icons-material';
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

interface AdminTokenScopesProps {
    mode: string;
}

const AdminTokenScopes: React.FC<AdminTokenScopesProps> = ({ mode }) => {
    const theme = useTheme();
    const [tokens, setTokens] = useState([]);
    const [connections, setConnections] = useState([]);
    const [mcpPrivileges, setMcpPrivileges] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // Edit scope dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editToken, setEditToken] = useState(null);
    const [editConnections, setEditConnections] = useState([]);
    const [editMcpPrivileges, setEditMcpPrivileges] = useState([]);
    const [editLoading, setEditLoading] = useState(false);
    const [editError, setEditError] = useState(null);

    const getConnectionName = useCallback((id) => {
        if (id === 0) return 'All Connections';
        const conn = connections.find(c => c.id === id);
        return conn ? conn.name : `Connection ${id}`;
    }, [connections]);

    const getMcpPrivilegeName = useCallback((id) => {
        const priv = mcpPrivileges.find(p => p.id === id);
        return priv ? priv.identifier : `Privilege ${id}`;
    }, [mcpPrivileges]);

    const fetchTokens = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const [tokRes, connRes, mcpRes] = await Promise.all([
                fetch(`${API_BASE_URL}/rbac/tokens`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/connections`, { credentials: 'include' }),
                fetch(`${API_BASE_URL}/rbac/privileges/mcp`, { credentials: 'include' }),
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
        } catch (err) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchTokens();
    }, [fetchTokens]);

    const handleOpenEdit = (token) => {
        setEditToken(token);
        const scopeConnIds = token.scope?.connection_ids || [];
        setEditConnections(connections.filter(c => scopeConnIds.includes(c.id)));
        const scopeMcpIds = token.scope?.mcp_privileges || [];
        setEditMcpPrivileges(mcpPrivileges.filter(p => scopeMcpIds.includes(p.id)));
        setEditError(null);
        setEditOpen(true);
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
                        connection_ids: editConnections.map(c => c.id),
                        mcp_privileges: editMcpPrivileges.map(p => p.identifier),
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update scope');
            }
            setEditOpen(false);
            fetchTokens();
        } catch (err) {
            setEditError(err.message);
        } finally {
            setEditLoading(false);
        }
    };

    const handleClearScope = async (token) => {
        try {
            const response = await fetch(
                `${API_BASE_URL}/rbac/tokens/${token.id}/scope`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) throw new Error('Failed to clear scope');
            fetchTokens();
        } catch (err) {
            setError(err.message);
        }
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

    const connectionChipSx = {
        bgcolor: alpha(theme.palette.primary.main, 0.15),
        color: theme.palette.primary.main,
        fontSize: '0.75rem',
    };

    const mcpChipSx = {
        bgcolor: alpha(theme.palette.custom.status.purple, 0.15),
        color: theme.palette.custom.status.purple,
        fontSize: '0.75rem',
    };

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Token Scopes
            </Typography>

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
                            <TableCell sx={tableHeaderCellSx}>Token</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Token ID</TableCell>
                            <TableCell sx={tableHeaderCellSx}>User</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Scope</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {tokens.length > 0 ? (
                            tokens.map((token) => {
                                const hasScope = token.scope?.scoped;
                                return (
                                    <TableRow key={token.id}>
                                        <TableCell>
                                            {token.name || token.token_prefix || `Token #${token.id}`}
                                        </TableCell>
                                        <TableCell>{token.id}</TableCell>
                                        <TableCell>{token.username || '-'}</TableCell>
                                        <TableCell>
                                            {hasScope ? (
                                                <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                                                    {token.scope.connection_ids?.map((id, i) => (
                                                        <Chip
                                                            key={`c-${i}`}
                                                            label={getConnectionName(id)}
                                                            size="small"
                                                            sx={connectionChipSx}
                                                        />
                                                    ))}
                                                    {token.scope.mcp_privileges?.map((id, i) => (
                                                        <Chip
                                                            key={`m-${i}`}
                                                            label={getMcpPrivilegeName(id)}
                                                            size="small"
                                                            sx={mcpChipSx}
                                                        />
                                                    ))}
                                                </Box>
                                            ) : (
                                                <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                    Unrestricted
                                                </Typography>
                                            )}
                                        </TableCell>
                                        <TableCell align="right">
                                            <IconButton
                                                size="small"
                                                onClick={() => handleOpenEdit(token)}
                                                aria-label="edit scope"
                                            >
                                                <EditIcon fontSize="small" />
                                            </IconButton>
                                            {hasScope && (
                                                <IconButton
                                                    size="small"
                                                    onClick={() => handleClearScope(token)}
                                                    sx={deleteIconSx}
                                                    aria-label="clear scope"
                                                >
                                                    <DeleteIcon fontSize="small" />
                                                </IconButton>
                                            )}
                                        </TableCell>
                                    </TableRow>
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

            {/* Edit Scope Dialog */}
            <Dialog open={editOpen} onClose={() => !editLoading && setEditOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle sx={{ ...dialogTitleSx, display: 'flex', alignItems: 'center' }}>
                    <Box sx={{ flex: 1 }}>
                        Edit Scope for {editToken?.name || editToken?.token_prefix || 'Token'}
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
                        multiple
                        options={connections}
                        getOptionLabel={(option) => option.name || ''}
                        isOptionEqualToValue={(option, value) => option.id === value.id}
                        value={editConnections}
                        onChange={(e, value) => setEditConnections(value)}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed Connections"
                                margin="dense"
                            />
                        )}
                        disabled={editLoading}
                    />
                    <Typography
                        variant="subtitle2"
                        sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                    >
                        MCP Privileges
                    </Typography>
                    <Autocomplete
                        multiple
                        options={mcpPrivileges}
                        getOptionLabel={(option) => option.identifier || ''}
                        isOptionEqualToValue={(option, value) => option.id === value.id}
                        value={editMcpPrivileges}
                        onChange={(e, value) => setEditMcpPrivileges(value)}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Allowed MCP Privileges"
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
        </Box>
    );
};

export default AdminTokenScopes;
