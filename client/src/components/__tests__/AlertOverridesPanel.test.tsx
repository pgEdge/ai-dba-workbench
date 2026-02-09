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
import { ThemeProvider, createTheme } from '@mui/material';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import AlertOverridesPanel from '../AlertOverridesPanel';

const mockFetch = vi.fn();
global.fetch = mockFetch;

const darkTheme = createTheme({ palette: { mode: 'dark' } });

const renderWithTheme = (component: React.ReactElement) => {
    return render(
        <ThemeProvider theme={darkTheme}>
            {component}
        </ThemeProvider>
    );
};

const mockOverrides = [
    {
        rule_id: 1,
        name: 'High CPU Usage',
        description: 'CPU usage is high',
        category: 'System',
        metric_name: 'cpu_usage_percent',
        metric_unit: '%',
        default_operator: '>',
        default_threshold: 80,
        default_severity: 'warning',
        default_enabled: true,
        has_override: true,
        override_operator: '>',
        override_threshold: 90,
        override_severity: 'critical',
        override_enabled: true,
    },
    {
        rule_id: 2,
        name: 'Low Disk Space',
        description: 'Disk space is low',
        category: 'System',
        metric_name: 'disk_usage_percent',
        metric_unit: '%',
        default_operator: '>',
        default_threshold: 85,
        default_severity: 'warning',
        default_enabled: true,
        has_override: false,
        override_operator: null,
        override_threshold: null,
        override_severity: null,
        override_enabled: null,
    },
    {
        rule_id: 3,
        name: 'High Connection Count',
        description: 'Too many connections',
        category: 'Database',
        metric_name: 'connection_count',
        metric_unit: null,
        default_operator: '>',
        default_threshold: 100,
        default_severity: 'warning',
        default_enabled: true,
        has_override: false,
        override_operator: null,
        override_threshold: null,
        override_severity: null,
        override_enabled: null,
    },
];

describe('AlertOverridesPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders loading state', () => {
        mockFetch.mockReturnValue(new Promise(() => {}));

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('renders rules from API', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });
        expect(screen.getByText('Low Disk Space')).toBeInTheDocument();
        expect(screen.getByText('High Connection Count')).toBeInTheDocument();

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/v1/alert-overrides/server/1',
            expect.objectContaining({ credentials: 'include' })
        );
    });

    it('shows override values when present', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        // The overridden rule should show override values: operator > threshold unit
        expect(screen.getByText('> 90 %')).toBeInTheDocument();
        // The severity chip should show 'critical' for the overridden rule
        expect(screen.getByText('critical')).toBeInTheDocument();
    });

    it('shows default values when no override', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Low Disk Space')).toBeInTheDocument();
        });

        // The non-overridden rule should show default values
        expect(screen.getByText('> 85 %')).toBeInTheDocument();
    });

    it('groups rules by category', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('System')).toBeInTheDocument();
        });
        expect(screen.getByText('Database')).toBeInTheDocument();
    });

    it('edit button opens dialog with pre-populated values', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        // Categories are sorted alphabetically: Database, System
        // The overridden rule (High CPU Usage) is in the System category,
        // so its edit button appears after the Database category edit button.
        // Find the edit button in the same row as High CPU Usage
        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        expect(cpuRow).not.toBeNull();
        const cpuEditButton = (cpuRow as HTMLElement).querySelector('[aria-label="edit override"]');
        expect(cpuEditButton).not.toBeNull();
        fireEvent.click(cpuEditButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit Override: High CPU Usage/)).toBeInTheDocument();
        });

        // Verify the threshold field is pre-populated with the override value
        const thresholdInput = screen.getByLabelText('Threshold');
        expect(thresholdInput).toHaveValue(90);
    });

    it('reset button calls DELETE endpoint', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockOverrides,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockOverrides,
            });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        // Only the overridden rule (High CPU Usage) should have a reset button
        const resetButton = screen.getByLabelText('reset override to default');
        fireEvent.click(resetButton);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/alert-overrides/server/1/1',
                expect.objectContaining({
                    method: 'DELETE',
                    credentials: 'include',
                })
            );
        });
    });

    it('shows empty state when no rules are returned', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('No alert rules found.')).toBeInTheDocument();
        });
    });

    it('shows error when fetch fails', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: false,
            status: 500,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Failed to fetch alert overrides')).toBeInTheDocument();
        });
    });

    it('renders table headers', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Name')).toBeInTheDocument();
        });
        expect(screen.getByText('Metric')).toBeInTheDocument();
        expect(screen.getByText('Condition')).toBeInTheDocument();
        expect(screen.getByText('Severity')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();
    });

    it('uses correct API path for different scopes', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(
            <AlertOverridesPanel scope="cluster" scopeId={5} />
        );

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/alert-overrides/cluster/5',
                expect.objectContaining({ credentials: 'include' })
            );
        });
    });

    it('only shows reset button on rows with overrides', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        // Only one rule has has_override=true, so there should be one reset button
        const resetButtons = screen.getAllByLabelText('reset override to default');
        expect(resetButtons).toHaveLength(1);

        // All rules should have an edit button
        const editButtons = screen.getAllByLabelText('edit override');
        expect(editButtons).toHaveLength(3);
    });
});
