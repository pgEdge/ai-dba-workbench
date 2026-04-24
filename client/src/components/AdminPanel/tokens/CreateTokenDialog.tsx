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
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    TextField,
    MenuItem,
    Autocomplete,
    Alert,
    Typography,
    CircularProgress,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    dialogTitleSx,
    dialogActionsSx,
    subsectionLabelSx,
    getContainedButtonSx,
} from '../styles';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import ScopeMultiSelect from './ScopeMultiSelect';
import ConnectionScopeTable from './ConnectionScopeTable';
import {
    EXPIRY_OPTIONS,
    ALL_MCP_OPTION,
    ALL_ADMIN_OPTION,
} from './tokenTypes';
import type {
    User,
    Connection,
    ScopedConnection,
    McpPrivilege,
    McpPrivilegeOption,
    AdminPermissionEntry,
    AdminPermissionOption,
} from './tokenTypes';

export interface CreateTokenDialogProps {
    /** Whether the dialog is open. */
    open: boolean;
    /** Handler to close the dialog. */
    onClose: () => void;
    /** Handler to create the token. */
    onSubmit: () => void;
    /** Whether the create operation is in progress. */
    loading: boolean;
    /** Error message to display, if any. */
    error: string | null;

    // Token fields
    /** The token annotation/name. */
    annotation: string;
    /** Handler for annotation changes. */
    onAnnotationChange: (value: string) => void;
    /** The selected owner user. */
    owner: User | null;
    /** Handler for owner changes. */
    onOwnerChange: (user: User | null) => void;
    /** The list of available users. */
    users: User[];
    /** The selected expiry duration. */
    expiry: string;
    /** Handler for expiry changes. */
    onExpiryChange: (value: string) => void;

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
 * Dialog for creating a new API token with optional scope restrictions.
 */
const CreateTokenDialog: React.FC<CreateTokenDialogProps> = ({
    open,
    onClose,
    onSubmit,
    loading,
    error,
    annotation,
    onAnnotationChange,
    owner,
    onOwnerChange,
    users,
    expiry,
    onExpiryChange,
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

    const canSubmit = owner && annotation.trim();

    return (
        <Dialog
            open={open}
            onClose={() => void (!loading && onClose())}
            maxWidth="xs"
            fullWidth
        >
            <DialogTitle sx={dialogTitleSx}>Create token</DialogTitle>
            <DialogContent>
                {error && (
                    <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }}>
                        {error}
                    </Alert>
                )}
                <TextField
                    fullWidth
                    label="Name"
                    value={annotation}
                    onChange={(e) => { onAnnotationChange(e.target.value); }}
                    disabled={loading}
                    margin="dense"
                    required
                    InputLabelProps={{ shrink: true }}
                    sx={SELECT_FIELD_SX}
                />
                <Autocomplete<User>
                    options={users}
                    getOptionLabel={(option) => option.username || ''}
                    isOptionEqualToValue={(option, value) =>
                        option.id === value.id
                    }
                    value={owner}
                    onChange={(_e, value) => { onOwnerChange(value); }}
                    renderInput={(params) => (
                        <TextField
                            {...params}
                            label="Owner"
                            margin="dense"
                            required
                            InputLabelProps={{
                                ...params.InputLabelProps,
                                shrink: true,
                            }}
                            sx={SELECT_FIELD_SX}
                        />
                    )}
                    disabled={loading}
                />
                <TextField
                    fullWidth
                    select
                    label="Expiry"
                    value={expiry}
                    onChange={(e) => { onExpiryChange(e.target.value); }}
                    disabled={loading}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={SELECT_FIELD_SX}
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
                    disabled={loading || !canSubmit}
                    sx={containedButtonSx}
                >
                    {loading ? (
                        <CircularProgress
                            size={20}
                            color="inherit"
                            aria-label="Creating"
                        />
                    ) : (
                        'Create'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default CreateTokenDialog;
