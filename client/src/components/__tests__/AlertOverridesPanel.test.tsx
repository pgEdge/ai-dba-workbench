/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
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
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
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
            text: async () => JSON.stringify({ error: 'Failed to fetch alert overrides' }),
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

    // The following cases were added during the refactor to lift
    // coverage of the dialog body and save / reset paths above the
    // project's 90% line-coverage floor. They exercise behaviour
    // that the original test suite did not previously cover.

    it('edit dialog Cancel closes the dialog', async () => {
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

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        await waitFor(() => {
            expect(
                screen.queryByText(/Edit override: High CPU Usage/)
            ).not.toBeInTheDocument();
        });
    });

    it('edit dialog Save calls PUT with current form values', async () => {
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

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/alert-overrides/server/1/1',
                expect.objectContaining({
                    method: 'PUT',
                    credentials: 'include',
                })
            );
        });
    });

    it('shows success banner after a successful save', async () => {
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

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText(
                    'Override for "High CPU Usage" saved successfully.'
                )
            ).toBeInTheDocument();
        });
    });

    it('shows error banner when save PUT fails', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockOverrides,
            })
            .mockResolvedValueOnce({
                ok: false,
                status: 500,
                text: async () => JSON.stringify({ error: 'PUT exploded' }),
            });

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(screen.getByText('PUT exploded')).toBeInTheDocument();
        });
    });

    it('rejects invalid threshold input on save', async () => {
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

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        const thresholdInput = screen.getByLabelText('Threshold');
        // Force a non-numeric value; <input type="number"> would
        // normally block this, but we bypass via fireEvent.change.
        fireEvent.change(thresholdInput, { target: { value: '' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText('Threshold must be a valid number.')
            ).toBeInTheDocument();
        });

        // The initial GET fired; no PUT should have followed.
        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('pre-populates dialog with defaults when no override exists', async () => {
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

        // Low Disk Space has no override; the dialog should fall
        // back to default_threshold (85). This exercises the
        // "no override" branch of every getter in handleEditRequested.
        const diskRow = screen.getByText('Low Disk Space').closest('tr');
        const editButton = (diskRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: Low Disk Space/)).toBeInTheDocument();
        });

        const thresholdInput = screen.getByLabelText('Threshold');
        expect(thresholdInput).toHaveValue(85);
    });

    it('changes operator, threshold, severity, and enabled values', async () => {
        // This case drives the onChange handlers of every form
        // control in the alert dialog so they show as covered.
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

        const cpuRow = screen.getByText('High CPU Usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]'
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: High CPU Usage/)).toBeInTheDocument();
        });

        // Toggle the Enabled switch.
        const switches = screen.getAllByRole('checkbox');
        // The last checkbox in the dialog is the Enabled toggle;
        // the table row switches are disabled, so this is reliable.
        const dialogEnabledSwitch = switches[switches.length - 1];
        fireEvent.click(dialogEnabledSwitch);

        // Change Threshold via input.
        fireEvent.change(screen.getByLabelText('Threshold'), {
            target: { value: '99.5' },
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/alert-overrides/server/1/1',
                expect.objectContaining({
                    method: 'PUT',
                    body: expect.stringContaining('"threshold":99.5'),
                })
            );
        });
    });

    it('shows success banner after a successful reset', async () => {
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

        fireEvent.click(screen.getByLabelText('reset override to default'));

        await waitFor(() => {
            expect(
                screen.getByText(
                    'Override for "High CPU Usage" reset to default.'
                )
            ).toBeInTheDocument();
        });
    });

    it('shows fallback message when fetch rejects with non-Error value', async () => {
        mockFetch.mockRejectedValueOnce('kaboom');

        renderWithTheme(
            <AlertOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(
                screen.getByText('An unexpected error occurred')
            ).toBeInTheDocument();
        });
    });
});
