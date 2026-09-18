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
import type { TimelineTimeRange } from '../../utils/timelineRange';

export interface EventTimelineProps {
    selection: Selection | null;
}

export interface EventCluster {
    events: TimelineEvent[];
    position: number;
    startPosition: number;
}

/*
 * One event as returned by GET /api/v1/timeline/events, whose wire
 * shape is the Go `database.TimelineEvent` struct. This is the single
 * definition of the payload; hooks/useTimelineEvents.ts re-exports it
 * rather than declaring its own.
 *
 * The server emits every field on every event, but they are declared
 * optional here because the renderers tolerate their absence and the
 * fixtures build partial events.
 */
export interface TimelineEvent {
    id?: number | string;
    event_type: string;
    connection_id?: number;
    title: string;
    summary?: string;
    severity?: string;
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

/*
 * Detail payload shapes.
 *
 * The timeline API attaches a free-form `details` object to each event,
 * whose contents depend on the event type. Each shape below describes
 * one of those variants; EventDetails narrows the raw payload to the
 * variant its event type implies before handing it to a renderer.
 *
 * They are declared as type aliases rather than interfaces so that they
 * keep an implicit index signature, which is what lets a raw
 * Record<string, unknown> payload be narrowed to one of them directly.
 */

/** A single changed configuration setting in the diff format. */
export type ConfigChangeEntry = {
    change_type?: string;
    name?: string;
    old_value?: string | number;
    new_value?: string | number;
};

/** A configuration setting in the older snapshot format. */
export type ConfigSettingEntry = {
    name?: string;
    value?: string | number;
};

export type ConfigChangeDetailsPayload = {
    changes?: ConfigChangeEntry[];
    change_count?: number;
    settings?: ConfigSettingEntry[];
    setting_count?: number;
};

/** A single extension change in the diff format. */
export type ExtensionChangeEntry = {
    change_type?: string;
    name?: string;
    version?: string;
    old_version?: string;
    database?: string;
};

/** An installed extension in the older snapshot format. */
export type ExtensionEntry = {
    name?: string;
    version?: string;
    database?: string;
};

export type ExtensionChangeDetailsPayload = {
    changes?: ExtensionChangeEntry[];
    change_count?: number;
    extensions?: ExtensionEntry[];
    extension_count?: number;
};

/** A pg_hba.conf rule, as carried by both the diff and snapshot formats. */
export type HbaRuleEntry = {
    type?: string;
    database?: string;
    user_name?: string;
    address?: string;
    auth_method?: string;
};

/** A pg_hba.conf change, carrying the previous rule for modifications. */
export type HbaChangeEntry = HbaRuleEntry & {
    change_type?: string;
    prev_type?: string;
    prev_database?: string;
    prev_user_name?: string;
    prev_address?: string;
    prev_auth_method?: string;
};

export type HbaChangeDetailsPayload = {
    changes?: HbaChangeEntry[];
    change_count?: number;
    rules?: HbaRuleEntry[];
    rule_count?: number;
};

/** A pg_ident.conf user mapping. */
export type IdentMappingEntry = {
    map_name?: string;
    sys_name?: string;
    pg_username?: string;
};

export type IdentChangeDetailsPayload = {
    mappings?: IdentMappingEntry[];
    mapping_count?: number;
};

export type AlertDetailsPayload = {
    database_name?: string;
    acknowledged_by?: string;
    false_positive?: boolean;
    message?: string;
    metric_value?: string | number;
    metric_unit?: string;
    threshold_value?: string | number;
    severity?: string;
    original_severity?: string;
};

export type RestartDetailsPayload = {
    previous_timeline?: string | number;
    old_timeline_id?: string | number;
    new_timeline?: string | number;
    new_timeline_id?: string | number;
};

export type BlackoutDetailsPayload = {
    scope?: string;
    reason?: string;
    created_by?: string;
    end_time?: string;
};

/*
 * Component props.
 */

export interface ExpandableListProps<T> {
    items?: T[];
    initialLimit: number;
    renderItem: (item: T, index: number, total: number) => React.ReactNode;
    emptyText?: string;
}

export interface ConfigChangeDetailsProps {
    details?: ConfigChangeDetailsPayload;
}

export interface ExtensionChangeDetailsProps {
    details?: ExtensionChangeDetailsPayload;
}

export interface HbaChangeDetailsProps {
    details?: HbaChangeDetailsPayload;
}

export interface IdentChangeDetailsProps {
    details?: IdentChangeDetailsPayload;
}

export interface AlertDetailsProps {
    details?: AlertDetailsPayload;
    config: ResolvedEventConfig;
}

export interface RestartDetailsProps {
    details?: RestartDetailsPayload;
}

export interface BlackoutDetailsProps {
    details?: BlackoutDetailsPayload;
    eventType: string;
}

export interface EventDetailsProps {
    event: TimelineEvent;
    config: ResolvedEventConfig;
}

export interface SingleEventCardProps {
    event: TimelineEvent;
    isCompact?: boolean;
}

export interface CollapsibleEventCardProps {
    event: TimelineEvent;
    defaultExpanded?: boolean;
}

export interface EventDetailPanelProps {
    events: TimelineEvent[] | null;
    onClose: () => void;
}

/** Invoked when a marker, and so a whole cluster of events, is clicked. */
export type EventClickHandler = (
    event: React.MouseEvent<HTMLElement>,
    cluster: EventCluster,
) => void;

export interface EventMarkerProps {
    cluster: EventCluster;
    showServer: boolean;
    onClick: EventClickHandler;
}

export interface TimelineCanvasProps {
    events: TimelineEvent[];
    timeRange: TimelineTimeRange;
    showServer: boolean;
    onEventClick: EventClickHandler;
}

export interface TimelineHeaderProps {
    expanded: boolean;
    onExpandToggle: () => void;
    eventCount: number;
    timeRange: TimelineTimeRange;
    onTimeRangeChange: (timeRange: TimelineTimeRange) => void;
    eventTypes: string[];
    onEventTypesChange: (eventTypes: string[]) => void;
}
