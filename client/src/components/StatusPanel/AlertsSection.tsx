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
    Collapse,
    Skeleton,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    CheckCircle as HealthyIcon,
    NotificationsActive as AlertIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    CheckCircleOutline as AckIcon,
} from '@mui/icons-material';
import AlertItem from './AlertItem';
import GroupedAlertItem from './GroupedAlertItem';
import {
    getSeverityColors,
    getSectionPanelSx,
    groupAlertsByTitleAndSeverity,
    CHIP_LABEL_SX,
    EXPAND_BUTTON_SX,
    ICON_16_SX,
    ALERTS_HEADER_SX,
    ALERTS_TITLE_SX,
    ALERTS_TYPE_COUNT_SX,
    ACTIVE_LIST_SX,
    NO_ALERTS_TEXT_BASE_SX,
    ACK_HEADER_BASE_SX,
    ACK_TITLE_SX,
    ACK_LIST_SX,
} from './styles';

/**
 * AlertsSection - Collapsible alerts list with active/acknowledged separation
 */
const AlertsSection = ({ alerts, loading, showServer = false, onAcknowledge, onUnacknowledge, onAnalyze, onEditOverride, onAcknowledgeGroup }) => {
    const theme = useTheme();
    const severityColors = getSeverityColors(theme);
    const [expanded, setExpanded] = useState(true);
    const [ackExpanded, setAckExpanded] = useState(false);

    // Separate active and acknowledged alerts
    const activeAlerts = alerts.filter(a => !a.acknowledgedAt);
    const acknowledgedAlerts = alerts.filter(a => !!a.acknowledgedAt);

    // Group active alerts by title and severity
    const groupedActiveAlerts = useMemo(() => groupAlertsByTitleAndSeverity(activeAlerts), [activeAlerts]);
    const groupedAcknowledgedAlerts = useMemo(() => groupAlertsByTitleAndSeverity(acknowledgedAlerts), [acknowledgedAlerts]);

    // Convert grouped object to sorted array of [key, alerts] pairs
    const sortedActiveGroups = useMemo(() => {
        return Object.entries(groupedActiveAlerts).sort((a, b) => {
            const getSeverityWeight = (alerts) => {
                const s = alerts[0].severity;
                if (s === 'critical') {return 3;}
                if (s === 'warning') {return 2;}
                return 1;
            };
            const severityDiff = getSeverityWeight(b[1]) - getSeverityWeight(a[1]);
            if (severityDiff !== 0) {return severityDiff;}
            return b[1].length - a[1].length;
        });
    }, [groupedActiveAlerts]);

    const sortedAcknowledgedGroups = useMemo(() => {
        return Object.entries(groupedAcknowledgedAlerts).sort((a, b) => b[1].length - a[1].length);
    }, [groupedAcknowledgedAlerts]);

    const panelSx = useMemo(() => getSectionPanelSx(theme), [theme]);

    const skeletonSx = useMemo(() => ({
        bgcolor: theme.palette.divider,
    }), [theme.palette.divider]);

    const activeCountChipSx = useMemo(() => ({
        height: 18,
        fontSize: '0.875rem',
        fontWeight: 600,
        bgcolor: activeAlerts.length > 0
            ? alpha(severityColors.warning, 0.15)
            : alpha(theme.palette.success.main, 0.12),
        color: activeAlerts.length > 0 ? severityColors.warning : theme.palette.success.main,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [activeAlerts.length, severityColors, theme.palette.success]);

    const noAlertsBoxSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 0.75,
        py: 2,
        borderRadius: 1,
        bgcolor: alpha(theme.palette.success.main, 0.10),
        border: '1px solid',
        borderColor: alpha(theme.palette.success.main, 0.12),
    }), [theme.palette.success]);

    const noAlertsTextSx = useMemo(() => ({
        ...NO_ALERTS_TEXT_BASE_SX,
        color: theme.palette.success.main,
    }), [theme.palette.success]);

    const ackCountChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        fontWeight: 600,
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.disabled',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    if (loading) {
        return (
            <Box sx={panelSx}>
                <Skeleton variant="text" width={120} height={20} />
                <Box sx={{ mt: 1, display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                    {[1, 2].map((i) => (
                        <Skeleton
                            key={i}
                            variant="rounded"
                            height={48}
                            sx={skeletonSx}
                        />
                    ))}
                </Box>
            </Box>
        );
    }

    // Render either a single AlertItem or a GroupedAlertItem depending on count.
    // groupKey is "title::severity"; extract the display title for GroupedAlertItem.
    const renderAlertGroup = (groupKey, alertsInGroup) => {
        const displayTitle = groupKey.split('::')[0];
        if (alertsInGroup.length === 1) {
            return (
                <AlertItem
                    key={alertsInGroup[0].id}
                    alert={alertsInGroup[0]}
                    showServer={showServer}
                    onAcknowledge={onAcknowledge}
                    onUnacknowledge={onUnacknowledge}
                    onAnalyze={onAnalyze}
                    onEditOverride={onEditOverride}
                />
            );
        }
        return (
            <GroupedAlertItem
                key={groupKey}
                title={displayTitle}
                alerts={alertsInGroup}
                showServer={showServer}
                onAcknowledge={onAcknowledge}
                onUnacknowledge={onUnacknowledge}
                onAnalyze={onAnalyze}
                onEditOverride={onEditOverride}
                onAcknowledgeGroup={onAcknowledgeGroup}
            />
        );
    };

    return (
        <Box sx={panelSx}>
            {/* Active Alerts Header */}
            <Box onClick={() => setExpanded(!expanded)} sx={ALERTS_HEADER_SX}>
                <AlertIcon sx={{ fontSize: 16, color: 'primary.main' }} />
                <Typography sx={ALERTS_TITLE_SX}>
                    Active Alerts
                </Typography>
                <Chip label={activeAlerts.length} size="small" sx={activeCountChipSx} />
                {sortedActiveGroups.length > 0 && sortedActiveGroups.length !== activeAlerts.length && (
                    <Typography sx={ALERTS_TYPE_COUNT_SX}>
                        ({sortedActiveGroups.length} type{sortedActiveGroups.length !== 1 ? 's' : ''})
                    </Typography>
                )}
                <Box sx={{ flex: 1 }} />
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {expanded ? (
                        <ExpandLessIcon sx={ICON_16_SX} />
                    ) : (
                        <ExpandMoreIcon sx={ICON_16_SX} />
                    )}
                </IconButton>
            </Box>

            {/* Active Alerts List */}
            <Collapse in={expanded}>
                <Box sx={ACTIVE_LIST_SX}>
                    {activeAlerts.length === 0 ? (
                        <Box sx={noAlertsBoxSx}>
                            <HealthyIcon sx={{ fontSize: 16, color: theme.palette.success.main }} />
                            <Typography sx={noAlertsTextSx}>
                                No active alerts
                            </Typography>
                        </Box>
                    ) : (
                        sortedActiveGroups.map(([groupKey, alertsInGroup]) =>
                            renderAlertGroup(groupKey, alertsInGroup)
                        )
                    )}
                </Box>
            </Collapse>

            {/* Acknowledged Alerts Section */}
            {acknowledgedAlerts.length > 0 && (
                <>
                    <Box onClick={() => setAckExpanded(!ackExpanded)} sx={ACK_HEADER_BASE_SX}>
                        <AckIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                        <Typography sx={ACK_TITLE_SX}>
                            Acknowledged
                        </Typography>
                        <Chip label={acknowledgedAlerts.length} size="small" sx={ackCountChipSx} />
                        <Box sx={{ flex: 1 }} />
                        <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                            {ackExpanded ? (
                                <ExpandLessIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            ) : (
                                <ExpandMoreIcon sx={{ fontSize: 14, color: 'text.disabled' }} />
                            )}
                        </IconButton>
                    </Box>

                    <Collapse in={ackExpanded}>
                        <Box sx={ACK_LIST_SX}>
                            {sortedAcknowledgedGroups.map(([groupKey, alertsInGroup]) =>
                                renderAlertGroup(groupKey, alertsInGroup)
                            )}
                        </Box>
                    </Collapse>
                </>
            )}
        </Box>
    );
};

export default AlertsSection;
