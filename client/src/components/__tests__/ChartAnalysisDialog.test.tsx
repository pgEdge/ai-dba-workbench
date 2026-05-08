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
 * Component coverage for ChartAnalysisDialog. The dialog wires the
 * `useChartAnalysis` hook into a `BaseAnalysisDialog` shell and adds
 * chart-specific toolbar pills (connection / database / time range).
 * The tests mock the hook and the BaseAnalysisDialog shell so the
 * focus stays on the wiring logic owned by this component:
 *
 *   - the open/close lifecycle that arms `analyze()` exactly once
 *     when the dialog opens
 *   - the conditional toolbar pills driven by the analysis context
 *   - the download handler that builds a markdown report and calls
 *     `downloadAsMarkdown`
 *
 * The hook and `downloadAsMarkdown` are mocked so we can drive each
 * branch deterministically.
 */

import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ChartAnalysisDialog from '../ChartAnalysisDialog';
import type { ChartAnalysisContext, ChartData } from '../Chart/types';

// useChartAnalysis is the productive hook; we mock it entirely.
const analyzeMock = vi.fn();
let hookState = {
    analysis: null as string | null,
    loading: false,
    error: null as string | null,
    progressMessage: '',
    activeTools: [] as string[],
    analyze: analyzeMock,
};

vi.mock('../../hooks/useChartAnalysis', () => ({
    useChartAnalysis: () => hookState,
}));

// Mock the heavy BaseAnalysisDialog shell with a thin stand-in that
// exposes the props the dialog forwards. This lets the assertions
// focus on ChartAnalysisDialog's own logic without depending on the
// shell's internal markup.
vi.mock('../shared/BaseAnalysisDialog', () => ({
    BaseAnalysisDialog: (props: Record<string, unknown>) => {
        const open = props.open as boolean;
        if (!open) {return null;}
        return (
            <div data-testid="base-dialog">
                <div data-testid="title">{String(props.title)}</div>
                <div data-testid="toolbar">
                    {props.toolbarContent as React.ReactNode}
                </div>
                <div data-testid="markdown">
                    {String(props.markdownContent ?? '')}
                </div>
                <button
                    type="button"
                    onClick={props.onDownload as () => void}
                >
                    download
                </button>
                <button
                    type="button"
                    onClick={props.onClose as () => void}
                >
                    close
                </button>
            </div>
        );
    },
    default: (props: Record<string, unknown>) => {
        const open = props.open as boolean;
        if (!open) {return null;}
        return <div data-testid="default-dialog" />;
    },
}));

// Mock downloadMarkdown / textHelpers so we can observe their inputs
// without exercising the real DOM-blob/anchor side effects.
const downloadAsMarkdownMock = vi.fn();
vi.mock('../../utils/downloadMarkdown', () => ({
    downloadAsMarkdown: (...args: unknown[]) => downloadAsMarkdownMock(...args),
}));

vi.mock('../../utils/textHelpers', () => ({
    slugify: (s: string) => s.toLowerCase().replace(/\s+/g, '-'),
}));

// MarkdownExports / analysisStyles are imported only for their
// returned style objects. Stub them with simple sentinels so the
// dialog can render without pulling MUI typography internals.
vi.mock('../shared/MarkdownExports', () => ({
    getIconColorSx: () => ({}),
}));

vi.mock('../analysisStyles', () => ({
    getConnectionBadgeSx: () => ({}),
    getDatabaseBadgeSx: () => ({}),
    getDatabaseTextSx: () => ({}),
    sxMonoSmall: {},
}));

const baseChartData: ChartData = {
    categories: ['a', 'b'],
    series: [{ name: 'cpu', data: [1, 2] }],
};

const baseAnalysisContext: ChartAnalysisContext = {
    metricDescription: 'CPU usage',
    connectionId: 5,
    connectionName: 'prod-db',
    databaseName: 'app',
    timeRange: 'last 1 hour',
};

const renderDialog = (
    overrides: Partial<React.ComponentProps<typeof ChartAnalysisDialog>> = {},
) =>
    render(
        <ChartAnalysisDialog
            open={true}
            onClose={vi.fn()}
            isDark={false}
            analysisContext={baseAnalysisContext}
            chartData={baseChartData}
            {...overrides}
        />,
    );

describe('ChartAnalysisDialog', () => {
    beforeEach(() => {
        analyzeMock.mockReset();
        downloadAsMarkdownMock.mockReset();
        hookState = {
            analysis: null,
            loading: false,
            error: null,
            progressMessage: '',
            activeTools: [],
            analyze: analyzeMock,
        };
    });

    it('renders nothing when closed', () => {
        renderDialog({ open: false });
        expect(screen.queryByTestId('base-dialog')).not.toBeInTheDocument();
    });

    it('triggers analyze() once when the dialog opens with no cached analysis', () => {
        renderDialog();
        expect(analyzeMock).toHaveBeenCalledTimes(1);
        expect(analyzeMock).toHaveBeenCalledWith(
            expect.objectContaining({
                metricDescription: 'CPU usage',
                connectionId: 5,
                connectionName: 'prod-db',
                databaseName: 'app',
                timeRange: 'last 1 hour',
                data: baseChartData,
            }),
        );
    });

    it('does not re-trigger analyze() on subsequent renders while open', () => {
        const { rerender } = renderDialog();
        expect(analyzeMock).toHaveBeenCalledTimes(1);
        rerender(
            <ChartAnalysisDialog
                open={true}
                onClose={vi.fn()}
                isDark={false}
                analysisContext={baseAnalysisContext}
                chartData={{
                    ...baseChartData,
                    series: [{ name: 'cpu', data: [3, 4] }],
                }}
            />,
        );
        // The chart data changed (dashboard polling) but analyze must
        // not run again until the dialog is closed and re-opened.
        expect(analyzeMock).toHaveBeenCalledTimes(1);
    });

    it('does not call analyze() when an analysis is already present', () => {
        hookState = { ...hookState, analysis: '## Existing analysis' };
        renderDialog();
        expect(analyzeMock).not.toHaveBeenCalled();
    });

    it('renders the metric description in the toolbar', () => {
        renderDialog();
        const toolbar = screen.getByTestId('toolbar');
        expect(toolbar).toHaveTextContent('CPU usage');
    });

    it('falls back to "Chart" when metric description is empty', () => {
        renderDialog({
            analysisContext: {
                ...baseAnalysisContext,
                metricDescription: '',
            },
        });
        expect(screen.getByTestId('toolbar')).toHaveTextContent('Chart');
    });

    it('renders all four toolbar pills when context fields are present', () => {
        renderDialog();
        const toolbar = screen.getByTestId('toolbar');
        expect(toolbar).toHaveTextContent('prod-db');
        expect(toolbar).toHaveTextContent('app');
        expect(toolbar).toHaveTextContent('last 1 hour');
    });

    it('omits optional pills when their fields are absent', () => {
        renderDialog({
            analysisContext: {
                metricDescription: 'CPU',
            },
        });
        const toolbar = screen.getByTestId('toolbar');
        expect(toolbar).not.toHaveTextContent('prod-db');
        expect(toolbar).not.toHaveTextContent('app');
        expect(toolbar).not.toHaveTextContent('last 1 hour');
    });

    it('passes a markdownContent string when analysis is available', () => {
        hookState = { ...hookState, analysis: '## Result' };
        renderDialog();
        const md = screen.getByTestId('markdown');
        expect(md.textContent).toContain('# Chart Analysis: CPU usage');
        expect(md.textContent).toContain('## Result');
    });

    it('omits markdownContent when analysis is null', () => {
        renderDialog();
        expect(screen.getByTestId('markdown').textContent).toBe('');
    });

    it('does not call downloadAsMarkdown when analysis is null', () => {
        renderDialog();
        fireEvent.click(screen.getByText('download'));
        expect(downloadAsMarkdownMock).not.toHaveBeenCalled();
    });

    it('builds a markdown report and calls downloadAsMarkdown with a slugged filename', () => {
        hookState = { ...hookState, analysis: '## Insights\n- thing' };
        renderDialog();
        fireEvent.click(screen.getByText('download'));
        expect(downloadAsMarkdownMock).toHaveBeenCalledTimes(1);
        const [content, filename] = downloadAsMarkdownMock.mock.calls[0];
        expect(content).toContain('# Chart Analysis Report');
        expect(content).toContain('- **Metric:** CPU usage');
        expect(content).toContain('- **Connection:** prod-db');
        expect(content).toContain('- **Database:** app');
        expect(content).toContain('- **Time Range:** last 1 hour');
        expect(content).toContain('## Insights');
        expect(filename).toMatch(/^chart-analysis-cpu-usage-\d{4}-\d{2}-\d{2}\.md$/);
    });

    it('omits absent context lines from the downloaded markdown', () => {
        hookState = { ...hookState, analysis: 'plain' };
        renderDialog({
            analysisContext: {
                metricDescription: 'Network',
            },
        });
        fireEvent.click(screen.getByText('download'));
        const [content] = downloadAsMarkdownMock.mock.calls[0];
        expect(content).not.toContain('Connection:');
        expect(content).not.toContain('Database:');
        expect(content).not.toContain('Time Range:');
    });

    it('uses the "chart" slug fallback when metric description is empty', () => {
        hookState = { ...hookState, analysis: 'x' };
        renderDialog({
            analysisContext: { metricDescription: '' },
        });
        fireEvent.click(screen.getByText('download'));
        const [, filename] = downloadAsMarkdownMock.mock.calls[0];
        expect(filename).toMatch(/^chart-analysis-chart-\d{4}-\d{2}-\d{2}\.md$/);
    });

    it('falls back to "N/A" for the metric line when description is empty', () => {
        hookState = { ...hookState, analysis: 'x' };
        renderDialog({
            analysisContext: { metricDescription: '' },
        });
        fireEvent.click(screen.getByText('download'));
        const [content] = downloadAsMarkdownMock.mock.calls[0];
        expect(content).toContain('- **Metric:** N/A');
    });

    it('arms analyze() again the next time it is opened', () => {
        const { rerender } = renderDialog();
        expect(analyzeMock).toHaveBeenCalledTimes(1);

        rerender(
            <ChartAnalysisDialog
                open={false}
                onClose={vi.fn()}
                isDark={false}
                analysisContext={baseAnalysisContext}
                chartData={baseChartData}
            />,
        );
        rerender(
            <ChartAnalysisDialog
                open={true}
                onClose={vi.fn()}
                isDark={false}
                analysisContext={baseAnalysisContext}
                chartData={baseChartData}
            />,
        );
        expect(analyzeMock).toHaveBeenCalledTimes(2);
    });
});
