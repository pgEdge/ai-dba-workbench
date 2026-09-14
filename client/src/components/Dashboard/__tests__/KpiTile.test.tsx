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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import KpiTile from '../KpiTile';

// Mock the Sparkline component to avoid chart rendering complexity
vi.mock('../Sparkline', () => ({
    default: () => <div data-testid="sparkline" />,
}));

// Mock AICapabilitiesContext so KpiTile can render without the provider
vi.mock('../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: true, loading: false }),
}));

let mockCached = false;
vi.mock('../../../hooks/useChartAnalysis', () => ({
    hasCachedAnalysis: () => mockCached,
}));

// Expose the dialog's props so the tests can see the chart data the
// tile hands over, including any null gaps, and drive onClose.
vi.mock('../../ChartAnalysisDialog', () => ({
    ChartAnalysisDialog: ({ open, onClose, chartData }: {
        open: boolean;
        onClose: () => void;
        chartData: { categories?: string[]; series: { name: string; data: unknown[] }[] };
    }) => (
        <div
            data-testid="analysis-dialog"
            data-open={String(open)}
            data-series={chartData.series[0].name}
            data-values={JSON.stringify(chartData.series[0].data)}
            data-categories={JSON.stringify(chartData.categories)}
        >
            <button type="button" onClick={onClose}>close</button>
        </div>
    ),
}));

const theme = createTheme();

const renderKpiTile = (props: Record<string, unknown> = {}) => {
    const defaultProps = {
        label: 'CPU Usage',
        value: '75%',
    };

    return render(
        <ThemeProvider theme={theme}>
            <KpiTile {...defaultProps} {...props} />
        </ThemeProvider>,
    );
};

describe('KpiTile', () => {
    it('renders the label and value', () => {
        renderKpiTile();

        expect(screen.getByText('CPU Usage')).toBeInTheDocument();
        expect(screen.getByText('75%')).toBeInTheDocument();
    });

    it('renders the unit when provided', () => {
        renderKpiTile({ value: 42, unit: 'ms' });

        expect(screen.getByText('42')).toBeInTheDocument();
        expect(screen.getByText('ms')).toBeInTheDocument();
    });

    it('does not render unit when not provided', () => {
        const { container } = renderKpiTile({ value: 42 });

        // Should only have one Typography in the value area (the value itself)
        expect(screen.getByText('42')).toBeInTheDocument();
        expect(container.textContent).not.toContain('ms');
    });

    it('applies the correct aria-label', () => {
        renderKpiTile({ label: 'Latency', value: '12', unit: 'ms' });

        expect(screen.getByLabelText('Latency: 12 ms')).toBeInTheDocument();
    });

    it('applies aria-label without unit when unit is absent', () => {
        renderKpiTile({ label: 'Count', value: 99 });

        expect(screen.getByLabelText('Count: 99')).toBeInTheDocument();
    });

    it('renders trend indicator when trend and trendValue are provided', () => {
        renderKpiTile({ trend: 'up', trendValue: '+5%' });

        expect(screen.getByText('+5%')).toBeInTheDocument();
    });

    it('does not render trend indicator when trend is absent', () => {
        renderKpiTile();

        // No trend text should appear
        expect(screen.queryByText('+5%')).not.toBeInTheDocument();
    });

    it('renders sparkline when sparklineData is provided', () => {
        const sparklineData = [
            { time: '2025-01-01T00:00:00Z', value: 10 },
            { time: '2025-01-01T01:00:00Z', value: 20 },
        ];

        renderKpiTile({ sparklineData });

        expect(screen.getByTestId('sparkline')).toBeInTheDocument();
    });

    it('sets role=button and is clickable when onClick is provided', () => {
        const onClick = vi.fn();
        renderKpiTile({ onClick });

        const tile = screen.getByRole('button');
        fireEvent.click(tile);

        expect(onClick).toHaveBeenCalledTimes(1);
    });

    it('does not set role=button when onClick is absent', () => {
        renderKpiTile();

        expect(screen.queryByRole('button')).not.toBeInTheDocument();
    });

    it('responds to keyboard activation when clickable', () => {
        const onClick = vi.fn();
        renderKpiTile({ onClick });

        const tile = screen.getByRole('button');
        fireEvent.keyDown(tile, { key: 'Enter' });
        fireEvent.keyDown(tile, { key: ' ' });
        fireEvent.keyDown(tile, { key: 'a' });

        expect(onClick).toHaveBeenCalledTimes(2);
    });

    it('renders every trend direction and status colour', () => {
        (['up', 'down', 'flat'] as const).forEach((trend, idx) => {
            renderKpiTile({ trend, trendValue: `t${idx}` });
            expect(screen.getByText(`t${idx}`)).toBeInTheDocument();
        });

        renderKpiTile({ value: 'g', status: 'good' });
        expect(screen.getByText('g')).toHaveStyle({ color: theme.palette.success.main });
        renderKpiTile({ value: 'w', status: 'warning' });
        expect(screen.getByText('w')).toHaveStyle({ color: theme.palette.warning.main });
        renderKpiTile({ value: 'c', status: 'critical' });
        expect(screen.getByText('c')).toHaveStyle({ color: theme.palette.error.main });
    });

    describe('AI analysis', () => {
        const sparklineData = [
            { time: '2025-01-01T00:00:00Z', value: 10 },
            { time: '2025-01-01T01:00:00Z', value: null },
            { time: '2025-01-01T02:00:00Z', value: 30 },
        ];
        const analysisContext = { metricDescription: 'CPU over time' };

        it('does not offer analysis without a sparkline or context', () => {
            renderKpiTile({ analysisContext });
            expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();

            renderKpiTile({ sparklineData });
            expect(screen.queryByTestId('analysis-dialog')).not.toBeInTheDocument();
        });

        it('opens and closes the dialog and hands over the sparkline with its gaps', () => {
            const onClick = vi.fn();
            renderKpiTile({ sparklineData, analysisContext, onClick });

            const dialog = screen.getByTestId('analysis-dialog');
            expect(dialog).toHaveAttribute('data-open', 'false');
            expect(dialog).toHaveAttribute('data-series', 'CPU Usage');
            expect(dialog).toHaveAttribute('data-values', '[10,null,30]');
            expect(dialog).toHaveAttribute(
                'data-categories',
                JSON.stringify(sparklineData.map(p => p.time)),
            );

            // The analyse button sits inside the clickable tile, so the
            // click must not also fire the tile's own onClick.
            const analyse = screen.getAllByRole('button')
                .find(el => el !== screen.getByLabelText(/CPU Usage/)
                    && el.textContent !== 'close') as HTMLElement;
            fireEvent.click(analyse);
            expect(onClick).not.toHaveBeenCalled();
            expect(dialog).toHaveAttribute('data-open', 'true');

            fireEvent.click(screen.getByText('close'));
            expect(dialog).toHaveAttribute('data-open', 'false');
        });

        it('marks the analyse button when a cached analysis exists', () => {
            mockCached = true;
            try {
                renderKpiTile({ sparklineData, analysisContext });
            } finally {
                mockCached = false;
            }
            const analyse = screen.getAllByRole('button')
                .find(el => el.textContent !== 'close') as HTMLElement;
            expect(analyse.className).toContain('colorWarning');
        });
    });
});
