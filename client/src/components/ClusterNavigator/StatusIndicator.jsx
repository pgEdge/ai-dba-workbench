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
import { Box, Tooltip } from '@mui/material';
import {
    CheckCircle as HealthyIcon,
    Warning as WarningIcon,
    Error as ErrorIcon,
    HourglassEmpty,
} from '@mui/icons-material';

/**
 * StatusIndicator - Shows node health status with appropriate icon
 * - Red error icon for offline/down nodes
 * - Yellow warning icon with count for nodes with alerts
 * - Green checkmark for healthy nodes
 */
const StatusIndicator = ({ status, alertCount = 0, isDark, connectionError }) => {
    // Offline/down nodes - red error icon
    if (status === 'offline') {
        return (
            <Tooltip title={connectionError || "Offline"} placement="right">
                <ErrorIcon
                    sx={{
                        fontSize: 14,
                        color: '#EF4444',
                        filter: 'drop-shadow(0 0 2px #EF4444)',
                    }}
                />
            </Tooltip>
        );
    }

    // Initialising nodes - blue hourglass icon with pulse
    if (status === 'initialising') {
        return (
            <Tooltip title="Initialising - waiting for first probe results" placement="right">
                <HourglassEmpty
                    sx={{
                        fontSize: 14,
                        color: '#3B82F6',
                        animation: 'pulse 2s ease-in-out infinite',
                        '@keyframes pulse': {
                            '0%, 100%': { opacity: 1 },
                            '50%': { opacity: 0.4 },
                        },
                    }}
                />
            </Tooltip>
        );
    }

    // Nodes with alerts - yellow warning icon with count
    if (alertCount > 0) {
        return (
            <Tooltip title={`${alertCount} active alert${alertCount !== 1 ? 's' : ''}`} placement="right">
                <Box sx={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                    <WarningIcon
                        sx={{
                            fontSize: 14,
                            color: '#F59E0B',
                            filter: 'drop-shadow(0 0 2px #F59E0B)',
                        }}
                    />
                    <Box
                        sx={{
                            position: 'absolute',
                            top: -4,
                            left: -6,
                            minWidth: 12,
                            height: 12,
                            px: 0.25,
                            borderRadius: '6px',
                            bgcolor: isDark ? '#64748B' : '#6B7280',
                            color: '#FFF',
                            fontSize: '0.5rem',
                            fontWeight: 700,
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            lineHeight: 1,
                        }}
                    >
                        {alertCount > 99 ? '99+' : alertCount}
                    </Box>
                </Box>
            </Tooltip>
        );
    }

    // Healthy nodes - green checkmark
    return (
        <Tooltip title="Online" placement="right">
            <HealthyIcon
                sx={{
                    fontSize: 14,
                    color: '#22C55E',
                    filter: 'drop-shadow(0 0 2px #22C55E)',
                }}
            />
        </Tooltip>
    );
};

export default StatusIndicator;
