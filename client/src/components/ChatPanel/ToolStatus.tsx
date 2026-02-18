/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Box, Chip, CircularProgress, alpha } from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    CheckCircle as CheckIcon,
    Warning as WarningIcon,
} from '@mui/icons-material';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ToolActivity {
    name: string;
    status: 'running' | 'completed' | 'error';
    startedAt?: string;
}

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const containerSx = {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 0.75,
    px: 2,
    py: 1,
};

const getChipSx = (status: ToolActivity['status']) => (theme: Theme) => {
    if (status === 'error') {
        return {
            height: 24,
            fontSize: '1rem',
            fontWeight: 500,
            bgcolor: alpha(theme.palette.warning.main, 0.12),
            color: theme.palette.mode === 'dark'
                ? theme.palette.warning.light
                : theme.palette.warning.dark,
            borderColor: alpha(theme.palette.warning.main, 0.3),
            '& .MuiChip-icon': {
                color: theme.palette.warning.main,
                fontSize: 16,
            },
        };
    }
    if (status === 'completed') {
        return {
            height: 24,
            fontSize: '1rem',
            fontWeight: 500,
            bgcolor: alpha(theme.palette.success.main, 0.12),
            color: theme.palette.mode === 'dark'
                ? theme.palette.success.light
                : theme.palette.success.dark,
            borderColor: alpha(theme.palette.success.main, 0.3),
            '& .MuiChip-icon': {
                color: theme.palette.success.main,
                fontSize: 16,
            },
        };
    }
    // running
    return {
        height: 24,
        fontSize: '1rem',
        fontWeight: 500,
        bgcolor: alpha(theme.palette.primary.main, 0.12),
        color: theme.palette.mode === 'dark'
            ? theme.palette.primary.light
            : theme.palette.primary.dark,
        borderColor: alpha(theme.palette.primary.main, 0.3),
        '& .MuiChip-icon': {
            color: theme.palette.primary.main,
            marginLeft: '6px',
        },
    };
};

const spinnerSx = { ml: 0 };

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ToolStatusProps {
    tools: ToolActivity[];
}

const getStatusIcon = (status: ToolActivity['status']) => {
    switch (status) {
        case 'completed':
            return <CheckIcon sx={{ fontSize: 16 }} />;
        case 'error':
            return <WarningIcon sx={{ fontSize: 16 }} />;
        case 'running':
        default:
            return <CircularProgress size={12} sx={spinnerSx} aria-label="Running" />;
    }
};

const ToolStatus: React.FC<ToolStatusProps> = ({ tools }) => {
    if (!tools || tools.length === 0) {
        return null;
    }

    return (
        <Box sx={containerSx}>
            {tools.map((tool, index) => (
                <Chip
                    key={`${tool.name}-${index}`}
                    icon={getStatusIcon(tool.status)}
                    label={tool.name}
                    variant="outlined"
                    size="small"
                    sx={getChipSx(tool.status)}
                />
            ))}
        </Box>
    );
};

export default ToolStatus;
