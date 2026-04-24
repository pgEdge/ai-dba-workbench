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
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { ThemeProvider, createTheme } from '@mui/material';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import AlertOverrideEditDialog from '../AlertOverrideEditDialog';

const mockFetch = vi.fn();
global.fetch = mockFetch;

const darkTheme = createTheme({ palette: { mode: 'dark' } });

/**
 * Find a MUI Select combobox element by its associated InputLabel text.
 * MUI Select does not always create a proper aria label association, so
 * we locate the FormControl via its label and query the combobox within.
 */
const getSelectByLabel = (labelText: string): HTMLElement => {
    const label = screen.getByText(labelText, { selector: 'label' });
    const formControl = label.closest('.MuiFormControl-root') as HTMLElement;
    return within(formControl).getByRole('combobox');
};

const renderWithTheme = (component: React.ReactElement) => {
    return render(
        <ThemeProvider theme={darkTheme}>
            {component}
        </ThemeProvider>
    );
};

const mockContextResponse = {
    hierarchy: {
        connection_id: 5,
        cluster_id: 2,
        group_id: 1,
        server_name: 'pg-primary-01',
        cluster_name: 'us-east',
        group_name: 'production',
    },
    rule: {
        id: 12,
        name: 'High CPU Usage',
        description: 'CPU usage exceeds threshold',
        category: 'System',
        metric_name: 'cpu_usage_percent',
        metric_unit: '%',
        default_operator: '>',
        default_threshold: 80,
        default_severity: 'warning',
        default_enabled: true,
    },
    overrides: {
        server: null,
        cluster: null,
        group: {
            operator: '>',
            threshold: 75,
            severity: 'critical',
            enabled: true,
        },
    },
};

const mockStandaloneContextResponse = {
    ...mockContextResponse,
    hierarchy: {
        ...mockContextResponse.hierarchy,
        cluster_id: null,
        group_id: null,
        cluster_name: null,
        group_name: null,
    },
    overrides: {
        server: null,
        cluster: null,
        group: null,
    },
};

const mockAlert = {
    ruleId: 12,
    connectionId: 5,
    title: 'High CPU Usage',
    server: 'pg-primary-01',
};

describe('AlertOverrideEditDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders nothing when closed', () => {
        renderWithTheme(
            <AlertOverrideEditDialog
                open={false}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        expect(screen.queryByText('Edit Alert Override')).not.toBeInTheDocument();
    });

    it('shows loading state when open', () => {
        mockFetch.mockReturnValue(new Promise(() => {}));

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('fetches context and displays form', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockContextResponse,
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        expect(getSelectByLabel('Scope')).toBeInTheDocument();
        expect(getSelectByLabel('Operator')).toBeInTheDocument();
        expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
        expect(getSelectByLabel('Severity')).toBeInTheDocument();
    });

    it('shows all scope options for full hierarchy', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockContextResponse,
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        const scopeSelect = getSelectByLabel('Scope');
        fireEvent.mouseDown(scopeSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        expect(screen.getByText('Server: pg-primary-01')).toBeVisible();
        expect(screen.getByText('Cluster: us-east')).toBeVisible();
        const groupOptions = screen.getAllByText(/Group: production/);
        expect(groupOptions.length).toBeGreaterThanOrEqual(1);
    });

    it('shows only server scope for standalone server', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockStandaloneContextResponse,
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        const scopeSelect = getSelectByLabel('Scope');
        fireEvent.mouseDown(scopeSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        const serverOptions = screen.getAllByText(/Server: pg-primary-01/);
        expect(serverOptions.length).toBeGreaterThanOrEqual(1);
        expect(screen.queryByText(/Cluster:/)).not.toBeInTheDocument();
        expect(screen.queryByText(/Group:/)).not.toBeInTheDocument();
    });

    it('shows error when fetch fails', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: false,
            json: async () => ({}),
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(
                screen.getByText('Failed to fetch override context')
            ).toBeInTheDocument();
        });
    });

    it('calls onClose when cancel is clicked', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockContextResponse,
        });

        const mockOnClose = vi.fn();

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={mockOnClose}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Cancel'));

        expect(mockOnClose).toHaveBeenCalled();
    });

    it('saves override via PUT', async () => {
        mockFetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => mockContextResponse,
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({ status: 'ok' }),
            });

        const mockOnClose = vi.fn();

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={mockOnClose}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Save'));

        await waitFor(() => {
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/alert-overrides/group/1/12',
                expect.objectContaining({
                    method: 'PUT',
                    credentials: 'include',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        operator: '>',
                        threshold: 75,
                        severity: 'critical',
                        enabled: true,
                    }),
                })
            );
        });
    });

    it('shows info alert when no override at selected scope', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockContextResponse,
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        // Default scope is 'group' which has an override; switch to 'server'
        const scopeSelect = getSelectByLabel('Scope');
        fireEvent.mouseDown(scopeSelect);

        await waitFor(() => {
            expect(screen.getByRole('listbox')).toBeInTheDocument();
        });

        fireEvent.click(screen.getByText('Server: pg-primary-01'));

        await waitFor(() => {
            expect(
                screen.getByText(/No override at this scope/)
            ).toBeInTheDocument();
        });
    });

    it('pre-populates with group override values when group has override', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            json: async () => mockContextResponse,
        });

        renderWithTheme(
            <AlertOverrideEditDialog
                open={true}
                alert={mockAlert}
                onClose={vi.fn()}
            />
        );

        await waitFor(() => {
            expect(screen.getByText('High CPU Usage')).toBeInTheDocument();
        });

        const thresholdInput = screen.getByLabelText('Threshold');
        expect(thresholdInput).toHaveValue(75);
    });
});
