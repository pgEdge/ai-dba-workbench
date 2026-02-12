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
    if (theme.palette.mode === 'dark') {
        return [
            '#22D3EE', // bright cyan (pgEdge brand family)
            '#818CF8', // light indigo
            '#4ADE80', // bright green
            '#FBBF24', // bright amber
            '#F87171', // light red
            '#A78BFA', // light purple
            '#F472B6', // light pink
            '#2DD4BF', // bright teal
            '#FB923C', // light orange
        ];
    }

    return [
        '#0C8599', // pgEdge teal (darker cyan for white contrast)
        '#6366F1', // indigo
        '#22C55E', // green
        '#F59E0B', // amber
        '#EF4444', // red
        '#8B5CF6', // purple
        '#EC4899', // pink
        '#14B8A6', // teal
        '#F97316', // orange
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
                    color: palette.text.primary,
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
                    fontSize: 14,
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
                    fontSize: 14,
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
