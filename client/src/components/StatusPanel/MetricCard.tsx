/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import {
    Box,
    Typography,
    Paper,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    TrendingUp as TrendingUpIcon,
    TrendingDown as TrendingDownIcon,
} from '@mui/icons-material';
import {
    METRIC_LABEL_SX,
    METRIC_VALUE_BASE_SX,
    METRIC_TREND_CONTAINER_SX,
} from './styles';

/**
 * MetricCard - Display a key metric with trend indicator
 */
const MetricCard = ({ label, value, trend, trendValue, icon: Icon, color }) => {
    const theme = useTheme();
    const TrendIcon = trend === 'up' ? TrendingUpIcon : TrendingDownIcon;
    const trendColor = trend === 'up' ? theme.palette.success.main : theme.palette.error.main;

    const paperSx = useMemo(() => ({
        p: 2,
        borderRadius: 2,
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.grey[800], 0.8)
            : alpha(theme.palette.grey[100], 0.8),
        border: '1px solid',
        borderColor: theme.palette.divider,
        flex: 1,
        minWidth: 120,
    }), [theme]);

    const iconSx = useMemo(() => ({
        fontSize: 18,
        color: color || theme.palette.grey[500],
    }), [color, theme.palette.grey]);

    const valueSx = useMemo(() => ({
        ...METRIC_VALUE_BASE_SX,
        color: color || 'text.primary',
    }), [color]);

    return (
        <Paper elevation={0} sx={paperSx}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                {Icon && <Icon sx={iconSx} />}
                <Typography variant="caption" sx={METRIC_LABEL_SX}>
                    {label}
                </Typography>
            </Box>
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1 }}>
                <Typography variant="h4" sx={valueSx}>
                    {value}
                </Typography>
                {trend && (
                    <Box sx={METRIC_TREND_CONTAINER_SX}>
                        <TrendIcon sx={{ fontSize: 14, color: trendColor }} />
                        <Typography
                            variant="caption"
                            sx={{ color: trendColor, fontSize: '0.6875rem', fontWeight: 600 }}
                        >
                            {trendValue}
                        </Typography>
                    </Box>
                )}
            </Box>
        </Paper>
    );
};

export default MetricCard;
