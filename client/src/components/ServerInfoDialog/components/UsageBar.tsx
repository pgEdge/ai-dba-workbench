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
import { Box, Typography, LinearProgress } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { formatBytes, pct } from '../serverInfoFormatters';
import { sxMono, getKvLabelSx, getProgressBarSx } from '../serverInfoStyles';

export interface UsageBarProps {
    label: string;
    used: number | null;
    total: number | null;
}

/**
 * Small usage bar with label showing used/total and percentage.
 */
const UsageBar: React.FC<UsageBarProps> = ({ label, used, total }) => {
    const theme = useTheme();
    const percentage = pct(used, total);
    if (percentage == null) {
        return null;
    }

    return (
        <Box sx={{ gridColumn: '1 / -1' }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.25 }}>
                <Typography sx={getKvLabelSx(theme)}>{label}</Typography>
                <Typography sx={{
                    fontSize: '0.875rem',
                    color: 'text.disabled',
                    ...sxMono,
                }}>
                    {formatBytes(used)} / {formatBytes(total)} ({percentage}%)
                </Typography>
            </Box>
            <LinearProgress
                variant="determinate"
                value={percentage}
                sx={getProgressBarSx(theme, percentage)}
            />
        </Box>
    );
};

export default UsageBar;
