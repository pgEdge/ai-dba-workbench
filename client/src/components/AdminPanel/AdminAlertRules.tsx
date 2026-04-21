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
    MenuItem,
    Chip,
    CircularProgress,
    Alert,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
} from '@mui/icons-material';
import { SELECT_FIELD_SX } from '../shared/formStyles';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    categoryLabelSx,
    getContainedButtonSx,
    getTableContainerSx,
} from './styles';
import { apiGet, apiPut } from '../../utils/apiClient';
import { getFriendlyTitle } from '../../utils/friendlyNames';

const OPERATORS = ['>', '>=', '<', '<=', '==', '!='];
const SEVERITIES = ['info', 'warning', 'critical'];

interface AlertRule {
    id: number;
    name: string;
    category: string;
    metric_name: string;
    default_operator: string;
    default_threshold: number;
    default_severity: string;
    default_enabled: boolean;
}

const severityColor = (severity: string): 'default' | 'info' | 'warning' | 'error' => {
    switch (severity) {
        case 'critical': return 'error';
        case 'warning': return 'warning';
        case 'info': return 'info';
        default: return 'default';
    }
};

const AdminAlertRules: React.FC = () => {
    const theme = useTheme();

    const [rules, setRules] = useState<AlertRule[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Edit rule dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editRule, setEditRule] = useState<AlertRule | null>(null);
    const [editEnabled, setEditEnabled] = useState(false);
    const [editOperator, setEditOperator] = useState('');
    const [editThreshold, setEditThreshold] = useState('');
    const [editSeverity, setEditSeverity] = useState('');
    const [saving, setSaving] = useState(false);

    const fetchRules = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<{ alert_rules?: AlertRule[] } | AlertRule[]>(
                '/api/v1/alert-rules',
            );
            if (Array.isArray(data)) {
                setRules(data);
            } else {
                setRules(data.alert_rules || []);
            }
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchRules();
    }, [fetchRules]);

    const handleEditRule = (rule: AlertRule, e: React.MouseEvent) => {
        e.stopPropagation();
        setEditRule(rule);
        setEditEnabled(rule.default_enabled);
        setEditOperator(rule.default_operator);
        setEditThreshold(String(rule.default_threshold));
        setEditSeverity(rule.default_severity);
        setError(null);
        setEditOpen(true);
    };

    const handleSaveRule = async () => {
        if (!editRule) {return;}
        const thresholdNum = parseFloat(editThreshold);
        if (isNaN(thresholdNum)) {
            setError('Threshold must be a valid number.');
            return;
        }
        try {
            setSaving(true);
            setError(null);
            await apiPut(`/api/v1/alert-rules/${editRule.id}`, {
                default_enabled: editEnabled,
                default_operator: editOperator,
                default_threshold: thresholdNum,
                default_severity: editSeverity,
            });
            setEditOpen(false);
            setSuccess(`Alert rule "${getFriendlyTitle(editRule.name)}" updated successfully.`);
            fetchRules();
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

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading alert rules" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    // Group rules by category
    const categories = Array.from(new Set(rules.map((r) => r.category))).sort();

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Alert defaults
            </Typography>

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
                <Table size="small">
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
                        {rules.length > 0 ? (
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
                                    {rules
                                        .filter((r) => r.category === category)
                                        .map((rule) => (
                                            <TableRow
                                                key={rule.id}
                                                hover
                                            >
                                                <TableCell>
                                                    {getFriendlyTitle(rule.name)}
                                                    <Typography variant="caption" display="block" color="text.secondary">
                                                        {rule.name}
                                                    </Typography>
                                                </TableCell>
                                                <TableCell>{rule.metric_name}</TableCell>
                                                <TableCell>
                                                    {rule.default_operator} {rule.default_threshold}
                                                </TableCell>
                                                <TableCell>
                                                    <Chip
                                                        label={rule.default_severity}
                                                        size="small"
                                                        color={severityColor(rule.default_severity)}
                                                    />
                                                </TableCell>
                                                <TableCell>
                                                    <Switch
                                                        checked={rule.default_enabled}
                                                        size="small"
                                                        disabled
                                                    />
                                                </TableCell>
                                                <TableCell align="right">
                                                    <IconButton
                                                        size="small"
                                                        onClick={(e) => handleEditRule(rule, e)}
                                                        aria-label="edit alert rule"
                                                    >
                                                        <EditIcon fontSize="small" />
                                                    </IconButton>
                                                </TableCell>
                                            </TableRow>
                                        ))}
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

            {/* Edit Rule Dialog */}
            <Dialog
                open={editOpen}
                onClose={() => !saving && setEditOpen(false)}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    Edit alert rule: {editRule ? getFriendlyTitle(editRule.name) : ''}
                </DialogTitle>
                <DialogContent>
                    <Box sx={{ display: 'flex', alignItems: 'center', mt: 1, mb: 2 }}>
                        <Typography sx={{ flex: 1 }}>Enabled</Typography>
                        <Switch
                            checked={editEnabled}
                            onChange={(e) => setEditEnabled(e.target.checked)}
                            disabled={saving}
                            inputProps={{ 'aria-label': 'Enabled' }}
                        />
                    </Box>
                    <TextField
                        select
                        fullWidth
                        label="Operator"
                        value={editOperator}
                        onChange={(e) => setEditOperator(e.target.value)}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    >
                        {OPERATORS.map((op) => (
                            <MenuItem key={op} value={op}>{op}</MenuItem>
                        ))}
                    </TextField>
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
                            <MenuItem key={s} value={s}>{s}</MenuItem>
                        ))}
                    </TextField>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={() => setEditOpen(false)} disabled={saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveRule}
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

export default AdminAlertRules;
