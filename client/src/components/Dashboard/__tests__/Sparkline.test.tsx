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
import { render } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import Sparkline from '../Sparkline';
import type { MetricDataPoint } from '../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock the Chart component to avoid echarts complexity in tests
vi.mock('../../Chart', () => ({
    Chart: (props: Record<string, unknown>) => (
        <div
            data-testid="chart-mock"
            data-height={props.height}
            data-type={props.type}
            data-smooth={props.smooth}
            data-area-fill={props.areaFill}
            data-show-toolbar={props.showToolbar}
            data-show-legend={props.showLegend}
            data-show-tooltip={props.showTooltip}
            data-series={JSON.stringify(
                (props.data as { series: { data: unknown[] }[] }).series[0].data
            )}
            {...(props.colorPalette !== undefined && {
                'data-color-palette': JSON.stringify(props.colorPalette),
            })}
        />
    ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const createDataPoints = (count: number): MetricDataPoint[] => {
    return Array.from({ length: count }, (_, i) => ({
        time: `2025-01-01T${String(i).padStart(2, '0')}:00:00Z`,
        value: Math.random() * 100,
    }));
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Sparkline', () => {
    it('renders nothing when data is empty', () => {
        const { container } = render(<Sparkline data={[]} />);

        expect(container.firstChild).toBeNull();
    });

    // Note: The component expects data to always be provided (not undefined)
    // This test verifies behavior with an empty array instead

    it('passes null values through as gaps rather than zeros', () => {
        const data = [
            { time: '2025-01-01T00:00:00Z', value: 10 },
            { time: '2025-01-01T01:00:00Z', value: null },
            { time: '2025-01-01T02:00:00Z', value: 30 },
        ];
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock'))
            .toHaveAttribute('data-series', '[10,null,30]');
    });

    it('renders Chart component when data is provided', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toBeInTheDocument();
    });

    it('passes correct chart type', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-type', 'line');
    });

    it('uses default height of 40', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-height', '40');
    });

    it('uses custom height when provided', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} height={60} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-height', '60');
    });

    it('enables smooth lines by default', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-smooth', 'true');
    });

    it('enables area fill by default', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-area-fill', 'true');
    });

    it('disables area fill when showArea is false', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} showArea={false} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-area-fill', 'false');
    });

    it('disables toolbar', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-show-toolbar', 'false');
    });

    it('disables legend', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-show-legend', 'false');
    });

    it('enables tooltip', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        expect(getByTestId('chart-mock')).toHaveAttribute('data-show-tooltip', 'true');
    });

    it('does not pass color palette when color is not provided', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} />);

        // When colorPalette is undefined, the attribute is not set at all
        expect(getByTestId('chart-mock')).not.toHaveAttribute('data-color-palette');
    });

    it('passes color palette when color is provided', () => {
        const data = createDataPoints(5);
        const { getByTestId } = render(<Sparkline data={data} color="#ff5722" />);

        expect(getByTestId('chart-mock')).toHaveAttribute(
            'data-color-palette',
            JSON.stringify(['#ff5722']),
        );
    });
});
