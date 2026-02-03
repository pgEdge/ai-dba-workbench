/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Add Menu Component
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import {
    Menu,
    MenuItem,
    ListItemIcon,
    ListItemText,
    Divider,
} from '@mui/material';
import {
    Storage as StorageIcon,
    Folder as FolderIcon,
} from '@mui/icons-material';

interface AddMenuProps {
    anchorEl: HTMLElement | null;
    open: boolean;
    onClose: () => void;
    onAddServer?: () => void;
    onAddGroup?: () => void;
}

/**
 * A dropdown menu for adding servers or cluster groups.
 */
const AddMenu: React.FC<AddMenuProps> = ({
    anchorEl,
    open,
    onClose,
    onAddServer,
    onAddGroup,
}) => {
    const handleAddServer = () => {
        if (onAddServer) {
            onAddServer();
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
            <MenuItem onClick={handleAddServer}>
                <ListItemIcon>
                    <StorageIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText primary="Add Server" />
            </MenuItem>
            <Divider />
            <MenuItem onClick={handleAddGroup}>
                <ListItemIcon>
                    <FolderIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText primary="Add Cluster Group" />
            </MenuItem>
        </Menu>
    );
};

export default AddMenu;
