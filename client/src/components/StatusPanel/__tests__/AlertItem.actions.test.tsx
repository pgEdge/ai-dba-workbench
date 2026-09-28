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
import { render, screen, fireEvent, within } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect, vi } from 'vitest';
import AlertItem from '../AlertItem';
import GroupedAlertItem from '../GroupedAlertItem';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';
import type { TransformedAlert } from '../types';

const renderWithTheme = (ui: React.ReactElement, mode: 'light' | 'dark' = 'dark') =>
    render(<ThemeProvider theme={createPgedgeTheme(mode)}>{ui}</ThemeProvider>);

// The acknowledge and restore buttons sit inside a span so their
// tooltip still works when the button is disabled; the tooltip labels
// the span, so find the button inside it.
const ackButton = (label: string, index = 0): HTMLButtonElement => {
    const button = screen.getAllByLabelText(label)[index].querySelector('button');
    if (!button) {throw new Error(`no button for ${label}`);}
    return button;
};

const connectionAlert: TransformedAlert = {
    id: 1,
    severity: 'critical',
    title: 'High CPU Usage',
    description: 'CPU usage exceeded threshold',
    time: '5 min ago',
    server: 'server-1',
    connectionId: 7,
    databaseName: 'appdb',
    objectName: 'public.orders',
    alertType: 'threshold',
    ruleId: 3,
    metricValue: 95,
    thresholdValue: 80,
    operator: '>',
    metricUnit: '%',
};

const systemAlert: TransformedAlert = {
    id: 2,
    severity: 'warning',
    title: 'Anomaly detection degraded: Tier 2 embedding provider ollama failing',
    description: 'Provider ollama has failed 3 consecutive times.',
    time: 'just now',
    objectName: 'ollama/nomic-embed-text',
    alertType: 'system',
    isSystem: true,
};

describe('AlertItem actions', () => {
    it('calls the analyse, override and acknowledge handlers', () => {
        const onAnalyze = vi.fn();
        const onEditOverride = vi.fn();
        const onAcknowledge = vi.fn();
        renderWithTheme(
            <AlertItem
                alert={connectionAlert}
                showServer
                onAnalyze={onAnalyze}
                onEditOverride={onEditOverride}
                onAcknowledge={onAcknowledge}
            />,
        );

        expect(screen.getByText('server-1')).toBeInTheDocument();
        expect(screen.getByText('appdb')).toBeInTheDocument();
        expect(screen.getByText('public.orders')).toBeInTheDocument();
        expect(screen.getByText('Threshold')).toBeInTheDocument();

        fireEvent.click(screen.getByLabelText('Analyze with AI'));
        expect(onAnalyze).toHaveBeenCalledWith(connectionAlert);

        fireEvent.click(screen.getByLabelText('Edit alert override'));
        expect(onEditOverride).toHaveBeenCalledWith(connectionAlert);

        fireEvent.click(ackButton('Acknowledge'));
        expect(onAcknowledge).toHaveBeenCalledWith(connectionAlert);
    });

    it('restores an acknowledged alert and shows the ack details', () => {
        const onUnacknowledge = vi.fn();
        renderWithTheme(
            <AlertItem
                alert={{
                    ...connectionAlert,
                    acknowledgedAt: '2026-09-28T10:00:00Z',
                    acknowledgedBy: 'jane.doe',
                    ackMessage: 'Known issue',
                    falsePositive: true,
                    aiAnalysis: 'cached',
                }}
                onUnacknowledge={onUnacknowledge}
                onAnalyze={vi.fn()}
            />,
            'light',
        );

        expect(screen.getByText('Acked by jane.doe: Known issue')).toBeInTheDocument();
        expect(screen.getByText('False Positive')).toBeInTheDocument();
        expect(screen.getByLabelText('View cached analysis')).toBeInTheDocument();

        fireEvent.click(ackButton('Restore to active'));
        expect(onUnacknowledge).toHaveBeenCalledWith(1);
    });

    it('disables the restore button while the request is in flight', () => {
        renderWithTheme(
            <AlertItem
                alert={{ ...connectionAlert, acknowledgedAt: '2026-09-28T10:00:00Z' }}
                isUnacknowledging={() => true}
            />,
        );
        expect(ackButton('Restore to active')).toBeDisabled();
    });

    it('labels an anomaly alert', () => {
        renderWithTheme(
            <AlertItem alert={{ ...connectionAlert, alertType: 'anomaly' }} />,
        );
        expect(screen.getByText('Anomaly')).toBeInTheDocument();
    });

    it('hides the server chip when showServer is false', () => {
        renderWithTheme(<AlertItem alert={connectionAlert} />);
        expect(screen.queryByText('server-1')).not.toBeInTheDocument();
    });
});

describe('AlertItem system alerts', () => {
    it('labels the alert as coming from the Workbench, not a server', () => {
        renderWithTheme(<AlertItem alert={systemAlert} showServer />);

        expect(screen.getByText('AI DBA Workbench')).toBeInTheDocument();
        expect(screen.getByText('System')).toBeInTheDocument();
        expect(screen.queryByText('Threshold')).not.toBeInTheDocument();
        expect(screen.getByText('ollama/nomic-embed-text')).toBeInTheDocument();
        expect(screen.getByTestId('MemoryIcon')).toBeInTheDocument();
        expect(screen.queryByTestId('TableChartIcon')).not.toBeInTheDocument();
        expect(screen.getByText(/failed 3 consecutive times/)).toBeInTheDocument();
    });

    it('shows the Workbench label even where server chips are hidden', () => {
        renderWithTheme(<AlertItem alert={systemAlert} />, 'light');
        expect(screen.getByText('AI DBA Workbench')).toBeInTheDocument();
    });

    it('treats a system alert type without the flag as a system alert', () => {
        renderWithTheme(
            <AlertItem alert={{ ...systemAlert, isSystem: undefined }} />,
        );
        expect(screen.getByText('System')).toBeInTheDocument();
    });

    it('hides the analyse action but keeps acknowledge', () => {
        const onAcknowledge = vi.fn();
        renderWithTheme(
            <AlertItem
                alert={systemAlert}
                onAnalyze={vi.fn()}
                onEditOverride={vi.fn()}
                onAcknowledge={onAcknowledge}
            />,
        );

        expect(screen.queryByLabelText('Analyze with AI')).not.toBeInTheDocument();
        expect(screen.queryByLabelText('Edit alert override')).not.toBeInTheDocument();
        fireEvent.click(ackButton('Acknowledge'));
        expect(onAcknowledge).toHaveBeenCalledWith(systemAlert);
    });
});

describe('GroupedAlertItem', () => {
    const second: TransformedAlert = {
        ...connectionAlert,
        id: 11,
        server: 'server-2',
        acknowledgedAt: '2026-09-28T10:00:00Z',
        acknowledgedBy: 'jane.doe',
        falsePositive: true,
    };

    it('renders the group and wires every action', () => {
        const onAnalyze = vi.fn();
        const onEditOverride = vi.fn();
        const onAcknowledge = vi.fn();
        const onUnacknowledge = vi.fn();
        const onAcknowledgeGroup = vi.fn();
        renderWithTheme(
            <GroupedAlertItem
                title="High CPU Usage"
                alerts={[connectionAlert, second]}
                showServer
                onAnalyze={onAnalyze}
                onEditOverride={onEditOverride}
                onAcknowledge={onAcknowledge}
                onUnacknowledge={onUnacknowledge}
                onAcknowledgeGroup={onAcknowledgeGroup}
            />,
        );

        expect(screen.getByText('2 instances')).toBeInTheDocument();
        expect(screen.getByText('server-1')).toBeInTheDocument();
        expect(screen.getByText('server-2')).toBeInTheDocument();
        expect(screen.getByText('Acked by jane.doe')).toBeInTheDocument();
        expect(screen.getByText('False Positive')).toBeInTheDocument();

        fireEvent.click(screen.getByLabelText('Acknowledge all in group'));
        expect(onAcknowledgeGroup).toHaveBeenCalledWith([connectionAlert]);

        fireEvent.click(screen.getAllByLabelText('Analyze with AI')[0]);
        expect(onAnalyze).toHaveBeenCalledWith(connectionAlert);

        fireEvent.click(screen.getAllByLabelText('Edit alert override')[0]);
        expect(onEditOverride).toHaveBeenCalledWith(connectionAlert);

        fireEvent.click(ackButton('Acknowledge'));
        expect(onAcknowledge).toHaveBeenCalledWith(connectionAlert);

        fireEvent.click(ackButton('Restore to active'));
        expect(onUnacknowledge).toHaveBeenCalledWith(11);
    });

    it('collapses and expands the instance list', () => {
        renderWithTheme(
            <GroupedAlertItem
                title="High CPU Usage"
                alerts={[connectionAlert, { ...connectionAlert, id: 12 }]}
            />,
            'light',
        );

        fireEvent.click(screen.getByText('High CPU Usage'));
        expect(screen.getByTestId('ExpandMoreIcon')).toBeInTheDocument();
        fireEvent.click(screen.getByText('High CPU Usage'));
        expect(screen.getByTestId('ExpandLessIcon')).toBeInTheDocument();
    });

    it('omits the group acknowledge action when every alert is acknowledged', () => {
        renderWithTheme(
            <GroupedAlertItem
                title="High CPU Usage"
                alerts={[second, { ...second, id: 13 }]}
                onAcknowledgeGroup={vi.fn()}
                isUnacknowledging={() => true}
            />,
        );
        expect(screen.queryByLabelText('Acknowledge all in group')).not.toBeInTheDocument();
        expect(ackButton('Restore to active', 0)).toBeDisabled();
        expect(ackButton('Restore to active', 1)).toBeDisabled();
    });

    it('labels system instances and hides their analyse action', () => {
        const onAnalyze = vi.fn();
        const { container } = renderWithTheme(
            <GroupedAlertItem
                title={systemAlert.title}
                alerts={[systemAlert, { ...systemAlert, id: 3 }]}
                onAnalyze={onAnalyze}
            />,
        );

        const list = within(container);
        expect(list.getAllByText('AI DBA Workbench')).toHaveLength(2);
        expect(list.getAllByText('System')).toHaveLength(2);
        expect(list.getAllByTestId('MemoryIcon')).toHaveLength(2);
        expect(list.queryByLabelText('Analyze with AI')).not.toBeInTheDocument();
    });
});
