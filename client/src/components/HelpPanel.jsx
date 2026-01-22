/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Help Panel
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useRef } from 'react';
import {
    Drawer,
    Box,
    Typography,
    IconButton,
    Divider,
    List,
    ListItemButton,
    ListItemIcon,
    ListItemText,
    alpha,
    Breadcrumbs,
    Link,
    Chip,
} from '@mui/material';
import {
    Close as CloseIcon,
    Home as HomeIcon,
    AccountTree as NavigatorIcon,
    Dashboard as StatusIcon,
    NotificationsActive as AlertsIcon,
    Storage as ServerIcon,
    Settings as SettingsIcon,
    ChevronRight as ChevronIcon,
    ArrowBack as BackIcon,
    Palette as ThemeIcon,
    DragIndicator as DragIcon,
    Search as SearchIcon,
    Warning as WarningIcon,
    CheckCircleOutline as AckIcon,
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Folder as GroupIcon,
    Dns as ClusterIcon,
    Star as PrimaryIcon,
    Hub as SpockIcon,
} from '@mui/icons-material';
import { CLIENT_VERSION } from '../lib/version';

// Help page identifiers
const HELP_PAGES = {
    overview: 'overview',
    navigator: 'navigator',
    statusPanel: 'statusPanel',
    alerts: 'alerts',
    serverManagement: 'serverManagement',
    settings: 'settings',
};

// Map context to help page
const contextToPage = (context) => {
    if (!context) return HELP_PAGES.overview;

    switch (context) {
        case 'navigator':
        case 'cluster':
        case 'group':
            return HELP_PAGES.navigator;
        case 'server':
        case 'status':
            return HELP_PAGES.statusPanel;
        case 'alerts':
        case 'alert':
            return HELP_PAGES.alerts;
        case 'serverDialog':
        case 'addServer':
        case 'editServer':
            return HELP_PAGES.serverManagement;
        case 'settings':
        case 'theme':
            return HELP_PAGES.settings;
        default:
            return HELP_PAGES.overview;
    }
};

/**
 * HelpNavItem - Navigation item in the help sidebar
 */
const HelpNavItem = ({ icon: Icon, label, pageId, currentPage, onClick, isDark }) => {
    const isActive = currentPage === pageId;

    return (
        <ListItemButton
            onClick={() => onClick(pageId)}
            sx={{
                borderRadius: 1,
                mb: 0.5,
                py: 0.75,
                bgcolor: isActive
                    ? (isDark ? alpha('#15AABF', 0.15) : alpha('#15AABF', 0.1))
                    : 'transparent',
                '&:hover': {
                    bgcolor: isActive
                        ? (isDark ? alpha('#15AABF', 0.2) : alpha('#15AABF', 0.15))
                        : (isDark ? alpha('#64748B', 0.1) : alpha('#64748B', 0.08)),
                },
            }}
        >
            <ListItemIcon sx={{ minWidth: 32 }}>
                <Icon
                    sx={{
                        fontSize: 18,
                        color: isActive ? '#15AABF' : 'text.secondary',
                    }}
                />
            </ListItemIcon>
            <ListItemText
                primary={label}
                primaryTypographyProps={{
                    fontSize: '0.8125rem',
                    fontWeight: isActive ? 600 : 500,
                    color: isActive ? '#15AABF' : 'text.primary',
                }}
            />
            {isActive && (
                <ChevronIcon sx={{ fontSize: 16, color: '#15AABF' }} />
            )}
        </ListItemButton>
    );
};

/**
 * SectionTitle - Section header within help content
 */
const SectionTitle = ({ children, icon: Icon, isDark }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5, mt: 3 }}>
        {Icon && <Icon sx={{ fontSize: 18, color: 'primary.main' }} />}
        <Typography
            variant="h6"
            sx={{
                fontSize: '1rem',
                fontWeight: 600,
                color: 'text.primary',
            }}
        >
            {children}
        </Typography>
    </Box>
);

/**
 * HelpTip - Highlighted tip or important note
 */
const HelpTip = ({ children, isDark }) => (
    <Box
        sx={{
            display: 'flex',
            gap: 1,
            p: 1.5,
            mt: 2,
            borderRadius: 1,
            bgcolor: isDark ? alpha('#15AABF', 0.08) : alpha('#15AABF', 0.06),
            border: '1px solid',
            borderColor: isDark ? alpha('#15AABF', 0.2) : alpha('#15AABF', 0.15),
        }}
    >
        <Typography
            sx={{
                fontSize: '0.8125rem',
                color: 'text.secondary',
                lineHeight: 1.5,
            }}
        >
            <strong style={{ color: '#15AABF' }}>Tip:</strong> {children}
        </Typography>
    </Box>
);

/**
 * FeatureItem - Single feature in a feature list
 */
const FeatureItem = ({ title, description }) => (
    <Box sx={{ mb: 1.5 }}>
        <Typography
            sx={{
                fontWeight: 600,
                fontSize: '0.8125rem',
                color: 'text.primary',
                mb: 0.25,
            }}
        >
            {title}
        </Typography>
        <Typography
            sx={{
                fontSize: '0.8125rem',
                color: 'text.secondary',
                lineHeight: 1.5,
            }}
        >
            {description}
        </Typography>
    </Box>
);

/**
 * KeyboardShortcut - Display a keyboard shortcut
 */
const KeyboardShortcut = ({ keys, description, isDark }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <Box sx={{ display: 'flex', gap: 0.5 }}>
            {keys.map((key, i) => (
                <Box
                    key={i}
                    sx={{
                        px: 0.75,
                        py: 0.25,
                        borderRadius: 0.5,
                        bgcolor: isDark ? alpha('#64748B', 0.3) : alpha('#64748B', 0.15),
                        border: '1px solid',
                        borderColor: isDark ? '#475569' : '#D1D5DB',
                        fontFamily: '"JetBrains Mono", monospace',
                        fontSize: '0.6875rem',
                        fontWeight: 600,
                        color: 'text.primary',
                    }}
                >
                    {key}
                </Box>
            ))}
        </Box>
        <Typography sx={{ fontSize: '0.8125rem', color: 'text.secondary' }}>
            {description}
        </Typography>
    </Box>
);

/**
 * Overview Page - Introduction to the workbench
 */
const OverviewPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Welcome to AI DBA Workbench
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            The AI DBA Workbench provides AI-powered tools for PostgreSQL database
            administration, monitoring, and optimization. This interface allows you to
            manage database servers organized in clusters and groups, monitor their
            health, and respond to alerts.
        </Typography>

        <SectionTitle isDark={isDark}>Key Features</SectionTitle>
        <FeatureItem
            title="Cluster Organization"
            description="Organize your database servers into logical clusters and groups for easier management."
        />
        <FeatureItem
            title="Real-time Monitoring"
            description="Monitor server status, connection health, and performance metrics in real-time."
        />
        <FeatureItem
            title="Alert Management"
            description="View and manage alerts for threshold violations and system issues. Acknowledge alerts with notes and track false positives."
        />
        <FeatureItem
            title="Replication Support"
            description="Support for binary replication (primary/standby), Spock multi-master replication, and logical replication."
        />

        <SectionTitle isDark={isDark}>Getting Started</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Use the <strong>Cluster Navigator</strong> on the left to browse your
            server hierarchy. Click on a server to view its details and alerts in
            the main panel. You can also view aggregated information for clusters
            or the entire estate.
        </Typography>

        <HelpTip isDark={isDark}>
            Click the estate header at the top of the navigator to see a summary of
            all servers across all clusters.
        </HelpTip>
    </Box>
);

/**
 * Navigator Page - Cluster Navigator help
 */
const NavigatorPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Cluster Navigator
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            The Cluster Navigator provides a hierarchical view of your database
            infrastructure, organized by groups, clusters, and individual servers.
        </Typography>

        <SectionTitle icon={GroupIcon} isDark={isDark}>Groups</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Groups are top-level containers for organizing related clusters. They
            might represent different environments (Production, Staging), regions,
            or business units.
        </Typography>

        <SectionTitle icon={ClusterIcon} isDark={isDark}>Clusters</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Clusters contain one or more database servers that work together. A
            cluster typically represents a replication set with a primary server
            and one or more standbys, or a Spock multi-master replication group.
        </Typography>

        <SectionTitle icon={ServerIcon} isDark={isDark}>Servers</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            Individual PostgreSQL server connections. Each server displays its role
            and current status:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Roles"
                description="Primary, Standby, Cascading standby, Spock node, Standalone, Publisher, or Subscriber."
            />
            <FeatureItem
                title="Status Indicators"
                description="Green checkmark for healthy servers, yellow warning with alert count for servers with active alerts, red error for offline servers."
            />
        </Box>

        <SectionTitle icon={SearchIcon} isDark={isDark}>Search</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Use the search bar at the top of the navigator to quickly filter
            servers by name. The search filters in real-time as you type.
        </Typography>

        <SectionTitle icon={DragIcon} isDark={isDark}>Drag and Drop</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Reorganize your server hierarchy by dragging servers between clusters
            or clusters between groups. Drag a server onto a cluster to move it,
            or drag a cluster onto a group to reassign it.
        </Typography>

        <HelpTip isDark={isDark}>
            Click on a cluster name to view aggregated status and alerts for all
            servers in that cluster.
        </HelpTip>
    </Box>
);

/**
 * Status Panel Page - Status panel and server details help
 */
const StatusPanelPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Status Panel
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            The Status Panel displays detailed information about your current
            selection, whether it's a single server, a cluster, or the entire
            estate.
        </Typography>

        <SectionTitle icon={ServerIcon} isDark={isDark}>Server View</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            When viewing a single server, you'll see:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Connection Details"
                description="Host, port, database name, and username for the connection."
            />
            <FeatureItem
                title="Server Information"
                description="PostgreSQL version, operating system, and server role."
            />
            <FeatureItem
                title="Replication Status"
                description="For Spock servers, displays the Spock version and node name."
            />
            <FeatureItem
                title="Active Alerts"
                description="Any alerts currently active for this server."
            />
        </Box>

        <SectionTitle icon={ClusterIcon} isDark={isDark}>Cluster View</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            When viewing a cluster, you'll see metric cards summarizing the health
            of all servers in the cluster:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Online Count"
                description="Number of servers currently online and healthy."
            />
            <FeatureItem
                title="Warning Count"
                description="Number of servers with active alerts."
            />
            <FeatureItem
                title="Offline Count"
                description="Number of servers that are unreachable."
            />
        </Box>

        <SectionTitle isDark={isDark}>Estate View</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            The estate view shows a summary of all servers across all clusters
            and groups, including total counts for clusters and groups.
        </Typography>

        <HelpTip isDark={isDark}>
            The status indicator in the header shows the overall health of your
            selection at a glance.
        </HelpTip>
    </Box>
);

/**
 * Alerts Page - Alert management help
 */
const AlertsPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Alert Management
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            Alerts notify you when database metrics exceed configured thresholds
            or when system issues are detected. You can view, acknowledge, and
            manage alerts from the Status Panel.
        </Typography>

        <SectionTitle icon={WarningIcon} isDark={isDark}>Alert Types</SectionTitle>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Connection Alerts"
                description="Triggered when connection counts exceed limits, including max connections, active connections, or idle connections."
            />
            <FeatureItem
                title="Performance Alerts"
                description="Triggered by high CPU usage, memory usage, or low disk space."
            />
            <FeatureItem
                title="Database Alerts"
                description="Triggered by deadlocks, high rollback rates, low cache hit ratios, or replication lag."
            />
            <FeatureItem
                title="Query Alerts"
                description="Triggered by long-running queries or blocked queries."
            />
        </Box>

        <SectionTitle isDark={isDark}>Alert Severity</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            Alerts are classified by severity:
        </Typography>
        <Box sx={{ display: 'flex', gap: 1, mb: 3 }}>
            <Chip
                label="Critical"
                size="small"
                sx={{
                    bgcolor: alpha('#EF4444', 0.15),
                    color: '#EF4444',
                    fontWeight: 600,
                    fontSize: '0.75rem',
                }}
            />
            <Chip
                label="Warning"
                size="small"
                sx={{
                    bgcolor: alpha('#F59E0B', 0.15),
                    color: '#F59E0B',
                    fontWeight: 600,
                    fontSize: '0.75rem',
                }}
            />
            <Chip
                label="Info"
                size="small"
                sx={{
                    bgcolor: alpha('#3B82F6', 0.15),
                    color: '#3B82F6',
                    fontWeight: 600,
                    fontSize: '0.75rem',
                }}
            />
        </Box>

        <SectionTitle icon={AckIcon} isDark={isDark}>Acknowledging Alerts</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            Click the checkmark icon on an alert to acknowledge it. You can:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Add a Reason"
                description="Explain why the alert is being acknowledged (e.g., 'Investigating', 'Known issue', 'Scheduled maintenance')."
            />
            <FeatureItem
                title="Mark as False Positive"
                description="Flag alerts that were triggered incorrectly to help improve alert accuracy over time."
            />
        </Box>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Acknowledged alerts are moved to a separate collapsed section below
            active alerts. Use the undo icon on an acknowledged alert to restore
            it to active status.
        </Typography>

        <SectionTitle isDark={isDark}>Threshold Information</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            For threshold-based alerts, you'll see the current value and the
            threshold that was exceeded (e.g., "108 exceeds threshold of 100").
            This helps you understand the severity of the issue.
        </Typography>

        <HelpTip isDark={isDark}>
            Acknowledged alerts remain visible but are separated from active
            alerts. The alert count in the navigator only includes active
            (non-acknowledged) alerts.
        </HelpTip>
    </Box>
);

/**
 * Server Management Page - Adding/editing servers help
 */
const ServerManagementPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Server Management
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            The AI DBA Workbench allows you to add, edit, and organize database
            server connections within your cluster hierarchy.
        </Typography>

        <SectionTitle icon={AddIcon} isDark={isDark}>Adding Servers</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            Click the + button in the navigator header or right-click on a
            cluster to add a new server. You'll need to provide:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Connection Name"
                description="A friendly name to identify this server in the navigator."
            />
            <FeatureItem
                title="Host & Port"
                description="The hostname or IP address and port number of the PostgreSQL server."
            />
            <FeatureItem
                title="Database & User"
                description="The database name and username for the connection."
            />
            <FeatureItem
                title="Password"
                description="The password for authentication. Stored securely in the datastore."
            />
            <FeatureItem
                title="Cluster Assignment"
                description="Select which cluster this server belongs to."
            />
        </Box>

        <SectionTitle icon={EditIcon} isDark={isDark}>Editing Servers</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Click the pencil icon that appears when hovering over a server name
            in the navigator to edit its configuration. You can modify connection
            details, move it to a different cluster, or update credentials.
        </Typography>

        <SectionTitle icon={DeleteIcon} isDark={isDark}>Deleting Servers</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Click the trash icon to remove a server connection. You'll be asked
            to confirm before the server is deleted. This removes the connection
            from the workbench but does not affect the actual database server.
        </Typography>

        <SectionTitle icon={GroupIcon} isDark={isDark}>Managing Groups</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Groups can be created from the + menu in the navigator header. Edit
            group names by clicking the pencil icon on the group header. Groups
            can contain multiple clusters.
        </Typography>

        <SectionTitle icon={PrimaryIcon} isDark={isDark}>Server Roles</SectionTitle>
        <Typography sx={{ color: 'text.secondary', mb: 2, lineHeight: 1.6 }}>
            Server roles are automatically detected based on the PostgreSQL
            configuration:
        </Typography>
        <Box sx={{ pl: 2, mb: 2 }}>
            <FeatureItem
                title="Primary"
                description="The main read-write server in a binary replication setup."
            />
            <FeatureItem
                title="Standby"
                description="A read-only replica receiving changes from a primary."
            />
            <FeatureItem
                title="Cascading"
                description="A standby that also replicates to other standbys."
            />
            <FeatureItem
                title="Spock Node"
                description="A node in a Spock multi-master replication cluster."
            />
            <FeatureItem
                title="Standalone"
                description="A server not participating in replication."
            />
            <FeatureItem
                title="Publisher/Subscriber"
                description="Servers using logical replication."
            />
        </Box>

        <HelpTip isDark={isDark}>
            Drag servers between clusters to reorganize your infrastructure
            without editing each server individually.
        </HelpTip>
    </Box>
);

/**
 * Settings Page - Theme and user settings help
 */
const SettingsPage = ({ isDark }) => (
    <Box>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 2 }}>
            Settings & Preferences
        </Typography>
        <Typography sx={{ color: 'text.secondary', mb: 3, lineHeight: 1.6 }}>
            Customize your AI DBA Workbench experience with the available
            settings and preferences.
        </Typography>

        <SectionTitle icon={ThemeIcon} isDark={isDark}>Theme</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Click the sun/moon icon in the header to toggle between light and
            dark mode. Your preference is saved automatically and persists
            across sessions.
        </Typography>

        <SectionTitle isDark={isDark}>User Account</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            Click your avatar in the header to access the user menu. From here
            you can see your username and sign out of the workbench.
        </Typography>

        <SectionTitle isDark={isDark}>Navigator State</SectionTitle>
        <Typography sx={{ color: 'text.secondary', lineHeight: 1.6 }}>
            The Cluster Navigator remembers which groups and clusters are
            expanded or collapsed, as well as your current selection. This
            state is preserved when you return to the workbench.
        </Typography>

        <HelpTip isDark={isDark}>
            Your theme preference and navigator state are stored in your
            browser's local storage.
        </HelpTip>
    </Box>
);

/**
 * HelpPanel - Main help panel component
 */
const HelpPanel = ({ open, onClose, helpContext, mode }) => {
    const isDark = mode === 'dark';
    const [currentPage, setCurrentPage] = useState(HELP_PAGES.overview);
    const contentRef = useRef(null);

    // Scroll to top when page changes
    useEffect(() => {
        if (contentRef.current) {
            contentRef.current.scrollTop = 0;
        }
    }, [currentPage]);

    // Update page when context changes and panel opens
    useEffect(() => {
        if (open && helpContext) {
            setCurrentPage(contextToPage(helpContext));
        }
    }, [open, helpContext]);

    // Reset to overview when closed
    useEffect(() => {
        if (!open) {
            // Small delay to avoid visual jump during close animation
            const timer = setTimeout(() => {
                if (!helpContext) {
                    setCurrentPage(HELP_PAGES.overview);
                }
            }, 300);
            return () => clearTimeout(timer);
        }
    }, [open, helpContext]);

    const renderPage = () => {
        switch (currentPage) {
            case HELP_PAGES.navigator:
                return <NavigatorPage isDark={isDark} />;
            case HELP_PAGES.statusPanel:
                return <StatusPanelPage isDark={isDark} />;
            case HELP_PAGES.alerts:
                return <AlertsPage isDark={isDark} />;
            case HELP_PAGES.serverManagement:
                return <ServerManagementPage isDark={isDark} />;
            case HELP_PAGES.settings:
                return <SettingsPage isDark={isDark} />;
            default:
                return <OverviewPage isDark={isDark} />;
        }
    };

    const getPageTitle = (page) => {
        switch (page) {
            case HELP_PAGES.navigator:
                return 'Cluster Navigator';
            case HELP_PAGES.statusPanel:
                return 'Status Panel';
            case HELP_PAGES.alerts:
                return 'Alerts';
            case HELP_PAGES.serverManagement:
                return 'Server Management';
            case HELP_PAGES.settings:
                return 'Settings';
            default:
                return 'Overview';
        }
    };

    return (
        <Drawer
            anchor="right"
            open={open}
            onClose={onClose}
            sx={{
                '& .MuiDrawer-paper': {
                    width: { xs: '100%', sm: 560 },
                    bgcolor: isDark ? '#0F172A' : '#FFFFFF',
                },
            }}
        >
            <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
                {/* Header */}
                <Box
                    sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 1,
                        p: 2,
                        borderBottom: '1px solid',
                        borderColor: isDark ? '#334155' : '#E5E7EB',
                    }}
                >
                    {currentPage !== HELP_PAGES.overview && (
                        <IconButton
                            onClick={() => setCurrentPage(HELP_PAGES.overview)}
                            size="small"
                            sx={{ mr: 0.5 }}
                        >
                            <BackIcon sx={{ fontSize: 20 }} />
                        </IconButton>
                    )}
                    <Box sx={{ flex: 1 }}>
                        <Breadcrumbs
                            separator={<ChevronIcon sx={{ fontSize: 14, color: 'text.disabled' }} />}
                            sx={{ '& .MuiBreadcrumbs-ol': { flexWrap: 'nowrap' } }}
                        >
                            {currentPage !== HELP_PAGES.overview && (
                                <Link
                                    component="button"
                                    onClick={() => setCurrentPage(HELP_PAGES.overview)}
                                    sx={{
                                        fontSize: '0.8125rem',
                                        color: 'text.secondary',
                                        textDecoration: 'none',
                                        '&:hover': { textDecoration: 'underline' },
                                    }}
                                >
                                    Help
                                </Link>
                            )}
                            <Typography
                                sx={{
                                    fontSize: '0.8125rem',
                                    fontWeight: 600,
                                    color: 'text.primary',
                                }}
                            >
                                {currentPage === HELP_PAGES.overview ? 'Help & Documentation' : getPageTitle(currentPage)}
                            </Typography>
                        </Breadcrumbs>
                    </Box>
                    <IconButton onClick={onClose} aria-label="close help" size="small">
                        <CloseIcon sx={{ fontSize: 20 }} />
                    </IconButton>
                </Box>

                {/* Main content area */}
                <Box sx={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
                    {/* Navigation sidebar */}
                    <Box
                        sx={{
                            width: 180,
                            borderRight: '1px solid',
                            borderColor: isDark ? '#334155' : '#E5E7EB',
                            p: 1.5,
                            overflowY: 'auto',
                        }}
                    >
                        <List dense disablePadding>
                            <HelpNavItem
                                icon={HomeIcon}
                                label="Overview"
                                pageId={HELP_PAGES.overview}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                            <HelpNavItem
                                icon={NavigatorIcon}
                                label="Navigator"
                                pageId={HELP_PAGES.navigator}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                            <HelpNavItem
                                icon={StatusIcon}
                                label="Status Panel"
                                pageId={HELP_PAGES.statusPanel}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                            <HelpNavItem
                                icon={AlertsIcon}
                                label="Alerts"
                                pageId={HELP_PAGES.alerts}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                            <HelpNavItem
                                icon={ServerIcon}
                                label="Servers"
                                pageId={HELP_PAGES.serverManagement}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                            <HelpNavItem
                                icon={SettingsIcon}
                                label="Settings"
                                pageId={HELP_PAGES.settings}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                                isDark={isDark}
                            />
                        </List>
                    </Box>

                    {/* Content area */}
                    <Box
                        ref={contentRef}
                        sx={{
                            flex: 1,
                            p: 3,
                            overflowY: 'auto',
                        }}
                    >
                        {renderPage()}
                    </Box>
                </Box>

                {/* Footer */}
                <Box
                    sx={{
                        p: 2,
                        borderTop: '1px solid',
                        borderColor: isDark ? '#334155' : '#E5E7EB',
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                    }}
                >
                    <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.75rem' }}>
                        AI DBA Workbench v{CLIENT_VERSION}
                    </Typography>
                    <Typography variant="body2" color="text.disabled" sx={{ fontSize: '0.6875rem' }}>
                        &copy; 2025-2026 pgEdge, Inc.
                    </Typography>
                </Box>
            </Box>
        </Drawer>
    );
};

export default HelpPanel;
