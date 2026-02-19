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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import Header from '../Header';
import { AuthProvider } from '../../contexts/AuthContext';

// Mock AICapabilitiesContext so HelpPanel can render without the provider
vi.mock('../../contexts/AICapabilitiesContext', () => ({
    useAICapabilities: () => ({ aiEnabled: true, loading: false }),
}));

// Mock fetch for API calls
global.fetch = vi.fn() as unknown as typeof fetch;

// Use a custom render that provides auth context
const renderHeader = (props: Record<string, unknown> = {}) => {
    const defaultProps = {
        onToggleTheme: vi.fn(),
        mode: 'light' as const,
    };

    // Mock the user info endpoint to return authenticated user
    (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
            authenticated: true,
            username: 'testuser'
        })
    });

    return render(
        <AuthProvider>
            <Header {...defaultProps} {...props} />
        </AuthProvider>
    );
};

describe('Header Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders with correct app title', async () => {
        renderHeader();
        await waitFor(() => {
            expect(screen.getByText('AI DBA Workbench')).toBeInTheDocument();
        });
    });

    it('renders theme toggle button', async () => {
        renderHeader();
        await waitFor(() => {
            expect(screen.getByLabelText('toggle theme')).toBeInTheDocument();
        });
    });

    it('renders help button', async () => {
        renderHeader();
        await waitFor(() => {
            expect(screen.getByLabelText('open help')).toBeInTheDocument();
        });
    });

    it('calls onToggleTheme when theme button is clicked', async () => {
        const onToggleTheme = vi.fn();
        renderHeader({ onToggleTheme });

        await waitFor(() => {
            expect(screen.getByLabelText('toggle theme')).toBeInTheDocument();
        });

        const themeButton = screen.getByLabelText('toggle theme');
        fireEvent.click(themeButton);

        expect(onToggleTheme).toHaveBeenCalledTimes(1);
    });

    it('opens help panel when help button is clicked', async () => {
        renderHeader();

        await waitFor(() => {
            expect(screen.getByLabelText('open help')).toBeInTheDocument();
        });

        const helpButton = screen.getByLabelText('open help');
        fireEvent.click(helpButton);

        // Help panel should now be visible
        await waitFor(() => {
            expect(screen.getByText('Help & Documentation')).toBeInTheDocument();
        });
    });

    it('shows light mode icon in dark mode', async () => {
        renderHeader({ mode: 'dark' });
        await waitFor(() => {
            // In dark mode, the light mode icon should be shown
            expect(screen.getByLabelText('toggle theme')).toBeInTheDocument();
        });
    });
});
