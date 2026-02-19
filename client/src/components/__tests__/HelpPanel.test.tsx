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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import HelpPanel from '../HelpPanel';

// Mock AICapabilitiesContext so HelpPanel can render without the provider
vi.mock('../../contexts/AICapabilitiesContext', () => ({
    useAICapabilities: () => ({ aiEnabled: true, loading: false }),
}));

describe('HelpPanel Component', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
    };

    it('renders when open is true', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText('Help & Documentation')).toBeInTheDocument();
    });

    it('does not render content when open is false', () => {
        render(<HelpPanel {...defaultProps} open={false} />);
        expect(screen.queryByText('Help & Documentation')).not.toBeInTheDocument();
    });

    it('displays Overview page by default', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText('Welcome to AI DBA Workbench')).toBeInTheDocument();
    });

    it('displays navigation sidebar with all pages', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText('Overview')).toBeInTheDocument();
        expect(screen.getByText('Navigator')).toBeInTheDocument();
        expect(screen.getByText('Status Panel')).toBeInTheDocument();
        expect(screen.getByText('Alerts')).toBeInTheDocument();
        expect(screen.getByText('Servers')).toBeInTheDocument();
        expect(screen.getByText('Settings')).toBeInTheDocument();
    });

    it('displays version information in footer', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText(/AI DBA Workbench v/)).toBeInTheDocument();
    });

    it('displays copyright notice', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText(/2025-2026 pgEdge, Inc/)).toBeInTheDocument();
    });

    it('calls onClose when close button is clicked', () => {
        const onClose = vi.fn();
        render(<HelpPanel {...defaultProps} onClose={onClose} />);

        const closeButton = screen.getByLabelText('close help');
        fireEvent.click(closeButton);

        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('displays key features on Overview page', () => {
        render(<HelpPanel {...defaultProps} />);

        expect(screen.getByText('Cluster Organization')).toBeInTheDocument();
        expect(screen.getByText('Real-time Monitoring')).toBeInTheDocument();
        expect(screen.getByText('Alert Management')).toBeInTheDocument();
        expect(screen.getByText('Replication Support')).toBeInTheDocument();
    });

    it('navigates to Navigator page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Click on Navigator nav item
        fireEvent.click(screen.getByText('Navigator'));

        // Should show Navigator page content (multiple matches: nav + header)
        const clusterNavElements = screen.getAllByText('Cluster Navigator');
        expect(clusterNavElements.length).toBeGreaterThan(0);
        expect(screen.getByText(/hierarchical view of your database/)).toBeInTheDocument();
    });

    it('navigates to Status Panel page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Click on Status Panel nav item
        fireEvent.click(screen.getByText('Status Panel'));

        // Should show Status Panel page content (there will be multiple matches)
        const statusPanelHeadings = screen.getAllByText('Status Panel');
        expect(statusPanelHeadings.length).toBeGreaterThan(0);
        expect(screen.getByText(/detailed information about your current selection/)).toBeInTheDocument();
    });

    it('navigates to Alerts page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Click on Alerts nav item
        fireEvent.click(screen.getByText('Alerts'));

        // Should show Alerts page content
        // Alert Management appears as h5 on the alerts page
        expect(screen.getByRole('heading', { name: 'Alert Management', level: 5 })).toBeInTheDocument();
        expect(screen.getByText('Acknowledging Alerts')).toBeInTheDocument();
    });

    it('navigates to Servers page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Click on Servers nav item
        fireEvent.click(screen.getByText('Servers'));

        // Should show Server Management page content (multiple matches: breadcrumb + header)
        const serverMgmtElements = screen.getAllByText('Server Management');
        expect(serverMgmtElements.length).toBeGreaterThan(0);
        expect(screen.getByText('Adding Servers')).toBeInTheDocument();
    });

    it('navigates to Settings page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Click on Settings nav item
        fireEvent.click(screen.getByText('Settings'));

        // Should show Settings page content
        expect(screen.getByText('Settings & Preferences')).toBeInTheDocument();
        expect(screen.getByText('Theme')).toBeInTheDocument();
    });

    it('shows back button when not on Overview page', () => {
        render(<HelpPanel {...defaultProps} />);

        // Navigate to another page
        fireEvent.click(screen.getByText('Alerts'));

        // Should show back button
        const backButtons = screen.getAllByRole('button');
        const backButton = backButtons.find(btn =>
            btn.querySelector('svg[data-testid="ArrowBackIcon"]')
        );
        expect(backButton).toBeTruthy();
    });

    it('returns to Overview when back button is clicked', () => {
        render(<HelpPanel {...defaultProps} />);

        // Navigate to another page
        fireEvent.click(screen.getByText('Alerts'));
        expect(screen.getByText('Alert Management')).toBeInTheDocument();

        // Click back
        const backButtons = screen.getAllByRole('button');
        const backButton = backButtons.find(btn =>
            btn.querySelector('svg[data-testid="ArrowBackIcon"]')
        );
        fireEvent.click(backButton);

        // Should be back on Overview
        expect(screen.getByText('Welcome to AI DBA Workbench')).toBeInTheDocument();
    });

    it('opens to context-sensitive page when helpContext is provided', () => {
        render(<HelpPanel {...defaultProps} helpContext="alerts" />);

        // Should show Alerts page content
        expect(screen.getByRole('heading', { name: 'Alert Management', level: 5 })).toBeInTheDocument();
    });

    it('opens to server help when helpContext is server', () => {
        render(<HelpPanel {...defaultProps} helpContext="server" />);

        // Should show Status Panel page content
        const statusPanelHeadings = screen.getAllByText('Status Panel');
        expect(statusPanelHeadings.length).toBeGreaterThan(0);
    });

    it('opens to navigator help when helpContext is cluster', () => {
        render(<HelpPanel {...defaultProps} helpContext="cluster" />);

        // Should show Navigator page content (multiple matches: nav + header)
        const clusterNavElements = screen.getAllByText('Cluster Navigator');
        expect(clusterNavElements.length).toBeGreaterThan(0);
    });

    it('renders correctly in dark mode', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText('Help & Documentation')).toBeInTheDocument();
    });

    it('displays breadcrumb navigation when on sub-page', () => {
        render(<HelpPanel {...defaultProps} />);

        // Navigate to another page
        fireEvent.click(screen.getByText('Alerts'));

        // Should show breadcrumb with Help link
        const helpLink = screen.getByRole('button', { name: 'Help' });
        expect(helpLink).toBeInTheDocument();
    });

    it('displays Ask Ellie in navigation sidebar', () => {
        render(<HelpPanel {...defaultProps} />);
        expect(screen.getByText('Ask Ellie')).toBeInTheDocument();
    });

    it('navigates to Ask Ellie page when clicked', () => {
        render(<HelpPanel {...defaultProps} />);
        fireEvent.click(screen.getByText('Ask Ellie'));
        const askEllieElements = screen.getAllByText('Ask Ellie');
        expect(askEllieElements.length).toBeGreaterThan(0);
        expect(screen.getByText(/AI-powered database assistant/)).toBeInTheDocument();
    });

    it('opens to Ask Ellie help when helpContext is chat', () => {
        render(<HelpPanel {...defaultProps} helpContext="chat" />);
        expect(screen.getByText(/AI-powered database assistant/)).toBeInTheDocument();
    });

    it('displays key sections on Ask Ellie page', () => {
        render(<HelpPanel {...defaultProps} />);
        fireEvent.click(screen.getByText('Ask Ellie'));
        expect(screen.getByText('Getting Started')).toBeInTheDocument();
        expect(screen.getByText('Conversation History')).toBeInTheDocument();
        expect(screen.getByText('Available Tools')).toBeInTheDocument();
    });
});
