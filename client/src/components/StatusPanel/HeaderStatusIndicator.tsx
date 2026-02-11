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
    Tooltip,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Warning as WarningIcon,
    Error as ErrorIcon,
    CheckCircle as HealthyIcon,
} from '@mui/icons-material';
import { INDICATOR_SIZES } from './styles';

/**
 * HeaderStatusIndicator - Shows node health status with appropriate icon
 * Matches the ClusterNavigator's status indicator style but sized for header
 */
const HeaderStatusIndicator = ({ status, alertCount = 0, size = 'large' }) => {
    const theme = useTheme();
    const fontSize = INDICATOR_SIZES[size];

    const offlineIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.error.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.error.main})`,
    }), [fontSize, theme.palette.error.main]);

    const warningIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.warning.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.warning.main})`,
    }), [fontSize, theme.palette.warning.main]);

    const badgeSx = useMemo(() => ({
        position: 'absolute',
        top: -5,
        left: -7,
        minWidth: 14,
        height: 14,
        px: 0.25,
        borderRadius: '7px',
        bgcolor: theme.palette.grey[500],
        color: 'common.white',
        fontSize: '0.875rem',
        fontWeight: 700,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        lineHeight: 1,
    }), [theme.palette.grey]);

    const healthyIconSx = useMemo(() => ({
        fontSize,
        color: theme.palette.success.main,
        filter: `drop-shadow(0 0 3px ${theme.palette.success.main})`,
    }), [fontSize, theme.palette.success.main]);

    // Offline/down nodes - red error icon
    if (status === 'offline') {
        return (
            <Tooltip title="Offline" placement="left">
                <ErrorIcon sx={offlineIconSx} />
            </Tooltip>
        );
    }

    // Nodes with alerts - yellow warning icon with count
    if (alertCount > 0) {
        return (
            <Tooltip title={`${alertCount} active alert${alertCount !== 1 ? 's' : ''}`} placement="left">
                <Box sx={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                    <WarningIcon sx={warningIconSx} />
                    <Box sx={badgeSx}>
                        {alertCount > 99 ? '99+' : alertCount}
                    </Box>
                </Box>
            </Tooltip>
        );
    }

    // Healthy nodes - green checkmark
    return (
        <Tooltip title="Online" placement="left">
            <HealthyIcon sx={healthyIconSx} />
        </Tooltip>
    );
};

export default HeaderStatusIndicator;
