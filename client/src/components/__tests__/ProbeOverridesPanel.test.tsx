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

    it('rejects fractional interval input on save', async () => {
        // parseInt("1.5", 10) would silently produce 1 and save a
        // different value than the user typed. The Number() +
        // Number.isInteger() guard must reject the fractional input
        // before any PUT fires.
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

        const intervalInput = screen.getByLabelText(
            'Collection Interval (seconds)',
        );
        fireEvent.change(intervalInput, { target: { value: '1.5' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText(
                    'Collection interval must be a positive integer.',
                ),
            ).toBeInTheDocument();
        });

        // No PUT should have fired; only the initial GET.
        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('rejects fractional retention input on save', async () => {
        // Mirror coverage of the retention validator: "30.7" -> 30
        // under parseInt would be a silent data corruption.
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
        fireEvent.change(retentionInput, { target: { value: '30.7' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(
                screen.getByText('Retention days must be a positive integer.'),
            ).toBeInTheDocument();
        });

        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('encodes probe name with reserved characters in PUT URL', async () => {
        // Probe names today look like safe identifiers, but if one
        // ever contains "/" or "+" the URL path would break without
        // encoding. The PUT URL must carry the encoded name.
        const reservedNameOverrides = [
            {
                name: 'probe/with+reserved#chars',
                description: 'Defensive encoding check',
                default_enabled: true,
                default_interval_seconds: 60,
                default_retention_days: 30,
                has_override: true,
                override_enabled: false,
                override_interval_seconds: 120,
                override_retention_days: 14,
            },
        ];

        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => reservedNameOverrides,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => reservedNameOverrides,
            });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(
                screen.getByText('probe/with+reserved#chars'),
            ).toBeInTheDocument();
        });

        const editButton = screen.getByLabelText('edit override');
        fireEvent.click(editButton);

        await waitFor(() => {
            expect(
                screen.getByText(/Edit override: probe\/with\+reserved#chars/),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            // The raw "/", "+", and "#" should be percent-encoded.
            const expected =
                '/api/v1/probe-overrides/server/1/' +
                encodeURIComponent('probe/with+reserved#chars');
            expect(mockFetch).toHaveBeenCalledWith(
                expected,
                expect.objectContaining({
                    method: 'PUT',
                    credentials: 'include',
                }),
            );
        });
    });

    it('encodes itemKey with reserved characters in DELETE URL', async () => {
        // The shared scaffold's reset handler also interpolates the
        // item key into the URL. The encoding must cover that path
        // too — caller-supplied identifiers may contain anything.
        const reservedNameOverrides = [
            {
                name: 'probe with space/slash',
                description: 'Defensive encoding check',
                default_enabled: true,
                default_interval_seconds: 60,
                default_retention_days: 30,
                has_override: true,
                override_enabled: false,
                override_interval_seconds: 120,
                override_retention_days: 14,
            },
        ];

        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => reservedNameOverrides,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => reservedNameOverrides,
            });

        renderWithTheme(<ProbeOverridesPanel scope="server" scopeId={1} />);

        await waitFor(() => {
            expect(
                screen.getByText('probe with space/slash'),
            ).toBeInTheDocument();
        });

        fireEvent.click(screen.getByLabelText('reset override to default'));

        await waitFor(() => {
            const expected =
                '/api/v1/probe-overrides/server/1/' +
                encodeURIComponent('probe with space/slash');
            expect(mockFetch).toHaveBeenCalledWith(
                expected,
                expect.objectContaining({
                    method: 'DELETE',
                    credentials: 'include',
                }),
            );
        });
    });

    it('discards stale fetch responses when scope changes mid-flight', async () => {
        // Setting up the race: the first GET (scope=server, id=1)
        // returns a promise we resolve manually AFTER the second GET
        // (scope=cluster, id=5) has already resolved. Without the
        // request-token guard, the stale server response would land
        // last and overwrite the cluster rows. With the guard in
        // place, the stale response is dropped silently.

        // First request: deferred. We'll resolve it explicitly later.
        let resolveFirst: (value: {
            ok: true;
            json: () => Promise<typeof mockOverrides>;
        }) => void = () => {};
        const firstPromise = new Promise<{
            ok: true;
            json: () => Promise<typeof mockOverrides>;
        }>((resolve) => {
            resolveFirst = resolve;
        });
        mockFetch.mockReturnValueOnce(firstPromise);

        // Second request: resolves immediately with a different,
        // recognisable payload so we can assert it survives.
        const clusterOverrides = [
            {
                name: 'cluster_only_probe',
                description: 'Only visible after the rerender',
                default_enabled: true,
                default_interval_seconds: 15,
                default_retention_days: 7,
                has_override: false,
                override_enabled: null,
                override_interval_seconds: null,
                override_retention_days: null,
            },
        ];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => clusterOverrides,
        });

        const { rerender } = render(
            <ThemeProvider theme={darkTheme}>
                <ProbeOverridesPanel scope="server" scopeId={1} />
            </ThemeProvider>,
        );

        // Force scope/scopeId to change while the first request is
        // still pending. This triggers a second fetch with a new
        // request token.
        rerender(
            <ThemeProvider theme={darkTheme}>
                <ProbeOverridesPanel scope="cluster" scopeId={5} />
            </ThemeProvider>,
        );

        // Wait for the second (newer) response to land.
        await waitFor(() => {
            expect(
                screen.getByText('cluster_only_probe'),
            ).toBeInTheDocument();
        });

        // Now resolve the stale first request. It must NOT replace
        // the cluster rows we just rendered.
        resolveFirst({
            ok: true,
            json: async () => mockOverrides,
        });

        // Give microtasks a chance to flush. Even after the stale
        // response resolves, cluster_only_probe must remain and
        // none of the server-scope probe names should appear.
        await waitFor(() => {
            expect(
                screen.getByText('cluster_only_probe'),
            ).toBeInTheDocument();
            expect(screen.queryByText('cpu_usage')).not.toBeInTheDocument();
            expect(screen.queryByText('disk_usage')).not.toBeInTheDocument();
        });
    });

    it('discards stale fetch error responses when scope changes mid-flight', async () => {
        // Mirror of the previous test but exercising the catch
        // branch: the stale request rejects AFTER the newer request
        // has succeeded. The error banner must NOT appear, because
        // that error belongs to a discarded scope.
        let rejectFirst: (reason: unknown) => void = () => {};
        const firstPromise = new Promise((_, reject) => {
            rejectFirst = reject;
        });
        mockFetch.mockReturnValueOnce(firstPromise);

        const clusterOverrides = [
            {
                name: 'cluster_only_probe',
                description: 'Only visible after the rerender',
                default_enabled: true,
                default_interval_seconds: 15,
                default_retention_days: 7,
                has_override: false,
                override_enabled: null,
                override_interval_seconds: null,
                override_retention_days: null,
            },
        ];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => clusterOverrides,
        });

        const { rerender } = render(
            <ThemeProvider theme={darkTheme}>
                <ProbeOverridesPanel scope="server" scopeId={1} />
            </ThemeProvider>,
        );

        rerender(
            <ThemeProvider theme={darkTheme}>
                <ProbeOverridesPanel scope="cluster" scopeId={5} />
            </ThemeProvider>,
        );

        await waitFor(() => {
            expect(
                screen.getByText('cluster_only_probe'),
            ).toBeInTheDocument();
        });

        // Reject the stale request with a recognisable message;
        // the error banner must NOT surface this stale failure.
        rejectFirst(new Error('stale request boom'));

        // Allow microtasks to settle. cluster_only_probe should
        // still be rendered and no stale error should be shown.
        await waitFor(() => {
            expect(
                screen.getByText('cluster_only_probe'),
            ).toBeInTheDocument();
        });
        expect(
            screen.queryByText('stale request boom'),
        ).not.toBeInTheDocument();
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
