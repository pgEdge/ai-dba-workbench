/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Blackout management panel for StatusPanel integration
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import {
    Box,
    Typography,
    Chip,
    alpha,
    Button,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    DarkMode as MoonIcon,
    Stop as StopIcon,
    Language as EstateIcon,
    FolderSpecial as GroupIcon,
    Dns as ClusterIcon,
    Storage as ServerIcon,
} from '@mui/icons-material';
import { useBlackouts } from '../contexts/useBlackouts';
import type { Selection } from '../types/selection';

// ---- Static style constants ----

const CHIP_LABEL_SX = { px: 0.5 };
const ICON_14_SX = { fontSize: 14 };

const BANNER_TITLE_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    lineHeight: 1.2,
};

const BANNER_REASON_SX = {
    fontSize: '0.875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

// ---- Helper functions ----

const getScopeIcon = (scope: string) => {
    switch (scope) {
        case 'estate': return EstateIcon;
        case 'group': return GroupIcon;
        case 'cluster': return ClusterIcon;
        case 'server': return ServerIcon;
        default: return ServerIcon;
    }
};

const getScopeLabel = (scope: string) => {
    switch (scope) {
        case 'estate': return 'Estate';
        case 'group': return 'Group';
        case 'cluster': return 'Cluster';
        case 'server': return 'Server';
        default: return scope;
    }
};

const formatTimeRemaining = (endTime: string): string => {
    const end = new Date(endTime);
    const now = new Date();
    const diffMs = end.getTime() - now.getTime();

    if (diffMs <= 0) {return 'Ending...';}

    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const remainingMins = diffMins % 60;

    if (diffHours > 0) {
        return `${diffHours}h ${remainingMins}m remaining`;
    }
    return `${diffMins}m remaining`;
};

// ---- Sub-components ----

/**
 * ActiveBlackoutBanner - Prominent amber banner for active blackouts
 */
const ActiveBlackoutBanner = ({ blackout, onStop }) => {
    const theme = useTheme();
    const amberColor = theme.palette.warning.main;
    const ScopeIcon = getScopeIcon(blackout.scope);

    const containerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        borderRadius: 1,
        bgcolor: alpha(amberColor, 0.12),
        border: '1px solid',
        borderColor: alpha(amberColor, 0.25),
    }), [amberColor]);

    const iconSx = useMemo(() => ({
        fontSize: 18,
        color: amberColor,
        flexShrink: 0,
    }), [amberColor]);

    const scopeChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        bgcolor: alpha(amberColor, 0.15),
        color: amberColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [amberColor]);

    const timeChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        fontWeight: 600,
        bgcolor: alpha(amberColor, 0.12),
        color: amberColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [amberColor]);

    const stopButtonSx = useMemo(() => ({
        fontSize: '0.875rem',
        textTransform: 'none',
        fontWeight: 600,
        color: amberColor,
        borderColor: alpha(amberColor, 0.4),
        '&:hover': {
            borderColor: amberColor,
            bgcolor: alpha(amberColor, 0.12),
        },
    }), [amberColor]);

    return (
        <Box sx={containerSx}>
            <MoonIcon sx={iconSx} />
            <Box sx={{ flex: 1, minWidth: 0 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
                    <Typography sx={{ ...BANNER_TITLE_SX, color: amberColor }}>
                        Blackout Active
                    </Typography>
                    <Chip
                        icon={<ScopeIcon sx={{ fontSize: '0.5rem !important' }} />}
                        label={getScopeLabel(blackout.scope)}
                        size="small"
                        sx={scopeChipSx}
                    />
                    <Chip
                        label={formatTimeRemaining(blackout.end_time)}
                        size="small"
                        sx={timeChipSx}
                    />
                </Box>
                {blackout.reason && (
                    <Typography sx={{ ...BANNER_REASON_SX, color: alpha(amberColor, 0.85) }}>
                        {blackout.reason}
                    </Typography>
                )}
            </Box>
            <Button
                variant="outlined"
                size="small"
                startIcon={<StopIcon sx={ICON_14_SX} />}
                onClick={() => onStop(blackout.id)}
                sx={stopButtonSx}
            >
                Stop
            </Button>
        </Box>
    );
};

// ---- Main component ----

interface BlackoutPanelProps {
    selection: Selection | null;
}

/**
 * BlackoutPanel - Displays active blackout banners for the current selection.
 */
const BlackoutPanel: React.FC<BlackoutPanelProps> = ({ selection }) => {
    const { activeBlackoutsForSelection, stopBlackout } = useBlackouts();

    if (!selection) {return null;}

    const activeCount = (activeBlackoutsForSelection || []).length;
    if (activeCount === 0) {return null;}

    return (
        <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 0.5 }}>
            {activeBlackoutsForSelection.map((blackout) => (
                <ActiveBlackoutBanner
                    key={blackout.id}
                    blackout={blackout}
                    onStop={stopBlackout}
                />
            ))}
        </Box>
    );
};

export default BlackoutPanel;
