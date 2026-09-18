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
import { useMemo } from 'react';
import { Chart } from '../Chart';
import type { SparklineProps } from './types';

/**
 * A small inline chart for embedding in KPI tiles and summary rows.
 * Uses the existing Chart component with minimal configuration:
 * no axes, no legend, no toolbar. Configurable height (default 40px)
 * with optional area fill and smooth lines. A point with a null value
 * is passed through to ECharts as a gap in the line.
 */
const Sparkline: React.FC<SparklineProps> = ({
    data,
    color,
    height = 40,
    showArea = true,
}) => {
    const chartData = useMemo(() => ({
        categories: data.map(d => d.time),
        series: [{ name: 'value', data: data.map(d => d.value) }],
    }), [data]);

    // A sparkline draws no axes, so it must also opt out of the
    // contain-label layout pass that `buildGrid()` turns on: that pass
    // reserves room for the axis labels' estimated rects whether or not
    // the axis itself is shown, which at sparkline heights leaves no
    // plot area at all. Measured on echarts 6.1.0 at 275x30, the grid
    // rect is 235.8 x -6.1 with `containLabel: true` against 271 x 26
    // without it (issue #458). `containLabel: false` is sufficient on
    // its own; the axes' own `show: false` keeps the lines and ticks
    // off the canvas.
    const echartsOverrides = useMemo(() => ({
        grid: { top: 2, right: 2, bottom: 2, left: 2, containLabel: false },
        xAxis: { show: false, boundaryGap: false },
        yAxis: { show: false },
    }), []);

    /*
     * A line series is drawn as segments between neighbouring values,
     * so a series without a single adjacent pair of values has no
     * geometry to paint: a lone point, and points separated by null
     * gaps, both paint nothing at all with markers off (measured on
     * echarts 6.1.0: 0 of 8250 canvas pixels at 275x30). That is the
     * state of a freshly registered server, and of the two-collection
     * repro on issue #458, so those sparklines fall back to markers,
     * which put a visible dot on every observation.
     */
    const showMarkers = useMemo(
        () => !data?.some(
            (point, index) => index > 0
                && point.value !== null
                && data[index - 1].value !== null,
        ),
        [data],
    );

    // Nothing to draw when there are no points, or when every bucket
    // is a null gap the server could not fill.
    if (!data || !data.some(d => d.value !== null)) {
        return null;
    }

    return (
        <Chart
            type="line"
            data={chartData}
            height={height}
            smooth
            areaFill={showArea}
            showMarkers={showMarkers}
            markerShape="circle"
            showToolbar={false}
            showLegend={false}
            showTooltip
            colorPalette={color ? [color] : undefined}
            echartsOptions={echartsOverrides}
        />
    );
};

export default Sparkline;
