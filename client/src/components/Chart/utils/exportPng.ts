/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { EChartsType } from 'echarts/core';

/* Only the snapshot method is needed here, so the chart is accepted
   through the narrowest slice of the ECharts instance type. */
export type ExportableChart = Pick<EChartsType, 'getDataURL'>;

export const exportChartAsPng = (
    chartInstance: ExportableChart,
    filename: string
): void => {
    const dataUrl = chartInstance.getDataURL({
        type: 'png',
        pixelRatio: 2,
        backgroundColor: '#fff',
    });

    const anchor = document.createElement('a');
    anchor.href = dataUrl;
    anchor.download = `${filename}.png`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
};
