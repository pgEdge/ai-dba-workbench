/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Add Menu Component
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

/**
 * A dropdown menu for adding servers or cluster groups.
 *
 * @param {Object} props
 * @param {Element} props.anchorEl - Element to anchor menu to
 * @param {boolean} props.open - Controls menu visibility
 * @param {function} props.onClose - Called when menu should close
 * @param {function} props.onAddServer - Called when "Add Server" is selected
 * @param {function} props.onAddGroup - Called when "Add Cluster Group" is selected
 */
const AddMenu = ({
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
