/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { EChartsOption } from 'echarts';

import type { ChartData } from '../types';
import {
    buildTooltip,
    buildLegend,
    buildGrid,
    buildXAxis,
    buildYAxis,
    buildDataZoom,
    buildSeriesData,
    buildFilledMarkArea,
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
): EChartsOption {
    /*
     * Stretches the server carried forward from an earlier observation
     * are shaded once for the whole chart rather than once per series,
     * so overlapping bands cannot compound; the band therefore hangs
     * off the first series.
     */
    const markArea = buildFilledMarkArea(data.categories, data.series);

    const series = data.series.map((s, idx) => ({
        type: 'line' as const,
        name: s.name,
        data: buildSeriesData(s.data, s.filled),
        smooth: options.smooth ?? false,
        stack: options.stacked ? 'total' : undefined,
        areaStyle: options.areaFill ? {} : undefined,
        symbol: options.showMarkers
            ? (options.markerSymbol ?? 'circle')
            : 'none',
        symbolSize: options.showMarkers ? 8 : 0,
        markArea: idx === 0 ? markArea : undefined,
    }));

    return {
        tooltip: buildTooltip(options.showTooltip ?? true),
        legend: buildLegend(options.showLegend ?? true),
        grid: buildGrid(),
        xAxis: buildXAxis(data.categories),
        // A line fills from the zero baseline when it is stacked or
        // carries an area fill, so the value axis is zero-anchored in
        // either case; a plain line keeps its tight degenerate window.
        yAxis: buildYAxis(
            data.series.map((s) => s.data),
            options.stacked,
            Boolean(options.stacked || options.areaFill),
        ),
        dataZoom: buildDataZoom(options.enableZoom ?? false),
        series,
    };
}
