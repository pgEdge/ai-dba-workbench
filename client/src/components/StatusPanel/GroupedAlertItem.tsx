/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo } from 'react';
import {
    Box,
    Typography,
    Chip,
    IconButton,
    Tooltip,
    Collapse,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Warning as WarningIcon,
    Error as ErrorIcon,

    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
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
    CHIP_LABEL_075_SX,
    EXPAND_BUTTON_SX,
    ICON_16_SX,
    ICON_14_SX,

    SEVERITY_CHIP_BASE_SX,
    ALERT_TYPE_CHIP_BASE_SX,
    getAlertTypeColor,
    INSTANCE_TIME_SX,
    INSTANCE_THRESHOLD_SX,
    ALERT_ACK_TEXT_SX,
    GROUP_TITLE_SX,
    GROUP_INSTANCES_LIST_SX,
} from './styles';

/**
 * GroupedAlertInstance - A single instance row within a grouped alert panel
 */
const GroupedAlertInstance = ({ alert, showServer, onAcknowledge, onUnacknowledge, onAnalyze, onEditOverride }) => {
    const theme = useTheme();
    const isAcknowledged = !!alert.acknowledgedAt;
    const thresholdInfo = formatThresholdInfo(alert);

    const containerSx = useMemo(() => ({
        display: 'flex',
        flexDirection: 'column',
        gap: 0.25,
        px: 1,
        py: 0.5,
        borderRadius: 0.5,
        bgcolor: isAcknowledged
            ? alpha(theme.palette.grey[500], 0.10)
            : 'transparent',
        '&:hover': {
            bgcolor: alpha(theme.palette.grey[500], 0.12),
        },
    }), [isAcknowledged, theme]);

    const serverChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

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

    const alertTypeLabel = alert.alertType === 'anomaly' ? 'Anomaly' : 'Threshold';
    const alertTypeColor = getAlertTypeColor(theme, alert.alertType || 'threshold');

    const alertTypeChipSx = useMemo(() => ({
        ...ALERT_TYPE_CHIP_BASE_SX,
        bgcolor: alpha(alertTypeColor, 0.15),
        color: alertTypeColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [alertTypeColor]);

    const analyzeButtonSx = useMemo(() => ({
        p: 0.25,
        color: theme.palette.secondary.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.secondary.main, 0.12),
        },
    }), [theme.palette.secondary]);

    const overrideButtonSx = useMemo(() => ({
        p: 0.25,
        color: theme.palette.info.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.info.main, 0.12),
        },
    }), [theme.palette.info]);

    const ackButtonSx = useMemo(() => ({
        p: 0.25,
        color: isAcknowledged ? theme.palette.grey[500] : theme.palette.success.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.success.main, 0.1),
        },
    }), [isAcknowledged, theme.palette.grey, theme.palette.success]);

    const falsePositiveChipSx = useMemo(() => ({
        height: 14,
        fontSize: '0.5rem',
        fontWeight: 600,
        bgcolor: alpha(theme.palette.error.main, 0.12),
        color: theme.palette.error.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.error]);

    return (
        <Box sx={containerSx}>
            {/* Existing row with chips, time, buttons */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, width: '100%' }}>
                {/* Context chips */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap', flex: 1, minWidth: 0 }}>
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
                    <Chip label={alertTypeLabel} size="small" sx={alertTypeChipSx} />
                    {thresholdInfo ? (
                        <Typography sx={INSTANCE_THRESHOLD_SX}>
                            {thresholdInfo}
                        </Typography>
                    ) : alert.description && (
                        <Typography sx={INSTANCE_THRESHOLD_SX}>
                            {alert.description}
                        </Typography>
                    )}
                </Box>

                {/* Time */}
                <Typography sx={INSTANCE_TIME_SX}>
                    {alert.time}
                </Typography>

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
                            <AnalyzeIcon sx={{ ...ICON_14_SX, ...(alert.aiAnalysis && { color: 'success.main' }) }} />
                        </IconButton>
                    </Tooltip>
                )}

                {/* Edit override button */}
                {alert.ruleId && onEditOverride && (
                    <Tooltip title="Edit alert override" placement="left">
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onEditOverride?.(alert);
                            }}
                            sx={overrideButtonSx}
                        >
                            <TuneRounded sx={ICON_14_SX} />
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
                            <UnackIcon sx={ICON_14_SX} />
                        ) : (
                            <AckIcon sx={ICON_14_SX} />
                        )}
                    </IconButton>
                </Tooltip>
            </Box>

            {/* Ack info - only shown when acknowledged */}
            {isAcknowledged && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, ml: 1 }}>
                    <Typography sx={ALERT_ACK_TEXT_SX}>
                        Acked by {alert.acknowledgedBy}{alert.ackMessage ? `: ${alert.ackMessage}` : ''}
                    </Typography>
                    {alert.falsePositive && (
                        <Chip label="False Positive" size="small" sx={falsePositiveChipSx} />
                    )}
                </Box>
            )}
        </Box>
    );
};

/**
 * GroupedAlertItem - Display a group of alerts with the same title in a single panel
 */
const GroupedAlertItem = ({ title, alerts, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze, onEditOverride, onAcknowledgeGroup }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const [expanded, setExpanded] = useState(true);

    // All alerts in the group share the same severity
    const highestSeverity = alerts[0].severity || 'info';

    const baseColor = severityColors[highestSeverity] || severityColors.info;
    const SeverityIcon = highestSeverity === 'critical' ? ErrorIcon : WarningIcon;
    const friendlyTitle = getFriendlyTitle(title);

    const containerSx = useMemo(() => ({
        borderRadius: 1,
        bgcolor: alpha(baseColor, 0.08),
        border: '1px solid',
        borderColor: alpha(baseColor, 0.15),
        overflow: 'hidden',
    }), [baseColor]);

    const headerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        cursor: 'pointer',
        bgcolor: alpha(baseColor, 0.10),
        '&:hover': {
            bgcolor: alpha(baseColor, 0.1),
        },
    }), [baseColor]);

    const countChipSx = useMemo(() => ({
        height: 18,
        fontSize: '0.875rem',
        fontWeight: 600,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_075_SX,
    }), [baseColor]);

    const severityChipSx = useMemo(() => ({
        ...SEVERITY_CHIP_BASE_SX,
        bgcolor: alpha(baseColor, 0.15),
        color: baseColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [baseColor]);

    const hasUnacknowledged = alerts.some(a => !a.acknowledgedAt);

    const groupAckButtonSx = useMemo(() => ({
        p: 0.25,
        color: theme.palette.success.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.success.main, 0.1),
        },
    }), [theme.palette.success]);

    return (
        <Box sx={containerSx}>
            {/* Group header */}
            <Box onClick={() => setExpanded(!expanded)} sx={headerSx}>
                <SeverityIcon sx={{ fontSize: 16, color: baseColor, flexShrink: 0 }} />
                <Typography sx={GROUP_TITLE_SX}>
                    {friendlyTitle}
                </Typography>
                <Chip
                    label={`${alerts.length} instance${alerts.length !== 1 ? 's' : ''}`}
                    size="small"
                    sx={countChipSx}
                />
                <Chip label={highestSeverity} size="small" sx={severityChipSx} />
                {hasUnacknowledged && onAcknowledgeGroup && (
                    <Tooltip title="Acknowledge all in group" placement="left">
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                e.stopPropagation();
                                onAcknowledgeGroup(alerts.filter(a => !a.acknowledgedAt));
                            }}
                            sx={groupAckButtonSx}
                        >
                            <AckIcon sx={ICON_14_SX} />
                        </IconButton>
                    </Tooltip>
                )}
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {expanded ? (
                        <ExpandLessIcon sx={ICON_16_SX} />
                    ) : (
                        <ExpandMoreIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Box>

            {/* Instances list */}
            <Collapse in={expanded}>
                <Box sx={GROUP_INSTANCES_LIST_SX}>
                    {alerts.map((alert) => (
                        <GroupedAlertInstance
                            key={alert.id}
                            alert={alert}
                            showServer={showServer}
                            onAcknowledge={onAcknowledge}
                            onUnacknowledge={onUnacknowledge}
                            onAnalyze={onAnalyze}
                            onEditOverride={onEditOverride}
                        />
                    ))}
                </Box>
            </Collapse>
        </Box>
    );
};

export default GroupedAlertItem;
