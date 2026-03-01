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

import useTheme from '@mui/material/styles/useTheme';
import { ChartProps, ChartData } from './types';
import { ChartAnalysisDialog } from '../ChartAnalysisDialog';
import { CHART_CONTAINER_SX, CHART_PAPER_SX, CHART_TITLE_SX } from './styles';
import { useEChartsTheme } from './ChartThemeBridge';
import { exportChartAsPng } from './utils/exportPng';
import { getMarkerSymbol } from './utils/markers';
import { normalizeChartData } from './utils/dataTransform';
import { buildLineOptions } from './options/line';
import { buildBarOptions } from './options/bar';
import { buildPieOptions } from './options/pie';
import { ChartToolbar } from './ChartToolbar';
import { hasCachedAnalysis } from '../../hooks/useChartAnalysis';

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
        analysisContext,
    } = props;

    const chartRef = useRef<EChartsInstance | null>(null);
    const [liveData, setLiveData] = useState<ChartData | null>(null);
    const [analysisOpen, setAnalysisOpen] = useState(false);

    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';

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

    /* Inject theme-derived axis label, legend, and tooltip styles into
       the built options. ECharts applies the registered theme first, but
       any option object provided to the chart instance *replaces* the
       corresponding theme object entirely. When buildXAxis/buildYAxis
       set axisLabel with a formatter but no color, the theme color is
       lost. By merging the theme defaults here (before the caller's
       echartsOptions override), every axis label, legend, and tooltip
       inherits the correct palette colors automatically. */
    const themedOptions = useMemo(() => {
        const options = { ...baseOptions };

        const axisLabelDefaults = {
            color: echartsTheme.xAxis?.axisLabel?.color,
            fontSize: echartsTheme.xAxis?.axisLabel?.fontSize,
        };

        const injectAxisDefaults = (axis: Record<string, unknown>) => {
            if (axis.axisLabel && typeof axis.axisLabel === 'object') {
                axis.axisLabel = { ...axisLabelDefaults, ...(axis.axisLabel as Record<string, unknown>) };
            }
        };

        if (options.xAxis && typeof options.xAxis === 'object') {
            if (Array.isArray(options.xAxis)) {
                options.xAxis = options.xAxis.map((a: Record<string, unknown>) => {
                    const copy = { ...a };
                    injectAxisDefaults(copy);
                    return copy;
                });
            } else {
                const copy = { ...(options.xAxis as Record<string, unknown>) };
                injectAxisDefaults(copy);
                options.xAxis = copy;
            }
        }

        if (options.yAxis) {
            if (Array.isArray(options.yAxis)) {
                options.yAxis = options.yAxis.map((a: Record<string, unknown>) => {
                    const copy = { ...a };
                    injectAxisDefaults(copy);
                    return copy;
                });
            } else if (typeof options.yAxis === 'object') {
                const copy = { ...(options.yAxis as Record<string, unknown>) };
                injectAxisDefaults(copy);
                options.yAxis = copy;
            }
        }

        if (options.legend && typeof options.legend === 'object') {
            const legend = { ...(options.legend as Record<string, unknown>) };
            const themeTextStyle = echartsTheme.legend?.textStyle ?? {};
            if (legend.textStyle && typeof legend.textStyle === 'object') {
                legend.textStyle = { ...themeTextStyle, ...(legend.textStyle as Record<string, unknown>) };
            } else if (!legend.textStyle) {
                legend.textStyle = { ...themeTextStyle };
            }
            options.legend = legend;
        }

        if (options.tooltip && typeof options.tooltip === 'object') {
            const tooltip = { ...(options.tooltip as Record<string, unknown>) };
            const themeTooltip = echartsTheme.tooltip ?? {};
            if (!tooltip.backgroundColor) {
                tooltip.backgroundColor = themeTooltip.backgroundColor;
            }
            if (!tooltip.borderColor) {
                tooltip.borderColor = themeTooltip.borderColor;
            }
            if (tooltip.textStyle && typeof tooltip.textStyle === 'object') {
                tooltip.textStyle = {
                    ...(themeTooltip.textStyle ?? {}),
                    ...(tooltip.textStyle as Record<string, unknown>),
                };
            } else if (!tooltip.textStyle) {
                tooltip.textStyle = { ...(themeTooltip.textStyle ?? {}) };
            }
            options.tooltip = tooltip;
        }

        /* Allow the initial render to animate (ECharts default) but
           make all subsequent data updates instant. */
        options.animationDurationUpdate = 0;

        return options;
    }, [baseOptions, echartsTheme]);

    const mergedOptions = useMemo(() => {
        let options = themedOptions;
        if (colorPalette) {
            options = { ...options, color: colorPalette };
        }
        if (echartsOptions) {
            options = deepMerge(options, echartsOptions);
        }
        return options;
    }, [themedOptions, colorPalette, echartsOptions]);

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

    const handleAnalyze = useCallback(() => setAnalysisOpen(true), []);
    const handleAnalysisClose = useCallback(() => setAnalysisOpen(false), []);

    const chartEvents = useMemo((): Record<string, (...args: unknown[]) => void> => {
        if (!onChartClick) {
            return {};
        }
        return { click: onChartClick };
    }, [onChartClick]);

    const isCached = analysisContext ? hasCachedAnalysis(
        analysisContext.metricDescription,
        analysisContext.connectionId,
        analysisContext.databaseName,
        analysisContext.timeRange,
    ) : false;

    return (
        <Paper sx={CHART_PAPER_SX} elevation={1}>
            <Box sx={CHART_CONTAINER_SX}>
                {title && (
                    <Typography sx={CHART_TITLE_SX}>{title}</Typography>
                )}
                {showToolbar && (
                    <ChartToolbar
                        cached={isCached}
                        showAnalyze={!!analysisContext}
                        showExport={enableExport}
                        showRefresh={liveUpdate}
                        onAnalyze={handleAnalyze}
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
            {analysisContext && (
                <ChartAnalysisDialog
                    open={analysisOpen}
                    onClose={handleAnalysisClose}
                    isDark={isDark}
                    analysisContext={analysisContext}
                    chartData={activeData}
                />
            )}
        </Paper>
    );
}
