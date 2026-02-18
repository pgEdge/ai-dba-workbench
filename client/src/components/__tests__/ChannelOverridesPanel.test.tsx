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
import ChannelOverridesPanel from '../ChannelOverridesPanel';

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

const mockChannels = [
    {
        channel_id: 1,
        channel_name: 'Production Alerts',
        channel_type: 'slack',
        description: 'Main production channel',
        is_estate_default: true,
        has_override: false,
        override_enabled: null,
    },
    {
        channel_id: 2,
        channel_name: 'Dev Webhook',
        channel_type: 'webhook',
        description: null,
        is_estate_default: false,
        has_override: true,
        override_enabled: true,
    },
];

describe('ChannelOverridesPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders loading state', () => {
        mockFetch.mockReturnValue(new Promise(() => {}));

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('renders channels from API', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockChannels,
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Production Alerts')).toBeInTheDocument();
        });
        expect(screen.getByText('Dev Webhook')).toBeInTheDocument();

        expect(mockFetch).toHaveBeenCalledWith(
            '/api/v1/channel-overrides/server/1',
            expect.objectContaining({ credentials: 'include' })
        );
    });

    it('renders table headers', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockChannels,
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Name')).toBeInTheDocument();
        });
        expect(screen.getByText('Type')).toBeInTheDocument();
        expect(screen.getByText('Description')).toBeInTheDocument();
        expect(screen.getByText('Estate Default')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();
    });

    it('shows channel type as chip', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockChannels,
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('slack')).toBeInTheDocument();
        });
        expect(screen.getByText('webhook')).toBeInTheDocument();
    });

    it('shows em dash for null description', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockChannels,
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Dev Webhook')).toBeInTheDocument();
        });

        // The description column for Dev Webhook (null description) shows an em dash
        const devRow = screen.getByText('Dev Webhook').closest('tr');
        expect(devRow).not.toBeNull();
        const cells = (devRow as HTMLElement).querySelectorAll('td');
        expect(cells[2].textContent).toBe('\u2014');
    });

    it('toggle switch calls PUT with correct payload', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Production Alerts')).toBeInTheDocument();
        });

        // Production Alerts: has_override=false, is_estate_default=true,
        // so effective enabled is true; toggling should send enabled=false
        const prodRow = screen.getByText('Production Alerts').closest('tr');
        expect(prodRow).not.toBeNull();
        const prodCheckbox = (prodRow as HTMLElement).querySelector('input[type="checkbox"]');
        expect(prodCheckbox).not.toBeNull();
        fireEvent.click(prodCheckbox as HTMLElement);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/channel-overrides/server/1/1',
                expect.objectContaining({
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ enabled: false }),
                })
            );
        });
    });

    it('toggle switch for overridden channel sends correct value', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Dev Webhook')).toBeInTheDocument();
        });

        // Dev Webhook: has_override=true, override_enabled=true,
        // so toggling should send enabled=false
        const devRow = screen.getByText('Dev Webhook').closest('tr');
        expect(devRow).not.toBeNull();
        const devCheckbox = (devRow as HTMLElement).querySelector('input[type="checkbox"]');
        expect(devCheckbox).not.toBeNull();
        fireEvent.click(devCheckbox as HTMLElement);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/channel-overrides/server/1/2',
                expect.objectContaining({
                    method: 'PUT',
                    body: JSON.stringify({ enabled: false }),
                })
            );
        });
    });

    it('reset button calls DELETE and refreshes data', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({}),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Dev Webhook')).toBeInTheDocument();
        });

        // Only the overridden channel (Dev Webhook) should have a reset button
        const resetButton = screen.getByLabelText('reset override to default');
        fireEvent.click(resetButton);

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/channel-overrides/server/1/2',
                expect.objectContaining({
                    method: 'DELETE',
                    credentials: 'include',
                })
            );
        });
    });

    it('only shows reset button on rows with overrides', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockChannels,
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Production Alerts')).toBeInTheDocument();
        });

        // Only one channel has has_override=true, so one reset button
        const resetButtons = screen.getAllByLabelText('reset override to default');
        expect(resetButtons).toHaveLength(1);
    });

    it('shows error when fetch fails', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: false,
            status: 500,
            text: async () => JSON.stringify({ error: 'Failed to fetch channel overrides' }),
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Failed to fetch channel overrides')).toBeInTheDocument();
        });
    });

    it('shows error when toggle fails', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            })
            .mockResolvedValueOnce({
                ok: false,
                status: 400,
                text: async () => JSON.stringify({ error: 'Permission denied' }),
            });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Production Alerts')).toBeInTheDocument();
        });

        const prodRow = screen.getByText('Production Alerts').closest('tr');
        expect(prodRow).not.toBeNull();
        const prodCheckbox = (prodRow as HTMLElement).querySelector('input[type="checkbox"]');
        expect(prodCheckbox).not.toBeNull();
        fireEvent.click(prodCheckbox as HTMLElement);

        await waitFor(() => {
            expect(screen.getByText('Permission denied')).toBeInTheDocument();
        });
    });

    it('shows error when reset fails', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockChannels,
            })
            .mockResolvedValueOnce({
                ok: false,
                status: 400,
                text: async () => JSON.stringify({ error: 'Reset not allowed' }),
            });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('Dev Webhook')).toBeInTheDocument();
        });

        const resetButton = screen.getByLabelText('reset override to default');
        fireEvent.click(resetButton);

        await waitFor(() => {
            expect(screen.getByText('Reset not allowed')).toBeInTheDocument();
        });
    });

    it('shows empty state when no channels are returned', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="server" scopeId={1} />
        );

        await waitFor(() => {
            expect(screen.getByText('No notification channels found.')).toBeInTheDocument();
        });
    });

    it('uses correct API path for different scopes', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="cluster" scopeId={5} />
        );

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/channel-overrides/cluster/5',
                expect.objectContaining({ credentials: 'include' })
            );
        });
    });

    it('uses correct API path for group scope', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => [],
        });

        renderWithTheme(
            <ChannelOverridesPanel scope="group" scopeId={10} />
        );

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/channel-overrides/group/10',
                expect.objectContaining({ credentials: 'include' })
            );
        });
    });
});
