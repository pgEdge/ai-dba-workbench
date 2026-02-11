/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo, memo } from 'react';
import {
    Box,
    Typography,
    IconButton,
    Collapse,
    alpha,
    useTheme,
} from '@mui/material';
import {
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Close as CloseIcon,
} from '@mui/icons-material';
import { getEventConfig, formatFullTime } from './utils';
import { EVENT_TYPE_CONFIG } from './config';
import { EventDetails } from './EventDetailComponents';
import {
    getCollapsibleCardSx,
    getCollapsibleHeaderHoverSx,
    collapsibleTitleSx,
    collapsibleTimeSx,
    serverDisabledSx,
    collapseToggleSx,
    collapseContentSx,
    summarySx,
    getDetailPanelSx,
    getDetailPanelHeaderSx,
    detailPanelTitleSx,
    detailPanelSubtitleSx,
    getCloseButtonSx,
    closeIconSx,
    clusterListSx,
} from './styles';

/**
 * Single Event Card in the detail popover
 */
const SingleEventCard = memo(({ event, isCompact = false }) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const config = getEventConfig(event, theme.palette);
    const EventIcon = config.icon;

    const iconBoxSx = useMemo(() => ({
        width: isCompact ? 24 : 32,
        height: isCompact ? 24 : 32,
        borderRadius: 1,
        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
    }), [isCompact, config.color, isDark]);

    const iconSx = useMemo(
        () => ({ fontSize: isCompact ? 14 : 18, color: config.color }),
        [isCompact, config.color]
    );

    return (
        <Box sx={{ mb: isCompact ? 1.5 : 0 }}>
            {/* Header */}
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
                <Box sx={iconBoxSx}>
                    <EventIcon sx={iconSx} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography
                        sx={{
                            fontWeight: 600,
                            fontSize: isCompact ? '1rem' : '0.875rem',
                            color: 'text.primary',
                            lineHeight: 1.3,
                        }}
                    >
                        {event.title}
                    </Typography>
                    <Typography
                        sx={{
                            fontSize: '0.875rem',
                            color: 'text.secondary',
                            mt: 0.25,
                        }}
                    >
                        {formatFullTime(event.occurred_at)}
                        {event.server_name && (
                            <Typography
                                component="span"
                                sx={serverDisabledSx}
                            >
                                {' \u00b7 '}{event.server_name}
                                {event.details?.database_name && ` / ${event.details.database_name}`}
                            </Typography>
                        )}
                    </Typography>
                </Box>
            </Box>

            {/* Summary */}
            {event.summary && (
                <Typography
                    sx={{
                        mt: 0.75,
                        fontSize: '0.875rem',
                        color: 'text.secondary',
                        lineHeight: 1.4,
                        pl: isCompact ? 4 : 5.5,
                    }}
                >
                    {event.summary}
                </Typography>
            )}

            {/* Type-specific details */}
            <Box sx={{ pl: isCompact ? 4 : 5.5 }}>
                <EventDetails event={event} config={config} />
            </Box>
        </Box>
    );
});

SingleEventCard.displayName = 'SingleEventCard';

/**
 * CollapsibleEventCard - A single event card that can be expanded/collapsed
 */
const CollapsibleEventCard = memo(({ event, defaultExpanded = true }) => {
    const [expanded, setExpanded] = useState(defaultExpanded);
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const config = getEventConfig(event, theme.palette);
    const EventIcon = config.icon;

    const cardSx = useMemo(() => getCollapsibleCardSx(theme), [theme]);
    const headerSx = useMemo(() => getCollapsibleHeaderHoverSx(theme), [theme]);

    const iconBoxSx = useMemo(() => ({
        width: 28,
        height: 28,
        borderRadius: 1,
        bgcolor: alpha(config.color, isDark ? 0.15 : 0.1),
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
    }), [config.color, isDark]);

    return (
        <Box sx={cardSx}>
            {/* Collapsible header */}
            <Box
                onClick={() => setExpanded(!expanded)}
                sx={headerSx}
            >
                <Box sx={iconBoxSx}>
                    <EventIcon sx={{ fontSize: 16, color: config.color }} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography sx={collapsibleTitleSx}>
                        {event.title}
                    </Typography>
                    <Typography sx={collapsibleTimeSx}>
                        {formatFullTime(event.occurred_at)}
                        {event.server_name && (
                            <Typography
                                component="span"
                                sx={serverDisabledSx}
                            >
                                {' \u00b7 '}{event.server_name}
                                {event.details?.database_name && ` / ${event.details.database_name}`}
                            </Typography>
                        )}
                    </Typography>
                </Box>
                <IconButton
                    size="small"
                    sx={collapseToggleSx}
                >
                    {expanded ? (
                        <ExpandLessIcon sx={{ fontSize: 18 }} />
                    ) : (
                        <ExpandMoreIcon sx={{ fontSize: 18 }} />
                    )}
                </IconButton>
            </Box>

            {/* Collapsible content */}
            <Collapse in={expanded}>
                <Box sx={collapseContentSx}>
                    {/* Summary */}
                    {event.summary && (
                        <Typography sx={summarySx}>
                            {event.summary}
                        </Typography>
                    )}

                    {/* Type-specific details */}
                    <EventDetails event={event} config={config} />
                </Box>
            </Collapse>
        </Box>
    );
});

CollapsibleEventCard.displayName = 'CollapsibleEventCard';

/**
 * EventDetailPanel - Shows detailed information about an event or cluster in an inline panel
 */
const EventDetailPanel = memo(({ events, onClose }) => {
    const theme = useTheme();

    const isCluster = events && events.length > 1;

    // Count events by type for cluster header
    const typeCounts = useMemo(() => {
        if (!isCluster || !events) {return null;}
        const counts = {};
        events.forEach(e => {
            const typeConfig = EVENT_TYPE_CONFIG[e.event_type];
            const label = typeConfig?.label || e.event_type;
            counts[label] = (counts[label] || 0) + 1;
        });
        return Object.entries(counts).map(([label, count]) => `${count} ${label}`).join(', ');
    }, [events, isCluster]);

    const panelSx = useMemo(() => getDetailPanelSx(theme), [theme]);
    const panelHeaderSx = useMemo(() => getDetailPanelHeaderSx(theme), [theme]);
    const closeBtnSx = useMemo(() => getCloseButtonSx(theme), [theme]);

    if (!events || events.length === 0) {return null;}

    const primaryEvent = events[0];

    return (
        <Box sx={panelSx}>
            {/* Panel header with close button */}
            <Box sx={panelHeaderSx}>
                <Box>
                    <Typography sx={detailPanelTitleSx}>
                        {isCluster ? `${events.length} Events` : 'Event Details'}
                    </Typography>
                    {isCluster && (
                        <Typography sx={detailPanelSubtitleSx}>
                            {typeCounts}
                        </Typography>
                    )}
                </Box>
                <IconButton
                    size="small"
                    onClick={onClose}
                    sx={closeBtnSx}
                >
                    <CloseIcon sx={closeIconSx} />
                </IconButton>
            </Box>

            {/* Event content - stacked vertically */}
            {isCluster ? (
                <Box sx={clusterListSx}>
                    {events.map((event, index) => (
                        <CollapsibleEventCard
                            key={event.id || index}
                            event={event}
                            defaultExpanded={index === 0}
                        />
                    ))}
                </Box>
            ) : (
                <SingleEventCard event={primaryEvent} isCompact={false} />
            )}
        </Box>
    );
});

EventDetailPanel.displayName = 'EventDetailPanel';

export default EventDetailPanel;
