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
import { SvgIconComponent } from '@mui/icons-material';
import { styles } from '../helpPanelStyles';

export interface SectionTitleProps {
    children: React.ReactNode;
    icon?: SvgIconComponent;
}

/**
 * SectionTitle - Section header within help content
 */
const SectionTitle: React.FC<SectionTitleProps> = ({ children, icon: Icon }) => (
    <Box sx={styles.sectionTitleWrapper}>
        {Icon && <Icon sx={styles.sectionTitleIcon} />}
        <Typography variant="h6" sx={styles.sectionTitleText}>
            {children}
        </Typography>
    </Box>
);

export default SectionTitle;
