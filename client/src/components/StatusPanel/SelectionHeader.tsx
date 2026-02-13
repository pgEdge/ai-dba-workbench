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
import HeaderStatusIndicator from './HeaderStatusIndicator';
import {
    HEADER_CONTAINER_SX,
    HEADER_ICON_BOX_BASE_SX,
    HEADER_LABEL_SX,
    HEADER_NAME_SX,
} from './styles';

/**
 * SelectionHeader - Header showing what's currently selected
 */
const SelectionHeader = ({ selection, alertCount = 0, onBlackoutClick }) => {
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

    const iconBoxSx = useMemo(() => ({
        ...HEADER_ICON_BOX_BASE_SX,
        bgcolor: alpha(theme.palette.primary.main, 0.12),
    }), [theme.palette.primary]);

    return (
        <Box sx={HEADER_CONTAINER_SX}>
            <Box sx={iconBoxSx}>
                <Icon sx={{ fontSize: 24, color: 'primary.main' }} />
            </Box>
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
            <HeaderStatusIndicator
                status={selection.status}
                alertCount={alertCount}
            />
        </Box>
    );
};

export default SelectionHeader;
