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
import { useState, useMemo, useCallback, useEffect, memo } from 'react';
import {
    Box,
    Collapse,
    useTheme,
} from '@mui/material';
import { useTimelineEvents } from '../../hooks/useTimelineEvents';
import { TIME_RANGE_STORAGE_KEY, getInitialTimeRange } from './config';
import { TimelineHeader, LoadingSkeleton, EmptyState } from './TimelineHeader';
import TimelineCanvas from './TimelineCanvas';
import EventDetailPanel from './EventDetailPanel';
import { getOuterContainerSx } from './styles';
import type { EventTimelineProps } from './types';

/**
 * EventTimeline - Main component for displaying server events on a timeline
 */
const EventTimeline: React.FC<EventTimelineProps> = ({ selection }) => {
    const theme = useTheme();

    // Internal state
    const [timeRange, setTimeRange] = useState(getInitialTimeRange);
    const [eventTypes, setEventTypes] = useState(['all']);
    const [expanded, setExpanded] = useState(true);
    const [selectedEvents, setSelectedEvents] = useState(null);

    // Persist time range preference to localStorage
    useEffect(() => {
        try {
            localStorage.setItem(TIME_RANGE_STORAGE_KEY, timeRange);
        } catch {
            // localStorage not available
        }
    }, [timeRange]);

    // Stable string key for serverIds to use in dependency arrays
    const serverIdsKey = selection?.type === 'cluster' ? selection.serverIds?.join(',') : undefined;

    // Close detail panel when selection changes
    const selectionId = selection && 'id' in selection ? selection.id : undefined;
    useEffect(() => {
        setSelectedEvents(null);
    }, [selection?.type, selectionId, serverIdsKey]);

    // Determine connection ID(s) based on selection - memoize to prevent unnecessary re-fetches
    const connectionId = selection?.type === 'server' ? selection.id : null;
    const connectionIds = useMemo(
        () => (selection?.type === 'cluster' ? selection.serverIds : null),
        // Only recreate when serverIds actually changes (by value, not reference)
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [selection?.type, serverIdsKey]
    );

    // Memoize the eventTypes array to prevent creating new references on each render
    // This is critical to avoid infinite re-fetch loops in useTimelineEvents
    const memoizedEventTypes = useMemo(
        () => (eventTypes.includes('all') ? ['all'] : eventTypes),
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [eventTypes.join(',')]
    );

    // Fetch events using the hook
    const { events, loading, totalCount } = useTimelineEvents({
        connectionId,
        connectionIds,
        timeRange,
        eventTypes: memoizedEventTypes,
        enabled: Boolean(selection),
    });

    // Handle event click - shows all events in cluster in the detail panel
    const handleEventClick = useCallback((e, cluster) => {
        setSelectedEvents(cluster.events);
    }, []);

    // Handle panel close
    const handlePanelClose = useCallback(() => {
        setSelectedEvents(null);
    }, []);

    // Show server name when not viewing a single server
    const showServer = selection?.type !== 'server';

    const outerSx = useMemo(() => getOuterContainerSx(theme), [theme]);

    if (!selection) {
        return null;
    }

    return (
        <Box sx={outerSx}>
            <TimelineHeader
                expanded={expanded}
                onExpandToggle={() => setExpanded(!expanded)}
                eventCount={totalCount}
                timeRange={timeRange}
                onTimeRangeChange={setTimeRange}
                eventTypes={eventTypes}
                onEventTypesChange={setEventTypes}
            />

            <Collapse in={expanded}>
                {loading ? (
                    <LoadingSkeleton />
                ) : events.length === 0 ? (
                    <EmptyState />
                ) : (
                    <>
                        <TimelineCanvas
                            events={events}
                            timeRange={timeRange}
                            showServer={showServer}
                            onEventClick={handleEventClick}
                        />
                        <EventDetailPanel
                            events={selectedEvents}
                            onClose={handlePanelClose}
                        />
                    </>
                )}
            </Collapse>
        </Box>
    );
};

export default memo(EventTimeline);
