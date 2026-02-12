/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import { Chart } from '../Chart';
import { SparklineProps } from './types';

/**
 * A small inline chart for embedding in KPI tiles and summary rows.
 * Uses the existing Chart component with minimal configuration:
 * no axes, no legend, no toolbar. Configurable height (default 40px)
 * with optional area fill and smooth lines.
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

    const echartsOverrides = useMemo(() => ({
        grid: { top: 2, right: 2, bottom: 2, left: 2 },
        xAxis: { show: false, boundaryGap: false },
        yAxis: { show: false },
    }), []);

    if (!data || data.length === 0) {
        return null;
    }

    return (
        <Chart
            type="line"
            data={chartData}
            height={height}
            smooth
            areaFill={showArea}
            showToolbar={false}
            showLegend={false}
            showTooltip
            colorPalette={color ? [color] : undefined}
            echartsOptions={echartsOverrides}
        />
    );
};

export default Sparkline;
