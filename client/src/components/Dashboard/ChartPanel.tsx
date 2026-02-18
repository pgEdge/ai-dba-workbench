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
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { CHART_PAPER_SX, CHART_TITLE_SX } from '../Chart/styles';

interface ChartPanelProps {
    /** The chart title displayed at the top of the panel. */
    title: string;
    /** Whether data is currently loading. */
    loading: boolean;
    /** Whether chart data is available to render. */
    hasData: boolean;
    /** Message to display when no data is available. */
    emptyMessage: string;
    /** Height of the chart area in pixels. */
    height: number;
    /** The Chart component to render when data is available. */
    children: React.ReactNode;
}

/**
 * ChartPanel wraps chart content with a consistent container that
 * handles loading and empty states. When data is available, the
 * children (typically a Chart component) render directly since the
 * Chart component provides its own Paper wrapper. When loading or
 * empty, the panel renders a matching Paper container with the
 * title and appropriate content.
 */
const ChartPanel: React.FC<ChartPanelProps> = ({
    title,
    loading,
    hasData,
    emptyMessage,
    height,
    children,
}) => {
    if (hasData && !loading) {
        return <>{children}</>;
    }

    return (
        <Paper sx={CHART_PAPER_SX} elevation={1}>
            <Typography sx={CHART_TITLE_SX}>{title}</Typography>
            <Box sx={{
                display: 'flex',
                justifyContent: 'center',
                alignItems: 'center',
                height,
            }}>
                {loading ? (
                    <CircularProgress size={24} aria-label="Loading chart" />
                ) : (
                    <Typography variant="body2" color="text.secondary">
                        {emptyMessage}
                    </Typography>
                )}
            </Box>
        </Paper>
    );
};

export default ChartPanel;
