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
import { useCallback } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import Backdrop from '@mui/material/Backdrop';
import Slide from '@mui/material/Slide';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import CloseIcon from '@mui/icons-material/Close';
import { alpha, useTheme } from '@mui/material/styles';
import { useDashboard } from '../../contexts/useDashboard';
import {
    OVERLAY_CONTAINER_SX,
    OVERLAY_HEADER_SX,
    OVERLAY_TITLE_SX,
    OVERLAY_CONTENT_SX,
} from './styles';

interface MetricOverlayProps {
    children: React.ReactNode;
}

/**
 * Overlay component that manages drill-down overlays for dashboard
 * navigation. Renders inside the StatusPanel content area with a
 * dimmed backdrop and slide-in animation.
 */
const MetricOverlay: React.FC<MetricOverlayProps> = ({ children }) => {
    const { currentOverlay, overlayStack, popOverlay, clearOverlays } = useDashboard();
    const theme = useTheme();

    const handleBack = useCallback((): void => {
        popOverlay();
    }, [popOverlay]);

    const handleClose = useCallback((): void => {
        clearOverlays();
    }, [clearOverlays]);

    if (!currentOverlay) {
        return null;
    }

    return (
        <>
            <Backdrop
                open={true}
                sx={{
                    position: 'absolute',
                    zIndex: 9,
                    backgroundColor: alpha(theme.palette.background.default, 0.7),
                }}
            />
            <Slide direction="up" in={true} mountOnEnter unmountOnExit>
                <Box
                    sx={{
                        ...OVERLAY_CONTAINER_SX as object,
                        bgcolor: 'background.paper',
                    }}
                >
                    <Box sx={OVERLAY_HEADER_SX}>
                        {overlayStack.length > 1 && (
                            <IconButton
                                size="small"
                                onClick={handleBack}
                                aria-label="Go back"
                            >
                                <ArrowBackIcon fontSize="small" />
                            </IconButton>
                        )}
                        <Typography sx={OVERLAY_TITLE_SX}>
                            {currentOverlay.title}
                        </Typography>
                        <IconButton
                            size="small"
                            onClick={handleClose}
                            aria-label="Close overlay"
                        >
                            <CloseIcon fontSize="small" />
                        </IconButton>
                    </Box>
                    <Box sx={OVERLAY_CONTENT_SX}>
                        {children}
                    </Box>
                </Box>
            </Slide>
        </>
    );
};

export default MetricOverlay;
