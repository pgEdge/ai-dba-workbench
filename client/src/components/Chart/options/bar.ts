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
    buildLabelGrid,
    buildXAxis,
    buildLabelAxis,
    isTimeCategories,
    buildYAxis,
    buildDataZoom,
    buildSeriesData,
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
): EChartsOption {
    const series = data.series.map((s) => ({
        type: 'bar' as const,
        name: s.name,
        data: buildSeriesData(s.data, s.filled),
        stack: options.stacked ? 'total' : undefined,
        barMaxWidth: 50,
    }));

    // Time buckets use the time-series axis, which formats timestamps
    // and thins labels to fit; named items such as servers label every
    // bar instead.
    const timeCategories = isTimeCategories(data.categories);
    const categoryAxis = timeCategories
        ? buildXAxis(data.categories)
        : buildLabelAxis(data.categories, options.horizontal);
    // Bars always grow from the zero baseline, so the value axis is
    // zero-anchored regardless of whether the series are stacked.
    const valueAxis = buildYAxis(
        data.series.map((s) => s.data),
        options.stacked,
        true,
    );

    return {
        tooltip: buildTooltip(options.showTooltip ?? true),
        legend: buildLegend(options.showLegend ?? true),
        grid: timeCategories ? buildGrid() : buildLabelGrid(),
        xAxis: options.horizontal ? valueAxis : categoryAxis,
        yAxis: options.horizontal ? categoryAxis : valueAxis,
        dataZoom: buildDataZoom(options.enableZoom ?? false),
        series,
    };
}
