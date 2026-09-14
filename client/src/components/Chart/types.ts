/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * One chart series. A null entry marks a bucket with no value and is
 * drawn as a gap in line charts (and as an empty slot in bar charts)
 * rather than as zero.
 */
export interface ChartDataSeries {
    name: string;
    data: (number | null)[];
}

export interface ChartData {
    categories?: string[];
    series: ChartDataSeries[];
}

export interface ChartAnalysisContext {
    metricDescription: string;
    connectionId?: number;
    connectionName?: string;
    databaseName?: string;
    timeRange?: string;
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
    analysisContext?: ChartAnalysisContext;
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
