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
 * AlertOverridesPanel renders and manages per-scope overrides for
 * alert rules. The cross-cutting list, fetch, banner, reset and
 * scaffolding behaviour is delegated to ScopedOverridesPanel; this
 * file owns only the alert-specific row projection (with category
 * grouping) and the alert-specific edit dialog.
 */

import type React from 'react';
import { useState } from 'react';
import {
    Box,
    Button,
    Chip,
    CircularProgress,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    MenuItem,
    Switch,
    TableCell,
    TextField,
    Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { apiPut } from '../utils/apiClient';
import {
    SELECT_FIELD_DEFAULT_BG_SX,
    SELECT_FIELD_SX,
} from './shared/formStyles';
import {
    dialogActionsSx,
    dialogTitleSx,
    getContainedButtonSx,
} from './AdminPanel/styles';
import ScopedOverridesPanel, {
    type ScopedOverridesColumn,
    type ScopedOverridesEditHelpers,
} from './shared/ScopedOverridesPanel';

const API_BASE_URL = '/api/v1';

const OPERATORS = ['>', '>=', '<', '<=', '==', '!='];
const SEVERITIES = ['info', 'warning', 'critical'];

interface AlertOverride {
    rule_id: number;
    name: string;
    description: string;
    category: string;
    metric_name: string;
    metric_unit: string | null;
    default_operator: string;
    default_threshold: number;
    default_severity: string;
    default_enabled: boolean;
    has_override: boolean;
    override_operator: string | null;
    override_threshold: number | null;
    override_severity: string | null;
    override_enabled: boolean | null;
}

interface AlertOverridesPanelProps {
    scope: 'server' | 'cluster' | 'group';
    scopeId: number;
}

/** Map a severity string to the matching MUI Chip color variant. */
const severityColor = (
    severity: string,
): 'default' | 'info' | 'warning' | 'error' => {
    switch (severity) {
        case 'critical':
            return 'error';
        case 'warning':
            return 'warning';
        case 'info':
            return 'info';
        default:
            return 'default';
    }
};

/** Column definitions for the alert overrides table. */
const ALERT_COLUMNS: ScopedOverridesColumn[] = [
    { label: 'Name' },
    { label: 'Metric' },
    { label: 'Condition' },
    { label: 'Severity' },
    { label: 'Enabled' },
];

/** Italic, dimmed style applied to cells displaying inherited defaults. */
const inheritedCellSx = {
    fontStyle: 'italic',
    opacity: 0.6,
};

/**
 * Resolve the effective values for an alert rule row. Mirrors the
 * server-side resolution: explicit override fields win, anything
 * left null falls through to the corresponding default.
 */
const getDisplayValues = (item: AlertOverride) => {
    if (item.has_override) {
        return {
            operator: item.override_operator ?? item.default_operator,
            threshold: item.override_threshold ?? item.default_threshold,
            severity: item.override_severity ?? item.default_severity,
            enabled: item.override_enabled ?? item.default_enabled,
        };
    }
    return {
        operator: item.default_operator,
        threshold: item.default_threshold,
        severity: item.default_severity,
        enabled: item.default_enabled,
    };
};

const AlertOverridesPanel: React.FC<AlertOverridesPanelProps> = ({
    scope,
    scopeId,
}) => {
    const theme = useTheme();

    // The shared scaffold owns the list, alerts, and reset action;
    // the dialog and its form state remain local because the field
    // set is alert-specific.
    const [editOpen, setEditOpen] = useState(false);
    const [editOverride, setEditOverride] = useState<AlertOverride | null>(
        null,
    );
    const [editEnabled, setEditEnabled] = useState(false);
    const [editOperator, setEditOperator] = useState('');
    const [editThreshold, setEditThreshold] = useState('');
    const [editSeverity, setEditSeverity] = useState('');
    const [saving, setSaving] = useState(false);

    /**
     * Pre-populate the edit dialog with the row's effective values
     * so the user starts from "current state" rather than blanks.
     */
    const handleEditRequested = (override: AlertOverride) => {
        setEditOverride(override);
        const enabledValue = override.has_override
            ? override.override_enabled ?? override.default_enabled
            : override.default_enabled;
        setEditEnabled(enabledValue);
        const operatorValue = override.has_override
            ? override.override_operator ?? override.default_operator
            : override.default_operator;
        setEditOperator(operatorValue);
        const thresholdValue = override.has_override
            ? override.override_threshold ?? override.default_threshold
            : override.default_threshold;
        setEditThreshold(String(thresholdValue));
        const severityValue = override.has_override
            ? override.override_severity ?? override.default_severity
            : override.default_severity;
        setEditSeverity(severityValue);
        setEditOpen(true);
    };

    const renderRowCells = (item: AlertOverride) => {
        const display = getDisplayValues(item);
        const cellSx = item.has_override ? {} : inheritedCellSx;
        return (
            <>
                <TableCell sx={cellSx}>{item.name}</TableCell>
                <TableCell sx={cellSx}>{item.metric_name}</TableCell>
                <TableCell sx={cellSx}>
                    {display.operator} {display.threshold}
                    {item.metric_unit ? ` ${item.metric_unit}` : ''}
                </TableCell>
                <TableCell>
                    <Chip
                        label={display.severity}
                        size="small"
                        color={severityColor(display.severity)}
                        sx={!item.has_override ? { opacity: 0.6 } : undefined}
                    />
                </TableCell>
                <TableCell>
                    <Switch
                        checked={display.enabled}
                        size="small"
                        disabled
                        sx={!item.has_override ? { opacity: 0.6 } : undefined}
                    />
                </TableCell>
            </>
        );
    };

    const containedButtonSx = getContainedButtonSx(theme);

    /**
     * Render the alert-specific edit dialog. Both validation
     * failures and server errors flow through helpers.onSaveError
     * so the user sees a single error banner above the table.
     */
    const renderEditDialog = (helpers: ScopedOverridesEditHelpers) => {
        const handleSaveOverride = async () => {
            if (!editOverride) {
                return;
            }
            const thresholdNum = parseFloat(editThreshold);
            if (Number.isNaN(thresholdNum)) {
                helpers.onSaveError(
                    new Error('Threshold must be a valid number.'),
                );
                return;
            }
            try {
                setSaving(true);
                await apiPut(
                    `${API_BASE_URL}/alert-overrides/${scope}/${String(scopeId)}/${String(editOverride.rule_id)}`,
                    {
                        operator: editOperator,
                        threshold: thresholdNum,
                        severity: editSeverity,
                        enabled: editEnabled,
                    },
                );
                setEditOpen(false);
                helpers.onSaveSuccess(
                    `Override for "${editOverride.name}" saved successfully.`,
                );
                helpers.refresh();
            } catch (err: unknown) {
                helpers.onSaveError(err);
            } finally {
                setSaving(false);
            }
        };

        return (
            <Dialog
                open={editOpen}
                onClose={() => {
                    if (!saving) {
                        setEditOpen(false);
                    }
                }}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    Edit override: {editOverride?.name}
                </DialogTitle>
                <DialogContent>
                    <Box
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            mt: 1,
                            mb: 2,
                        }}
                    >
                        <Typography sx={{ flex: 1 }}>Enabled</Typography>
                        <Switch
                            checked={editEnabled}
                            onChange={(e) => {
                                setEditEnabled(e.target.checked);
                            }}
                            disabled={saving}
                        />
                    </Box>
                    <TextField
                        select
                        fullWidth
                        label="Operator"
                        value={editOperator}
                        onChange={(e) => {
                            setEditOperator(e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_DEFAULT_BG_SX}
                    >
                        {OPERATORS.map((op) => (
                            <MenuItem key={op} value={op}>
                                {op}
                            </MenuItem>
                        ))}
                    </TextField>
                    <TextField
                        label="Threshold"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editThreshold}
                        onChange={(e) => {
                            setEditThreshold(e.target.value);
                        }}
                        disabled={saving}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                    <TextField
                        select
                        fullWidth
                        label="Severity"
                        value={editSeverity}
                        onChange={(e) => {
                            setEditSeverity(e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_DEFAULT_BG_SX}
                    >
                        {SEVERITIES.map((s) => (
                            <MenuItem key={s} value={s}>
                                {s}
                            </MenuItem>
                        ))}
                    </TextField>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={() => {
                            setEditOpen(false);
                        }}
                        disabled={saving}
                    >
                        Cancel
                    </Button>
                    <Button
                        onClick={() => {
                            void handleSaveOverride();
                        }}
                        variant="contained"
                        disabled={saving}
                        sx={containedButtonSx}
                    >
                        {saving ? (
                            <CircularProgress
                                size={20}
                                color="inherit"
                                aria-label="Saving"
                            />
                        ) : (
                            'Save'
                        )}
                    </Button>
                </DialogActions>
            </Dialog>
        );
    };

    return (
        <ScopedOverridesPanel<AlertOverride>
            scope={scope}
            scopeId={scopeId}
            apiBasePath={`${API_BASE_URL}/alert-overrides`}
            itemKey={(item) => item.rule_id}
            itemDisplayName={(item) => item.name}
            hasOverride={(item) => item.has_override}
            columns={ALERT_COLUMNS}
            renderRowCells={renderRowCells}
            groupBy={(item) => item.category}
            emptyMessage="No alert rules found."
            loadingLabel="Loading alert overrides"
            onEditRequested={handleEditRequested}
            renderEditDialog={renderEditDialog}
        />
    );
};

export default AlertOverridesPanel;
