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
    Box,
    Typography,
    Button,
    CircularProgress,
    Autocomplete,
    TextField,
    MenuItem,
    IconButton,
    Divider,
    List,
    ListItem,
    ListItemText,
    ListItemSecondaryAction,
    Chip,
} from '@mui/material';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import { SELECT_FIELD_DEFAULT_BG_SX } from '../shared/formStyles';
import type { ClusterServerInfo } from '../ServerDialog/ServerDialog.types';
import type { UnassignedConnection, RoleOption } from './topologyHelpers';

export interface ServerManagementSectionProps {
    unassignedConnections: UnassignedConnection[];
    selectedConnection: UnassignedConnection | null;
    selectedRole: string;
    roleOptions: RoleOption[];
    addingServer: boolean;
    clusterServers: ClusterServerInfo[];
    onConnectionChange: (connection: UnassignedConnection | null) => void;
    onRoleChange: (role: string) => void;
    onAddServer: () => void;
    onRemoveTarget: (server: ClusterServerInfo) => void;
}

/**
 * ServerManagementSection provides the UI for adding servers to a cluster
 * and displaying/removing current cluster members.
 */
const ServerManagementSection: React.FC<ServerManagementSectionProps> = ({
    unassignedConnections,
    selectedConnection,
    selectedRole,
    roleOptions,
    addingServer,
    clusterServers,
    onConnectionChange,
    onRoleChange,
    onAddServer,
    onRemoveTarget,
}) => {
    return (
        <Box sx={{ mb: 3 }}>
            <Typography
                variant="subtitle2"
                sx={{
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    fontSize: '0.875rem',
                    letterSpacing: '0.05em',
                    mb: 1.5,
                }}
            >
                Add Server
            </Typography>
            <Box
                sx={{
                    p: 2,
                    border: '1px solid',
                    borderColor: 'divider',
                    borderRadius: 1.5,
                    bgcolor: 'background.paper',
                }}
            >
                <Box
                    sx={{
                        display: 'flex',
                        gap: 1.5,
                        alignItems: 'center',
                    }}
                >
                    <Autocomplete
                        options={unassignedConnections}
                        getOptionLabel={(option) =>
                            `${option.name} (${option.host}:${option.port})`
                        }
                        value={selectedConnection}
                        onChange={(_, val) => { onConnectionChange(val); }}
                        renderInput={(params) => (
                            <TextField
                                {...params}
                                label="Server"
                                margin="dense"
                                placeholder="Search unassigned servers..."
                                InputLabelProps={{
                                    ...params.InputLabelProps,
                                    shrink: true,
                                }}
                                sx={SELECT_FIELD_DEFAULT_BG_SX}
                            />
                        )}
                        sx={{ flex: 2 }}
                        disabled={addingServer}
                        isOptionEqualToValue={(a, b) => a.id === b.id}
                    />
                    {roleOptions.length > 0 && (
                        <TextField
                            select
                            margin="dense"
                            sx={{ flex: 1, minWidth: 160, ...SELECT_FIELD_DEFAULT_BG_SX }}
                            disabled={addingServer}
                            label="Role"
                            value={selectedRole}
                            onChange={(e) => { onRoleChange(e.target.value); }}
                            InputLabelProps={{ shrink: true }}
                        >
                            {roleOptions.map((r) => (
                                <MenuItem key={r.value} value={r.value}>
                                    {r.label}
                                </MenuItem>
                            ))}
                        </TextField>
                    )}
                    <Button
                        variant="outlined"
                        startIcon={<AddIcon />}
                        onClick={onAddServer}
                        disabled={!selectedConnection || addingServer}
                        aria-label="Add server"
                        sx={{
                            textTransform: 'none',
                            whiteSpace: 'nowrap',
                            height: 40,
                        }}
                    >
                        {addingServer ? (
                            <CircularProgress
                                size={18}
                                sx={{ color: 'inherit' }}
                            />
                        ) : (
                            'Add'
                        )}
                    </Button>
                </Box>

                {/* Current servers list with remove buttons */}
                {clusterServers.length > 0 && (
                    <Box sx={{ mt: 2 }}>
                        <Divider sx={{ mb: 1 }} />
                        <List dense disablePadding>
                            {clusterServers.map((server) => (
                                <ListItem
                                    key={server.id}
                                    disableGutters
                                    sx={{ pr: 6 }}
                                >
                                    <ListItemText
                                        primary={
                                            <Box
                                                sx={{
                                                    display: 'flex',
                                                    alignItems: 'center',
                                                    gap: 1,
                                                }}
                                            >
                                                <Typography
                                                    variant="body2"
                                                    sx={{ fontWeight: 500 }}
                                                >
                                                    {server.name}
                                                </Typography>
                                                {server.role && (
                                                    <Chip
                                                        label={server.role
                                                            .replace(/_/g, ' ')
                                                            .replace(
                                                                /\b\w/g,
                                                                (c) => c.toUpperCase(),
                                                            )}
                                                        size="small"
                                                        variant="outlined"
                                                        sx={{
                                                            height: 20,
                                                            fontSize: '0.7rem',
                                                        }}
                                                    />
                                                )}
                                            </Box>
                                        }
                                        secondary={`${server.host}:${server.port}`}
                                    />
                                    <ListItemSecondaryAction>
                                        <IconButton
                                            edge="end"
                                            size="small"
                                            onClick={() => { onRemoveTarget(server); }}
                                            aria-label={`Remove ${server.name} from cluster`}
                                            sx={{
                                                color: 'text.disabled',
                                                '&:hover': {
                                                    color: 'error.main',
                                                },
                                            }}
                                        >
                                            <DeleteIcon fontSize="small" />
                                        </IconButton>
                                    </ListItemSecondaryAction>
                                </ListItem>
                            ))}
                        </List>
                    </Box>
                )}
            </Box>
        </Box>
    );
};

export default ServerManagementSection;
