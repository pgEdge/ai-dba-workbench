/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
    Tabs,
    Tab,
    Box,
    Slide,
} from '@mui/material';
import {
    Close as CloseIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import AdminUsers from './AdminUsers';
import AdminGroups from './AdminGroups';
import AdminPrivileges from './AdminPrivileges';
import AdminPermissions from './AdminPermissions';
import AdminTokenScopes from './AdminTokenScopes';

// Cyan accent color used throughout pgEdge UI
const ACCENT_COLOR = '#15AABF';

const Transition = React.forwardRef(function Transition(props, ref) {
    return <Slide direction="up" ref={ref} {...props} />;
});

// Tab definitions with permission requirements
const TAB_DEFS = [
    { id: 'users', label: 'Users', permission: 'manage_users', Component: AdminUsers },
    { id: 'groups', label: 'Groups', permission: 'manage_groups', Component: AdminGroups },
    { id: 'privileges', label: 'Privileges', permission: 'manage_privileges', Component: AdminPrivileges },
    { id: 'permissions', label: 'Permissions', permission: null, Component: AdminPermissions }, // superuser only
    { id: 'token_scopes', label: 'Token Scopes', permission: 'manage_token_scopes', Component: AdminTokenScopes },
];

const AdminPanel = ({ open, onClose, mode }) => {
    const { user, hasPermission } = useAuth();
    const isDark = mode === 'dark';
    const [activeTab, setActiveTab] = useState(0);

    // Filter tabs based on the user permissions
    const visibleTabs = useMemo(() => {
        return TAB_DEFS.filter((tab) => {
            // The permissions tab is superuser-only
            if (tab.permission === null) {
                return !!user?.isSuperuser;
            }
            return hasPermission(tab.permission);
        });
    }, [user?.isSuperuser, hasPermission]);

    // Reset to first tab when reopened
    const handleEnter = () => {
        setActiveTab(0);
    };

    const handleTabChange = (event, newValue) => {
        setActiveTab(newValue);
    };

    const ActiveComponent = visibleTabs[activeTab]?.Component;

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
                    bgcolor: isDark ? '#1E293B' : '#FFFFFF',
                    borderBottom: '1px solid',
                    borderColor: isDark ? '#334155' : '#E5E7EB',
                }}
            >
                <Toolbar>
                    <IconButton
                        edge="start"
                        color="inherit"
                        onClick={onClose}
                        aria-label="close administration"
                        sx={{
                            color: isDark ? '#94A3B8' : '#6B7280',
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
                            color: isDark ? '#F1F5F9' : '#1F2937',
                        }}
                    >
                        Administration
                    </Typography>
                </Toolbar>
                <Tabs
                    value={activeTab}
                    onChange={handleTabChange}
                    variant="scrollable"
                    scrollButtons="auto"
                    sx={{
                        px: 2,
                        '& .MuiTab-root': {
                            textTransform: 'none',
                            fontWeight: 500,
                            fontSize: '0.875rem',
                            color: isDark ? '#94A3B8' : '#6B7280',
                            '&.Mui-selected': {
                                color: ACCENT_COLOR,
                                fontWeight: 600,
                            },
                        },
                        '& .MuiTabs-indicator': {
                            backgroundColor: ACCENT_COLOR,
                        },
                    }}
                >
                    {visibleTabs.map((tab) => (
                        <Tab key={tab.id} label={tab.label} />
                    ))}
                </Tabs>
            </AppBar>
            <Box
                sx={{
                    flex: 1,
                    overflow: 'auto',
                    bgcolor: isDark ? '#0F172A' : '#F8FAFC',
                    p: 3,
                }}
            >
                {ActiveComponent && <ActiveComponent mode={mode} />}
            </Box>
        </Dialog>
    );
};

export default AdminPanel;
