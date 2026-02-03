/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Help Panel
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
import { Theme } from '@mui/material/styles';
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

// ---------------------------------------------------------------------------
// Static style constants (Issue 23)
// ---------------------------------------------------------------------------

const styles = {
    navItemIcon: { minWidth: 32 },
    navItemIconSize: { fontSize: 18 },
    chevronActive: { fontSize: 16 },
    sectionTitleWrapper: {
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        mb: 1.5,
        mt: 3,
    },
    sectionTitleIcon: { fontSize: 18, color: 'primary.main' },
    sectionTitleText: {
        fontSize: '1rem',
        fontWeight: 600,
        color: 'text.primary',
    },
    helpTipText: {
        fontSize: '0.8125rem',
        color: 'text.secondary',
        lineHeight: 1.5,
    },
    featureTitle: {
        fontWeight: 600,
        fontSize: '0.8125rem',
        color: 'text.primary',
        mb: 0.25,
    },
    featureDescription: {
        fontSize: '0.8125rem',
        color: 'text.secondary',
        lineHeight: 1.5,
    },
    featureWrapper: { mb: 1.5 },
    shortcutKeyBase: {
        fontFamily: '"JetBrains Mono", monospace',
        fontSize: '0.6875rem',
        fontWeight: 600,
        color: 'text.primary',
        px: 0.75,
        py: 0.25,
        borderRadius: 0.5,
        border: '1px solid',
    },
    shortcutRow: { display: 'flex', alignItems: 'center', gap: 1, mb: 1 },
    shortcutKeysRow: { display: 'flex', gap: 0.5 },
    shortcutDescription: { fontSize: '0.8125rem', color: 'text.secondary' },
    drawerContent: {
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
    },
    headerWrapper: {
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        p: 2,
        borderBottom: '1px solid',
        borderColor: 'divider',
    },
    headerFlex: { flex: 1 },
    breadcrumbsOl: {
        '& .MuiBreadcrumbs-ol': { flexWrap: 'nowrap' },
    },
    breadcrumbLink: {
        fontSize: '0.8125rem',
        color: 'text.secondary',
        textDecoration: 'none',
        '&:hover': { textDecoration: 'underline' },
    },
    breadcrumbCurrent: {
        fontSize: '0.8125rem',
        fontWeight: 600,
        color: 'text.primary',
    },
    chevronSeparator: { fontSize: 14, color: 'text.disabled' },
    closeIconSize: { fontSize: 20 },
    backIconSize: { fontSize: 20 },
    backButton: { mr: 0.5 },
    mainContentArea: { display: 'flex', flex: 1, overflow: 'hidden' },
    navSidebar: {
        width: 180,
        borderRight: '1px solid',
        borderColor: 'divider',
        p: 1.5,
        overflowY: 'auto',
    },
    contentArea: {
        flex: 1,
        p: 3,
        overflowY: 'auto',
    },
    footerWrapper: {
        p: 2,
        borderTop: '1px solid',
        borderColor: 'divider',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
    },
    footerVersion: { fontSize: '0.75rem' },
    footerCopyright: { fontSize: '0.6875rem' },
    pageHeading: { fontWeight: 600, mb: 2 },
    bodyText: { color: 'text.secondary', lineHeight: 1.6 },
    bodyTextMb3: { color: 'text.secondary', mb: 3, lineHeight: 1.6 },
    bodyTextMb2: { color: 'text.secondary', mb: 2, lineHeight: 1.6 },
    indentedBlock: { pl: 2, mb: 2 },
    severityChipsRow: { display: 'flex', gap: 1, mb: 3 },
    severityChipBase: { fontWeight: 600, fontSize: '0.75rem' },
};

// ---------------------------------------------------------------------------
// Theme-dependent style getters (Issue 22 + 23)
// ---------------------------------------------------------------------------

const getDrawerPaperSx = () => ({
    '& .MuiDrawer-paper': {
        width: { xs: '100%', sm: 560 },
        bgcolor: 'background.default',
    },
});

const getNavItemSx = (isActive) => (theme) => ({
    borderRadius: 1,
    mb: 0.5,
    py: 0.75,
    bgcolor: isActive
        ? alpha(theme.palette.primary.main, 0.15)
        : 'transparent',
    '&:hover': {
        bgcolor: isActive
            ? alpha(theme.palette.primary.main, 0.2)
            : alpha(theme.palette.grey[500], 0.1),
    },
});

const getNavItemIconColor = (isActive) =>
    isActive ? 'primary.main' : 'text.secondary';

const getNavItemLabelProps = (isActive) => ({
    fontSize: '0.8125rem',
    fontWeight: isActive ? 600 : 500,
    color: isActive ? 'primary.main' : 'text.primary',
});

const getHelpTipSx = (theme: Theme) => ({
    display: 'flex',
    gap: 1,
    p: 1.5,
    mt: 2,
    borderRadius: 1,
    bgcolor: alpha(theme.palette.primary.main, 0.08),
    border: '1px solid',
    borderColor: alpha(theme.palette.primary.main, 0.2),
});

const getShortcutKeySx = (theme: Theme) => ({
    ...styles.shortcutKeyBase,
    bgcolor: alpha(theme.palette.grey[500], 0.2),
    borderColor: theme.palette.divider,
});

const getSeverityChipSx = (paletteKey) => (theme) => ({
    ...styles.severityChipBase,
    bgcolor: alpha(theme.palette[paletteKey].main, 0.15),
    color: theme.palette[paletteKey].main,
});

/**
 * HelpNavItem - Navigation item in the help sidebar
 */
const HelpNavItem = ({ icon: Icon, label, pageId, currentPage, onClick }) => {
    const isActive = currentPage === pageId;

    return (
        <ListItemButton
            onClick={() => onClick(pageId)}
            sx={getNavItemSx(isActive)}
        >
            <ListItemIcon sx={styles.navItemIcon}>
                <Icon
                    sx={{
                        ...styles.navItemIconSize,
                        color: getNavItemIconColor(isActive),
                    }}
                />
            </ListItemIcon>
            <ListItemText
                primary={label}
                primaryTypographyProps={getNavItemLabelProps(isActive)}
            />
            {isActive && (
                <ChevronIcon sx={{
                    ...styles.chevronActive,
                    color: 'primary.main',
                }} />
            )}
        </ListItemButton>
    );
};

/**
 * SectionTitle - Section header within help content
 */
const SectionTitle = ({ children, icon: Icon }) => (
    <Box sx={styles.sectionTitleWrapper}>
        {Icon && <Icon sx={styles.sectionTitleIcon} />}
        <Typography variant="h6" sx={styles.sectionTitleText}>
            {children}
        </Typography>
    </Box>
);

/**
 * HelpTip - Highlighted tip or important note
 */
const HelpTip = ({ children }) => (
    <Box sx={getHelpTipSx}>
        <Typography sx={styles.helpTipText}>
            <Box component="strong" sx={{ color: 'primary.main' }}>
                Tip:
            </Box>{' '}
            {children}
        </Typography>
    </Box>
);

/**
 * FeatureItem - Single feature in a feature list
 */
const FeatureItem = ({ title, description }) => (
    <Box sx={styles.featureWrapper}>
        <Typography sx={styles.featureTitle}>
            {title}
        </Typography>
        <Typography sx={styles.featureDescription}>
            {description}
        </Typography>
    </Box>
);

/**
 * KeyboardShortcut - Display a keyboard shortcut
 */
const KeyboardShortcut = ({ keys, description }) => (
    <Box sx={styles.shortcutRow}>
        <Box sx={styles.shortcutKeysRow}>
            {keys.map((key, i) => (
                <Box key={i} sx={getShortcutKeySx}>
                    {key}
                </Box>
            ))}
        </Box>
        <Typography sx={styles.shortcutDescription}>
            {description}
        </Typography>
    </Box>
);

/**
 * Overview Page - Introduction to the workbench
 */
const OverviewPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Welcome to AI DBA Workbench
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The AI DBA Workbench provides AI-powered tools for PostgreSQL database
            administration, monitoring, and optimization. This interface allows you to
            manage database servers organized in clusters and groups, monitor their
            health, and respond to alerts.
        </Typography>

        <SectionTitle>Key Features</SectionTitle>
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

        <SectionTitle>Getting Started</SectionTitle>
        <Typography sx={styles.bodyText}>
            Use the <strong>Cluster Navigator</strong> on the left to browse your
            server hierarchy. Click on a server to view its details and alerts in
            the main panel. You can also view aggregated information for clusters
            or the entire estate.
        </Typography>

        <HelpTip>
            Click the estate header at the top of the navigator to see a summary of
            all servers across all clusters.
        </HelpTip>
    </Box>
);

/**
 * Navigator Page - Cluster Navigator help
 */
const NavigatorPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Cluster Navigator
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The Cluster Navigator provides a hierarchical view of your database
            infrastructure, organized by groups, clusters, and individual servers.
        </Typography>

        <SectionTitle icon={GroupIcon}>Groups</SectionTitle>
        <Typography sx={styles.bodyText}>
            Groups are top-level containers for organizing related clusters. They
            might represent different environments (Production, Staging), regions,
            or business units.
        </Typography>

        <SectionTitle icon={ClusterIcon}>Clusters</SectionTitle>
        <Typography sx={styles.bodyText}>
            Clusters contain one or more database servers that work together. A
            cluster typically represents a replication set with a primary server
            and one or more standbys, or a Spock multi-master replication group.
        </Typography>

        <SectionTitle icon={ServerIcon}>Servers</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Individual PostgreSQL server connections. Each server displays its role
            and current status:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Roles"
                description="Primary, Standby, Cascading standby, Spock node, Standalone, Publisher, or Subscriber."
            />
            <FeatureItem
                title="Status Indicators"
                description="Green checkmark for healthy servers, yellow warning with alert count for servers with active alerts, red error for offline servers."
            />
        </Box>

        <SectionTitle icon={SearchIcon}>Search</SectionTitle>
        <Typography sx={styles.bodyText}>
            Use the search bar at the top of the navigator to quickly filter
            servers by name. The search filters in real-time as you type.
        </Typography>

        <SectionTitle icon={DragIcon}>Drag and Drop</SectionTitle>
        <Typography sx={styles.bodyText}>
            Reorganize your server hierarchy by dragging servers between clusters
            or clusters between groups. Drag a server onto a cluster to move it,
            or drag a cluster onto a group to reassign it.
        </Typography>

        <HelpTip>
            Click on a cluster name to view aggregated status and alerts for all
            servers in that cluster.
        </HelpTip>
    </Box>
);

/**
 * Status Panel Page - Status panel and server details help
 */
const StatusPanelPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Status Panel
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The Status Panel displays detailed information about your current
            selection, whether it's a single server, a cluster, or the entire
            estate.
        </Typography>

        <SectionTitle icon={ServerIcon}>Server View</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            When viewing a single server, you'll see:
        </Typography>
        <Box sx={styles.indentedBlock}>
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

        <SectionTitle icon={ClusterIcon}>Cluster View</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            When viewing a cluster, you'll see metric cards summarizing the health
            of all servers in the cluster:
        </Typography>
        <Box sx={styles.indentedBlock}>
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

        <SectionTitle>Estate View</SectionTitle>
        <Typography sx={styles.bodyText}>
            The estate view shows a summary of all servers across all clusters
            and groups, including total counts for clusters and groups.
        </Typography>

        <HelpTip>
            The status indicator in the header shows the overall health of your
            selection at a glance.
        </HelpTip>
    </Box>
);

/**
 * Alerts Page - Alert management help
 */
const AlertsPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Alert Management
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Alerts notify you when database metrics exceed configured thresholds
            or when system issues are detected. You can view, acknowledge, and
            manage alerts from the Status Panel.
        </Typography>

        <SectionTitle icon={WarningIcon}>Alert Types</SectionTitle>
        <Box sx={styles.indentedBlock}>
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

        <SectionTitle>Alert Severity</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Alerts are classified by severity:
        </Typography>
        <Box sx={styles.severityChipsRow}>
            <Chip
                label="Critical"
                size="small"
                sx={getSeverityChipSx('error')}
            />
            <Chip
                label="Warning"
                size="small"
                sx={getSeverityChipSx('warning')}
            />
            <Chip
                label="Info"
                size="small"
                sx={getSeverityChipSx('info')}
            />
        </Box>

        <SectionTitle icon={AckIcon}>Acknowledging Alerts</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the checkmark icon on an alert to acknowledge it. You can:
        </Typography>
        <Box sx={styles.indentedBlock}>
            <FeatureItem
                title="Add a Reason"
                description="Explain why the alert is being acknowledged (e.g., 'Investigating', 'Known issue', 'Scheduled maintenance')."
            />
            <FeatureItem
                title="Mark as False Positive"
                description="Flag alerts that were triggered incorrectly to help improve alert accuracy over time."
            />
        </Box>
        <Typography sx={styles.bodyText}>
            Acknowledged alerts are moved to a separate collapsed section below
            active alerts. Use the undo icon on an acknowledged alert to restore
            it to active status.
        </Typography>

        <SectionTitle>Threshold Information</SectionTitle>
        <Typography sx={styles.bodyText}>
            For threshold-based alerts, you'll see the current value and the
            threshold that was exceeded (e.g., "108 exceeds threshold of 100").
            This helps you understand the severity of the issue.
        </Typography>

        <HelpTip>
            Acknowledged alerts remain visible but are separated from active
            alerts. The alert count in the navigator only includes active
            (non-acknowledged) alerts.
        </HelpTip>
    </Box>
);

/**
 * Server Management Page - Adding/editing servers help
 */
const ServerManagementPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Server Management
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            The AI DBA Workbench allows you to add, edit, and organize database
            server connections within your cluster hierarchy.
        </Typography>

        <SectionTitle icon={AddIcon}>Adding Servers</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Click the + button in the navigator header or right-click on a
            cluster to add a new server. You'll need to provide:
        </Typography>
        <Box sx={styles.indentedBlock}>
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

        <SectionTitle icon={EditIcon}>Editing Servers</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click the pencil icon that appears when hovering over a server name
            in the navigator to edit its configuration. You can modify connection
            details, move it to a different cluster, or update credentials.
        </Typography>

        <SectionTitle icon={DeleteIcon}>Deleting Servers</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click the trash icon to remove a server connection. You'll be asked
            to confirm before the server is deleted. This removes the connection
            from the workbench but does not affect the actual database server.
        </Typography>

        <SectionTitle icon={GroupIcon}>Managing Groups</SectionTitle>
        <Typography sx={styles.bodyText}>
            Groups can be created from the + menu in the navigator header. Edit
            group names by clicking the pencil icon on the group header. Groups
            can contain multiple clusters.
        </Typography>

        <SectionTitle icon={PrimaryIcon}>Server Roles</SectionTitle>
        <Typography sx={styles.bodyTextMb2}>
            Server roles are automatically detected based on the PostgreSQL
            configuration:
        </Typography>
        <Box sx={styles.indentedBlock}>
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

        <HelpTip>
            Drag servers between clusters to reorganize your infrastructure
            without editing each server individually.
        </HelpTip>
    </Box>
);

/**
 * Settings Page - Theme and user settings help
 */
const SettingsPage = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Settings & Preferences
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Customize your AI DBA Workbench experience with the available
            settings and preferences.
        </Typography>

        <SectionTitle icon={ThemeIcon}>Theme</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click the sun/moon icon in the header to toggle between light and
            dark mode. Your preference is saved automatically and persists
            across sessions.
        </Typography>

        <SectionTitle>User Account</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click your avatar in the header to access the user menu. From here
            you can see your username and sign out of the workbench.
        </Typography>

        <SectionTitle>Navigator State</SectionTitle>
        <Typography sx={styles.bodyText}>
            The Cluster Navigator remembers which groups and clusters are
            expanded or collapsed, as well as your current selection. This
            state is preserved when you return to the workbench.
        </Typography>

        <HelpTip>
            Your theme preference and navigator state are stored in your
            browser's local storage.
        </HelpTip>
    </Box>
);

/**
 * HelpPanel - Main help panel component
 */
interface HelpPanelProps {
    open: boolean;
    onClose: () => void;
    helpContext: string | null;
    mode: string;
}

const HelpPanel: React.FC<HelpPanelProps> = ({ open, onClose, helpContext, mode }) => {
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
                return <NavigatorPage />;
            case HELP_PAGES.statusPanel:
                return <StatusPanelPage />;
            case HELP_PAGES.alerts:
                return <AlertsPage />;
            case HELP_PAGES.serverManagement:
                return <ServerManagementPage />;
            case HELP_PAGES.settings:
                return <SettingsPage />;
            default:
                return <OverviewPage />;
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
            sx={getDrawerPaperSx()}
        >
            <Box sx={styles.drawerContent}>
                {/* Header */}
                <Box sx={styles.headerWrapper}>
                    {currentPage !== HELP_PAGES.overview && (
                        <IconButton
                            onClick={() => setCurrentPage(HELP_PAGES.overview)}
                            size="small"
                            sx={styles.backButton}
                        >
                            <BackIcon sx={styles.backIconSize} />
                        </IconButton>
                    )}
                    <Box sx={styles.headerFlex}>
                        <Breadcrumbs
                            separator={<ChevronIcon sx={styles.chevronSeparator} />}
                            sx={styles.breadcrumbsOl}
                        >
                            {currentPage !== HELP_PAGES.overview && (
                                <Link
                                    component="button"
                                    onClick={() => setCurrentPage(HELP_PAGES.overview)}
                                    sx={styles.breadcrumbLink}
                                >
                                    Help
                                </Link>
                            )}
                            <Typography sx={styles.breadcrumbCurrent}>
                                {currentPage === HELP_PAGES.overview ? 'Help & Documentation' : getPageTitle(currentPage)}
                            </Typography>
                        </Breadcrumbs>
                    </Box>
                    <IconButton onClick={onClose} aria-label="close help" size="small">
                        <CloseIcon sx={styles.closeIconSize} />
                    </IconButton>
                </Box>

                {/* Main content area */}
                <Box sx={styles.mainContentArea}>
                    {/* Navigation sidebar */}
                    <Box sx={styles.navSidebar}>
                        <List dense disablePadding>
                            <HelpNavItem
                                icon={HomeIcon}
                                label="Overview"
                                pageId={HELP_PAGES.overview}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={NavigatorIcon}
                                label="Navigator"
                                pageId={HELP_PAGES.navigator}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={StatusIcon}
                                label="Status Panel"
                                pageId={HELP_PAGES.statusPanel}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={AlertsIcon}
                                label="Alerts"
                                pageId={HELP_PAGES.alerts}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={ServerIcon}
                                label="Servers"
                                pageId={HELP_PAGES.serverManagement}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={SettingsIcon}
                                label="Settings"
                                pageId={HELP_PAGES.settings}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                        </List>
                    </Box>

                    {/* Content area */}
                    <Box
                        ref={contentRef}
                        sx={styles.contentArea}
                    >
                        {renderPage()}
                    </Box>
                </Box>

                {/* Footer */}
                <Box sx={styles.footerWrapper}>
                    <Typography variant="body2" color="text.secondary" sx={styles.footerVersion}>
                        AI DBA Workbench v{CLIENT_VERSION}
                    </Typography>
                    <Typography variant="body2" color="text.disabled" sx={styles.footerCopyright}>
                        &copy; 2025-2026 pgEdge, Inc.
                    </Typography>
                </Box>
            </Box>
        </Drawer>
    );
};

export default HelpPanel;
