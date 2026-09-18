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
 * Shared types for StatusPanel sub-components
 */

import type React from 'react';
import type { Selection } from '../../types/selection';

export interface StatusPanelProps {
    selection: Selection | null;
}

/**
 * Raw alert record as returned by `GET /api/v1/alerts`, before it is
 * mapped into the camelCase `TransformedAlert` shape the display
 * components consume.
 */
export interface ApiAlert {
    id: number;
    severity?: string;
    title: string;
    description?: string;
    triggered_at?: string;
    last_updated?: string;
    server_name?: string;
    connection_id?: number;
    database_name?: string;
    object_name?: string;
    alert_type?: string;
    rule_id?: number;
    metric_value?: number | string;
    metric_unit?: string;
    threshold_value?: number | string;
    operator?: string;
    acknowledged_at?: string;
    acknowledged_by?: string;
    ack_message?: string;
    false_positive?: boolean;
    ai_analysis?: string;
    ai_analysis_metric_value?: number | string;
}

// Declared as a type alias rather than an interface so that it carries
// an implicit index signature and can be passed to components (such as
// AlertAnalysisDialog) that accept a `Record<string, unknown>` alert.
export type TransformedAlert = {
    id: number | string;
    severity: string;
    title: string;
    description?: string;
    time: string;
    // Raw ISO timestamp for the alert's original trigger time.
    triggeredAt?: string;
    // Raw ISO timestamp for the alert's most recent re-evaluation /
    // reactivation. When present and different from triggeredAt, it is
    // surfaced in the UI so users can see that a reactivated alert is
    // not stale.
    lastUpdated?: string;
    // Pre-formatted relative time for lastUpdated (mirrors `time`).
    lastUpdatedTime?: string;
    server?: string;
    connectionId?: number;
    databaseName?: string;
    objectName?: string;
    alertType?: string;
    ruleId?: number;
    metricValue?: number | string;
    metricUnit?: string;
    thresholdValue?: number | string;
    operator?: string;
    acknowledgedAt?: string;
    acknowledgedBy?: string;
    ackMessage?: string;
    falsePositive?: boolean;
    aiAnalysis?: string;
    aiAnalysisMetricValue?: number | string;
};

export interface AlertItemProps {
    alert: TransformedAlert;
    showServer?: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
    // Optional predicate that returns true while an unacknowledge
    // request for the given alert id is in flight. When true, the
    // ack/unack button renders disabled so the user cannot trigger
    // duplicate requests.
    isUnacknowledging?: (alertId: number | string) => boolean;
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
}

export interface GroupedAlertInstanceProps {
    alert: TransformedAlert;
    showServer: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
    isUnacknowledging?: (alertId: number | string) => boolean;
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
}

export interface GroupedAlertItemProps {
    title: string;
    alerts: TransformedAlert[];
    showServer?: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
    isUnacknowledging?: (alertId: number | string) => boolean;
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
    onAcknowledgeGroup?: (alerts: TransformedAlert[]) => void;
}

export interface AcknowledgeDialogProps {
    open: boolean;
    alert: TransformedAlert | null;
    alerts?: TransformedAlert[];
    onClose: () => void;
    onConfirm: (alertId: number | string, message: string, falsePositive: boolean) => void;
    onConfirmMultiple?: (alertIds: (number | string)[], message: string, falsePositive: boolean) => void;
}

export interface AlertsSectionProps {
    alerts: TransformedAlert[];
    loading: boolean;
    showServer?: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
    isUnacknowledging?: (alertId: number | string) => boolean;
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
    onAcknowledgeGroup?: (alerts: TransformedAlert[]) => void;
}

export interface MetricCardProps {
    label: string;
    value: React.ReactNode;
    trend?: 'up' | 'down';
    trendValue?: React.ReactNode;
    icon?: React.ElementType;
    color?: string;
}

export interface SelectionHeaderProps {
    selection: Selection;
    alertCount?: number;
    alertSeverities?: Record<string, number>;
    onBlackoutClick: () => void;
}
