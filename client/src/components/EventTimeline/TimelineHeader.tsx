/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, memo } from 'react';
import {
    Box,
    Typography,
    Chip,
    IconButton,
    Skeleton,
    ToggleButton,
    ToggleButtonGroup,
    useTheme,
} from '@mui/material';
import {
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Timeline as TimelineIcon,
} from '@mui/icons-material';
import { resolveColor } from './utils';
import { ALL_EVENT_TYPES, FILTER_CHIPS, TIME_RANGE_OPTIONS } from './config';
import {
    headerContainerSx,
    headerTitleGroupSx,
    headerTitleIconSx,
    headerTitleTextSx,
    headerExpandSx,
    expandIconMedSx,
    filterChipsSx,
    getToggleGroupSx,
    getEventCountChipSx,
    getFilterChipSx,
    loadingSkeletonRowSx,
    loadingSkeletonBarSx,
    loadingSkeletonTimeSx,
    getEmptyStateSx,
    emptyStateTitleSx,
    emptyStateSubtitleSx,
} from './styles';

/**
 * TimelineHeader - Collapsible header with controls
 */
export const TimelineHeader = memo(({
    expanded,
    onExpandToggle,
    eventCount,
    timeRange,
    onTimeRangeChange,
    eventTypes,
    onEventTypesChange,
}) => {
    const theme = useTheme();

    const toggleGroupSx = useMemo(() => getToggleGroupSx(theme), [theme]);
    const countChipSx = useMemo(() => getEventCountChipSx(theme, eventCount), [theme, eventCount]);

    return (
        <Box sx={headerContainerSx}>
            {/* Title with expand toggle */}
            <Box
                onClick={onExpandToggle}
                sx={headerTitleGroupSx}
            >
                <TimelineIcon sx={headerTitleIconSx} />
                <Typography sx={headerTitleTextSx}>
                    Event Timeline
                </Typography>
                <Chip
                    label={eventCount}
                    size="small"
                    sx={countChipSx}
                />
                <IconButton size="small" sx={headerExpandSx}>
                    {expanded ? (
                        <ExpandLessIcon sx={expandIconMedSx} />
                    ) : (
                        <ExpandMoreIcon sx={expandIconMedSx} />
                    )}
                </IconButton>
            </Box>

            <Box sx={{ flex: 1 }} />

            {/* Time range selector */}
            <ToggleButtonGroup
                value={timeRange}
                exclusive
                onChange={(e, value) => value && onTimeRangeChange(value)}
                size="small"
                sx={toggleGroupSx}
            >
                {TIME_RANGE_OPTIONS.map((option) => (
                    <ToggleButton key={option.value} value={option.value}>
                        {option.label}
                    </ToggleButton>
                ))}
            </ToggleButtonGroup>

            {/* Event type filter chips */}
            <Box sx={filterChipsSx}>
                {Object.entries(FILTER_CHIPS).map(([chipKey, chipConfig]) => {
                    const chipTypes = chipConfig.types;
                    const isSelected = eventTypes.includes('all') ||
                        chipTypes.every(t => eventTypes.includes(t));
                    const color = resolveColor(theme.palette, chipConfig.colorKey);
                    return (
                        <Chip
                            key={chipKey}
                            label={chipConfig.label}
                            size="small"
                            onClick={() => {
                                if (eventTypes.includes('all')) {
                                    // Switching from 'all' - select only this chip's types
                                    onEventTypesChange([...chipTypes]);
                                } else if (isSelected) {
                                    // Deselect this chip's types
                                    const remaining = eventTypes.filter(t => !chipTypes.includes(t));
                                    if (remaining.length === 0) {
                                        // Last chip selected - switch back to all
                                        onEventTypesChange(['all']);
                                    } else {
                                        onEventTypesChange(remaining);
                                    }
                                } else {
                                    // Select this chip's types
                                    const newTypes = [...eventTypes, ...chipTypes];
                                    if (newTypes.length >= ALL_EVENT_TYPES.length) {
                                        onEventTypesChange(['all']);
                                    } else {
                                        onEventTypesChange(newTypes);
                                    }
                                }
                            }}
                            sx={getFilterChipSx(theme, isSelected, color)}
                        />
                    );
                })}
            </Box>
        </Box>
    );
});

TimelineHeader.displayName = 'TimelineHeader';

/**
 * LoadingSkeleton - Skeleton state while loading
 */
export const LoadingSkeleton = () => {
    const theme = useTheme();
    const barSx = useMemo(() => loadingSkeletonBarSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1, mx: 1 }}>
            <Box sx={loadingSkeletonRowSx}>
                <Skeleton variant="circular" width={16} height={16} />
                <Skeleton variant="text" width={100} height={20} />
                <Skeleton variant="rounded" width={24} height={18} />
            </Box>
            <Skeleton
                variant="rounded"
                height={32}
                sx={barSx}
            />
            <Box sx={loadingSkeletonTimeSx}>
                {[1, 2, 3, 4, 5].map((i) => (
                    <Skeleton key={i} variant="text" width={40} height={14} />
                ))}
            </Box>
        </Box>
    );
};

/**
 * EmptyState - Shown when no events found
 */
export const EmptyState = () => {
    const theme = useTheme();
    const containerSx = useMemo(() => getEmptyStateSx(theme), [theme]);

    return (
        <Box sx={containerSx}>
            <TimelineIcon sx={{ fontSize: 24, color: 'text.disabled', mb: 0.5 }} />
            <Typography sx={emptyStateTitleSx}>
                No events in this time range
            </Typography>
            <Typography sx={emptyStateSubtitleSx}>
                Try expanding the time range or adjusting filters
            </Typography>
        </Box>
    );
};
