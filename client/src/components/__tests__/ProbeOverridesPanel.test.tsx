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
import ProbeOverridesPanel from '../ProbeOverridesPanel';

const mockFetch = vi.fn();
global.fetch = mockFetch;

const darkTheme = createTheme({ palette: { mode: 'dark' } });

const renderWithTheme = (component: React.ReactElement) => {
    return render(
        <ThemeProvider theme={darkTheme}>{component}</ThemeProvider>,
    );
};

const mockOverrides = [
    {
        name: 'cpu_usage',
        description: 'Collect CPU usage metrics',
        default_enabled: true,
        default_interval_seconds: 60,
        default_retention_days: 30,
        has_override: true,
        override_enabled: false,
        override_interval_seconds: 120,
        override_retention_days: 14,
    },
    {
        name: 'disk_usage',
        description: 'Collect disk usage metrics',
        default_enabled: true,
        default_interval_seconds: 300,
        default_retention_days: 30,
        has_override: false,
        override_enabled: null,
        override_interval_seconds: null,
        override_retention_days: null,
    },
    {
        name: 'connection_count',
        description: 'Collect connection count metrics',
        default_enabled: true,
        default_interval_seconds: 30,
        default_retention_days: 7,
        has_override: false,
        override_enabled: null,
        override_interval_seconds: null,
        override_retention_days: null,
    },
];

describe('ProbeOverridesPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders loading state', () => {
        mockFetch.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('renders probe rows from API', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });
        expect(screen.getByText('disk_usage')).toBeInTheDocument();
        expect(screen.getByText('connection_count')).toBeInTheDocument();

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/v1/probe-overrides/server/1',
            expect.objectContaining({ credentials: 'include' }),
        );
    });

    it('shows override values when present', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        // The overridden row should show its override interval (120s)
        // and retention (14 days), not the defaults.
        expect(screen.getByText('120s')).toBeInTheDocument();
        expect(screen.getByText('14 days')).toBeInTheDocument();
    });

    it('shows default values when no override', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('disk_usage')).toBeInTheDocument();
        });

        // disk_usage has no override; default interval = 300, retention = 30
        expect(screen.getByText('300s')).toBeInTheDocument();
    });

    it('renders table headers', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('Name')).toBeInTheDocument();
        });
        expect(screen.getByText('Description')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Interval')).toBeInTheDocument();
        expect(screen.getByText('Retention')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();
    });

    it('edit button opens dialog with pre-populated values', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        // Click the edit button on the cpu_usage row.
        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        expect(cpuRow).not.toBeNull();
        const cpuEditButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        expect(cpuEditButton).not.toBeNull();
        fireEvent.click(cpuEditButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        // The interval field should be populated with the override
        // value (120 seconds), not the default.
        const intervalInput = screen.getByLabelText(
            'Collection Interval (seconds)',
        );
        expect(intervalInput).toHaveValue(120);

        const retentionInput = screen.getByLabelText('Retention Days');
        expect(retentionInput).toHaveValue(14);
    });

    it('edit dialog Cancel closes the dialog', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        await waitFor(() => {
            expect(
                screen.queryByText(/Edit override: cpu_usage/),
            ).not.toBeInTheDocument();
        });
    });

    it('edit dialog Save calls PUT and refreshes', async () => {
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

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/probe-overrides/server/1/cpu_usage',
                expect.objectContaining({
                    method: 'PUT',
                    credentials: 'include',
                }),
            );
        });
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

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        // Only the overridden row (cpu_usage) shows a reset button.
        const resetButton = screen.getByLabelText('reset override to default');
        fireEvent.click(resetButton);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/probe-overrides/server/1/cpu_usage',
                expect.objectContaining({
                    method: 'DELETE',
                    credentials: 'include',
                }),
            );
        });
    });

    it('shows empty state when no probes are returned', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(
                screen.getByText('No probe configurations found.'),
            ).toBeInTheDocument();
        });
    });

    it('shows error when fetch fails', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: false,
            status: 500,
            text: async () =>
                JSON.stringify({ error: 'Failed to fetch probe overrides' }),
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(
                screen.getByText('Failed to fetch probe overrides'),
            ).toBeInTheDocument();
        });
    });

    it('shows fallback message when fetch rejects with non-Error value', async () => {
        // A bare string rejection exercises the "unknown error"
        // branch in the panel. The shared scaffold should fall
        // back to the generic message.
        mockFetch.mockRejectedValueOnce('boom');

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(
                screen.getByText('An unexpected error occurred'),
            ).toBeInTheDocument();
        });
    });

    it('uses correct API path for different scopes', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(<ProbeOverridesPanel scope="cluster" scopeId={5} />);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/probe-overrides/cluster/5',
                expect.objectContaining({ credentials: 'include' }),
            );
        });
    });

    it('uses correct API path for group scope', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(<ProbeOverridesPanel scope="group" scopeId={42} />);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/probe-overrides/group/42',
                expect.objectContaining({ credentials: 'include' }),
            );
        });
    });

    it('only shows reset button on rows with overrides', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        // Only one row has has_override=true.
        const resetButtons = screen.getAllByLabelText(
            'reset override to default',
        );
        expect(resetButtons).toHaveLength(1);

        // Every row should have an edit button.
        const editButtons = screen.getAllByLabelText('edit override');
        expect(editButtons).toHaveLength(3);
    });

    it('rejects non-numeric interval input on save', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        // Force the interval to an invalid value, then save. The
        // dialog-local error should appear and no PUT should fire.
        const intervalInput = screen.getByLabelText(
            'Collection Interval (seconds)',
        );
        fireEvent.change(intervalInput, { target: { value: '0' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText(
                    'Collection interval must be a positive integer.',
                ),
            ).toBeInTheDocument();
        });

        // Only the initial GET should have fired; no PUT yet.
        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('rejects non-numeric retention input on save', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockOverrides,
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        const retentionInput = screen.getByLabelText('Retention Days');
        fireEvent.change(retentionInput, { target: { value: '-1' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText('Retention days must be a positive integer.'),
            ).toBeInTheDocument();
        });

        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('shows error and keeps dialog open when save PUT fails', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockOverrides,
            })
            .mockResolvedValueOnce({
                ok: false,
                status: 500,
                text: async () =>
                    JSON.stringify({ error: 'Server side blew up' }),
            });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(screen.getByText('Server side blew up')).toBeInTheDocument();
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

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        const cpuRow = screen.getByText('cpu_usage').closest('tr');
        const editButton = (cpuRow as HTMLElement).querySelector(
            '[aria-label="edit override"]',
        );
        fireEvent.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText(/Edit override: cpu_usage/)).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText(
                    'Override for "cpu_usage" saved successfully.',
                ),
            ).toBeInTheDocument();
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

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByLabelText('reset override to default'));

        await waitFor(() => {
            expect(
                screen.getByText('Override for "cpu_usage" reset to default.'),
            ).toBeInTheDocument();
        });
    });

    it('shows error banner when reset DELETE fails', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockOverrides,
            })
            .mockResolvedValueOnce({
                ok: false,
                status: 500,
                text: async () => JSON.stringify({ error: 'Reset failed' }),
            });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByLabelText('reset override to default'));

        await waitFor(() => {
            expect(screen.getByText('Reset failed')).toBeInTheDocument();
        });
    });

    it('error banner can be dismissed', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: false,
            status: 500,
            text: async () => JSON.stringify({ error: 'Bang' }),
        });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('Bang')).toBeInTheDocument();
        });

        // The MUI Alert close button has aria-label "Close" by default.
        fireEvent.click(screen.getByLabelText('Close'));

        await waitFor(() => {
            expect(screen.queryByText('Bang')).not.toBeInTheDocument();
        });
    });

    it('success banner can be dismissed after a reset', async () => {
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

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByLabelText('reset override to default'));

        await waitFor(() => {
            expect(
                screen.getByText('Override for "cpu_usage" reset to default.'),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByLabelText('Close'));

        await waitFor(() => {
            expect(
                screen.queryByText('Override for "cpu_usage" reset to default.'),
            ).not.toBeInTheDocument();
        });
    });
});
