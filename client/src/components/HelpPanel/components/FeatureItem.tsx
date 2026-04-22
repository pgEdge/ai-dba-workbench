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
import { styles } from '../helpPanelStyles';

export interface FeatureItemProps {
    title: string;
    description: string;
}

/**
 * FeatureItem - Single feature in a feature list
 */
const FeatureItem: React.FC<FeatureItemProps> = ({ title, description }) => (
    <Box sx={styles.featureWrapper}>
        <Typography sx={styles.featureTitle}>
            {title}
        </Typography>
        <Typography sx={styles.featureDescription}>
            {description}
        </Typography>
    </Box>
);

export default FeatureItem;
