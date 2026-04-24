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
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme, type PaletteOptions } from '@mui/material/styles';
import { Chart } from '../Chart';
import type { ChartData } from '../types';

vi.mock('echarts-for-react/lib/core', () => ({
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    default: vi.fn(({ style, onChartReady }: { style: React.CSSProperties; onChartReady?: (instance: any) => void }) => {
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
