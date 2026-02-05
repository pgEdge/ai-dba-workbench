/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo } from 'react';
import {
    Dialog,
    AppBar,
    Toolbar,
    IconButton,
    Typography,
    Box,
    Slide,
    List,
    ListItemButton,
    ListItemText,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import { subsectionLabelSx } from './styles';
import AdminUsers from './AdminUsers';
import AdminGroups from './AdminGroups';
import AdminPermissions from './AdminPermissions';
import AdminTokenScopes from './AdminTokenScopes';
import AdminProbes from './AdminProbes';
import AdminAlertRules from './AdminAlertRules';
import AdminEmailChannels from './AdminEmailChannels';

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

interface NavItem {
    id: string;
    label: string;
    permission: string;
    Component: React.FC<any>;
}

interface NavSection {
    category: string;
    items: NavItem[];
}

const NAV_SECTIONS: NavSection[] = [
    {
        category: 'Security',
        items: [
            { id: 'users', label: 'Users', permission: 'manage_users', Component: AdminUsers },
            { id: 'groups', label: 'Groups', permission: 'manage_groups', Component: AdminGroups },
            { id: 'permissions', label: 'Permissions', permission: 'manage_permissions', Component: AdminPermissions },
            { id: 'token_scopes', label: 'Tokens', permission: 'manage_token_scopes', Component: AdminTokenScopes },
        ],
    },
    {
        category: 'Monitoring',
        items: [
            { id: 'probes', label: 'Probe Defaults', permission: 'manage_probes', Component: AdminProbes },
            { id: 'alert_rules', label: 'Alert Defaults', permission: 'manage_alert_rules', Component: AdminAlertRules },
        ],
    },
    {
        category: 'Notifications',
        items: [
            { id: 'email_channels', label: 'Email Channels', permission: 'manage_notification_channels', Component: AdminEmailChannels },
        ],
    },
];

interface AdminPanelProps {
    open: boolean;
    onClose: () => void;
    mode: string;
}

const AdminPanel: React.FC<AdminPanelProps> = ({ open, onClose, mode }) => {
    const theme = useTheme();
    const { user, hasPermission } = useAuth();
    const [activeId, setActiveId] = useState<string>('');

    // Filter sections and items based on user permissions
    const visibleSections = useMemo(() => {
        return NAV_SECTIONS.map((section) => ({
            ...section,
            items: section.items.filter((item) => {
                if (item.permission === null) {
                    return !!user?.isSuperuser;
                }
                return hasPermission(item.permission);
            }),
        })).filter((section) => section.items.length > 0);
    }, [user?.isSuperuser, hasPermission]);

    // Flat list of all visible items for lookup
    const allVisibleItems = useMemo(() => {
        return visibleSections.flatMap((section) => section.items);
    }, [visibleSections]);

    // Reset to first visible item when reopened
    const handleEnter = () => {
        if (allVisibleItems.length > 0) {
            setActiveId(allVisibleItems[0].id);
        }
    };

    // Find the active component based on the selected id
    const activeItem = allVisibleItems.find((item) => item.id === activeId);
    const ActiveComponent = activeItem?.Component;

    return (
        <Dialog
            fullScreen
            open={open}
            onClose={onClose}
            TransitionComponent={Transition}
            TransitionProps={{ onEnter: handleEnter }}
        >
            <AppBar
                position="static"
                elevation={0}
                sx={{
                    bgcolor: theme.palette.background.paper,
                    borderBottom: '1px solid',
                    borderColor: theme.palette.divider,
                }}
            >
                <Toolbar>
                    <IconButton
                        edge="start"
                        color="inherit"
                        onClick={onClose}
                        aria-label="close administration"
                        sx={{
                            color: theme.palette.text.secondary,
                            mr: 2,
                        }}
                    >
                        <CloseIcon />
                    </IconButton>
                    <Typography
                        variant="h6"
                        component="div"
                        sx={{
                            flexGrow: 1,
                            fontWeight: 600,
                            color: theme.palette.text.primary,
                        }}
                    >
                        Administration
                    </Typography>
                </Toolbar>
            </AppBar>
            <Box sx={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
                {/* Sidebar navigation */}
                <Box
                    sx={{
                        width: 240,
                        flexShrink: 0,
                        borderRight: '1px solid',
                        borderColor: theme.palette.divider,
                        bgcolor: theme.palette.background.paper,
                        overflowY: 'auto',
                    }}
                >
                    <List component="nav" disablePadding sx={{ py: 1 }}>
                        {visibleSections.map((section) => (
                            <React.Fragment key={section.category}>
                                <Typography
                                    sx={{
                                        ...subsectionLabelSx,
                                        px: 2,
                                        pt: 2,
                                        pb: 0.5,
                                    }}
                                >
                                    {section.category}
                                </Typography>
                                {section.items.map((item) => {
                                    const isSelected = item.id === activeId;
                                    return (
                                        <ListItemButton
                                            key={item.id}
                                            selected={isSelected}
                                            onClick={() => setActiveId(item.id)}
                                            sx={{
                                                borderRadius: 1,
                                                mx: 1,
                                                bgcolor: isSelected
                                                    ? theme.palette.action.selected
                                                    : 'transparent',
                                            }}
                                        >
                                            <ListItemText
                                                primary={item.label}
                                                primaryTypographyProps={{
                                                    fontSize: '0.875rem',
                                                }}
                                            />
                                        </ListItemButton>
                                    );
                                })}
                            </React.Fragment>
                        ))}
                    </List>
                </Box>
                {/* Content area */}
                <Box
                    sx={{
                        flex: 1,
                        overflow: 'auto',
                        bgcolor: theme.palette.background.default,
                        p: 3,
                    }}
                >
                    {ActiveComponent && <ActiveComponent mode={mode} />}
                </Box>
            </Box>
        </Dialog>
    );
};

export default AdminPanel;
