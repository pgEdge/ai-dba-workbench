/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Alert Analysis Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying AI-powered alert analysis with
 * professional analytics aesthetic and markdown rendering
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useEffect } from 'react';
import { Box, Typography, alpha, useTheme } from '@mui/material';
import type { Theme } from '@mui/material/styles';
import {
    Psychology as PsychologyIcon,
    Error as ErrorIcon,
    Warning as WarningIcon,
    Info as InfoIcon,
} from '@mui/icons-material';
import { useAlertAnalysis } from '../hooks/useAlertAnalysis';
import type { AlertInput } from '../hooks/useAlertAnalysis';
import { getIconColorSx, sxMonoFont } from './shared/MarkdownExports';
import {
    getServerBadgeSx,
    getDatabaseBadgeSx,
    getDatabaseTextSx,
    sxMonoSmall,
} from './analysisStyles';
import { BaseAnalysisDialog } from './shared/BaseAnalysisDialog';
import { downloadAsMarkdown } from '../utils/downloadMarkdown';

const TOOL_LABELS = [
    'Querying metrics',
    'Fetching metric baselines',
    'Reviewing alert history',
    'Checking alert rules',
    'Checking blackouts',
    'Validating query',
    'Searching knowledgebase',
];

// Severity color getter using theme palette
const getSeverityColor = (severity: string | undefined, theme: Theme) => {
    switch (severity) {
        case 'critical':
            return theme.palette.error.main;
        case 'warning':
            return theme.palette.warning.main;
        default:
            return theme.palette.info.main;
    }
};

// Get severity icon
const getSeverityIcon = (severity: string | undefined) => {
    switch (severity) {
        case 'critical':
            return ErrorIcon;
        case 'warning':
            return WarningIcon;
        default:
            return InfoIcon;
    }
};

// ---------------------------------------------------------------------------
// Alert-specific style constants and style-getter functions
// ---------------------------------------------------------------------------

const getSeverityDotSx = (severityColor: string, theme: Theme) => ({
    position: 'absolute',
    top: -4,
    right: -4,
    width: 14,
    height: 14,
    borderRadius: '50%',
    bgcolor: severityColor,
    border: '2px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    boxShadow: `0 0 8px ${alpha(severityColor, 0.5)}`,
});

const sxSeverityBadge = { display: 'flex', alignItems: 'center', gap: 0.5 };

const sxThresholdText = {
    fontSize: '0.875rem',
    color: 'text.disabled',
    ...sxMonoFont,
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * AlertAnalysisDialog - Dialog for displaying AI-powered alert analysis
 */
/**
 * The alert shape this dialog renders. It is a superset of the hook's
 * AlertInput: it also carries the presentational fields (server name,
 * database name, metric unit) shown in the header, and tolerates the
 * string ids that the alert list can produce.
 */
export interface AlertAnalysisAlert {
    id?: number | string;
    severity?: string;
    title?: string;
    description?: string;
    alertType?: string;
    metricName?: string;
    metricValue?: number | string | null;
    metricUnit?: string;
    operator?: string;
    thresholdValue?: number | string | null;
    connectionId?: number;
    databaseName?: string;
    server?: string;
    time?: string;
    triggeredAt?: string;
    aiAnalysis?: string | null;
    aiAnalysisMetricValue?: number | string | null;
}

export interface AlertAnalysisDialogProps {
    open: boolean;
    alert: AlertAnalysisAlert | null;
    onClose: () => void;
    onAnalysisComplete?: (alertId: number, analysis: string) => void;
}

/**
 * Map the dialog's alert onto the input the analysis hook expects,
 * supplying defaults for the fields the hook requires.
 */
const toAlertInput = (alert: AlertAnalysisAlert): AlertInput => ({
    id: typeof alert.id === 'string' ? Number(alert.id) : alert.id,
    aiAnalysis: alert.aiAnalysis,
    aiAnalysisMetricValue: typeof alert.aiAnalysisMetricValue === 'string'
        ? Number(alert.aiAnalysisMetricValue)
        : alert.aiAnalysisMetricValue,
    alertType: alert.alertType,
    severity: alert.severity ?? '',
    title: alert.title ?? '',
    description: alert.description,
    metricName: alert.metricName,
    metricValue: alert.metricValue,
    operator: alert.operator,
    thresholdValue: alert.thresholdValue,
    connectionId: alert.connectionId ?? 0,
    triggeredAt: alert.triggeredAt,
    time: alert.time,
});

const AlertAnalysisDialog: React.FC<AlertAnalysisDialogProps> = ({
    open,
    alert,
    onClose,
    onAnalysisComplete,
}) => {
    const theme = useTheme();
    const isDark = theme.palette.mode === 'dark';
    const {
        analysis,
        loading,
        error,
        progressMessage,
        activeTools,
        analyze,
        reset,
    } = useAlertAnalysis();

    // Trigger analysis when dialog opens with an alert
    useEffect(() => {
        if (open && alert) {
            void analyze(toAlertInput(alert));
        }
    }, [open, alert, analyze]);

    // Notify parent when analysis completes so the alert list updates
    useEffect(() => {
        if (analysis && alert?.id != null && onAnalysisComplete) {
            onAnalysisComplete(alert.id as number, analysis);
        }
    }, [analysis, alert, onAnalysisComplete]);

    // Download analysis as markdown file
    const handleDownload = () => {
        if (!analysis || !alert) { return; }

        const timestamp = new Date().toISOString().split('T')[0];
        const filename = `alert-analysis-${alert.id || 'unknown'}-${timestamp}.md`;

        // Build optional fields
        const serverLine = alert.server ? `- **Server:** ${alert.server}\n` : '';
        const databaseLine = alert.databaseName
            ? `- **Database:** ${alert.databaseName}\n`
            : '';
        const unit = alert.metricUnit || '';
        const metricDisplay = alert.metricValue !== undefined
            ? `${alert.metricValue}${unit ? ` ${unit}` : ''}`
            : 'N/A';
        const thresholdDisplay = alert.thresholdValue !== undefined
            ? `${alert.operator || '>'} ${alert.thresholdValue}${unit ? ` ${unit}` : ''}`
            : 'N/A';

        const content = `# Alert Analysis Report

## Alert Details

- **Title:** ${alert.title || 'N/A'}
- **Severity:** ${alert.severity || 'N/A'}
${serverLine}${databaseLine}- **Alert Type:** ${alert.alertType || 'threshold'}
- **Metric Value:** ${metricDisplay}
- **Threshold:** ${thresholdDisplay}
- **Triggered At:** ${alert.triggeredAt || alert.time || 'N/A'}

---

${analysis}

---

*Generated by pgEdge AI DBA Workbench on ${new Date().toISOString()}*
`;

        downloadAsMarkdown(content, filename);
    };

    const severityColor = getSeverityColor(
        alert?.severity,
        theme
    );
    const SeverityIcon = getSeverityIcon(alert?.severity);

    // Build icon with severity dot
    const iconElement = (
        <Box sx={{ position: 'relative' }}>
            <PsychologyIcon sx={getIconColorSx(theme)} />
            <Box sx={getSeverityDotSx(severityColor, theme)} />
        </Box>
    );

    // Build toolbar content
    const toolbarContent = (
        <>
            {/* Severity badge */}
            <Box sx={sxSeverityBadge}>
                <SeverityIcon sx={{ fontSize: 14, color: severityColor }} />
                <Typography
                    sx={{
                        color: severityColor,
                        fontWeight: 500,
                        textTransform: 'capitalize',
                    }}
                >
                    {alert?.severity || 'Unknown'}
                </Typography>
            </Box>

            {/* Alert title */}
            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                {alert?.title || 'Alert'}
            </Typography>

            {/* Server pill */}
            {alert?.server && (
                <Box sx={getServerBadgeSx(theme)}>
                    <Typography sx={sxMonoSmall}>
                        {alert.server}
                    </Typography>
                </Box>
            )}

            {/* Database pill */}
            {alert?.databaseName && (
                <Box sx={getDatabaseBadgeSx(theme)}>
                    <Typography sx={getDatabaseTextSx(theme)}>
                        {alert.databaseName}
                    </Typography>
                </Box>
            )}

            {/* Threshold text */}
            {alert?.metricValue !== undefined &&
                alert?.thresholdValue !== undefined && (
                <Typography sx={sxThresholdText}>
                    {typeof alert.metricValue === 'number'
                        ? alert.metricValue.toLocaleString(undefined, {
                            maximumFractionDigits: 2,
                        })
                        : alert.metricValue}
                    {alert.metricUnit && ` ${alert.metricUnit}`}
                    {' '}{alert.operator}{' '}
                    {typeof alert.thresholdValue === 'number'
                        ? alert.thresholdValue.toLocaleString(undefined, {
                            maximumFractionDigits: 2,
                        })
                        : alert.thresholdValue}
                    {alert.metricUnit && ` ${alert.metricUnit}`}
                </Typography>
            )}

            {/* Time text */}
            {alert?.time && (
                <Typography variant="body2" sx={{ color: 'text.disabled' }}>
                    {alert.time}
                </Typography>
            )}
        </>
    );

    return (
        <BaseAnalysisDialog
            open={open}
            onClose={onClose}
            title="Alert analysis"
            icon={iconElement}
            toolLabels={TOOL_LABELS}
            analysis={analysis}
            loading={loading}
            error={error}
            progressMessage={progressMessage}
            activeTools={activeTools}
            onDownload={handleDownload}
            onReset={reset}
            toolbarContent={toolbarContent}
            markdownContent={
                analysis
                    ? `# Alert Analysis: ${alert?.title || 'Alert'}\n\n${analysis}`
                    : undefined
            }
            markdownContentProps={{
                isDark,
                connectionId: alert?.connectionId,
                databaseName: alert?.databaseName,
                serverName: alert?.server,
            }}
        />
    );
};

export default AlertAnalysisDialog;
