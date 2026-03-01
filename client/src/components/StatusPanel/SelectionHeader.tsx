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
    IconButton,
    Tooltip,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Storage as ServerIcon,
    Dns as ClusterIcon,
    Language as EstateIcon,
    Info as InfoIcon,
    DarkMode as BlackoutIcon,
} from '@mui/icons-material';
import {
    HEADER_CONTAINER_SX,
    HEADER_ICON_BOX_BASE_SX,
    HEADER_LABEL_SX,
    HEADER_NAME_SX,
} from './styles';

/**
 * SelectionHeader - Header showing what's currently selected
 */
const SelectionHeader = ({ selection, alertCount = 0, alertSeverities = {}, onBlackoutClick }) => {
    const theme = useTheme();

    const getIcon = () => {
        switch (selection.type) {
            case 'server':
                return ServerIcon;
            case 'cluster':
                return ClusterIcon;
            case 'estate':
                return EstateIcon;
            default:
                return InfoIcon;
        }
    };

    const getLabel = () => {
        switch (selection.type) {
            case 'server':
                return 'Server';
            case 'cluster':
                return 'Cluster';
            case 'estate':
                return 'Estate Overview';
            default:
                return 'Selection';
        }
    };

    const Icon = getIcon();

    const status = selection.status;

    const iconColor = useMemo(() => {
        if (status === 'offline') {return 'error.main';}
        if (alertCount > 0) {return 'warning.main';}
        return 'success.main';
    }, [status, alertCount]);

    const iconBoxSx = useMemo(() => {
        let palette = theme.palette.success.main;
        if (status === 'offline') {palette = theme.palette.error.main;}
        else if (alertCount > 0) {palette = theme.palette.warning.main;}

        return {
            ...HEADER_ICON_BOX_BASE_SX,
            bgcolor: alpha(palette, 0.12),
            position: 'relative',
        };
    }, [theme.palette.success, theme.palette.error, theme.palette.warning, status, alertCount]);

    const tooltipTitle = useMemo(() => {
        const typeName = selection.type === 'server' ? 'Server'
            : selection.type === 'cluster' ? 'Cluster'
            : selection.type === 'estate' ? 'Estate'
            : 'Selection';

        if (status === 'offline') {return `${typeName} is offline`;}

        if (alertCount > 0) {
            const parts = Object.entries(alertSeverities)
                .sort(([a], [b]) => {
                    const order = { critical: 0, warning: 1, info: 2 };
                    return (order[a] ?? 99) - (order[b] ?? 99);
                })
                .map(([sev, count]) =>
                    `${count} ${sev.charAt(0).toUpperCase() + sev.slice(1)}`
                );
            const alertWord = alertCount === 1 ? 'alert' : 'alerts';
            return `${alertCount} active ${alertWord}: ${parts.join(', ')}`;
        }

        return `${typeName} is online`;
    }, [status, alertCount, alertSeverities, selection.type]);

    const badgeSx = useMemo(() => ({
        position: 'absolute',
        top: -4,
        right: -4,
        minWidth: 16,
        height: 16,
        px: 0.5,
        borderRadius: '8px',
        bgcolor: theme.palette.grey[500],
        color: 'common.white',
        fontSize: '0.625rem',
        fontWeight: 700,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        lineHeight: 1,
    }), [theme.palette.grey]);

    const showBadge = alertCount > 0;

    return (
        <Box sx={HEADER_CONTAINER_SX}>
            <Tooltip title={tooltipTitle} placement="bottom">
                <Box sx={iconBoxSx}>
                    <Icon sx={{ fontSize: 24, color: iconColor }} />
                    {showBadge && (
                        <Box sx={badgeSx}>
                            {alertCount > 99 ? '99+' : alertCount}
                        </Box>
                    )}
                </Box>
            </Tooltip>
            <Box sx={{ flex: 1 }}>
                <Typography variant="overline" sx={HEADER_LABEL_SX}>
                    {getLabel()}
                </Typography>
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5 }}>
                    <Typography variant="h5" sx={HEADER_NAME_SX}>
                        {selection.name}
                    </Typography>
                    {selection.description && (
                        <Typography variant="body2" sx={{ color: 'text.secondary', fontWeight: 400 }}>
                            {selection.description}
                        </Typography>
                    )}
                </Box>
            </Box>
            <Tooltip title="Blackout management" placement="bottom">
                <IconButton
                    size="small"
                    onClick={onBlackoutClick}
                    sx={{ color: 'text.secondary', mr: 1 }}
                >
                    <BlackoutIcon sx={{ fontSize: 20 }} />
                </IconButton>
            </Tooltip>
        </Box>
    );
};

export default SelectionHeader;
