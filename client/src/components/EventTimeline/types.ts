/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared types for EventTimeline sub-components
 */

import type { Selection } from '../../types/selection';

export interface EventTimelineProps {
    selection: Selection | null;
}

export interface EventCluster {
    events: TimelineEvent[];
    position: number;
    startPosition: number;
}

export interface TimelineEvent {
    id?: number | string;
    event_type: string;
    title: string;
    summary?: string;
    occurred_at: string;
    server_name?: string;
    details?: Record<string, unknown>;
}

export interface TimeMarker {
    position: number;
    label: string;
    time: Date;
}

export interface EventTypeConfigEntry {
    icon: React.ElementType;
    colorKey: string;
    label: string;
    getSeverityColorKey?: (severity: string) => string;
    getSeverityIcon?: (severity: string) => React.ElementType;
}

export interface FilterChipEntry {
    label: string;
    colorKey: string;
    types: string[];
}

export interface ResolvedEventConfig extends EventTypeConfigEntry {
    color: string;
}
