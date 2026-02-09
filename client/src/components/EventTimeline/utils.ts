/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { EVENT_TYPE_CONFIG } from './config';

/**
 * Resolve a dotted path like 'primary.main' from the theme palette
 */
export const resolveColor = (palette, colorKey: string): string => {
    const parts = colorKey.split('.');
    let value = palette;
    for (const part of parts) {
        value = value?.[part];
    }
    return typeof value === 'string' ? value : palette?.primary?.main ?? '#1976d2';
};

/**
 * Get event configuration with potential severity override, resolved against theme
 */
export const getEventConfig = (event, palette) => {
    const config = EVENT_TYPE_CONFIG[event.event_type] || EVENT_TYPE_CONFIG.config_change;

    let colorKey = config.colorKey;
    let icon = config.icon;

    // Handle severity-based color and icon for alerts
    if (event.event_type === 'alert_fired' && config.getSeverityColorKey) {
        const severity = event.details?.severity;
        colorKey = config.getSeverityColorKey(severity);
        icon = config.getSeverityIcon(severity);
    }

    return {
        ...config,
        icon,
        colorKey,
        color: palette ? resolveColor(palette, colorKey) : colorKey,
    };
};

/**
 * Format timestamp for display
 */
export const formatEventTime = (timestamp) => {
    if (!timestamp) {return '';}
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now - date;
    const diffMins = Math.floor(diffMs / (1000 * 60));
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) {return 'just now';}
    if (diffMins < 60) {return `${diffMins}m ago`;}
    if (diffHours < 24) {return `${diffHours}h ago`;}
    if (diffDays < 7) {return `${diffDays}d ago`;}

    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
};

/**
 * Format full timestamp for detail view
 */
export const formatFullTime = (timestamp) => {
    if (!timestamp) {return '';}
    const date = new Date(timestamp);
    return date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
};

/**
 * Calculate position of event on timeline as percentage
 */
export const calculatePosition = (eventTime, startTime, endTime) => {
    const eventTs = new Date(eventTime).getTime();
    const startTs = startTime.getTime();
    const endTs = endTime.getTime();
    const range = endTs - startTs;
    if (range <= 0) {return 0;}
    const position = ((eventTs - startTs) / range) * 100;
    return Math.max(0, Math.min(100, position));
};

/**
 * Get time range boundaries
 */
export const getTimeRangeBounds = (timeRange) => {
    const now = new Date();
    let startTime;

    switch (timeRange) {
        case '1h':
            startTime = new Date(now.getTime() - 60 * 60 * 1000);
            break;
        case '6h':
            startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000);
            break;
        case '24h':
            startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
            break;
        case '7d':
            startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            break;
        case '30d':
            startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
            break;
        default:
            startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
    }

    return { startTime, endTime: now };
};

/**
 * Cluster nearby events
 */
export const clusterEvents = (events, startTime, endTime, minDistancePercent = 2) => {
    if (!events || events.length === 0) {return [];}

    // Sort by timestamp
    const sorted = [...events].sort(
        (a, b) => new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime()
    );

    const clusters = [];
    let currentCluster = null;

    sorted.forEach((event) => {
        const position = calculatePosition(event.occurred_at, startTime, endTime);

        if (!currentCluster) {
            currentCluster = {
                events: [event],
                position,
                startPosition: position,
            };
        } else if (position - currentCluster.position < minDistancePercent) {
            // Add to current cluster
            currentCluster.events.push(event);
            // Update position to average
            currentCluster.position =
                (currentCluster.startPosition + position) / 2;
        } else {
            // Start new cluster
            clusters.push(currentCluster);
            currentCluster = {
                events: [event],
                position,
                startPosition: position,
            };
        }
    });

    if (currentCluster) {
        clusters.push(currentCluster);
    }

    return clusters;
};

/**
 * Generate time axis markers
 */
export const generateTimeMarkers = (startTime, endTime, count = 5) => {
    const markers = [];
    const range = endTime.getTime() - startTime.getTime();
    const step = range / (count - 1);

    for (let i = 0; i < count; i++) {
        const time = new Date(startTime.getTime() + step * i);
        const position = (i / (count - 1)) * 100;

        let label;
        if (range <= 60 * 60 * 1000) {
            // 1 hour or less - show time
            label = time.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
        } else if (range <= 24 * 60 * 60 * 1000) {
            // 24 hours or less - show time
            label = time.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
        } else {
            // More than 24 hours - show date and time
            label = time.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
        }

        markers.push({ position, label, time });
    }

    return markers;
};
