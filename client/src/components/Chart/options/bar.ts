/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ChartData } from '../types';
import {
    buildTooltip,
    buildLegend,
    buildGrid,
    buildXAxis,
    buildYAxis,
    buildDataZoom,
} from './common';

export function buildBarOptions(
    data: ChartData,
    options: {
        stacked?: boolean;
        horizontal?: boolean;
        enableZoom?: boolean;
        showLegend?: boolean;
        showTooltip?: boolean;
    }
): object {
    const series = data.series.map((s) => ({
        type: 'bar' as const,
        name: s.name,
        data: s.data,
        stack: options.stacked ? 'total' : undefined,
        barMaxWidth: 50,
    }));

    const categoryAxis = buildXAxis(data.categories);
    const valueAxis = buildYAxis();

    return {
        tooltip: buildTooltip(options.showTooltip ?? true),
        legend: buildLegend(options.showLegend ?? true),
        grid: buildGrid(),
        xAxis: options.horizontal ? valueAxis : categoryAxis,
        yAxis: options.horizontal ? categoryAxis : valueAxis,
        dataZoom: buildDataZoom(options.enableZoom ?? false),
        series,
    };
}
