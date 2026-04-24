/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useMemo, useCallback } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import PsychologyIcon from '@mui/icons-material/Psychology';
import TrendingUpIcon from '@mui/icons-material/TrendingUp';
import TrendingDownIcon from '@mui/icons-material/TrendingDown';
import TrendingFlatIcon from '@mui/icons-material/TrendingFlat';
import { type Theme, useTheme } from '@mui/material/styles';
import type { ChartAnalysisContext, ChartData } from '../Chart/types';
import { ChartAnalysisDialog } from '../ChartAnalysisDialog';
import type { KpiTileData, MetricDataPoint } from './types';
import Sparkline from './Sparkline';
import { useAICapabilities } from '../../contexts/useAICapabilities';
import { hasCachedAnalysis } from '../../hooks/useChartAnalysis';
import {
    getDashboardTileSx,
    KPI_LABEL_SX,
    KPI_VALUE_SX,
    KPI_UNIT_SX,
    KPI_TREND_SX,
} from './styles';

interface KpiTileProps extends KpiTileData {
    onClick?: () => void;
    analysisContext?: ChartAnalysisContext;
}

/**
 * Convert sparkline MetricDataPoint array into ChartData format
 * suitable for the analysis dialog.
 */
const sparklineToChartData = (
    sparklineData: MetricDataPoint[],
    seriesName: string
): ChartData => ({
    categories: sparklineData.map(d => d.time),
    series: [{
        name: seriesName,
        data: sparklineData.map(d => d.value),
    }],
});

const TREND_ICON_SX = { fontSize: 16 };
const TREND_TEXT_SX = { fontSize: '0.75rem', fontWeight: 500 };
const SPARKLINE_CONTAINER_SX = { mt: 1, flex: 1, minHeight: 0 };
const ANALYZE_BUTTON_SX = {
    position: 'absolute',
    top: 8,
    right: 8,
};

/**
 * Return the color for a status indicator.
 */
const getStatusColor = (
    status: 'good' | 'warning' | 'critical' | undefined,
    theme: Theme
): string | undefined => {
    switch (status) {
        case 'good':
            return theme.palette.success.main;
        case 'warning':
            return theme.palette.warning.main;
        case 'critical':
            return theme.palette.error.main;
        default:
            return undefined;
    }
};

/**
 * Return the color for a trend direction.
 */
const getTrendColor = (
    trend: 'up' | 'down' | 'flat' | undefined,
    theme: Theme
): string => {
    switch (trend) {
        case 'up':
            return theme.palette.success.main;
        case 'down':
            return theme.palette.error.main;
        default:
            return theme.palette.text.secondary;
    }
};

/**
 * A metric tile that displays a KPI value with optional sparkline,
 * trend indicator, and status color. Clickable to trigger drill-down
 * via overlay push.
 */
const KpiTile: React.FC<KpiTileProps> = ({
    label,
    value,
    unit,
    trend,
    trendValue,
    sparklineData,
    status,
    onClick,
    analysisContext,
}) => {
    const theme = useTheme();
    const { aiEnabled } = useAICapabilities();
    const [analysisOpen, setAnalysisOpen] = useState(false);
    const isDark = theme.palette.mode === 'dark';
    const tileSx = useMemo(() => getDashboardTileSx(theme), [theme]);
    const statusColor = getStatusColor(status, theme);
    const trendColor = getTrendColor(trend, theme);

    const handleClick = useCallback((): void => {
        if (onClick) {
            onClick();
        }
    }, [onClick]);

    const handleKeyDown = useCallback((e: React.KeyboardEvent): void => {
        if (onClick && (e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault();
            onClick();
        }
    }, [onClick]);

    const isCached = analysisContext ? hasCachedAnalysis(
        analysisContext.metricDescription,
        analysisContext.connectionId,
        analysisContext.databaseName,
        analysisContext.timeRange,
    ) : false;

    const handleAnalyzeClick = useCallback((e: React.MouseEvent) => {
        e.stopPropagation();
        setAnalysisOpen(true);
    }, []);

    const handleAnalysisClose = useCallback(() => {
        setAnalysisOpen(false);
    }, []);

    const TrendIcon = useMemo(() => {
        switch (trend) {
            case 'up':
                return TrendingUpIcon;
            case 'down':
                return TrendingDownIcon;
            case 'flat':
                return TrendingFlatIcon;
            default:
                return null;
        }
    }, [trend]);

    return (
        <Paper
            elevation={0}
            sx={{
                ...tileSx as object,
                display: 'flex',
                flexDirection: 'column',
                position: 'relative',
                cursor: onClick ? 'pointer' : 'default',
            }}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            tabIndex={onClick ? 0 : undefined}
            role={onClick ? 'button' : undefined}
            aria-label={`${label}: ${value}${unit ? ` ${unit}` : ''}`}
        >
            <Typography sx={KPI_LABEL_SX}>
                {label}
            </Typography>
            <Box sx={{ display: 'flex', alignItems: 'baseline' }}>
                <Typography
                    sx={{
                        ...KPI_VALUE_SX as object,
                        color: statusColor || 'text.primary',
                    }}
                >
                    {value}
                </Typography>
                {unit && (
                    <Typography sx={KPI_UNIT_SX}>
                        {unit}
                    </Typography>
                )}
            </Box>
            {trend && trendValue && (
                <Box sx={KPI_TREND_SX}>
                    {TrendIcon && (
                        <TrendIcon
                            sx={{ ...TREND_ICON_SX, color: trendColor }}
                        />
                    )}
                    <Typography
                        sx={{ ...TREND_TEXT_SX, color: trendColor }}
                    >
                        {trendValue}
                    </Typography>
                </Box>
            )}
            {sparklineData && sparklineData.length > 0 && (
                <Box sx={SPARKLINE_CONTAINER_SX}>
                    <Sparkline
                        data={sparklineData}
                        color={statusColor || theme.palette.primary.main}
                    />
                </Box>
            )}
            {aiEnabled && analysisContext && sparklineData && sparklineData.length > 0 && (
                <IconButton
                    size="small"
                    color={isCached ? 'warning' : 'secondary'}
                    onClick={handleAnalyzeClick}
                    sx={ANALYZE_BUTTON_SX}
                >
                    <PsychologyIcon sx={{ fontSize: 16 }} />
                </IconButton>
            )}
            {aiEnabled && analysisContext && sparklineData && sparklineData.length > 0 && (
                <ChartAnalysisDialog
                    open={analysisOpen}
                    onClose={handleAnalysisClose}
                    isDark={isDark}
                    analysisContext={analysisContext}
                    chartData={sparklineToChartData(sparklineData, label)}
                />
            )}
        </Paper>
    );
};

export default KpiTile;
