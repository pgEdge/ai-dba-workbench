/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Header Component
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Styled to match pgEdge Cloud product aesthetics
 *
 *-------------------------------------------------------------------------
 */

import React, { useState } from 'react';
import { ThemeMode } from '../types/theme';
import {
    AppBar,
    Toolbar,
    Typography,
    IconButton,
    Box,
    Avatar,
    Menu,
    MenuItem,
    ListItemIcon,
    ListItemText,
    Divider,
    Tooltip,
    alpha,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    DarkMode as DarkModeIcon,
    LightMode as LightModeIcon,
    Logout as LogoutIcon,
    HelpOutline as HelpIcon,
    Settings as SettingsIcon,
} from '@mui/icons-material';
import logoLight from '../assets/images/logo-light.png';
import logoDark from '../assets/images/logo-dark.png';
import { useAuth } from '../contexts/AuthContext';
import { useMenu } from '../hooks/useMenu';
import HelpPanel from './HelpPanel';
import AdminPanel from './AdminPanel';

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const toolbarSx = { minHeight: { xs: 56, sm: 64 } };

const logoContainerSx = {
    display: 'flex',
    alignItems: 'center',
    flexGrow: 1,
    gap: 1.5,
};

const logoImgSx = { height: 28, width: 'auto' };

const actionsContainerSx = { display: 'flex', alignItems: 'center', gap: 0.5 };

const menuInfoBoxSx = { px: 2, py: 1.5 };

const getAppBarSx = (theme: Theme) => ({
    bgcolor: theme.palette.background.paper,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
});

const getDividerSx = (theme: Theme) => ({
    height: 24,
    alignSelf: 'center',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[600]
        : theme.palette.grey[200],
});

const getTitleSx = (theme: Theme) => ({
    fontWeight: 500,
    color: theme.palette.text.primary,
    letterSpacing: '-0.01em',
});

const getIconButtonSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[400]
        : theme.palette.grey[500],
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.primary.main, 0.08)
            : alpha(theme.palette.primary.main, 0.04),
        color: theme.palette.primary.main,
    },
});

const getUserAvatarButtonSx = (theme: Theme) => ({
    ml: 0.5,
    p: 0.5,
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.primary.main, 0.08)
            : alpha(theme.palette.primary.main, 0.04),
    },
});

const getAvatarSx = (theme: Theme) => ({
    width: 32,
    height: 32,
    bgcolor: theme.palette.primary.main,
    fontSize: '1rem',
    fontWeight: 600,
});

const getMenuPaperSx = (theme: Theme) => ({
    minWidth: 180,
    mt: 1,
    borderRadius: 1,
    border: '1px solid',
    borderColor: theme.palette.divider,
    boxShadow: theme.palette.mode === 'dark'
        ? '0 10px 15px -3px rgba(0, 0, 0, 0.3)'
        : '0 10px 15px -3px rgba(0, 0, 0, 0.1)',
});

const getSignedInLabelSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.grey[500]
        : theme.palette.text.disabled,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    fontWeight: 600,
    fontSize: '0.65rem',
});

const getUsernameSx = (theme: Theme) => ({
    fontWeight: 500,
    color: theme.palette.text.primary,
    mt: 0.25,
});

const getLogoutMenuItemSx = (theme: Theme) => ({
    mx: 1,
    my: 0.5,
    borderRadius: 1,
    color: theme.palette.error.main,
    '&:hover': {
        bgcolor: alpha(theme.palette.error.main, 0.08),
    },
});

const listItemIconSx = { color: 'inherit' };

const signOutTypographyProps = {
    fontSize: '1rem',
    fontWeight: 500,
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface HeaderProps {
    onToggleTheme: () => void;
    mode: ThemeMode;
    helpContext: string | null;
}

const Header: React.FC<HeaderProps> = ({ onToggleTheme, mode, helpContext }) => {
    const { user, logout, hasAnyAdminAccess } = useAuth();
    const userMenu = useMenu();
    const [helpOpen, setHelpOpen] = useState(false);
    const [adminOpen, setAdminOpen] = useState(false);

    const handleLogout = () => {
        userMenu.handleClose();
        logout();
    };

    const handleHelpOpen = () => {
        setHelpOpen(true);
    };

    const handleHelpClose = () => {
        setHelpOpen(false);
    };

    const getInitials = (name: string | undefined) => {
        if (!name) {return '?';}
        const parts = name.split(' ');
        if (parts.length === 1) {return parts[0].charAt(0).toUpperCase();}
        return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
    };

    const isDark = mode === 'dark';

    return (
        <>
            <AppBar
                position="static"
                elevation={0}
                sx={getAppBarSx}
            >
                <Toolbar sx={toolbarSx}>
                    {/* Logo and Title */}
                    <Box sx={logoContainerSx}>
                        <Box
                            component="img"
                            src={isDark ? logoDark : logoLight}
                            alt="pgEdge"
                            sx={logoImgSx}
                        />
                        <Divider
                            orientation="vertical"
                            flexItem
                            sx={getDividerSx}
                        />
                        <Typography
                            variant="subtitle1"
                            component="div"
                            sx={getTitleSx}
                        >
                            AI DBA Workbench
                        </Typography>
                    </Box>

                    {/* Action Icons */}
                    <Box sx={actionsContainerSx}>
                        {/* Theme Toggle */}
                        <Tooltip title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}>
                            <IconButton
                                onClick={onToggleTheme}
                                aria-label="toggle theme"
                                sx={getIconButtonSx}
                            >
                                {isDark ? <LightModeIcon /> : <DarkModeIcon />}
                            </IconButton>
                        </Tooltip>

                        {/* Admin Settings Button */}
                        {hasAnyAdminAccess && (
                            <Tooltip title="Administration">
                                <IconButton
                                    onClick={() => setAdminOpen(true)}
                                    aria-label="open administration"
                                    sx={getIconButtonSx}
                                >
                                    <SettingsIcon />
                                </IconButton>
                            </Tooltip>
                        )}

                        {/* Help Button */}
                        <Tooltip title="Help">
                            <IconButton
                                onClick={handleHelpOpen}
                                aria-label="open help"
                                sx={getIconButtonSx}
                            >
                                <HelpIcon />
                            </IconButton>
                        </Tooltip>

                        {/* User Avatar */}
                        {user && (
                            <Tooltip title={user.username}>
                                <IconButton
                                    onClick={userMenu.handleOpen}
                                    size="small"
                                    aria-label="user menu"
                                    aria-controls="user-menu"
                                    aria-haspopup="true"
                                    sx={getUserAvatarButtonSx}
                                >
                                    <Avatar sx={getAvatarSx}>
                                        {getInitials(user.username)}
                                    </Avatar>
                                </IconButton>
                            </Tooltip>
                        )}
                    </Box>
                </Toolbar>
            </AppBar>

            {/* User Menu */}
            <Menu
                id="user-menu"
                anchorEl={userMenu.anchorEl}
                open={userMenu.open}
                onClose={userMenu.handleClose}
                anchorOrigin={{
                    vertical: 'bottom',
                    horizontal: 'right',
                }}
                transformOrigin={{
                    vertical: 'top',
                    horizontal: 'right',
                }}
                PaperProps={{
                    sx: getMenuPaperSx,
                }}
            >
                <Box sx={menuInfoBoxSx}>
                    <Typography
                        variant="caption"
                        sx={getSignedInLabelSx}
                    >
                        Signed in as
                    </Typography>
                    <Typography
                        variant="body2"
                        sx={getUsernameSx}
                    >
                        {user?.username}
                    </Typography>
                </Box>
                <Divider />
                <MenuItem
                    onClick={handleLogout}
                    sx={getLogoutMenuItemSx}
                >
                    <ListItemIcon sx={listItemIconSx}>
                        <LogoutIcon fontSize="small" />
                    </ListItemIcon>
                    <ListItemText
                        primary="Sign out"
                        primaryTypographyProps={signOutTypographyProps}
                    />
                </MenuItem>
            </Menu>

            {/* Help Panel */}
            <HelpPanel open={helpOpen} onClose={handleHelpClose} helpContext={helpContext} />

            {/* Admin Panel */}
            <AdminPanel open={adminOpen} onClose={() => setAdminOpen(false)} mode={mode} />
        </>
    );
};

export default Header;
