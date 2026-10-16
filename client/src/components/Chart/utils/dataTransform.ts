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

    /*
     * `filled` marks the buckets a series carried forward from an
     * earlier observation, and the option builders draw those
     * distinctly, so it has to survive normalisation; a series that
     * has no such notion keeps none.
     */
    const normalizedSeries = series.map((s) => ({
        name: s.name || 'Unnamed',
        data: Array.isArray(s.data) ? s.data : [],
        ...(Array.isArray(s.filled) ? { filled: s.filled } : {}),
    }));

    return {
        ...data,
        series: normalizedSeries,
    };
};
