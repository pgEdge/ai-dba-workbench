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
import { Box } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { getClusterTypeColors } from './constants';
import { getClusterType } from './utils';
import type { Cluster } from './utils';

interface ClusterContainerProps {
    children: React.ReactNode;
    cluster: Cluster;
    isDark: boolean;
}

/**
 * ClusterContainer - Wraps entire cluster (header + servers) with styled border
 * Color varies by cluster type: spock (amber), binary (cyan), logical (purple), default (gray)
 */
const ClusterContainer: React.FC<ClusterContainerProps> = ({ children, cluster, isDark }) => {
    const theme = useTheme();
    const clusterType = getClusterType(cluster);
    const colors = getClusterTypeColors(theme)[clusterType];
    const borderColor = isDark ? colors.border.dark : colors.border.light;
    const bgColor = isDark ? colors.bg.dark : colors.bg.light;

    return (
        <Box
            sx={{
                border: `1px solid`,
                borderColor: borderColor,
                bgcolor: bgColor,
                borderRadius: '8px',
                mx: 1,
                my: 0.5,
                overflow: 'hidden',
            }}
        >
            {children}
        </Box>
    );
};

export default ClusterContainer;
