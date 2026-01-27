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
import { Chip, alpha } from '@mui/material';
import { ROLE_CONFIGS, ROLE_ICONS } from './constants';

/**
 * RolePill - Displays a colored chip based on the server role
 */
const RolePill = ({ role, isDark }) => {
    const config = ROLE_CONFIGS[role];
    if (!config) return null;

    const color = isDark ? config.darkColor : config.color;
    const IconComponent = ROLE_ICONS[role];

    return (
        <Chip
            icon={IconComponent ? <IconComponent sx={{ fontSize: '10px !important', color: `${color} !important` }} /> : undefined}
            label={config.label}
            size="small"
            sx={{
                height: 18,
                fontSize: '0.625rem',
                fontWeight: 600,
                bgcolor: alpha(color, isDark ? 0.2 : 0.12),
                color: color,
                '& .MuiChip-icon': { ml: 0.5, mr: -0.25 },
                '& .MuiChip-label': { pl: 0.75, pr: 0.75 },
            }}
        />
    );
};

export default RolePill;
