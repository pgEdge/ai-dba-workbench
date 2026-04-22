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
import { styles, getHelpTipSx } from '../helpPanelStyles';

export interface HelpTipProps {
    children: React.ReactNode;
}

/**
 * HelpTip - Highlighted tip or important note
 */
const HelpTip: React.FC<HelpTipProps> = ({ children }) => (
    <Box sx={getHelpTipSx}>
        <Typography sx={styles.helpTipText}>
            <Box component="strong" sx={{ color: 'primary.main' }}>
                Tip:
            </Box>{' '}
            {children}
        </Typography>
    </Box>
);

export default HelpTip;
