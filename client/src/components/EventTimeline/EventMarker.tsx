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
    Tooltip,
    alpha,
    useTheme,
} from '@mui/material';
import {
    EventNote as EventNoteIcon,
} from '@mui/icons-material';
import { getEventConfig, formatEventTime } from './utils';
import {
    tooltipPaddingSx,
    tooltipClusterTitleSx,
    tooltipClusterItemSx,
    tooltipClusterMoreSx,
    tooltipSingleTitleSx,
    tooltipSingleTimeSx,
    tooltipSingleServerSx,
    getClusterBadgeSx,
} from './styles';

/**
 * EventMarker - Single event or cluster marker on the timeline
 */
const EventMarker = memo(({ cluster, showServer, onClick }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const isCluster = cluster.events.length > 1;
    const primaryEvent = cluster.events[0];
    const config = getEventConfig(primaryEvent, theme.palette);
    const EventIcon = config.icon;

    // For clusters, determine if there are mixed types or severities
    const hasMixedTypes = isCluster && new Set(cluster.events.map(e => e.event_type)).size > 1;
    const hasCritical = cluster.events.some(
        e => e.event_type === 'alert_fired' && e.details?.severity === 'critical'
    );

    const markerColor = hasCritical ? theme.palette.error.main : config.color;

    const markerOuterSx = useMemo(() => ({
        position: 'absolute',
        left: `${cluster.position}%`,
        top: '50%',
        transform: 'translate(-50%, -50%)',
        cursor: 'pointer',
        zIndex: 10,
        '&:hover': {
            zIndex: 20,
            '& > div': {
                transform: 'scale(1.2)',
            },
        },
    }), [cluster.position]);

    const markerInnerSx = useMemo(() => ({
        position: 'relative',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: isCluster ? 28 : 24,
        height: isCluster ? 28 : 24,
        borderRadius: '50%',
        bgcolor: alpha(markerColor, isDark ? 0.2 : 0.15),
        border: '2px solid',
        borderColor: markerColor,
        transition: 'transform 0.15s ease',
    }), [isCluster, markerColor, isDark]);

    const iconColorSx = useMemo(() => ({ fontSize: 14, color: markerColor }), [markerColor]);

    const badgeSx = useMemo(() => getClusterBadgeSx(theme), [theme]);

    return (
        <Tooltip
            title={
                <Box sx={tooltipPaddingSx}>
                    {isCluster ? (
                        <>
                            <Typography sx={tooltipClusterTitleSx}>
                                {cluster.events.length} events
                            </Typography>
                            {cluster.events.slice(0, 3).map((e, i) => (
                                <Typography key={i} sx={tooltipClusterItemSx}>
                                    {e.title}
                                </Typography>
                            ))}
                            {cluster.events.length > 3 && (
                                <Typography sx={tooltipClusterMoreSx}>
                                    +{cluster.events.length - 3} more
                                </Typography>
                            )}
                        </>
                    ) : (
                        <>
                            <Typography sx={tooltipSingleTitleSx}>
                                {primaryEvent.title}
                            </Typography>
                            <Typography sx={tooltipSingleTimeSx}>
                                {formatEventTime(primaryEvent.occurred_at)}
                            </Typography>
                            {showServer && primaryEvent.server_name && (
                                <Typography sx={tooltipSingleServerSx}>
                                    {primaryEvent.server_name}
                                    {primaryEvent.details?.database_name && ` / ${primaryEvent.details.database_name}`}
                                </Typography>
                            )}
                        </>
                    )}
                </Box>
            }
            placement="top"
            arrow
        >
            <Box
                onClick={(e) => onClick(e, cluster)}
                sx={markerOuterSx}
            >
                <Box sx={markerInnerSx}>
                    {hasMixedTypes ? (
                        <EventNoteIcon sx={iconColorSx} />
                    ) : (
                        <EventIcon sx={iconColorSx} />
                    )}
                    {isCluster && (
                        <Box sx={badgeSx}>
                            {cluster.events.length > 99 ? '99+' : cluster.events.length}
                        </Box>
                    )}
                </Box>
            </Box>
        </Tooltip>
    );
});

EventMarker.displayName = 'EventMarker';

export default EventMarker;
