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
import { screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiGet = vi.fn();
const mockApiPut = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiPut: (...args: unknown[]) => mockApiPut(...args),
}));

import AdminProbes from '../AdminProbes';

const MOCK_PROBES = [
    {
        id: 1,
        name: 'pg_stat_activity',
        description: 'Current database connections and queries',
        is_enabled: true,
        collection_interval_seconds: 60,
        retention_days: 7,
        connection_id: null,
    },
    {
        id: 2,
        name: 'pg_stat_database',
        description: 'Per-database statistics',
        is_enabled: true,
        collection_interval_seconds: 300,
        retention_days: 30,
        connection_id: null,
    },
    {
        id: 3,
        name: 'pg_stat_replication',
        description: 'Replication status and lag',
        is_enabled: false,
        collection_interval_seconds: 120,
        retention_days: 14,
        connection_id: null,
    },
];

describe('AdminProbes', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('displays loading state initially', () => {
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<AdminProbes />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(screen.getByLabelText('Loading probes')).toBeInTheDocument();
    });

    it('renders probes from API', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Probe defaults')).toBeInTheDocument();
        });

        // Check table headers
        expect(screen.getByText('Name')).toBeInTheDocument();
        expect(screen.getByText('Description')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Interval')).toBeInTheDocument();
        expect(screen.getByText('Retention (days)')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();

        // Check that probes are displayed with friendly names
        // These are the friendly names from FRIENDLY_PROBE_NAMES
        expect(screen.getByText('Database Activity')).toBeInTheDocument(); // pg_stat_activity
        expect(screen.getByText('Database Statistics')).toBeInTheDocument(); // pg_stat_database
        expect(screen.getByText('Replication Status')).toBeInTheDocument(); // pg_stat_replication

        // Verify API was called correctly
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/probe-configs');
    });

    it('handles API returning probe_configs wrapped', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });
    });

    it('displays probe names with technical names as caption', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('pg_stat_activity')).toBeInTheDocument();
        });

        expect(screen.getByText('pg_stat_database')).toBeInTheDocument();
        expect(screen.getByText('pg_stat_replication')).toBeInTheDocument();
    });

    it('displays collection intervals in the table', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('60s')).toBeInTheDocument();
        });

        expect(screen.getByText('300s')).toBeInTheDocument();
        expect(screen.getByText('120s')).toBeInTheDocument();
    });

    it('displays retention days in the table', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('7')).toBeInTheDocument();
        });

        expect(screen.getByText('30')).toBeInTheDocument();
        expect(screen.getByText('14')).toBeInTheDocument();
    });

    it('shows empty state when no probes exist', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: [] });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('No probe configurations found.')).toBeInTheDocument();
        });
    });

    it('displays error message when API fails', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });
    });

    it('opens edit dialog when edit button is clicked', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText(/Edit probe:/)).toBeInTheDocument();
        });
    });

    it('pre-populates edit dialog with current values', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toHaveValue(60);
        });

        expect(screen.getByLabelText(/Retention Days/i)).toHaveValue(7);
    });

    it('closes edit dialog when Cancel is clicked', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText(/Edit probe:/)).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Cancel/i }));

        await waitFor(() => {
            expect(screen.queryByText(/Edit probe:/)).not.toBeInTheDocument();
        });
    });

    it('saves changes when Save is clicked', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toBeInTheDocument();
        });

        // Change the interval
        const intervalInput = screen.getByLabelText(/Collection Interval/i);
        await user.clear(intervalInput);
        await user.type(intervalInput, '90');

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/probe-configs/1',
                expect.objectContaining({
                    collection_interval_seconds: 90,
                })
            );
        });
    });

    it('displays error message when save fails', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockRejectedValue(new Error('Save failed'));
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText('Save failed')).toBeInTheDocument();
        });
    });

    it('displays success message after successful save', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText(/updated successfully/)).toBeInTheDocument();
        });
    });

    it('can dismiss error alerts', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });

        const closeButton = screen.getByRole('button', { name: /close/i });
        fireEvent.click(closeButton);

        await waitFor(() => {
            expect(screen.queryByText('Network error')).not.toBeInTheDocument();
        });
    });

    it('can dismiss success alerts', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText(/updated successfully/)).toBeInTheDocument();
        });

        const closeButtons = screen.getAllByRole('button', { name: /close/i });
        fireEvent.click(closeButtons[0]);

        await waitFor(() => {
            expect(screen.queryByText(/updated successfully/)).not.toBeInTheDocument();
        });
    });

    it('toggles enabled state in edit dialog', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        // Wait for dialog to open by looking for the Collection Interval field
        await waitFor(() => {
            expect(screen.getByLabelText(/Collection Interval/i)).toBeInTheDocument();
        });

        // Find the switch in the dialog
        const switches = screen.getAllByRole('checkbox');
        const enabledSwitch = switches.find((s) =>
            s.closest('[class*="MuiDialogContent"]')
        );
        expect(enabledSwitch).toBeDefined();
        expect(enabledSwitch).toBeChecked();

        // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
        await user.click(enabledSwitch!);

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/probe-configs/1',
                expect.objectContaining({
                    is_enabled: false,
                })
            );
        });
    });

    it('updates retention days in edit dialog', async () => {
        mockApiGet.mockResolvedValue({ probe_configs: MOCK_PROBES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminProbes />);

        await waitFor(() => {
            expect(screen.getByText('Database Activity')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit probe/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText(/Retention Days/i)).toBeInTheDocument();
        });

        const retentionInput = screen.getByLabelText(/Retention Days/i);
        await user.clear(retentionInput);
        await user.type(retentionInput, '14');

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/probe-configs/1',
                expect.objectContaining({
                    retention_days: 14,
                })
            );
        });
    });
});
