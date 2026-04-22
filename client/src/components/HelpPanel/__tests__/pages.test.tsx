/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { renderWithTheme } from '../../../test/renderWithTheme';
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
} from '../pages';

describe('HelpPanel pages', () => {
    describe('OverviewPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(
                screen.getByText('Welcome to AI DBA Workbench')
            ).toBeInTheDocument();
        });

        it('shows AI-Powered Overview when aiEnabled is true', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(screen.getByText('AI-Powered Overview')).toBeInTheDocument();
        });

        it('shows AI Features (Disabled) when aiEnabled is false', () => {
            renderWithTheme(<OverviewPage aiEnabled={false} />);
            expect(screen.getByText('AI Features (Disabled)')).toBeInTheDocument();
        });

        it('does not show AI-Powered Overview when aiEnabled is false', () => {
            renderWithTheme(<OverviewPage aiEnabled={false} />);
            expect(
                screen.queryByText('AI-Powered Overview')
            ).not.toBeInTheDocument();
        });

        it('displays key features section', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(screen.getByText('Key Features')).toBeInTheDocument();
        });

        it('displays getting started section', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(screen.getByText('Getting Started')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<OverviewPage aiEnabled={true} />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('NavigatorPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Cluster Navigator')).toBeInTheDocument();
        });

        it('displays Groups section', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Groups')).toBeInTheDocument();
        });

        it('displays Clusters section', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Clusters')).toBeInTheDocument();
        });

        it('displays Servers section', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Servers')).toBeInTheDocument();
        });

        it('displays Search section', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Search')).toBeInTheDocument();
        });

        it('displays Drag and Drop section', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Drag and Drop')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<NavigatorPage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('StatusPanelPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByText('Status Panel')).toBeInTheDocument();
        });

        it('shows AI Overview section when aiEnabled is true', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByText('AI Overview')).toBeInTheDocument();
        });

        it('hides AI Overview section when aiEnabled is false', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={false} />);
            expect(screen.queryByText('AI Overview')).not.toBeInTheDocument();
        });

        it('shows Server & Cluster Analysis when aiEnabled is true', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(
                screen.getByText('Server & Cluster Analysis')
            ).toBeInTheDocument();
        });

        it('hides Server & Cluster Analysis when aiEnabled is false', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={false} />);
            expect(
                screen.queryByText('Server & Cluster Analysis')
            ).not.toBeInTheDocument();
        });

        it('displays Server View section', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByText('Server View')).toBeInTheDocument();
        });

        it('displays Cluster View section', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByText('Cluster View')).toBeInTheDocument();
        });

        it('displays Estate View section', () => {
            renderWithTheme(<StatusPanelPage aiEnabled={true} />);
            expect(screen.getByText('Estate View')).toBeInTheDocument();
        });
    });

    describe('AlertsPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Alert Management')).toBeInTheDocument();
        });

        it('displays Alert Types section', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Alert Types')).toBeInTheDocument();
        });

        it('displays Alert Severity section', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Alert Severity')).toBeInTheDocument();
        });

        it('displays Acknowledging Alerts section', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Acknowledging Alerts')).toBeInTheDocument();
        });

        it('shows AI Alert Analysis when aiEnabled is true', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('AI Alert Analysis')).toBeInTheDocument();
        });

        it('hides AI Alert Analysis when aiEnabled is false', () => {
            renderWithTheme(<AlertsPage aiEnabled={false} />);
            expect(
                screen.queryByText('AI Alert Analysis')
            ).not.toBeInTheDocument();
        });

        it('shows Code Block Actions when aiEnabled is true', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Code Block Actions')).toBeInTheDocument();
        });

        it('hides Code Block Actions when aiEnabled is false', () => {
            renderWithTheme(<AlertsPage aiEnabled={false} />);
            expect(
                screen.queryByText('Code Block Actions')
            ).not.toBeInTheDocument();
        });

        it('displays severity chips', () => {
            renderWithTheme(<AlertsPage aiEnabled={true} />);
            expect(screen.getByText('Critical')).toBeInTheDocument();
            expect(screen.getByText('Warning')).toBeInTheDocument();
            expect(screen.getByText('Info')).toBeInTheDocument();
        });
    });

    describe('ServerManagementPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Server Management')).toBeInTheDocument();
        });

        it('displays Adding Servers section', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Adding Servers')).toBeInTheDocument();
        });

        it('displays Editing Servers section', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Editing Servers')).toBeInTheDocument();
        });

        it('displays Deleting Servers section', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Deleting Servers')).toBeInTheDocument();
        });

        it('displays Managing Groups section', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Managing Groups')).toBeInTheDocument();
        });

        it('displays Server Roles section', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Server Roles')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<ServerManagementPage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('SettingsPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByText('Settings & Preferences')).toBeInTheDocument();
        });

        it('displays Theme section', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByText('Theme')).toBeInTheDocument();
        });

        it('displays User Account section', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByText('User Account')).toBeInTheDocument();
        });

        it('displays Navigator State section', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByText('Navigator State')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<SettingsPage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('AdministrationPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Administration')).toBeInTheDocument();
        });

        it('displays Security section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Security')).toBeInTheDocument();
        });

        it('displays Monitoring section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Monitoring')).toBeInTheDocument();
        });

        it('displays Notification Channels section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Notification Channels')).toBeInTheDocument();
        });

        it('displays AI section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('AI')).toBeInTheDocument();
        });

        it('displays Webhook Templates section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Webhook Templates')).toBeInTheDocument();
        });

        it('displays Configuration Overrides section', () => {
            renderWithTheme(<AdministrationPage />);
            expect(
                screen.getByText('Configuration Overrides')
            ).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<AdministrationPage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('BlackoutsPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Blackout Management')).toBeInTheDocument();
        });

        it('displays Scopes section', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Scopes')).toBeInTheDocument();
        });

        it('displays One-Time Blackouts section', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('One-Time Blackouts')).toBeInTheDocument();
        });

        it('displays Recurring Schedules section', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Recurring Schedules')).toBeInTheDocument();
        });

        it('displays Navigator Indicators section', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Navigator Indicators')).toBeInTheDocument();
        });

        it('displays Alert Suppression section', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Alert Suppression')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<BlackoutsPage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('AskElliePage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByText('Ask Ellie')).toBeInTheDocument();
        });

        it('displays Getting Started section', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByText('Getting Started')).toBeInTheDocument();
        });

        it('displays Conversation History section', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByText('Conversation History')).toBeInTheDocument();
        });

        it('displays Available Tools section', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByText('Available Tools')).toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<AskElliePage />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });

    describe('MonitoringPage', () => {
        it('renders without crashing', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByRole('heading', { level: 5 })).toBeInTheDocument();
        });

        it('displays expected heading text', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Monitoring Dashboards')).toBeInTheDocument();
        });

        it('displays Estate Dashboard section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Estate Dashboard')).toBeInTheDocument();
        });

        it('displays Cluster Dashboard section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Cluster Dashboard')).toBeInTheDocument();
        });

        it('displays Server Dashboard section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Server Dashboard')).toBeInTheDocument();
        });

        it('displays Database Dashboard section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Database Dashboard')).toBeInTheDocument();
        });

        it('displays Object Dashboard section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Object Dashboard')).toBeInTheDocument();
        });

        it('displays Query Plan section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Query Plan')).toBeInTheDocument();
        });

        it('displays Key Features section', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Key Features')).toBeInTheDocument();
        });

        it('shows AI Query Analysis when aiEnabled is true', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('AI Query Analysis')).toBeInTheDocument();
        });

        it('hides AI Query Analysis when aiEnabled is false', () => {
            renderWithTheme(<MonitoringPage aiEnabled={false} />);
            expect(
                screen.queryByText('AI Query Analysis')
            ).not.toBeInTheDocument();
        });

        it('shows AI Chart Analysis when aiEnabled is true', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('AI Chart Analysis')).toBeInTheDocument();
        });

        it('hides AI Chart Analysis when aiEnabled is false', () => {
            renderWithTheme(<MonitoringPage aiEnabled={false} />);
            expect(
                screen.queryByText('AI Chart Analysis')
            ).not.toBeInTheDocument();
        });

        it('displays a help tip', () => {
            renderWithTheme(<MonitoringPage aiEnabled={true} />);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });
    });
});
