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
import { useCallback, useState } from 'react';
import Box from '@mui/material/Box';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Tooltip from '@mui/material/Tooltip';
import PauseCircleOutlineIcon from '@mui/icons-material/PauseCircleOutline';
import dayjs from 'dayjs';
import { useDashboard } from '../../contexts/useDashboard';
import type { TimeRange } from './types';
import CustomTimeRangePopover from './CustomTimeRangePopover';
import {
    AUTO_REFRESH_SUSPENDED_ICON_SX,
    TIME_RANGE_CONTAINER_SX,
} from './styles';

/** Available preset time range options with display labels */
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
 * Wording for the auto-refresh pause indicator, shared between the tooltip
 * and the accessible name so that pointer and assistive-technology users
 * receive exactly the same explanation.
 */
const AUTO_REFRESH_SUSPENDED_LABEL =
    'Auto-refresh is paused whilst a custom time range is active';

/**
 * The indicator is purely informational, so it is anchored on a focusable
 * span rather than the icon itself: the span reliably takes keyboard focus
 * across browsers (SVG focus handling is inconsistent), which lets the
 * tooltip open on focus as well as hover, whilst inline-flex keeps the
 * rendered layout identical to the bare icon.
 */
const AUTO_REFRESH_SUSPENDED_ANCHOR_SX = {
    display: 'inline-flex',
    alignItems: 'center',
};

/** Compact display format for an applied custom window */
const WINDOW_FORMAT = 'DD MMM HH:mm';

/**
 * Render an applied custom window compactly, so that the toggle shows the
 * selection without the user having to open the popover. Falls back to the
 * word "Custom" whenever no usable window is set.
 */
const formatWindow = (startISO?: string, endISO?: string): string => {
    if (startISO === undefined || endISO === undefined) {
        return 'Custom';
    }
    const start = dayjs(startISO);
    const end = dayjs(endISO);
    if (!start.isValid() || !end.isValid()) {
        return 'Custom';
    }
    return `${start.format(WINDOW_FORMAT)} - ${end.format(WINDOW_FORMAT)}`;
};

/**
 * A compact time range selector that renders toggle buttons for
 * predefined ranges plus a custom option that opens a date-time picker.
 * Reads and updates the time range from DashboardContext.
 */
const TimeRangeSelector: React.FC = () => {
    const {
        timeRange,
        setTimeRange,
        setCustomTimeRange,
        autoRefreshSuspended,
    } = useDashboard();
    const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

    const handleChange = useCallback(
        (_event: React.MouseEvent<HTMLElement>, newRange: TimeRange | null) => {
            // The custom toggle opens the popover rather than selecting a
            // range directly, so its own click handler deals with it.
            if (newRange !== null && newRange !== 'custom') {
                setTimeRange(newRange);
            }
        },
        [setTimeRange]
    );

    const handleCustomClick = useCallback(
        (event: React.MouseEvent<HTMLElement>): void => {
            setAnchorEl(event.currentTarget);
        },
        []
    );

    const handlePopoverClose = useCallback((): void => {
        setAnchorEl(null);
    }, []);

    const handleApply = useCallback(
        (startISO: string, endISO: string): void => {
            setCustomTimeRange(startISO, endISO);
            setAnchorEl(null);
        },
        [setCustomTimeRange]
    );

    const isCustom = timeRange.range === 'custom';

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
                <ToggleButton
                    value="custom"
                    sx={TOGGLE_BUTTON_SX}
                    aria-label="Select custom time range"
                    onClick={handleCustomClick}
                >
                    {isCustom
                        ? formatWindow(
                            timeRange.customStart,
                            timeRange.customEnd,
                        )
                        : 'Custom'}
                </ToggleButton>
            </ToggleButtonGroup>
            {/*
              * Auto-refresh keeps polling on presets but is suspended for a
              * custom window. The anchor span carries role="img" and the
              * accessible name, so screen readers announce the pause without
              * needing the tooltip at all, and its tabIndex lets keyboard
              * users focus it to read the same explanation a pointer user
              * gets on hover.
              */}
            {autoRefreshSuspended && (
                <Tooltip title={AUTO_REFRESH_SUSPENDED_LABEL}>
                    <Box
                        component="span"
                        role="img"
                        aria-label={AUTO_REFRESH_SUSPENDED_LABEL}
                        tabIndex={0}
                        sx={AUTO_REFRESH_SUSPENDED_ANCHOR_SX}
                    >
                        <PauseCircleOutlineIcon
                            sx={AUTO_REFRESH_SUSPENDED_ICON_SX}
                        />
                    </Box>
                </Tooltip>
            )}
            <CustomTimeRangePopover
                open={anchorEl !== null}
                anchorEl={anchorEl}
                startISO={timeRange.customStart}
                endISO={timeRange.customEnd}
                onApply={handleApply}
                onClose={handlePopoverClose}
            />
        </Box>
    );
};

export default TimeRangeSelector;
