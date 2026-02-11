/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useMemo } from 'react';
import { useTheme, alpha, Theme } from '@mui/material/styles';

/**
 * Returns the default color palette for chart series, derived from the
 * MUI theme. This function can be used independently of the hook when
 * only the color array is needed.
 */
export function getDefaultColorPalette(theme: Theme): string[] {
    return [
        theme.palette.primary.main,
        theme.palette.secondary.main,
        theme.palette.success.main,
        theme.palette.warning.main,
        theme.palette.info.main,
        theme.palette.error.main,
        theme.palette.custom.status.purple,
        theme.palette.custom.status.cyan,
        theme.palette.custom.status.sky,
    ];
}

/**
 * A React hook that bridges the MUI theme to an ECharts-compatible theme
 * object. The returned object is memoized and only recomputes when the
 * palette mode changes between light and dark.
 */
export function useEChartsTheme() {
    const theme = useTheme();

    return useMemo(() => {
        const { palette, typography } = theme;
        const fontFamily = typography.fontFamily as string;
        const colorPalette = getDefaultColorPalette(theme);

        return {
            color: colorPalette,
            backgroundColor: 'transparent',
            textStyle: {
                color: palette.text.primary,
                fontFamily,
            },
            title: {
                textStyle: {
                    color: palette.text.primary,
                    fontSize: 16,
                    fontWeight: 600,
                    fontFamily,
                },
            },
            legend: {
                textStyle: {
                    color: palette.text.secondary,
                },
            },
            tooltip: {
                backgroundColor: palette.background.paper,
                borderColor: palette.divider,
                textStyle: {
                    color: palette.text.primary,
                },
            },
            xAxis: {
                axisLine: {
                    lineStyle: {
                        color: palette.divider,
                    },
                },
                axisTick: {
                    lineStyle: {
                        color: palette.divider,
                    },
                },
                axisLabel: {
                    color: palette.text.secondary,
                },
                splitLine: {
                    lineStyle: {
                        color: alpha(palette.divider, 0.5),
                    },
                },
            },
            yAxis: {
                axisLine: {
                    lineStyle: {
                        color: palette.divider,
                    },
                },
                axisTick: {
                    lineStyle: {
                        color: palette.divider,
                    },
                },
                axisLabel: {
                    color: palette.text.secondary,
                },
                splitLine: {
                    lineStyle: {
                        color: alpha(palette.divider, 0.5),
                    },
                },
            },
            dataZoom: [
                {
                    type: 'slider' as const,
                    backgroundColor: palette.background.default,
                    borderColor: palette.divider,
                    fillerColor: alpha(palette.primary.main, 0.2),
                    handleStyle: {
                        color: palette.primary.main,
                    },
                    textStyle: {
                        color: palette.text.secondary,
                    },
                },
            ],
        };
    }, [theme]);
}
