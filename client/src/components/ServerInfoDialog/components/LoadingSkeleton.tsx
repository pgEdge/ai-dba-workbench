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
import { Box, Skeleton } from '@mui/material';
import { useTheme } from '@mui/material/styles';

/**
 * Loading skeleton placeholder for the server info dialog.
 */
const LoadingSkeleton: React.FC = () => {
    const theme = useTheme();
    const bg = {
        bgcolor: theme.palette.mode === 'dark'
            ? theme.palette.grey[700]
            : theme.palette.grey[200],
    };

    return (
        <Box sx={{ p: 2.5 }}>
            {/* System section skeleton */}
            <Skeleton variant="text" width="40%" height={20} sx={{ ...bg, mb: 1.5 }} />
            <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 1.5, mb: 3 }}>
                {[1, 2, 3, 4, 5, 6].map(i => (
                    <Box key={i}>
                        <Skeleton variant="text" width="60%" height={12} sx={bg} />
                        <Skeleton variant="text" width="80%" height={18} sx={{ ...bg, mt: 0.5 }} />
                    </Box>
                ))}
            </Box>
            {/* PostgreSQL section skeleton */}
            <Skeleton variant="text" width="35%" height={20} sx={{ ...bg, mb: 1.5 }} />
            <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 1.5, mb: 3 }}>
                {[1, 2, 3].map(i => (
                    <Box key={i}>
                        <Skeleton variant="text" width="60%" height={12} sx={bg} />
                        <Skeleton variant="text" width="80%" height={18} sx={{ ...bg, mt: 0.5 }} />
                    </Box>
                ))}
            </Box>
            {/* Databases section skeleton */}
            <Skeleton variant="text" width="30%" height={20} sx={{ ...bg, mb: 1.5 }} />
            {[1, 2].map(i => (
                <Box key={i} sx={{ mb: 1.5 }}>
                    <Skeleton variant="text" width="25%" height={16} sx={bg} />
                    <Skeleton variant="text" width="90%" height={14} sx={{ ...bg, mt: 0.5 }} />
                </Box>
            ))}
        </Box>
    );
};

export default LoadingSkeleton;
