/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useCallback } from 'react';
import Box from '@mui/material/Box';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import { useDashboard } from '../../contexts/useDashboard';
import { TimeRange } from './types';
import { TIME_RANGE_CONTAINER_SX } from './styles';

/** Available time range options with display labels */
const TIME_RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
];

const TOGGLE_BUTTON_SX = {
    px: 1.5,
    py: 0.25,
    fontSize: '0.75rem',
    fontWeight: 600,
    textTransform: 'none' as const,
    minWidth: 36,
};

/**
 * A compact time range selector that renders toggle buttons for
 * predefined ranges. Reads and updates the time range from
 * DashboardContext.
 */
const TimeRangeSelector: React.FC = () => {
    const { timeRange, setTimeRange } = useDashboard();

    const handleChange = useCallback(
        (_event: React.MouseEvent<HTMLElement>, newRange: TimeRange | null) => {
            if (newRange !== null) {
                setTimeRange(newRange);
            }
        },
        [setTimeRange]
    );

    return (
        <Box sx={TIME_RANGE_CONTAINER_SX}>
            <ToggleButtonGroup
                value={timeRange.range}
                exclusive
                onChange={handleChange}
                size="small"
                aria-label="Time range selection"
            >
                {TIME_RANGE_OPTIONS.map(option => (
                    <ToggleButton
                        key={option.value}
                        value={option.value}
                        sx={TOGGLE_BUTTON_SX}
                        aria-label={`Select ${option.label} time range`}
                    >
                        {option.label}
                    </ToggleButton>
                ))}
            </ToggleButtonGroup>
        </Box>
    );
};

export default TimeRangeSelector;
