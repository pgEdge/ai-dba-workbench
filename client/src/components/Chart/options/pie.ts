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
import { buildLegend } from './common';

export function buildPieOptions(
    data: ChartData,
    options: {
        isDonut?: boolean;
        showPercentage?: boolean;
        showLegend?: boolean;
        showTooltip?: boolean;
    }
): object {
    const categories = data.categories ?? [];
    const sourceData = data.series[0]?.data ?? [];

    const pieData = categories.map((name, i) => ({
        name,
        value: sourceData[i] ?? 0,
    }));

    return {
        tooltip: {
            show: options.showTooltip ?? true,
            trigger: 'item',
        },
        legend: buildLegend(options.showLegend ?? true),
        series: [
            {
                type: 'pie',
                radius: options.isDonut ? ['40%', '70%'] : '70%',
                data: pieData,
                label: {
                    show: !options.isDonut,
                    formatter: options.showPercentage
                        ? '{b}: {d}%'
                        : '{b}: {c}',
                },
                labelLine: {
                    show: !options.isDonut,
                },
            },
        ],
    };
}
