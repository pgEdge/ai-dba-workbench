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
import PsychologyIcon from '@mui/icons-material/Psychology';
import SaveAltIcon from '@mui/icons-material/SaveAlt';
import RefreshIcon from '@mui/icons-material/Refresh';
import { useAICapabilities } from '../../contexts/useAICapabilities';
import { CHART_TOOLBAR_SX } from './styles';

interface ChartToolbarProps {
    onAnalyze?: () => void;
    onExport?: () => void;
    onRefresh?: () => void;
    showAnalyze?: boolean;
    showExport?: boolean;
    showRefresh?: boolean;
    cached?: boolean;
}

export function ChartToolbar({
    onAnalyze,
    onExport,
    onRefresh,
    showAnalyze,
    showExport,
    showRefresh,
    cached,
}: ChartToolbarProps) {
    const { aiEnabled } = useAICapabilities();
    return (
        <Box sx={CHART_TOOLBAR_SX}>
            {aiEnabled && showAnalyze && onAnalyze && (
                <Tooltip title="AI Analysis">
                    <IconButton size="small" onClick={onAnalyze} color={cached ? 'warning' : 'secondary'}>
                        <PsychologyIcon fontSize="small" />
                    </IconButton>
                </Tooltip>
            )}
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
