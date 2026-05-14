/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Add Menu Component
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { Menu, MenuItem, ListItemIcon, ListItemText, Divider } from '@mui/material';
import { Storage as StorageIcon, Folder as FolderIcon, Hub as HubIcon } from '@mui/icons-material';
import { useAuth } from '../contexts/useAuth';

interface AddMenuProps {
    anchorEl: HTMLElement | null;
    open: boolean;
    onClose: () => void;
    onAddServer?: () => void;
    onAddCluster?: () => void;
    onAddGroup?: () => void;
}

/**
 * A dropdown menu for adding servers or cluster groups.
 *
 * The "Add Server", "Add Cluster", and "Add Cluster Group" entries
 * require the `manage_connections` admin permission and are omitted
 * entirely for users that lack it. The server enforces the same
 * restriction with a 403 response, so hiding the menu items keeps
 * unauthorized users from seeing actions they cannot perform.
 */
const AddMenu: React.FC<AddMenuProps> = ({
    anchorEl,
    open,
    onClose,
    onAddServer,
    onAddCluster,
    onAddGroup,
}) => {
    const { hasPermission } = useAuth();
    const canManageConnections = hasPermission('manage_connections');

    const handleAddServer = () => {
        if (onAddServer) {
            onAddServer();
        }
        onClose();
    };

    const handleAddCluster = () => {
        if (onAddCluster) {
            onAddCluster();
        }
        onClose();
    };

    const handleAddGroup = () => {
        if (onAddGroup) {
            onAddGroup();
        }
        onClose();
    };

    return (
        <Menu
            anchorEl={anchorEl}
            open={open}
            onClose={onClose}
            anchorOrigin={{
                vertical: 'bottom',
                horizontal: 'left',
            }}
            transformOrigin={{
                vertical: 'top',
                horizontal: 'left',
            }}
            PaperProps={{
                sx: {
                    minWidth: 180,
                    mt: 0.5,
                },
            }}
        >
            {canManageConnections && (
                <MenuItem onClick={handleAddServer}>
                    <ListItemIcon>
                        <StorageIcon fontSize="small" />
                    </ListItemIcon>
                    <ListItemText primary="Add Server" />
                </MenuItem>
            )}
            {canManageConnections && (
                <MenuItem onClick={handleAddCluster}>
                    <ListItemIcon>
                        <HubIcon fontSize="small" />
                    </ListItemIcon>
                    <ListItemText primary="Add Cluster" />
                </MenuItem>
            )}
            {canManageConnections && <Divider />}
            {canManageConnections && (
                <MenuItem onClick={handleAddGroup}>
                    <ListItemIcon>
                        <FolderIcon fontSize="small" />
                    </ListItemIcon>
                    <ListItemText primary="Add Cluster Group" />
                </MenuItem>
            )}
        </Menu>
    );
};

export default AddMenu;
