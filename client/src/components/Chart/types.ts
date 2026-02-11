/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

export interface ChartDataSeries {
    name: string;
    data: number[];
}

export interface ChartData {
    categories?: string[];
    series: ChartDataSeries[];
}

export interface BaseChartProps {
    data: ChartData;
    width?: string | number;
    height?: string | number;
    title?: string;
    showLegend?: boolean;
    showTooltip?: boolean;
    showToolbar?: boolean;
    showMarkers?: boolean;
    markerShape?: 'circle' | 'square' | 'triangle' | 'diamond';
    enableZoom?: boolean;
    enableExport?: boolean;
    exportFilename?: string;
    liveUpdate?: boolean;
    updateInterval?: number;
    onDataRefresh?: () => Promise<ChartData>;
    colorPalette?: string[];
    echartsOptions?: object;
    onChartReady?: (chart: unknown) => void;
    onChartClick?: (params: unknown) => void;
}

export interface LineChartProps extends BaseChartProps {
    type: 'line';
    stacked?: boolean;
    smooth?: boolean;
    areaFill?: boolean;
}

export interface BarChartProps extends BaseChartProps {
    type: 'bar';
    stacked?: boolean;
    horizontal?: boolean;
}

export interface PieChartProps extends BaseChartProps {
    type: 'pie' | 'donut';
    showPercentage?: boolean;
}

export type ChartProps = LineChartProps | BarChartProps | PieChartProps;
