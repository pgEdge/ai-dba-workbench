/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Help page identifiers
 */
export const HELP_PAGES = {
    overview: 'overview',
    navigator: 'navigator',
    statusPanel: 'statusPanel',
    alerts: 'alerts',
    serverManagement: 'serverManagement',
    settings: 'settings',
    administration: 'administration',
    blackouts: 'blackouts',
    askEllie: 'askEllie',
    monitoring: 'monitoring',
};

/**
 * Map context to help page
 */
export const contextToPage = (context: string | null): string => {
    if (!context) {return HELP_PAGES.overview;}

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
        case 'administration':
        case 'admin':
            return HELP_PAGES.administration;
        case 'blackouts':
        case 'blackout':
        case 'maintenance':
            return HELP_PAGES.blackouts;
        case 'chat':
        case 'ellie':
        case 'askEllie':
            return HELP_PAGES.askEllie;
        case 'monitoring':
        case 'dashboard':
        case 'dashboards':
            return HELP_PAGES.monitoring;
        default:
            return HELP_PAGES.overview;
    }
};
