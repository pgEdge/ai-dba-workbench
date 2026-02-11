/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { ChartData } from '../types';
import {
    buildTooltip,
    buildLegend,
    buildGrid,
    buildXAxis,
    buildYAxis,
    buildDataZoom,
} from './common';

export function buildLineOptions(
    data: ChartData,
    options: {
        stacked?: boolean;
        smooth?: boolean;
        areaFill?: boolean;
        showMarkers?: boolean;
        markerSymbol?: string;
        enableZoom?: boolean;
        showLegend?: boolean;
        showTooltip?: boolean;
    }
): object {
    const series = data.series.map((s) => ({
        type: 'line' as const,
        name: s.name,
        data: s.data,
        smooth: options.smooth ?? false,
        stack: options.stacked ? 'total' : undefined,
        areaStyle: options.areaFill ? {} : undefined,
        symbol: options.showMarkers
            ? (options.markerSymbol || 'circle')
            : 'none',
        symbolSize: options.showMarkers ? 8 : 0,
    }));

    return {
        tooltip: buildTooltip(options.showTooltip ?? true),
        legend: buildLegend(options.showLegend ?? true),
        grid: buildGrid(),
        xAxis: buildXAxis(data.categories),
        yAxis: buildYAxis(),
        dataZoom: buildDataZoom(options.enableZoom ?? false),
        series,
    };
}
