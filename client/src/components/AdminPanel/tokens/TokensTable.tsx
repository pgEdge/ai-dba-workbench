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
    IconButton,
    Chip,
    Collapse,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
    Delete as DeleteIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import EffectivePermissionsPanel from '../EffectivePermissionsPanel';
import {
    tableHeaderCellSx,
    subsectionLabelSx,
    getDeleteIconSx,
    getTableContainerSx,
} from '../styles';
import type { Token, Connection, TokenScopeConnection } from './tokenTypes';

export interface TokensTableProps {
    /** List of tokens to display. */
    tokens: Token[];
    /** List of connections for name lookup. */
    connections: Connection[];
    /** ID of the currently expanded token, or null. */
    expandedToken: number | null;
    /** Handler when a token row is clicked. */
    onRowClick: (token: Token) => void;
    /** Handler when the edit button is clicked. */
    onEdit: (token: Token) => void;
    /** Handler when the delete button is clicked. */
    onDelete: (token: Token) => void;
    /** Function to get MCP privilege name by ID. */
    getMcpPrivilegeName: (id: number) => string;
}

/**
 * Table displaying tokens with expandable scope details.
 */
const TokensTable: React.FC<TokensTableProps> = ({
    tokens,
    connections,
    expandedToken,
    onRowClick,
    onEdit,
    onDelete,
    getMcpPrivilegeName,
}) => {
    const theme = useTheme();
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    // Format expiry date
    const formatExpiry = (expiresAt: string | null | undefined) => {
        if (!expiresAt) {
            return 'Never';
        }
        return new Date(expiresAt).toLocaleDateString();
    };

    return (
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
                        <TableCell sx={tableHeaderCellSx} align="right">
                            Actions
                        </TableCell>
                    </TableRow>
                </TableHead>
                <TableBody>
                    {tokens.length > 0 ? (
                        tokens.map((token) => {
                            const hasScope = token.scope?.scoped;
                            const isExpanded = expandedToken === token.id;

                            return (
                                <React.Fragment key={token.id}>
                                    <TableRow
                                        hover
                                        onClick={() => { onRowClick(token); }}
                                        sx={{ cursor: 'pointer' }}
                                    >
                                        <TableCell sx={{ px: 1 }}>
                                            {isExpanded ? (
                                                <ExpandLessIcon
                                                    sx={{ color: 'text.secondary' }}
                                                />
                                            ) : (
                                                <ExpandMoreIcon
                                                    sx={{ color: 'text.secondary' }}
                                                />
                                            )}
                                        </TableCell>
                                        <TableCell>
                                            {token.name ||
                                                token.token_prefix ||
                                                `Token #${token.id}`}
                                        </TableCell>
                                        <TableCell>
                                            <Box
                                                sx={{
                                                    display: 'flex',
                                                    alignItems: 'center',
                                                    gap: 0.5,
                                                    flexWrap: 'wrap',
                                                }}
                                            >
                                                {token.username || '-'}
                                                {token.is_service_account && (
                                                    <Chip
                                                        label="Service Account"
                                                        size="small"
                                                        sx={{
                                                            bgcolor: alpha(
                                                                theme.palette.info.main,
                                                                0.15
                                                            ),
                                                            color: theme.palette.info.main,
                                                            fontSize: '0.875rem',
                                                        }}
                                                    />
                                                )}
                                                {token.is_superuser && (
                                                    <Chip
                                                        label="Superuser"
                                                        size="small"
                                                        sx={{
                                                            bgcolor: alpha(
                                                                theme.palette.warning.main,
                                                                0.15
                                                            ),
                                                            color: theme.palette.warning.main,
                                                            fontSize: '0.875rem',
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
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    onEdit(token);
                                                }}
                                                aria-label="edit token"
                                            >
                                                <EditIcon fontSize="small" />
                                            </IconButton>
                                            <IconButton
                                                size="small"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    onDelete(token);
                                                }}
                                                sx={deleteIconSx}
                                                aria-label="delete token"
                                            >
                                                <DeleteIcon fontSize="small" />
                                            </IconButton>
                                        </TableCell>
                                    </TableRow>
                                    <TableRow>
                                        <TableCell
                                            colSpan={5}
                                            sx={{
                                                py: 0,
                                                borderBottom: isExpanded
                                                    ? undefined
                                                    : 'none',
                                            }}
                                        >
                                            <Collapse
                                                in={isExpanded}
                                                timeout="auto"
                                                unmountOnExit
                                            >
                                                <Box sx={{ py: 2, px: 2 }}>
                                                    <Typography
                                                        variant="subtitle2"
                                                        sx={{
                                                            ...subsectionLabelSx,
                                                            mb: 1,
                                                        }}
                                                    >
                                                        Token Scope
                                                    </Typography>
                                                    {hasScope ? (
                                                        <EffectivePermissionsPanel
                                                            connectionPrivileges={token.scope?.connections?.map(
                                                                (
                                                                    sc: TokenScopeConnection
                                                                ) => ({
                                                                    connection_id:
                                                                        sc.connection_id,
                                                                    access_level:
                                                                        sc.access_level,
                                                                })
                                                            )}
                                                            mcpPrivileges={
                                                                token.scope?.mcp_privileges?.some(
                                                                    (id: number) =>
                                                                        getMcpPrivilegeName(
                                                                            id
                                                                        ) === '*'
                                                                )
                                                                    ? [
                                                                          'All MCP Privileges',
                                                                      ]
                                                                    : token.scope?.mcp_privileges?.map(
                                                                          (
                                                                              id: number
                                                                          ) =>
                                                                              getMcpPrivilegeName(
                                                                                  id
                                                                              )
                                                                      )
                                                            }
                                                            adminPermissions={
                                                                token.scope?.admin_permissions?.includes(
                                                                    '*'
                                                                )
                                                                    ? [
                                                                          'All Admin Permissions',
                                                                      ]
                                                                    : token.scope
                                                                          ?.admin_permissions
                                                            }
                                                            isSuperuser={true}
                                                            isDark={
                                                                theme.palette.mode ===
                                                                'dark'
                                                            }
                                                            connections={connections}
                                                        />
                                                    ) : (
                                                        <Typography
                                                            color="text.secondary"
                                                            sx={{ fontSize: '1rem' }}
                                                        >
                                                            Unrestricted - this token
                                                            has access to all
                                                            permissions granted to its
                                                            owner.
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
                                <Typography color="text.secondary">
                                    No tokens found.
                                </Typography>
                            </TableCell>
                        </TableRow>
                    )}
                </TableBody>
            </Table>
        </TableContainer>
    );
};

export default TokensTable;
