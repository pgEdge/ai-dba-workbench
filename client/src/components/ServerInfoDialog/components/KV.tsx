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
import { Box, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { MONO_FONT, getKvLabelSx, getKvValueSx } from '../serverInfoStyles';

export interface KVProps {
    label: string;
    value: React.ReactNode;
    mono?: boolean;
    span?: boolean;
}

/**
 * Key-value pair display component.
 */
const KV: React.FC<KVProps> = ({ label, value, mono = true, span = false }) => {
    const theme = useTheme();
    return (
        <Box sx={span ? { gridColumn: '1 / -1' } : undefined}>
            <Typography sx={getKvLabelSx(theme)}>{label}</Typography>
            <Typography sx={{
                ...getKvValueSx(),
                fontFamily: mono ? MONO_FONT : 'inherit',
            }}>
                {value === null || value === undefined || value === '' ? '\u2014' : value}
            </Typography>
        </Box>
    );
};

export default KV;
