/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useCallback } from 'react';
import { apiGet, apiPut, apiDelete } from '../utils/apiClient';
import {
    Box,
    Typography,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Paper,
    Button,
    IconButton,
    Switch,
    TextField,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    Chip,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Tooltip,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
    RestartAlt as ResetIcon,
} from '@mui/icons-material';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    categoryLabelSx,
    getContainedButtonSx,
    getTableContainerSx,
    getFocusedLabelSx,
} from './AdminPanel/styles';

const API_BASE_URL = '/api/v1';

const OPERATORS = ['>', '>=', '<', '<=', '=', '!='];
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

const severityColor = (severity: string): 'default' | 'info' | 'warning' | 'error' => {
    switch (severity) {
        case 'critical': return 'error';
        case 'warning': return 'warning';
        case 'info': return 'info';
        default: return 'default';
    }
};

const AlertOverridesPanel: React.FC<AlertOverridesPanelProps> = ({ scope, scopeId }) => {
    const theme = useTheme();

    const [overrides, setOverrides] = useState<AlertOverride[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Edit override dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editOverride, setEditOverride] = useState<AlertOverride | null>(null);
    const [editEnabled, setEditEnabled] = useState(false);
    const [editOperator, setEditOperator] = useState('');
    const [editThreshold, setEditThreshold] = useState('');
    const [editSeverity, setEditSeverity] = useState('');
    const [saving, setSaving] = useState(false);

    const fetchOverrides = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<AlertOverride[]>(
                `${API_BASE_URL}/alert-overrides/${scope}/${scopeId}`
            );
            setOverrides(data || []);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setLoading(false);
        }
    }, [scope, scopeId]);

    useEffect(() => {
        fetchOverrides();
    }, [fetchOverrides]);

    const handleEditOverride = (override: AlertOverride, e: React.MouseEvent) => {
        e.stopPropagation();
        setEditOverride(override);
        setEditEnabled(
            override.has_override
                ? override.override_enabled ?? override.default_enabled
                : override.default_enabled
        );
        setEditOperator(
            override.has_override
                ? override.override_operator ?? override.default_operator
                : override.default_operator
        );
        setEditThreshold(
            String(
                override.has_override
                    ? override.override_threshold ?? override.default_threshold
                    : override.default_threshold
            )
        );
        setEditSeverity(
            override.has_override
                ? override.override_severity ?? override.default_severity
                : override.default_severity
        );
        setError(null);
        setEditOpen(true);
    };

    const handleSaveOverride = async () => {
        if (!editOverride) {
            return;
        }
        const thresholdNum = parseFloat(editThreshold);
        if (isNaN(thresholdNum)) {
            setError('Threshold must be a valid number.');
            return;
        }
        try {
            setSaving(true);
            setError(null);
            await apiPut(
                `${API_BASE_URL}/alert-overrides/${scope}/${scopeId}/${editOverride.rule_id}`,
                {
                    operator: editOperator,
                    threshold: thresholdNum,
                    severity: editSeverity,
                    enabled: editEnabled,
                }
            );
            setEditOpen(false);
            setSuccess(`Override for "${editOverride.name}" saved successfully.`);
            fetchOverrides();
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

    const handleResetOverride = async (override: AlertOverride, e: React.MouseEvent) => {
        e.stopPropagation();
        try {
            setError(null);
            await apiDelete(
                `${API_BASE_URL}/alert-overrides/${scope}/${scopeId}/${override.rule_id}`
            );
            setSuccess(`Override for "${override.name}" reset to default.`);
            fetchOverrides();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading alert overrides" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const focusedLabelSx = getFocusedLabelSx(theme);

    // Group rules by category
    const categories = Array.from(new Set(overrides.map((r) => r.category))).sort();

    /** Return the effective display values for a rule row. */
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

    /** Style applied to cells that display inherited default values. */
    const inheritedCellSx = {
        fontStyle: 'italic',
        opacity: 0.6,
    };

    return (
        <Box>
            {error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setError(null)}>
                    {error}
                </Alert>
            )}
            {success && (
                <Alert severity="success" sx={{ mb: 2, borderRadius: 1 }} onClose={() => setSuccess(null)}>
                    {success}
                </Alert>
            )}

            <TableContainer
                component={Paper}
                elevation={0}
                sx={tableContainerSx}
            >
                <Table>
                    <TableHead>
                        <TableRow>
                            <TableCell sx={tableHeaderCellSx}>Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Metric</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Condition</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Severity</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {overrides.length > 0 ? (
                            categories.map((category) => (
                                <React.Fragment key={category}>
                                    <TableRow>
                                        <TableCell
                                            colSpan={6}
                                            sx={{
                                                ...categoryLabelSx,
                                                bgcolor: theme.palette.action.hover,
                                                py: 0.75,
                                            }}
                                        >
                                            {category}
                                        </TableCell>
                                    </TableRow>
                                    {overrides
                                        .filter((r) => r.category === category)
                                        .map((item) => {
                                            const display = getDisplayValues(item);
                                            const cellSx = item.has_override ? {} : inheritedCellSx;

                                            return (
                                                <TableRow
                                                    key={item.rule_id}
                                                    hover
                                                >
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
                                                            sx={
                                                                !item.has_override
                                                                    ? { opacity: 0.6 }
                                                                    : undefined
                                                            }
                                                        />
                                                    </TableCell>
                                                    <TableCell>
                                                        <Switch
                                                            checked={display.enabled}
                                                            size="small"
                                                            disabled
                                                            sx={
                                                                !item.has_override
                                                                    ? { opacity: 0.6 }
                                                                    : undefined
                                                            }
                                                        />
                                                    </TableCell>
                                                    <TableCell align="right">
                                                        <Tooltip title="Edit override">
                                                            <IconButton
                                                                size="small"
                                                                onClick={(e) => handleEditOverride(item, e)}
                                                                aria-label="edit override"
                                                            >
                                                                <EditIcon fontSize="small" />
                                                            </IconButton>
                                                        </Tooltip>
                                                        {item.has_override && (
                                                            <Tooltip title="Reset to default">
                                                                <IconButton
                                                                    size="small"
                                                                    onClick={(e) => handleResetOverride(item, e)}
                                                                    aria-label="reset override to default"
                                                                >
                                                                    <ResetIcon fontSize="small" />
                                                                </IconButton>
                                                            </Tooltip>
                                                        )}
                                                    </TableCell>
                                                </TableRow>
                                            );
                                        })}
                                </React.Fragment>
                            ))
                        ) : (
                            <TableRow>
                                <TableCell colSpan={6} align="center" sx={emptyRowSx}>
                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                        No alert rules found.
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* Edit Override Dialog */}
            <Dialog
                open={editOpen}
                onClose={() => !saving && setEditOpen(false)}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    Edit override: {editOverride?.name}
                </DialogTitle>
                <DialogContent>
                    <Box sx={{ display: 'flex', alignItems: 'center', mt: 1, mb: 2 }}>
                        <Typography sx={{ flex: 1 }}>Enabled</Typography>
                        <Switch
                            checked={editEnabled}
                            onChange={(e) => setEditEnabled(e.target.checked)}
                            disabled={saving}
                        />
                    </Box>
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>Operator</InputLabel>
                        <Select
                            value={editOperator}
                            label="Operator"
                            onChange={(e) => setEditOperator(e.target.value)}
                            disabled={saving}
                        >
                            {OPERATORS.map((op) => (
                                <MenuItem key={op} value={op}>{op}</MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                    <TextField
                        label="Threshold"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editThreshold}
                        onChange={(e) => setEditThreshold(e.target.value)}
                        disabled={saving}
                        sx={(sxTheme) => ({
                            '& input[type=number]': {
                                colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                            },
                        })}
                    />
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>Severity</InputLabel>
                        <Select
                            value={editSeverity}
                            label="Severity"
                            onChange={(e) => setEditSeverity(e.target.value)}
                            disabled={saving}
                        >
                            {SEVERITIES.map((s) => (
                                <MenuItem key={s} value={s}>{s}</MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setEditOpen(false)} disabled={saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveOverride}
                        variant="contained"
                        disabled={saving}
                        sx={containedButtonSx}
                    >
                        {saving ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AlertOverridesPanel;
