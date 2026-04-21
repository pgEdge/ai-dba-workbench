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

export interface StatusPanelProps {
    selection: Record<string, unknown> | null;
}

export interface TransformedAlert {
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
}

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

export interface SelectionHeaderProps {
    selection: Record<string, unknown>;
    alertCount?: number;
    alertSeverities?: Record<string, number>;
    onBlackoutClick: () => void;
}
