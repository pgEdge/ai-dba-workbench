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
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
}

export interface GroupedAlertInstanceProps {
    alert: TransformedAlert;
    showServer: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
    onAnalyze?: (alert: TransformedAlert) => void;
    onEditOverride?: (alert: TransformedAlert) => void;
}

export interface GroupedAlertItemProps {
    title: string;
    alerts: TransformedAlert[];
    showServer?: boolean;
    onAcknowledge?: (alert: TransformedAlert) => void;
    onUnacknowledge?: (alertId: number | string) => void;
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
