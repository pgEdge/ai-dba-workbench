/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

interface ExportableChart {
    getDataURL: (opts: { type: string; pixelRatio: number; backgroundColor: string }) => string;
}

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
