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
import { useTheme } from '@mui/material/styles';
import { TransitionProps } from '@mui/material/transitions';
import {
    Close as CloseIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import AdminUsers from './AdminUsers';
import AdminGroups from './AdminGroups';
import AdminPermissions from './AdminPermissions';
import AdminTokenScopes from './AdminTokenScopes';
import AdminProbes from './AdminProbes';
import AdminAlertRules from './AdminAlertRules';

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

// Tab definitions with permission requirements
const TAB_DEFS = [
    { id: 'users', label: 'Users', permission: 'manage_users', Component: AdminUsers },
    { id: 'groups', label: 'Groups', permission: 'manage_groups', Component: AdminGroups },
    { id: 'permissions', label: 'Permissions', permission: 'manage_permissions', Component: AdminPermissions },
    { id: 'token_scopes', label: 'Token Scopes', permission: 'manage_token_scopes', Component: AdminTokenScopes },
    { id: 'probes', label: 'Probe Defaults', permission: 'manage_probes', Component: AdminProbes },
    { id: 'alert_rules', label: 'Alert Defaults', permission: 'manage_alert_rules', Component: AdminAlertRules },
];

interface AdminPanelProps {
    open: boolean;
    onClose: () => void;
    mode: string;
}

const AdminPanel: React.FC<AdminPanelProps> = ({ open, onClose, mode }) => {
    const theme = useTheme();
    const { user, hasPermission } = useAuth();
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

    const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
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
                            color: theme.palette.text.secondary,
                            '&.Mui-selected': {
                                color: theme.palette.primary.main,
                                fontWeight: 600,
                            },
                        },
                        '& .MuiTabs-indicator': {
                            backgroundColor: theme.palette.primary.main,
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
                    bgcolor: theme.palette.background.default,
                    p: 3,
                }}
            >
                {ActiveComponent && <ActiveComponent mode={mode} />}
            </Box>
        </Dialog>
    );
};

export default AdminPanel;
