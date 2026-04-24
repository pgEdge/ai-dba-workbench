/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
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
    List,
    Breadcrumbs,
    Link,
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
    AdminPanelSettings as AdminIcon,
    PauseCircle as BlackoutIcon,
    SmartToyOutlined as ChatBotIcon,
    MonitorHeart as MonitoringIcon,
} from '@mui/icons-material';
import { useAICapabilities } from '../../contexts/useAICapabilities';
import { CLIENT_VERSION } from '../../lib/version';
import { HELP_PAGES, contextToPage } from './helpPanelConstants';
import { styles, getDrawerPaperSx } from './helpPanelStyles';
import { HelpNavItem } from './components';
import {
    OverviewPage,
    NavigatorPage,
    StatusPanelPage,
    AlertsPage,
    ServerManagementPage,
    SettingsPage,
    AdministrationPage,
    BlackoutsPage,
    AskElliePage,
    MonitoringPage,
} from './pages';

/**
 * HelpPanel - Main help panel component
 */
export interface HelpPanelProps {
    open: boolean;
    onClose: () => void;
    helpContext?: string | null;
}

const HelpPanel: React.FC<HelpPanelProps> = ({ open, onClose, helpContext }) => {
    const { aiEnabled } = useAICapabilities();
    const [currentPage, setCurrentPage] = useState(HELP_PAGES.overview);
    const contentRef = useRef<HTMLDivElement>(null);

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

    // Normalize page when AI is disabled to prevent showing Overview
    // content under an "Ask Ellie" title
    useEffect(() => {
        if (!aiEnabled && currentPage === HELP_PAGES.askEllie) {
            setCurrentPage(HELP_PAGES.overview);
        }
    }, [aiEnabled, currentPage]);

    const renderPage = () => {
        switch (currentPage) {
            case HELP_PAGES.navigator:
                return <NavigatorPage />;
            case HELP_PAGES.statusPanel:
                return <StatusPanelPage aiEnabled={aiEnabled} />;
            case HELP_PAGES.alerts:
                return <AlertsPage aiEnabled={aiEnabled} />;
            case HELP_PAGES.serverManagement:
                return <ServerManagementPage />;
            case HELP_PAGES.settings:
                return <SettingsPage />;
            case HELP_PAGES.administration:
                return <AdministrationPage />;
            case HELP_PAGES.blackouts:
                return <BlackoutsPage />;
            case HELP_PAGES.askEllie:
                if (aiEnabled) {
                    return <AskElliePage />;
                }
                return <OverviewPage aiEnabled={aiEnabled} />;
            case HELP_PAGES.monitoring:
                return <MonitoringPage aiEnabled={aiEnabled} />;
            default:
                return <OverviewPage aiEnabled={aiEnabled} />;
        }
    };

    const getPageTitle = (page: string): string => {
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
            case HELP_PAGES.administration:
                return 'Administration';
            case HELP_PAGES.blackouts:
                return 'Blackouts';
            case HELP_PAGES.askEllie:
                return 'Ask Ellie';
            case HELP_PAGES.monitoring:
                return 'Monitoring';
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
                            aria-label="back to help overview"
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
                                icon={MonitoringIcon}
                                label="Monitoring"
                                pageId={HELP_PAGES.monitoring}
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
                            <HelpNavItem
                                icon={AdminIcon}
                                label="Administration"
                                pageId={HELP_PAGES.administration}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            <HelpNavItem
                                icon={BlackoutIcon}
                                label="Blackouts"
                                pageId={HELP_PAGES.blackouts}
                                currentPage={currentPage}
                                onClick={setCurrentPage}
                            />
                            {aiEnabled && (
                                <HelpNavItem
                                    icon={ChatBotIcon}
                                    label="Ask Ellie"
                                    pageId={HELP_PAGES.askEllie}
                                    currentPage={currentPage}
                                    onClick={setCurrentPage}
                                />
                            )}
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
