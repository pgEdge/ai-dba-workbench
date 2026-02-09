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
    useTheme,
} from '@mui/material';
import { getTimeRangeBounds, generateTimeMarkers, clusterEvents } from './utils';
import EventMarker from './EventMarker';
import {
    timelineCanvasContainerSx,
    timeAxisSx,
    getTickMarkSx,
    tickLabelSx,
    getTimelineTrackSx,
} from './styles';

/**
 * TimelineCanvas - The actual timeline visualization
 */
const TimelineCanvas = memo(({ events, timeRange, showServer, onEventClick }) => {
    const theme = useTheme();
    const { startTime, endTime } = useMemo(() => getTimeRangeBounds(timeRange), [timeRange]);
    const timeMarkers = useMemo(() => generateTimeMarkers(startTime, endTime), [startTime, endTime]);
    const eventClusters = useMemo(
        () => clusterEvents(events, startTime, endTime),
        [events, startTime, endTime]
    );

    const tickSx = useMemo(() => getTickMarkSx(theme), [theme]);
    const trackSx = useMemo(() => getTimelineTrackSx(theme), [theme]);

    return (
        <Box sx={timelineCanvasContainerSx}>
            {/* Time axis */}
            <Box sx={timeAxisSx}>
                {timeMarkers.map((marker, i) => (
                    <Box
                        key={i}
                        sx={{
                            position: 'absolute',
                            left: `${marker.position}%`,
                            transform: 'translateX(-50%)',
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                        }}
                    >
                        <Box sx={tickSx} />
                        <Typography sx={tickLabelSx}>
                            {marker.label}
                        </Typography>
                    </Box>
                ))}
            </Box>

            {/* Timeline track */}
            <Box sx={trackSx}>
                {/* Event markers */}
                {eventClusters.map((cluster, i) => (
                    <EventMarker
                        key={i}
                        cluster={cluster}
                        showServer={showServer}
                        onClick={onEventClick}
                    />
                ))}
            </Box>
        </Box>
    );
});

TimelineCanvas.displayName = 'TimelineCanvas';

export default TimelineCanvas;
