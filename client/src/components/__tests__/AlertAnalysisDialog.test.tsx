/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Component coverage for AlertAnalysisDialog. The analysis hook and the
 * BaseAnalysisDialog shell are mocked so the tests cover the dialog's
 * own wiring: when it starts an analysis, what it passes the hook, the
 * toolbar and download content, and the guard that refuses to analyse
 * a system alert, which belongs to no connection.
 */

import type React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import AlertAnalysisDialog, {
    ANALYSIS_UNAVAILABLE_MESSAGE,
    type AlertAnalysisAlert,
} from '../AlertAnalysisDialog';
import { createPgedgeTheme } from '../../theme/pgedgeTheme';

const analyzeMock = vi.fn();
const resetMock = vi.fn();
let hookState = {
    analysis: null as string | null,
    loading: false,
    error: null as string | null,
    progressMessage: '',
    activeTools: [] as string[],
    analyze: analyzeMock,
    reset: resetMock,
};

vi.mock('../../hooks/useAlertAnalysis', () => ({
    useAlertAnalysis: () => hookState,
}));

vi.mock('../shared/BaseAnalysisDialog', () => ({
    BaseAnalysisDialog: (props: Record<string, unknown>) => {
        if (!props.open) {return null;}
        const mdProps = props.markdownContentProps as Record<string, unknown>;
        return (
            <div data-testid="base-dialog">
                <div data-testid="toolbar">
                    {props.toolbarContent as React.ReactNode}
                </div>
                <div data-testid="loading">{String(props.loading)}</div>
                <div data-testid="error">{String(props.error ?? '')}</div>
                <div data-testid="markdown">{String(props.markdownContent ?? '')}</div>
                <div data-testid="md-connection">{String(mdProps.connectionId)}</div>
                <button type="button" onClick={props.onDownload as () => void}>
                    download
                </button>
            </div>
        );
    },
}));

const downloadAsMarkdownMock = vi.fn();
vi.mock('../../utils/downloadMarkdown', () => ({
    downloadAsMarkdown: (...args: unknown[]) => downloadAsMarkdownMock(...args),
}));

const renderDialog = (
    alert: AlertAnalysisAlert | null,
    props: Partial<React.ComponentProps<typeof AlertAnalysisDialog>> = {},
    mode: 'light' | 'dark' = 'light',
) => render(
    <ThemeProvider theme={createPgedgeTheme(mode)}>
        <AlertAnalysisDialog
            open
            alert={alert}
            onClose={vi.fn()}
            {...props}
        />
    </ThemeProvider>,
);

const connectionAlert: AlertAnalysisAlert = {
    id: '5',
    severity: 'critical',
    title: 'High CPU Usage',
    description: 'CPU high',
    alertType: 'threshold',
    metricValue: 95.123,
    metricUnit: '%',
    operator: '>',
    thresholdValue: 80,
    connectionId: 7,
    databaseName: 'appdb',
    server: 'server-1',
    time: '5 min ago',
    triggeredAt: '2026-09-28T10:00:00Z',
    aiAnalysisMetricValue: '90',
};

const systemAlert: AlertAnalysisAlert = {
    id: 9,
    severity: 'warning',
    title: 'Anomaly detection degraded: Tier 2 embedding provider ollama failing',
    description: 'Provider failing',
    alertType: 'system',
    isSystem: true,
    time: 'just now',
};

describe('AlertAnalysisDialog', () => {
    beforeEach(() => {
        analyzeMock.mockReset();
        resetMock.mockReset();
        downloadAsMarkdownMock.mockReset();
        hookState = {
            analysis: null,
            loading: false,
            error: null,
            progressMessage: '',
            activeTools: [],
            analyze: analyzeMock,
            reset: resetMock,
        };
    });

    it('analyses a connection alert with its real connection', () => {
        renderDialog(connectionAlert);

        expect(analyzeMock).toHaveBeenCalledTimes(1);
        const input = analyzeMock.mock.calls[0][0];
        expect(input.id).toBe(5);
        expect(input.connectionId).toBe(7);
        expect(input.aiAnalysisMetricValue).toBe(90);
        expect(input.severity).toBe('critical');

        const toolbar = screen.getByTestId('toolbar');
        expect(toolbar).toHaveTextContent('server-1');
        expect(toolbar).toHaveTextContent('appdb');
        expect(toolbar).toHaveTextContent('95.12 % > 80 %');
        expect(screen.getByTestId('md-connection')).toHaveTextContent('7');
        expect(screen.getByTestId('error')).toHaveTextContent('');
    });

    it('fills defaults for a sparse connection alert', () => {
        renderDialog({ id: 3, connectionId: 2, metricValue: 'n/a', thresholdValue: 'x' }, {}, 'dark');

        const input = analyzeMock.mock.calls[0][0];
        expect(input.severity).toBe('');
        expect(input.title).toBe('');
        expect(screen.getByTestId('toolbar')).toHaveTextContent('Unknown');
        expect(screen.getByTestId('toolbar')).toHaveTextContent('n/a');
    });

    it('does not analyse a system alert and says why', () => {
        renderDialog(systemAlert);

        expect(analyzeMock).not.toHaveBeenCalled();
        expect(screen.getByTestId('error')).toHaveTextContent(ANALYSIS_UNAVAILABLE_MESSAGE);
        expect(screen.getByTestId('loading')).toHaveTextContent('false');
        expect(screen.getByTestId('toolbar')).toHaveTextContent('AI DBA Workbench');
    });

    it('does not analyse an alert that has no connection', () => {
        renderDialog({ ...connectionAlert, connectionId: undefined });

        expect(analyzeMock).not.toHaveBeenCalled();
        expect(screen.getByTestId('error')).toHaveTextContent(ANALYSIS_UNAVAILABLE_MESSAGE);
    });

    it('does not analyse while closed or without an alert', () => {
        renderDialog(null);
        renderDialog(connectionAlert, { open: false });
        expect(analyzeMock).not.toHaveBeenCalled();
    });

    it('reports a finished analysis and downloads it', () => {
        hookState.analysis = '## Summary\nAll good';
        const onAnalysisComplete = vi.fn();
        renderDialog(connectionAlert, { onAnalysisComplete });

        expect(onAnalysisComplete).toHaveBeenCalledWith('5', '## Summary\nAll good');
        expect(screen.getByTestId('markdown')).toHaveTextContent('Alert Analysis: High CPU Usage');

        fireEvent.click(screen.getByText('download'));
        const [content, filename] = downloadAsMarkdownMock.mock.calls[0];
        expect(filename).toMatch(/^alert-analysis-5-/);
        expect(content).toContain('- **Server:** server-1');
        expect(content).toContain('- **Database:** appdb');
        expect(content).toContain('- **Metric Value:** 95.123 %');
        expect(content).toContain('- **Threshold:** > 80 %');
    });

    it('downloads with placeholders for missing fields', () => {
        hookState.analysis = 'text';
        renderDialog({ connectionId: 1 });

        fireEvent.click(screen.getByText('download'));
        const [content, filename] = downloadAsMarkdownMock.mock.calls[0];
        expect(filename).toMatch(/^alert-analysis-unknown-/);
        expect(content).not.toContain('**Server:**');
        expect(content).toContain('- **Metric Value:** N/A');
        expect(content).toContain('- **Threshold:** N/A');
        expect(content).toContain('- **Alert Type:** threshold');
    });

    it('does not download when there is no analysis', () => {
        renderDialog(connectionAlert);
        fireEvent.click(screen.getByText('download'));
        expect(downloadAsMarkdownMock).not.toHaveBeenCalled();
    });
});
