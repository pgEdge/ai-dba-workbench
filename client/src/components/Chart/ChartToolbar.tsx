/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import SaveAltIcon from '@mui/icons-material/SaveAlt';
import RefreshIcon from '@mui/icons-material/Refresh';
import { CHART_TOOLBAR_SX } from './styles';

interface ChartToolbarProps {
    onExport?: () => void;
    onRefresh?: () => void;
    showExport?: boolean;
    showRefresh?: boolean;
}

export function ChartToolbar({
    onExport,
    onRefresh,
    showExport,
    showRefresh,
}: ChartToolbarProps) {
    return (
        <Box sx={CHART_TOOLBAR_SX}>
            {showRefresh && onRefresh && (
                <Tooltip title="Refresh data">
                    <IconButton size="small" onClick={onRefresh}>
                        <RefreshIcon fontSize="small" />
                    </IconButton>
                </Tooltip>
            )}
            {showExport && onExport && (
                <Tooltip title="Export as PNG">
                    <IconButton size="small" onClick={onExport}>
                        <SaveAltIcon fontSize="small" />
                    </IconButton>
                </Tooltip>
            )}
        </Box>
    );
}
