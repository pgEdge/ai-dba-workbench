/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useCallback, useState } from 'react';
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
import { useCrudPanel } from './_shared';

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

/**
 * Fetches the alert-rule list from the server. The endpoint historically
 * returned a bare array and was later wrapped in `{ alert_rules: [...] }`;
 * both shapes are accepted so older deployments keep working.
 */
const fetchAlertRules = async (): Promise<AlertRule[]> => {
    const data = await apiGet<{ alert_rules?: AlertRule[] } | AlertRule[]>(
        '/api/v1/alert-rules',
    );
    if (Array.isArray(data)) {
        return data;
    }
    return data.alert_rules ?? [];
};

const AdminAlertRules: React.FC = () => {
    const theme = useTheme();

    // The shared hook owns list state, dialog/edit state, success/error
    // toasts, and the mutation try/catch boilerplate. The component is
    // left with the form fields and the render tree.
    const crud = useCrudPanel<AlertRule>({
        fetchItems: useCallback(() => fetchAlertRules(), []),
    });

    // Per-form fields for the edit dialog. The shared hook exposes
    // `editingItem` and the open/close lifecycle; the field-level state
    // remains here because it is rule-shape specific.
    const [editEnabled, setEditEnabled] = useState(false);
    const [editOperator, setEditOperator] = useState('');
    const [editThreshold, setEditThreshold] = useState('');
    const [editSeverity, setEditSeverity] = useState('');

    const handleEditRule = (rule: AlertRule, e: React.MouseEvent) => {
        e.stopPropagation();
        setEditEnabled(rule.default_enabled);
        setEditOperator(rule.default_operator);
        setEditThreshold(String(rule.default_threshold));
        setEditSeverity(rule.default_severity);
        // Clear any stale page-level error so the validation error from
        // the previous attempt does not bleed into a fresh edit.
        crud.setError(null);
        crud.openEdit(rule);
    };

    const handleSaveRule = async () => {
        const editRule = crud.editingItem;
        if (!editRule) { return; }
        const thresholdNum = parseFloat(editThreshold);
        if (Number.isNaN(thresholdNum)) {
            // Validation must surface as a page-level error to preserve
            // the historical UX where it appears above the table.
            crud.setError('Threshold must be a valid number.');
            return;
        }
        const result = await crud.runMutation(
            () =>
                apiPut(`/api/v1/alert-rules/${editRule.id}`, {
                    default_enabled: editEnabled,
                    default_operator: editOperator,
                    default_threshold: thresholdNum,
                    default_severity: editSeverity,
                }),
            {
                errorTarget: 'page',
                successMessage: `Alert rule "${getFriendlyTitle(editRule.name)}" updated successfully.`,
            },
        );
        if (result.ok) {
            crud.closeDialog();
        }
    };

    if (crud.loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading alert rules" />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    // Group rules by category
    const categories = Array.from(new Set(crud.items.map((r) => r.category))).sort();
    const editRule = crud.editingItem;

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Alert defaults
            </Typography>

            {crud.error && (
                <Alert severity="error" sx={{ mb: 2, borderRadius: 1 }} onClose={() => { crud.setError(null); }}>
                    {crud.error}
                </Alert>
            )}
            {crud.success && (
                <Alert severity="success" sx={{ mb: 2, borderRadius: 1 }} onClose={() => { crud.setSuccess(null); }}>
                    {crud.success}
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
                        {crud.items.length > 0 ? (
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
                                    {crud.items
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
                                                        onClick={(e) => { handleEditRule(rule, e); }}
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
                open={crud.dialogOpen}
                onClose={() => { crud.closeDialog(); }}
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
                            onChange={(e) => { setEditEnabled(e.target.checked); }}
                            disabled={crud.saving}
                            inputProps={{ 'aria-label': 'Enabled' }}
                        />
                    </Box>
                    <TextField
                        select
                        fullWidth
                        label="Operator"
                        value={editOperator}
                        onChange={(e) => { setEditOperator(e.target.value); }}
                        disabled={crud.saving}
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
                        onChange={(e) => { setEditThreshold(e.target.value); }}
                        disabled={crud.saving}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                    <TextField
                        select
                        fullWidth
                        label="Severity"
                        value={editSeverity}
                        onChange={(e) => { setEditSeverity(e.target.value); }}
                        disabled={crud.saving}
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
                    <Button onClick={() => { crud.closeDialog(); }} disabled={crud.saving}>
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveRule}
                        variant="contained"
                        disabled={crud.saving}
                        sx={containedButtonSx}
                    >
                        {crud.saving ? <CircularProgress size={20} color="inherit" aria-label="Saving" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminAlertRules;
