/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Chips shared by AlertItem and GroupedAlertInstance: the alert type
 * chip and the chip naming where the alert came from.
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useMemo } from 'react';
import { Chip, Tooltip, alpha } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { CHIP_LABEL_SX, getAlertTypeColor, getAlertTypeLabel } from './styles';
import { ALERT_TYPE_CHIP_BASE_SX } from '../../theme';
import {
    SYSTEM_ALERT_SOURCE_DESCRIPTION,
    SYSTEM_ALERT_SOURCE_LABEL,
    SYSTEM_ALERT_TYPE,
} from '../../utils/systemAlerts';

interface AlertTypeChipProps {
    alertType?: string;
}

/**
 * AlertTypeChip - Labels an alert as a threshold, anomaly or system
 * alert. The system chip uses the neutral outlined form, because no
 * status colour describes it (see color-contrast-guidelines.md).
 */
export const AlertTypeChip: React.FC<AlertTypeChipProps> = ({ alertType }) => {
    const theme = useTheme();
    const isSystem = alertType === SYSTEM_ALERT_TYPE;
    const color = getAlertTypeColor(theme, alertType || 'threshold');

    const sx = useMemo(() => (isSystem
        ? {
            ...ALERT_TYPE_CHIP_BASE_SX,
            color: 'text.primary',
            borderColor: theme.palette.mode === 'dark'
                ? theme.palette.grey[600]
                : theme.palette.grey[500],
            '& .MuiChip-label': CHIP_LABEL_SX,
        }
        : {
            ...ALERT_TYPE_CHIP_BASE_SX,
            bgcolor: alpha(color, 0.15),
            color,
            '& .MuiChip-label': CHIP_LABEL_SX,
        }), [isSystem, color, theme.palette.mode, theme.palette.grey]);

    return (
        <Chip
            label={getAlertTypeLabel(alertType)}
            size="small"
            variant={isSystem ? 'outlined' : 'filled'}
            sx={sx}
        />
    );
};

interface AlertSourceChipProps {
    isSystem: boolean;
    server?: string;
    showServer: boolean;
}

/**
 * AlertSourceChip - Names the server an alert belongs to, or, for a
 * system alert, labels it as coming from the Workbench itself. The
 * system label is shown even where server chips are hidden, since a
 * system alert must never read as belonging to the selected server.
 */
export const AlertSourceChip: React.FC<AlertSourceChipProps> = ({
    isSystem,
    server,
    showServer,
}) => {
    const theme = useTheme();

    const sx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    if (isSystem) {
        return (
            <Tooltip title={SYSTEM_ALERT_SOURCE_DESCRIPTION} placement="top">
                <Chip
                    label={SYSTEM_ALERT_SOURCE_LABEL}
                    size="small"
                    sx={sx}
                    aria-label={`Source: ${SYSTEM_ALERT_SOURCE_LABEL}`}
                />
            </Tooltip>
        );
    }

    if (!showServer || !server) {
        return null;
    }

    return <Chip label={server} size="small" sx={sx} />;
};
