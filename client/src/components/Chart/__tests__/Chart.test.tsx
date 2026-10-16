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
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme, type PaletteOptions } from '@mui/material/styles';
import { Chart } from '../Chart';
import type { ChartData } from '../types';

// Chart.tsx imports ReactEChartsCore from `echarts-for-react/esm/core`
// (switched in commit aa28aa8 to align with Vite's ESM resolution).
// The mock must target that exact path so the test environment short
// circuits before the real component reaches `echarts.init()` on the
// mocked `echarts/core` module below.
/*
 * The options the component hands to ECharts, captured so that the
 * tests can assert what was actually rendered rather than only that
 * something was.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const captured = vi.hoisted(() => ({ option: null as any, events: null as any }));

vi.mock('echarts-for-react/esm/core', () => ({
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    default: vi.fn(({ style, onChartReady, option, onEvents }: { style: React.CSSProperties; onChartReady?: (instance: any) => void; option?: unknown; onEvents?: unknown }) => {
        captured.option = option;
        captured.events = onEvents;
        if (onChartReady) {
            setTimeout(() => { onChartReady({
                getDataURL: vi.fn(() => 'data:image/png;base64,test'),
                dispose: vi.fn(),
            }); }, 0);
        }
        return React.createElement('div', {
            'data-testid': 'echarts-mock',
            style,
        });
    }),
}));

const mockLoggerError = vi.fn();
vi.mock('../../../utils/logger', () => ({
    logger: {
        error: (...args: unknown[]) => mockLoggerError(...args),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

vi.mock('../../ChartAnalysisDialog', () => ({
    ChartAnalysisDialog: ({ open, onClose }: {
        open: boolean;
        onClose: () => void;
    }) => (
        open
            ? React.createElement(
                'button',
                { 'data-testid': 'analysis-dialog', onClick: onClose },
                'close analysis',
            )
            : null
    ),
}));

vi.mock('echarts/core', () => ({
    use: vi.fn(),
    registerTheme: vi.fn(),
}));
vi.mock('echarts/charts', () => ({
    LineChart: {},
    BarChart: {},
    PieChart: {},
}));
vi.mock('echarts/components', () => ({
    TitleComponent: {},
    TooltipComponent: {},
    LegendComponent: {},
    GridComponent: {},
    DataZoomComponent: {},
}));
vi.mock('echarts/renderers', () => ({
    CanvasRenderer: {},
}));

// Mock AICapabilitiesContext so ChartToolbar can render without the provider
vi.mock('../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: true, loading: false }),
}));

const theme = createTheme({
    palette: {
        mode: 'light',
        custom: {
            status: {
                purple: '#8B5CF6',
                cyan: '#06B6D4',
                sky: '#0EA5E9',
            },
        },
    } as PaletteOptions,
});

const sampleData: ChartData = {
    categories: ['Jan', 'Feb', 'Mar'],
    series: [
        { name: 'Sales', data: [100, 200, 300] },
    ],
};

function renderChart(props: Partial<React.ComponentProps<typeof Chart>> = {}) {
    const defaultProps = {
        type: 'line',
        data: sampleData,
        ...props,
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any;

    return render(
        React.createElement(
            ThemeProvider,
            { theme },
            React.createElement(Chart, defaultProps)
        )
    );
}

describe('Chart component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders without crashing for type line', () => {
        renderChart({ type: 'line' });
        expect(screen.getByTestId('echarts-mock')).toBeDefined();
    });

    it('renders without crashing for type bar', () => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        renderChart({ type: 'bar' } as any);
        expect(screen.getByTestId('echarts-mock')).toBeDefined();
    });

    it('renders without crashing for type pie', () => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        renderChart({ type: 'pie' } as any);
        expect(screen.getByTestId('echarts-mock')).toBeDefined();
    });

    it('renders title when title prop is provided', () => {
        renderChart({ title: 'Monthly Sales' });
        expect(screen.getByText('Monthly Sales')).toBeDefined();
    });

    it('does not render title when title prop is omitted', () => {
        renderChart();
        expect(screen.queryByText('Monthly Sales')).toBeNull();
    });

    it('renders toolbar with export button when showToolbar and enableExport are true', () => {
        renderChart({ showToolbar: true, enableExport: true });
        expect(screen.getByLabelText('Export as PNG')).toBeDefined();
    });

    it('does not render toolbar when showToolbar is false', () => {
        renderChart({ showToolbar: false });
        expect(screen.queryByLabelText('Export as PNG')).toBeNull();
    });

    it('applies custom width and height to the chart style', () => {
        renderChart({ width: '800px', height: 600 });
        const chartEl = screen.getByTestId('echarts-mock');
        expect(chartEl.style.width).toBe('800px');
        expect(chartEl.style.height).toBe('600px');
    });

    it('renders with default dimensions when width and height are not specified', () => {
        renderChart();
        const chartEl = screen.getByTestId('echarts-mock');
        expect(chartEl.style.width).toBe('100%');
        expect(chartEl.style.height).toBe('400px');
    });
});

describe('Chart option plumbing', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        captured.option = null;
        captured.events = null;
    });

    it('merges caller-supplied options over the built ones', () => {
        const formatter = (v: number) => `${v} s`;
        renderChart({
            echartsOptions: {
                yAxis: { axisLabel: { formatter } },
                animation: false,
            },
        });

        expect(captured.option.yAxis.type).toBe('value');
        expect(captured.option.yAxis.axisLabel.formatter).toBe(formatter);
        expect(captured.option.animation).toBe(false);
        expect(captured.option.grid).toBeDefined();
    });

    it('applies a caller-supplied colour palette', () => {
        renderChart({ colorPalette: ['#123456'] });
        expect(captured.option.color).toEqual(['#123456']);
    });

    it('passes carried-forward markers and bands through to ECharts', () => {
        renderChart({
            data: {
                categories: ['Jan', 'Feb', 'Mar'],
                series: [{
                    name: 'Sales',
                    data: [1, 1, 3],
                    filled: [false, true, false],
                }],
            },
        });

        expect(captured.option.series[0].data[1]).toMatchObject({
            value: 1,
            filled: true,
            symbol: 'emptyCircle',
        });
        expect(captured.option.series[0].markArea.data).toEqual([
            [{ xAxis: 'Feb' }, { xAxis: 'Feb' }],
        ]);
    });

    it('wires a click handler into the chart events', () => {
        const onChartClick = vi.fn();
        renderChart({ onChartClick });
        expect(captured.events.click).toBe(onChartClick);
    });

    it('passes no events when no click handler is given', () => {
        renderChart();
        expect(captured.events).toEqual({});
    });
});

describe('Chart toolbar actions', () => {
    const freshData: ChartData = {
        categories: ['Apr'],
        series: [{ name: 'Sales', data: [999] }],
    };

    beforeEach(() => {
        vi.clearAllMocks();
        captured.option = null;
    });

    it('exports the chart as a PNG when asked', async () => {
        const click = vi.spyOn(HTMLAnchorElement.prototype, 'click')
            .mockImplementation(() => {});
        renderChart({
            showToolbar: true,
            enableExport: true,
            exportFilename: 'sales',
        });

        // The export needs the chart instance, which the mocked
        // ECharts component hands back asynchronously.
        await waitFor(() => {
            expect(screen.getByLabelText('Export as PNG')).toBeDefined();
        });
        await new Promise((resolve) => setTimeout(resolve, 0));
        fireEvent.click(screen.getByLabelText('Export as PNG'));

        expect(click).toHaveBeenCalled();
        click.mockRestore();
    });

    it('does nothing on export before the chart is ready', () => {
        const click = vi.spyOn(HTMLAnchorElement.prototype, 'click')
            .mockImplementation(() => {});
        renderChart({ showToolbar: true, enableExport: true });
        fireEvent.click(screen.getByLabelText('Export as PNG'));
        expect(click).not.toHaveBeenCalled();
        click.mockRestore();
    });

    it('redraws with the data a refresh returns', async () => {
        const onDataRefresh = vi.fn().mockResolvedValue(freshData);
        renderChart({ showToolbar: true, liveUpdate: true, onDataRefresh });

        fireEvent.click(screen.getByLabelText('Refresh data'));

        await waitFor(() => {
            expect(captured.option.series[0].data).toEqual([999]);
        });
    });

    it('logs a failed refresh and keeps the previous data', async () => {
        const onDataRefresh = vi.fn().mockRejectedValue(new Error('boom'));
        renderChart({ showToolbar: true, liveUpdate: true, onDataRefresh });

        fireEvent.click(screen.getByLabelText('Refresh data'));

        await waitFor(() => {
            expect(mockLoggerError).toHaveBeenCalledWith(
                'Chart refresh failed:', expect.any(Error),
            );
        });
        expect(captured.option.series[0].data).toEqual([100, 200, 300]);
    });

    it('ignores a refresh when no refresh callback is given', () => {
        renderChart({ showToolbar: true, liveUpdate: true });
        // The button is rendered, but pressing it must not throw.
        fireEvent.click(screen.getByLabelText('Refresh data'));
        expect(captured.option.series[0].data).toEqual([100, 200, 300]);
    });

    it('refreshes on the live-update interval and stops on unmount',
        async () => {
            vi.useFakeTimers();
            const onDataRefresh = vi.fn().mockResolvedValue(freshData);
            const { unmount } = renderChart({
                liveUpdate: true,
                updateInterval: 1000,
                onDataRefresh,
            });

            await vi.advanceTimersByTimeAsync(2000);
            expect(onDataRefresh).toHaveBeenCalledTimes(2);

            unmount();
            await vi.advanceTimersByTimeAsync(2000);
            expect(onDataRefresh).toHaveBeenCalledTimes(2);
            vi.useRealTimers();
        });

    it('opens and closes the analysis dialog', () => {
        renderChart({
            showToolbar: true,
            analysisContext: { metricDescription: 'Sales over time' },
        });

        expect(screen.queryByTestId('analysis-dialog')).toBeNull();
        fireEvent.click(screen.getByLabelText('AI Analysis'));
        fireEvent.click(screen.getByTestId('analysis-dialog'));
        expect(screen.queryByTestId('analysis-dialog')).toBeNull();
    });
});
