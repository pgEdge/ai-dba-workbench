/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Box, Tooltip, useTheme } from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    CheckCircle as HealthyIcon,
    Warning as WarningIcon,
    Error as ErrorIcon,
    HourglassEmpty,
} from '@mui/icons-material';
import { SxProps } from '@mui/material/styles';

// -- Static sx constants --------------------------------------------------

const iconFontSize = { fontSize: 14 };

const pulseAnimation = {
    animation: 'pulse 2s ease-in-out infinite',
    '@keyframes pulse': {
        '0%, 100%': { opacity: 1 },
        '50%': { opacity: 0.4 },
    },
};

const alertBadgeBase: SxProps<Theme> = {
    position: 'absolute',
    top: -4,
    left: -6,
    minWidth: 12,
    height: 12,
    px: 0.25,
    borderRadius: '6px',
    fontSize: '0.5rem',
    fontWeight: 700,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    lineHeight: 1,
};

const alertContainerSx = { position: 'relative', display: 'flex', alignItems: 'center' };

// -- Style-getter functions -----------------------------------------------

const getOfflineIconSx = (theme: Theme) => ({
    ...iconFontSize,
    color: theme.palette.error.main,
    filter: `drop-shadow(0 0 2px ${theme.palette.error.main})`,
});

const getInitialisingIconSx = (theme: Theme) => ({
    ...iconFontSize,
    color: theme.palette.info.main,
    ...pulseAnimation,
});

const getWarningIconSx = (theme: Theme) => ({
    ...iconFontSize,
    color: theme.palette.warning.main,
    filter: `drop-shadow(0 0 2px ${theme.palette.warning.main})`,
});

const getAlertBadgeSx = (theme: Theme) => ({
    ...alertBadgeBase,
    bgcolor: theme.palette.grey[500],
    color: theme.palette.background.paper,
});

const getHealthyIconSx = (theme: Theme) => ({
    ...iconFontSize,
    color: theme.palette.success.main,
    filter: `drop-shadow(0 0 2px ${theme.palette.success.main})`,
});

/**
 * StatusIndicator - Shows node health status with appropriate icon
 * - Red error icon for offline/down nodes
 * - Yellow warning icon with count for nodes with alerts
 * - Green checkmark for healthy nodes
 */
interface StatusIndicatorProps {
    status?: string;
    alertCount?: number;
    connectionError?: string;
}

const StatusIndicator: React.FC<StatusIndicatorProps> = ({ status, alertCount = 0, connectionError }) => {
    const theme = useTheme();

    // Offline/down nodes - red error icon
    if (status === 'offline') {
        return (
            <Tooltip title={connectionError || "Offline"} placement="right">
                <ErrorIcon sx={getOfflineIconSx(theme)} />
            </Tooltip>
        );
    }

    // Initialising nodes - blue hourglass icon with pulse
    if (status === 'initialising') {
        return (
            <Tooltip title="Initialising - waiting for first probe results" placement="right">
                <HourglassEmpty sx={getInitialisingIconSx(theme)} />
            </Tooltip>
        );
    }

    // Nodes with alerts - yellow warning icon with count
    if (alertCount > 0) {
        return (
            <Tooltip title={`${alertCount} active alert${alertCount !== 1 ? 's' : ''}`} placement="right">
                <Box sx={alertContainerSx}>
                    <WarningIcon sx={getWarningIconSx(theme)} />
                    <Box sx={getAlertBadgeSx(theme)}>
                        {alertCount > 99 ? '99+' : alertCount}
                    </Box>
                </Box>
            </Tooltip>
        );
    }

    // Healthy nodes - green checkmark
    return (
        <Tooltip title="Online" placement="right">
            <HealthyIcon sx={getHealthyIconSx(theme)} />
        </Tooltip>
    );
};

export default StatusIndicator;
