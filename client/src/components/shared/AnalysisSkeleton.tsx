/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - AnalysisSkeleton component. Loading
 * skeleton for analysis content.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { Box, Skeleton, useTheme } from '@mui/material';
import { getSkeletonBgSx, sxSkeletonContainer, sxSkeletonRow } from './markdownStyles';

const AnalysisSkeleton: React.FC = () => {
    const theme = useTheme();
    const skeletonBg = getSkeletonBgSx(theme);

    return (
        <Box sx={sxSkeletonContainer}>
            {/* Summary section */}
            <Skeleton variant="text" width="30%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            <Skeleton variant="text" width="100%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="85%" height={20} sx={{ ...skeletonBg, mb: 2.5 }} />

            {/* Analysis section */}
            <Skeleton variant="text" width="25%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            <Skeleton variant="text" width="100%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="90%" height={20} sx={skeletonBg} />
            <Skeleton variant="text" width="75%" height={20} sx={{ ...skeletonBg, mb: 2.5 }} />

            {/* Remediation section */}
            <Skeleton variant="text" width="35%" height={28} sx={{ ...skeletonBg, mb: 1 }} />
            {[1, 2, 3].map((i) => (
                <Box key={i} sx={sxSkeletonRow}>
                    <Skeleton variant="circular" width={8} height={8} sx={skeletonBg} />
                    <Skeleton variant="text" width={`${85 - i * 10}%`} height={20} sx={skeletonBg} />
                </Box>
            ))}
        </Box>
    );
};

export default AnalysisSkeleton;
