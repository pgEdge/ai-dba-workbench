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

export const normalizeChartData = (data: ChartData): ChartData => {
    const series = Array.isArray(data.series) ? data.series : [];

    const normalizedSeries = series.map((s) => ({
        name: s.name || 'Unnamed',
        data: Array.isArray(s.data) ? s.data : [],
    }));

    return {
        ...data,
        series: normalizedSeries,
    };
};
