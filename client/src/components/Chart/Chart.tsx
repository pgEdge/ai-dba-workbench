/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import * as echarts from 'echarts/core';
import { LineChart, BarChart, PieChart } from 'echarts/charts';
import {
    TitleComponent,
    TooltipComponent,
    LegendComponent,
    GridComponent,
    DataZoomComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import ReactEChartsCore from 'echarts-for-react/lib/core';

import { ChartProps, ChartData } from './types';
import { CHART_CONTAINER_SX, CHART_PAPER_SX, CHART_TITLE_SX } from './styles';
import { useEChartsTheme } from './ChartThemeBridge';
import { exportChartAsPng } from './utils/exportPng';
import { getMarkerSymbol } from './utils/markers';
import { normalizeChartData } from './utils/dataTransform';
import { buildLineOptions } from './options/line';
import { buildBarOptions } from './options/bar';
import { buildPieOptions } from './options/pie';
import { ChartToolbar } from './ChartToolbar';

interface EChartsInstance {
    getDataURL: (opts: { type: string; pixelRatio: number; backgroundColor: string }) => string;
    dispose: () => void;
}

echarts.use([
    LineChart,
    BarChart,
    PieChart,
    TitleComponent,
    TooltipComponent,
    LegendComponent,
    GridComponent,
    DataZoomComponent,
    CanvasRenderer,
]);

function deepMerge(target: Record<string, unknown>, source: Record<string, unknown>): Record<string, unknown> {
    const result = { ...target };
    for (const key of Object.keys(source)) {
        if (
            source[key] &&
            typeof source[key] === 'object' &&
            !Array.isArray(source[key])
        ) {
            result[key] = deepMerge(
                (result[key] as Record<string, unknown>) || {},
                source[key] as Record<string, unknown>
            );
        } else {
            result[key] = source[key];
        }
    }
    return result;
}

export function Chart(props: ChartProps) {
    const {
        data,
        width,
        height,
        title,
        showLegend,
        showTooltip,
        showToolbar = true,
        showMarkers,
        markerShape,
        enableZoom,
        enableExport = true,
        exportFilename = 'chart',
        liveUpdate,
        updateInterval = 5000,
        onDataRefresh,
        colorPalette,
        echartsOptions,
        onChartReady,
        onChartClick,
    } = props;

    const chartRef = useRef<EChartsInstance | null>(null);
    const [liveData, setLiveData] = useState<ChartData | null>(null);

    const echartsTheme = useEChartsTheme();
    echarts.registerTheme('pgedge', echartsTheme);

    const activeData = liveData ?? data;
    const normalizedData = useMemo(
        () => normalizeChartData(activeData),
        [activeData]
    );

    const baseOptions = useMemo(() => {
        switch (props.type) {
            case 'line': {
                const markerSymbol =
                    showMarkers && markerShape
                        ? getMarkerSymbol(markerShape)
                        : undefined;
                return buildLineOptions(normalizedData, {
                    stacked: props.stacked,
                    smooth: props.smooth,
                    areaFill: props.areaFill,
                    showMarkers,
                    markerSymbol,
                    enableZoom,
                    showLegend,
                    showTooltip,
                });
            }
            case 'bar':
                return buildBarOptions(normalizedData, {
                    stacked: props.stacked,
                    horizontal: props.horizontal,
                    enableZoom,
                    showLegend,
                    showTooltip,
                });
            case 'pie':
            case 'donut':
                return buildPieOptions(normalizedData, {
                    isDonut: props.type === 'donut',
                    showPercentage: props.showPercentage,
                    showLegend,
                    showTooltip,
                });
            default:
                return {};
        }
    }, [normalizedData, props, showMarkers, markerShape, enableZoom, showLegend, showTooltip]);

    const mergedOptions = useMemo(() => {
        let options = baseOptions;
        if (colorPalette) {
            options = { ...options, color: colorPalette };
        }
        if (echartsOptions) {
            options = deepMerge(options, echartsOptions);
        }
        return options;
    }, [baseOptions, colorPalette, echartsOptions]);

    useEffect(() => {
        if (!liveUpdate || !onDataRefresh) {
            return;
        }

        const id = setInterval(() => {
            onDataRefresh().then((refreshed) => {
                setLiveData(refreshed);
            });
        }, updateInterval);

        return () => clearInterval(id);
    }, [liveUpdate, onDataRefresh, updateInterval]);

    const handleChartReady = useCallback(
        (instance: EChartsInstance) => {
            chartRef.current = instance;
            onChartReady?.(instance);
        },
        [onChartReady]
    );

    const handleExport = useCallback(() => {
        if (chartRef.current) {
            exportChartAsPng(chartRef.current, exportFilename);
        }
    }, [exportFilename]);

    const handleRefresh = useCallback(() => {
        if (onDataRefresh) {
            onDataRefresh().then((refreshed) => {
                setLiveData(refreshed);
            });
        }
    }, [onDataRefresh]);

    const chartEvents = useMemo((): Record<string, (...args: unknown[]) => void> => {
        if (!onChartClick) {
            return {};
        }
        return { click: onChartClick };
    }, [onChartClick]);

    return (
        <Paper sx={CHART_PAPER_SX} elevation={1}>
            <Box sx={CHART_CONTAINER_SX}>
                {title && (
                    <Typography sx={CHART_TITLE_SX}>{title}</Typography>
                )}
                {showToolbar && (
                    <ChartToolbar
                        showExport={enableExport}
                        showRefresh={liveUpdate}
                        onExport={handleExport}
                        onRefresh={handleRefresh}
                    />
                )}
                <ReactEChartsCore
                    echarts={echarts}
                    option={mergedOptions}
                    theme="pgedge"
                    notMerge={false}
                    lazyUpdate={true}
                    style={{
                        width: width ?? '100%',
                        height: height ?? 400,
                    }}
                    onChartReady={handleChartReady}
                    onEvents={chartEvents}
                />
            </Box>
        </Paper>
    );
}
