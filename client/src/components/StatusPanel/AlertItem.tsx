/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import {
    Box,
    Typography,
    Chip,
    IconButton,
    Tooltip,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Warning as WarningIcon,
    Error as ErrorIcon,

    CheckCircleOutline as AckIcon,
    Undo as UnackIcon,
    Psychology as AnalyzeIcon,
    TableChart as TableIcon,
    TuneRounded,
} from '@mui/icons-material';
import {
    getSeverityColors,
    getFriendlyTitle,
    formatThresholdInfo,
    CHIP_LABEL_SX,
    FLEX_1_MIN0_SX,
    ICON_16_SX,

    ALERT_TITLE_BASE_SX,
    ALERT_THRESHOLD_SX,
    ALERT_DESCRIPTION_SX,
    ALERT_ACK_TEXT_SX,
    ALERT_TIME_SX,
    SEVERITY_CHIP_BASE_SX,
} from './styles';

/**
 * AlertItem - Compact alert entry with severity indicator and ack functionality
 */
const AlertItem = ({ alert, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze, onEditOverride }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const isAcknowledged = !!alert.acknowledgedAt;
    const baseColor = isAcknowledged ? theme.palette.grey[500] : (severityColors[alert.severity] || severityColors.info);
    const SeverityIcon = alert.severity === 'critical' ? ErrorIcon : WarningIcon;
    const thresholdInfo = formatThresholdInfo(alert);
    const friendlyTitle = getFriendlyTitle(alert.title);

    const containerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        borderRadius: 1,
        bgcolor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.12)
            : alpha(baseColor, 0.08),
        border: '1px solid',
        borderColor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.22)
            : alpha(baseColor, 0.18),
    }), [isAcknowledged, baseColor, theme]);

    const severityIconSx = useMemo(() => ({
        fontSize: 16,
        color: baseColor,
        flexShrink: 0,
    }), [baseColor]);

    const dbChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.secondary.main, 0.15),
        color: theme.palette.secondary.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.secondary]);

    const objectChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.custom.status.online, 0.15),
        color: theme.palette.custom.status.connected,
        '& .MuiChip-label': CHIP_LABEL_SX,
        '& .MuiChip-icon': {
            color: 'inherit',
            ml: 0.25,
            mr: -0.25,
        },
    }), [theme.palette.custom.status]);

    const falsePositiveChipSx = useMemo(() => ({
        height: 14,
        fontSize: '0.5rem',
        fontWeight: 600,
        bgcolor: alpha(theme.palette.error.main, 0.12),
        color: theme.palette.error.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.error]);

    const severityChipSx = useMemo(() => ({
        ...SEVERITY_CHIP_BASE_SX,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [baseColor]);

    const analyzeButtonSx = useMemo(() => ({
        p: 0.5,
        color: theme.palette.secondary.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.secondary.main, 0.12),
        },
    }), [theme.palette.secondary]);

    const overrideButtonSx = useMemo(() => ({
        p: 0.5,
        color: theme.palette.info.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.info.main, 0.12),
        },
    }), [theme.palette.info]);

    const ackButtonSx = useMemo(() => ({
        p: 0.5,
        color: isAcknowledged ? theme.palette.grey[500] : baseColor,
        '&:hover': {
            bgcolor: alpha(baseColor, 0.1),
        },
    }), [isAcknowledged, baseColor, theme.palette.grey]);

    const serverChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    return (
        <Box sx={containerSx}>
            {/* Severity indicator */}
            <SeverityIcon sx={severityIconSx} />

            {/* Main content */}
            <Box sx={FLEX_1_MIN0_SX}>
                {/* Title row */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                    <Typography
                        sx={{
                            ...ALERT_TITLE_BASE_SX,
                            color: isAcknowledged ? 'text.secondary' : 'text.primary',
                        }}
                    >
                        {friendlyTitle}
                    </Typography>
                    {showServer && alert.server && (
                        <Chip label={alert.server} size="small" sx={serverChipSx} />
                    )}
                    {alert.databaseName && (
                        <Chip label={alert.databaseName} size="small" sx={dbChipSx} />
                    )}
                    {alert.objectName && (
                        <Chip
                            icon={<TableIcon sx={{ fontSize: '0.875rem !important' }} />}
                            label={alert.objectName}
                            size="small"
                            sx={objectChipSx}
                        />
                    )}
                </Box>

                {/* Threshold info or description */}
                {thresholdInfo ? (
                    <Typography sx={ALERT_THRESHOLD_SX}>
                        {thresholdInfo}
                    </Typography>
                ) : alert.description && (
                    <Typography sx={ALERT_DESCRIPTION_SX}>
                        {alert.description}
                    </Typography>
                )}

                {/* Ack info if acknowledged */}
                {isAcknowledged && (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.25 }}>
                        <Typography sx={ALERT_ACK_TEXT_SX}>
                            Acked by {alert.acknowledgedBy}{alert.ackMessage ? `: ${alert.ackMessage}` : ''}
                        </Typography>
                        {alert.falsePositive && (
                            <Chip label="False Positive" size="small" sx={falsePositiveChipSx} />
                        )}
                    </Box>
                )}
            </Box>

            {/* Time and severity */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexShrink: 0 }}>
                <Typography sx={ALERT_TIME_SX}>
                    {alert.time}
                </Typography>
                <Chip label={alert.severity} size="small" sx={severityChipSx} />
            </Box>

            {/* Analyze button */}
            {onAnalyze && (
                <Tooltip title={alert.aiAnalysis ? "View cached analysis" : "Analyze with AI"} placement="left">
                    <IconButton
                        size="small"
                        onClick={(e) => {
                            e.stopPropagation();
                            onAnalyze(alert);
                        }}
                        sx={analyzeButtonSx}
                    >
                        <AnalyzeIcon sx={{ ...ICON_16_SX, ...(alert.aiAnalysis && { color: 'success.main' }) }} />
                    </IconButton>
                </Tooltip>
            )}

            {/* Edit override button */}
            {alert.ruleId && (
                <Tooltip title="Edit alert override" placement="left">
                    <IconButton
                        size="small"
                        onClick={(e) => {
                            e.stopPropagation();
                            onEditOverride?.(alert);
                        }}
                        sx={overrideButtonSx}
                    >
                        <TuneRounded sx={ICON_16_SX} />
                    </IconButton>
                </Tooltip>
            )}

            {/* Ack/Unack button */}
            <Tooltip title={isAcknowledged ? 'Restore to active' : 'Acknowledge'} placement="left">
                <IconButton
                    size="small"
                    onClick={(e) => {
                        e.stopPropagation();
                        if (isAcknowledged) {
                            onUnacknowledge?.(alert.id);
                        } else {
                            onAcknowledge?.(alert);
                        }
                    }}
                    sx={ackButtonSx}
                >
                    {isAcknowledged ? (
                        <UnackIcon sx={ICON_16_SX} />
                    ) : (
                        <AckIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Tooltip>
        </Box>
    );
};

export default AlertItem;
