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

import AdminAlertRules from '../AdminAlertRules';

const MOCK_ALERT_RULES = [
    {
        id: 1,
        name: 'high_cpu_usage',
        category: 'System',
        metric_name: 'cpu_usage_percent',
        default_operator: '>',
        default_threshold: 80,
        default_severity: 'warning',
        default_enabled: true,
    },
    {
        id: 2,
        name: 'disk_usage_critical',
        category: 'System',
        metric_name: 'disk_usage_percent',
        default_operator: '>',
        default_threshold: 90,
        default_severity: 'critical',
        default_enabled: true,
    },
    {
        id: 3,
        name: 'connection_utilization',
        category: 'Database',
        metric_name: 'connection_count',
        default_operator: '>',
        default_threshold: 100,
        default_severity: 'info',
        default_enabled: false,
    },
];

describe('AdminAlertRules', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('displays loading state initially', () => {
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<AdminAlertRules />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(screen.getByLabelText('Loading alert rules')).toBeInTheDocument();
    });

    it('renders alert rules from API', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('Alert defaults')).toBeInTheDocument();
        });

        // Check table headers
        expect(screen.getByText('Name')).toBeInTheDocument();
        expect(screen.getByText('Metric')).toBeInTheDocument();
        expect(screen.getByText('Condition')).toBeInTheDocument();
        expect(screen.getByText('Severity')).toBeInTheDocument();
        expect(screen.getByText('Enabled')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();

        // Check that rules are displayed with friendly names
        // high_cpu_usage falls through to title-casing: "High Cpu Usage"
        expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        // disk_usage_critical maps to "Critical Disk Usage" in FRIENDLY_ALERT_TITLES
        expect(screen.getByText('Critical Disk Usage')).toBeInTheDocument();
        // connection_utilization maps to "Connection Utilization" in FRIENDLY_ALERT_TITLES
        expect(screen.getByText('Connection Utilization')).toBeInTheDocument();

        // Verify API was called correctly
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/alert-rules');
    });

    it('handles API returning array directly', async () => {
        mockApiGet.mockResolvedValue(MOCK_ALERT_RULES);

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });
    });

    it('displays categories as group headers', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('System')).toBeInTheDocument();
        });

        expect(screen.getByText('Database')).toBeInTheDocument();
    });

    it('displays metric names in the table', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('cpu_usage_percent')).toBeInTheDocument();
        });

        expect(screen.getByText('disk_usage_percent')).toBeInTheDocument();
        expect(screen.getByText('connection_count')).toBeInTheDocument();
    });

    it('displays condition (operator and threshold) in the table', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('> 80')).toBeInTheDocument();
        });

        expect(screen.getByText('> 90')).toBeInTheDocument();
        expect(screen.getByText('> 100')).toBeInTheDocument();
    });

    it('displays severity chips with appropriate colors', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('warning')).toBeInTheDocument();
        });

        expect(screen.getByText('critical')).toBeInTheDocument();
        expect(screen.getByText('info')).toBeInTheDocument();
    });

    it('shows empty state when no rules exist', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: [] });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('No alert rules found.')).toBeInTheDocument();
        });
    });

    it('displays error message when API fails', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });
    });

    it('opens edit dialog when edit button is clicked', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        // Find the edit button in the row for high_cpu_usage
        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText(/Edit alert rule:/)).toBeInTheDocument();
        });
    });

    it('pre-populates edit dialog with current values', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        // Categories are sorted alphabetically: Database, System
        // First edit button is for connection_utilization (Database category, threshold: 100)
        // We want to click on a specific rule, so find the row and its edit button
        const highCpuRow = screen.getByText('High Cpu Usage').closest('tr');
        expect(highCpuRow).not.toBeNull();
        const editButton = (highCpuRow as HTMLElement).querySelector('[aria-label="edit alert rule"]');
        expect(editButton).not.toBeNull();
        await user.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toHaveValue(80);
        });

        // Check that the operator select has the correct value
        const operatorSelect = screen.getByLabelText('Operator');
        expect(operatorSelect).toHaveTextContent('>');

        // Check that the severity select has the correct value
        const severitySelect = screen.getByLabelText('Severity');
        expect(severitySelect).toHaveTextContent('warning');
    });

    it('closes edit dialog when Cancel is clicked', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByText(/Edit alert rule:/)).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Cancel/i }));

        await waitFor(() => {
            expect(screen.queryByText(/Edit alert rule:/)).not.toBeInTheDocument();
        });
    });

    it('saves changes when Save is clicked with valid data', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        // Find the specific row and its edit button
        const highCpuRow = screen.getByText('High Cpu Usage').closest('tr');
        expect(highCpuRow).not.toBeNull();
        const editButton = (highCpuRow as HTMLElement).querySelector('[aria-label="edit alert rule"]');
        await user.click(editButton as HTMLElement);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
        });

        // Change the threshold
        const thresholdInput = screen.getByLabelText('Threshold');
        await user.clear(thresholdInput);
        await user.type(thresholdInput, '85');

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(mockApiPut).toHaveBeenCalledWith(
                '/api/v1/alert-rules/1',
                expect.objectContaining({
                    default_threshold: 85,
                })
            );
        });
    });

    it('displays error when threshold is not a valid number', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
        });

        const thresholdInput = screen.getByLabelText('Threshold');
        await user.clear(thresholdInput);
        await user.type(thresholdInput, 'invalid');

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText('Threshold must be a valid number.')).toBeInTheDocument();
        });

        expect(mockApiPut).not.toHaveBeenCalled();
    });

    it('displays error message when save fails', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        mockApiPut.mockRejectedValue(new Error('Save failed'));
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText('Save failed')).toBeInTheDocument();
        });
    });

    it('displays success message after successful save', async () => {
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Save/i }));

        await waitFor(() => {
            expect(screen.getByText(/updated successfully/)).toBeInTheDocument();
        });
    });

    it('can dismiss error alerts', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminAlertRules />);

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
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        const editButtons = screen.getAllByRole('button', { name: /edit alert rule/i });
        await user.click(editButtons[0]);

        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
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
        mockApiGet.mockResolvedValue({ alert_rules: MOCK_ALERT_RULES });
        mockApiPut.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminAlertRules />);

        await waitFor(() => {
            expect(screen.getByText('High Cpu Usage')).toBeInTheDocument();
        });

        // Find the specific row and its edit button
        const highCpuRow = screen.getByText('High Cpu Usage').closest('tr');
        expect(highCpuRow).not.toBeNull();
        const editButton = (highCpuRow as HTMLElement).querySelector('[aria-label="edit alert rule"]');
        await user.click(editButton as HTMLElement);

        // Wait for dialog to open
        await waitFor(() => {
            expect(screen.getByLabelText('Threshold')).toBeInTheDocument();
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
                '/api/v1/alert-rules/1',
                expect.objectContaining({
                    default_enabled: false,
                })
            );
        });
    });
});
