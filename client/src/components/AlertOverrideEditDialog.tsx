/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useEffect, useCallback } from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    MenuItem,
    TextField,
    Switch,
    Typography,
    Box,
    CircularProgress,
    Alert,
    Chip,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { apiFetch } from '../utils/apiClient';
import { SELECT_FIELD_SX } from './shared/formStyles';
import {
    dialogTitleSx,
    dialogActionsSx,
    getContainedButtonSx,
} from './AdminPanel/styles';

interface AlertOverrideEditDialogProps {
    open: boolean;
    alert: {
        ruleId?: number;
        connectionId?: number;
        title?: string;
        server?: string;
    } | null;
    onClose: () => void;
}

interface OverrideDetail {
    operator: string;
    threshold: number;
    severity: string;
    enabled: boolean;
}

interface OverrideContextResponse {
    hierarchy: {
        connection_id: number;
        cluster_id: number | null;
        group_id: number | null;
        server_name: string;
        cluster_name: string | null;
        group_name: string | null;
    };
    rule: {
        id: number;
        name: string;
        description: string;
        category: string;
        metric_name: string;
        metric_unit: string | null;
        default_operator: string;
        default_threshold: number;
        default_severity: string;
        default_enabled: boolean;
    };
    overrides: {
        server: OverrideDetail | null;
        cluster: OverrideDetail | null;
        group: OverrideDetail | null;
    };
}

const API_BASE_URL = '/api/v1';
const OPERATORS = ['>', '>=', '<', '<=', '==', '!='];
const SEVERITIES = ['info', 'warning', 'critical'];

const AlertOverrideEditDialog: React.FC<AlertOverrideEditDialogProps> = ({
    open,
    alert,
    onClose,
}) => {
    const theme = useTheme();
    const containedButtonSx = getContainedButtonSx(theme);

    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    const [context, setContext] = useState<OverrideContextResponse | null>(null);
    const [scopeOptions, setScopeOptions] = useState<
        Array<{ value: string; label: string; disabled: boolean }>
    >([]);
    const [selectedScope, setSelectedScope] = useState<string>('server');

    const [editEnabled, setEditEnabled] = useState(true);
    const [editOperator, setEditOperator] = useState('>');
    const [editThreshold, setEditThreshold] = useState('');
    const [editSeverity, setEditSeverity] = useState('warning');

    // Determine if the selected scope has an existing override
    const hasOverrideAtScope = useCallback(
        (scope: string): boolean => {
            if (!context) {return false;}
            if (scope === 'server') {return context.overrides.server !== null;}
            if (scope === 'cluster') {return context.overrides.cluster !== null;}
            if (scope === 'group') {return context.overrides.group !== null;}
            return false;
        },
        [context]
    );

    // Populate form fields based on selected scope
    const populateFields = useCallback(
        (scope: string, ctx: OverrideContextResponse) => {
            let override: OverrideDetail | null = null;
            if (scope === 'server') {override = ctx.overrides.server;}
            if (scope === 'cluster') {override = ctx.overrides.cluster;}
            if (scope === 'group') {override = ctx.overrides.group;}

            if (override) {
                setEditOperator(override.operator);
                setEditThreshold(String(override.threshold));
                setEditSeverity(override.severity);
                setEditEnabled(override.enabled);
            } else {
                setEditOperator(ctx.rule.default_operator);
                setEditThreshold(String(ctx.rule.default_threshold));
                setEditSeverity(ctx.rule.default_severity);
                setEditEnabled(ctx.rule.default_enabled);
            }
        },
        []
    );

    // Fetch context when dialog opens
    useEffect(() => {
        if (!open || !alert?.ruleId || alert?.connectionId === undefined || alert?.connectionId === null) {
            return;
        }

        const fetchContext = async () => {
            setLoading(true);
            setError(null);
            setSuccess(null);
            setContext(null);

            try {
                const response = await apiFetch(
                    `${API_BASE_URL}/alert-overrides/context/${alert.connectionId}/${alert.ruleId}`,
                );

                if (!response.ok) {
                    const data = await response.json().catch(() => ({}));
                    throw new Error(data.error || 'Failed to fetch override context');
                }

                const data: OverrideContextResponse = await response.json();
                setContext(data);

                // Build scope options
                const options: Array<{ value: string; label: string; disabled: boolean }> = [];

                // Determine highest existing override scope
                // Hierarchy from highest to lowest: group > cluster > server
                let highestExistingScope: string | null = null;
                if (data.overrides.group !== null) {
                    highestExistingScope = 'group';
                } else if (data.overrides.cluster !== null) {
                    highestExistingScope = 'cluster';
                } else if (data.overrides.server !== null) {
                    highestExistingScope = 'server';
                }

                // Determine which scopes are disabled
                // Scopes ABOVE the highest existing override are disabled
                const disabledScopes = new Set<string>();
                if (highestExistingScope === 'server') {
                    disabledScopes.add('cluster');
                    disabledScopes.add('group');
                } else if (highestExistingScope === 'cluster') {
                    disabledScopes.add('group');
                }
                // If highestExistingScope is 'group' or null, nothing is disabled

                // Always include server
                options.push({
                    value: 'server',
                    label: `Server: ${data.hierarchy.server_name}`,
                    disabled: disabledScopes.has('server'),
                });

                // Include cluster if available
                if (data.hierarchy.cluster_id !== null) {
                    options.push({
                        value: 'cluster',
                        label: `Cluster: ${data.hierarchy.cluster_name}`,
                        disabled: disabledScopes.has('cluster'),
                    });
                }

                // Include group if available
                if (data.hierarchy.group_id !== null) {
                    options.push({
                        value: 'group',
                        label: `Group: ${data.hierarchy.group_name}`,
                        disabled: disabledScopes.has('group'),
                    });
                }

                setScopeOptions(options);

                // Default scope selection: highest existing override, or 'server'
                const defaultScope = highestExistingScope || 'server';
                setSelectedScope(defaultScope);
                populateFields(defaultScope, data);
            } catch (err: unknown) {
                if (err instanceof Error) {
                    setError(err.message);
                } else {
                    setError('An unexpected error occurred');
                }
            } finally {
                setLoading(false);
            }
        };

        fetchContext();
    }, [open, alert?.ruleId, alert?.connectionId, populateFields]);

    // Update form fields when scope changes
    const handleScopeChange = (newScope: string) => {
        setSelectedScope(newScope);
        if (context) {
            populateFields(newScope, context);
        }
    };

    // Get the scope ID for the selected scope
    const getScopeId = (): number | null => {
        if (!context) {return null;}
        if (selectedScope === 'server') {return context.hierarchy.connection_id;}
        if (selectedScope === 'cluster') {return context.hierarchy.cluster_id;}
        if (selectedScope === 'group') {return context.hierarchy.group_id;}
        return null;
    };

    // Save the override
    const handleSave = async () => {
        if (!context) {return;}

        const thresholdNum = parseFloat(editThreshold);
        if (Number.isNaN(thresholdNum)) {
            setError('Threshold must be a valid number.');
            return;
        }

        const scopeId = getScopeId();
        if (scopeId === null) {
            setError('Invalid scope selection.');
            return;
        }

        try {
            setSaving(true);
            setError(null);

            const response = await apiFetch(
                `${API_BASE_URL}/alert-overrides/${selectedScope}/${scopeId}/${context.rule.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        operator: editOperator,
                        threshold: thresholdNum,
                        severity: editSeverity,
                        enabled: editEnabled,
                    }),
                }
            );

            if (!response.ok) {
                const data = await response.json().catch(() => ({}));
                throw new Error(data.error || 'Failed to save override');
            }

            setSuccess('Override saved successfully.');
            setTimeout(() => {
                onClose();
            }, 1000);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setSaving(false);
        }
    };

    const handleClose = () => {
        if (!saving) {
            onClose();
        }
    };

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="xs"
            fullWidth
        >
            <DialogTitle sx={dialogTitleSx}>
                Edit alert override
                {context?.rule?.name && (
                    <Typography
                        variant="body2"
                        sx={{ color: 'text.secondary', mt: 0.5 }}
                    >
                        {context.rule.name}
                    </Typography>
                )}
            </DialogTitle>
            <DialogContent>
                {loading ? (
                    <Box
                        sx={{
                            display: 'flex',
                            justifyContent: 'center',
                            py: 4,
                        }}
                    >
                        <CircularProgress aria-label="Loading alert override" />
                    </Box>
                ) : error && !context ? (
                    <Alert severity="error" sx={{ mt: 1 }}>
                        {error}
                    </Alert>
                ) : context ? (
                    <>
                        {error && (
                            <Alert
                                severity="error"
                                sx={{ mt: 1, mb: 1 }}
                                onClose={() => setError(null)}
                            >
                                {error}
                            </Alert>
                        )}
                        {success && (
                            <Alert
                                severity="success"
                                sx={{ mt: 1, mb: 1 }}
                            >
                                {success}
                            </Alert>
                        )}

                        {/* Scope selector */}
                        <TextField
                            select
                            fullWidth
                            label="Scope"
                            value={selectedScope}
                            onChange={(e) => handleScopeChange(e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                            sx={{ mt: 1, ...SELECT_FIELD_SX }}
                        >
                            {scopeOptions.map((opt) => (
                                <MenuItem
                                    key={opt.value}
                                    value={opt.value}
                                    disabled={opt.disabled}
                                >
                                    <Box
                                        sx={{
                                            display: 'flex',
                                            alignItems: 'center',
                                            gap: 1,
                                        }}
                                    >
                                        {opt.label}
                                        {hasOverrideAtScope(opt.value) && (
                                            <Chip
                                                label="override"
                                                size="small"
                                                sx={{
                                                    height: 18,
                                                    fontSize: '0.875rem',
                                                    bgcolor: alpha(
                                                        theme.palette.primary.main,
                                                        0.12
                                                    ),
                                                    color: theme.palette.primary.main,
                                                }}
                                            />
                                        )}
                                    </Box>
                                </MenuItem>
                            ))}
                        </TextField>

                        {/* Info alert when no override at selected scope */}
                        {!hasOverrideAtScope(selectedScope) && (
                            <Alert severity="info" sx={{ mt: 1.5, mb: 0.5 }}>
                                No override at this scope. Values shown are defaults.
                            </Alert>
                        )}

                        {/* Enabled toggle */}
                        <Box
                            sx={{
                                display: 'flex',
                                alignItems: 'center',
                                mt: 1.5,
                                mb: 1,
                            }}
                        >
                            <Typography sx={{ flex: 1 }}>Enabled</Typography>
                            <Switch
                                checked={editEnabled}
                                onChange={(e) => setEditEnabled(e.target.checked)}
                                disabled={saving}
                            />
                        </Box>

                        {/* Operator select */}
                        <TextField
                            select
                            fullWidth
                            label="Operator"
                            value={editOperator}
                            onChange={(e) => { setEditOperator(e.target.value); }}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_SX}
                        >
                            {OPERATORS.map((op) => (
                                <MenuItem key={op} value={op}>
                                    {op}
                                </MenuItem>
                            ))}
                        </TextField>

                        {/* Threshold text field */}
                        <TextField
                            label="Threshold"
                            type="number"
                            fullWidth
                            margin="dense"
                            value={editThreshold}
                            onChange={(e) => setEditThreshold(e.target.value)}
                            disabled={saving}
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_SX}
                        />

                        {/* Severity select */}
                        <TextField
                            select
                            fullWidth
                            label="Severity"
                            value={editSeverity}
                            onChange={(e) => setEditSeverity(e.target.value)}
                            disabled={saving}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_SX}
                        >
                            {SEVERITIES.map((s) => (
                                <MenuItem key={s} value={s}>
                                    {s}
                                </MenuItem>
                            ))}
                        </TextField>
                    </>
                ) : null}
            </DialogContent>
            <DialogActions sx={dialogActionsSx}>
                <Button onClick={handleClose} disabled={saving}>
                    Cancel
                </Button>
                <Button
                    onClick={handleSave}
                    variant="contained"
                    disabled={saving || loading || !context}
                    sx={containedButtonSx}
                >
                    {saving ? (
                        <CircularProgress size={20} color="inherit" aria-label="Saving" />
                    ) : (
                        'Save'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default AlertOverrideEditDialog;
