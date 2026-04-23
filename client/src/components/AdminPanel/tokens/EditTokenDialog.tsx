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
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    TextField,
    Box,
    IconButton,
    Autocomplete,
    Alert,
    Typography,
    CircularProgress,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Close as CloseIcon } from '@mui/icons-material';
import {
    dialogTitleSx,
    dialogActionsSx,
    subsectionLabelSx,
    getContainedButtonSx,
} from '../styles';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import ScopeMultiSelect from './ScopeMultiSelect';
import ConnectionScopeTable from './ConnectionScopeTable';
import { ALL_MCP_OPTION, ALL_ADMIN_OPTION } from './tokenTypes';
import type {
    Token,
    Connection,
    ScopedConnection,
    McpPrivilege,
    McpPrivilegeOption,
    AdminPermissionEntry,
    AdminPermissionOption,
} from './tokenTypes';

export interface EditTokenDialogProps {
    /** Whether the dialog is open. */
    open: boolean;
    /** Handler to close the dialog. */
    onClose: () => void;
    /** Handler to save the scope changes. */
    onSubmit: () => void;
    /** Whether the save operation is in progress. */
    loading: boolean;
    /** Error message to display, if any. */
    error: string | null;
    /** The token being edited. */
    token: Token | null;

    // Scope fields
    /** Available connections filtered by owner's privileges. */
    availableConnections: Connection[];
    /** Currently selected scoped connections. */
    scopedConnections: ScopedConnection[];
    /** Handler for scoped connections changes. */
    onScopedConnectionsChange: (connections: ScopedConnection[]) => void;
    /** Map of connection ID to owner's max access level. */
    ownerConnectionLevels: Record<number, string>;
    /** Whether the owner is a superuser. */
    ownerIsSuperuser: boolean;

    /** Available MCP privileges filtered by owner's privileges. */
    availableMcpPrivileges: McpPrivilege[];
    /** Currently selected MCP privileges. */
    selectedMcpPrivileges: McpPrivilegeOption[];
    /** Handler for MCP privilege changes. */
    onMcpPrivilegesChange: (privileges: McpPrivilegeOption[]) => void;

    /** Available admin permissions filtered by owner's privileges. */
    availableAdminPermissions: AdminPermissionEntry[];
    /** Currently selected admin permissions. */
    selectedAdminPermissions: AdminPermissionOption[];
    /** Handler for admin permission changes. */
    onAdminPermissionsChange: (permissions: AdminPermissionOption[]) => void;
}

/**
 * Dialog for editing an existing token's scope restrictions.
 */
const EditTokenDialog: React.FC<EditTokenDialogProps> = ({
    open,
    onClose,
    onSubmit,
    loading,
    error,
    token,
    availableConnections,
    scopedConnections,
    onScopedConnectionsChange,
    ownerConnectionLevels,
    ownerIsSuperuser,
    availableMcpPrivileges,
    selectedMcpPrivileges,
    onMcpPrivilegesChange,
    availableAdminPermissions,
    selectedAdminPermissions,
    onAdminPermissionsChange,
}) => {
    const theme = useTheme();
    const containedButtonSx = getContainedButtonSx(theme);

    // Handle adding a connection to the scope
    const handleAddConnection = (connection: Connection | null) => {
        if (connection) {
            const maxLevel = ownerConnectionLevels[connection.id] || 'read_write';
            onScopedConnectionsChange([
                ...scopedConnections,
                {
                    id: connection.id,
                    name: connection.name,
                    access_level: maxLevel,
                },
            ]);
        }
    };

    // Filter out already-selected connections
    const availableConnectionOptions = availableConnections.filter(
        (c) => !scopedConnections.some((sc) => sc.id === c.id)
    );

    const tokenName = token?.name || token?.token_prefix || 'Token';

    return (
        <Dialog
            open={open}
            onClose={() => !loading && onClose()}
            maxWidth="sm"
            fullWidth
        >
            <DialogTitle
                sx={{ ...dialogTitleSx, display: 'flex', alignItems: 'center' }}
            >
                <Box sx={{ flex: 1 }}>Edit token: {tokenName}</Box>
                <IconButton
                    onClick={onClose}
                    size="small"
                    disabled={loading}
                    aria-label="close"
                >
                    <CloseIcon />
                </IconButton>
            </DialogTitle>
            <DialogContent>
                {error && (
                    <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>
                        {error}
                    </Alert>
                )}

                <Typography
                    variant="subtitle2"
                    sx={{ ...subsectionLabelSx, mb: 1, mt: 1 }}
                >
                    Connections
                </Typography>

                <Autocomplete<Connection>
                    options={availableConnectionOptions}
                    getOptionLabel={(option) => option.name || ''}
                    value={null}
                    onChange={(_e, value) => { handleAddConnection(value); }}
                    renderInput={(params) => (
                        <TextField
                            {...params}
                            label="Add Connection"
                            margin="dense"
                            placeholder="Select a connection to add..."
                            InputLabelProps={{
                                ...params.InputLabelProps,
                                shrink: true,
                            }}
                            sx={SELECT_FIELD_SX}
                        />
                    )}
                    disabled={loading}
                />

                <ConnectionScopeTable
                    connections={scopedConnections}
                    onAccessLevelChange={(id, level) => {
                        onScopedConnectionsChange(
                            scopedConnections.map((c) =>
                                c.id === id ? { ...c, access_level: level } : c
                            )
                        );
                    }}
                    onRemove={(id) => {
                        onScopedConnectionsChange(
                            scopedConnections.filter((c) => c.id !== id)
                        );
                    }}
                    ownerConnectionLevels={ownerConnectionLevels}
                    ownerIsSuperuser={ownerIsSuperuser}
                    disabled={loading}
                />

                <Typography
                    variant="subtitle2"
                    sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                >
                    MCP Privileges
                </Typography>

                <ScopeMultiSelect<McpPrivilegeOption>
                    label="Allowed MCP Privileges"
                    options={availableMcpPrivileges}
                    value={selectedMcpPrivileges}
                    onChange={onMcpPrivilegesChange}
                    getOptionLabel={(option) =>
                        option._isAll
                            ? 'All MCP Privileges'
                            : option.identifier || ''
                    }
                    allOption={ALL_MCP_OPTION}
                    disabled={loading}
                />

                <Typography
                    variant="subtitle2"
                    sx={{ ...subsectionLabelSx, mb: 1, mt: 2 }}
                >
                    Admin Permissions
                </Typography>

                <ScopeMultiSelect<AdminPermissionOption>
                    label="Allowed Admin Permissions"
                    options={availableAdminPermissions}
                    value={selectedAdminPermissions}
                    onChange={onAdminPermissionsChange}
                    getOptionLabel={(option) =>
                        option._isAll
                            ? 'All Admin Permissions'
                            : option.label || option.id || ''
                    }
                    allOption={ALL_ADMIN_OPTION}
                    disabled={loading}
                />
            </DialogContent>
            <DialogActions sx={dialogActionsSx}>
                <Button onClick={onClose} disabled={loading}>
                    Cancel
                </Button>
                <Button
                    onClick={onSubmit}
                    variant="contained"
                    disabled={loading}
                    sx={containedButtonSx}
                >
                    {loading ? (
                        <CircularProgress
                            size={20}
                            color="inherit"
                            aria-label="Saving"
                        />
                    ) : (
                        'Save'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default EditTokenDialog;
