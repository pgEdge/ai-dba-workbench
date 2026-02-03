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
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
} from '@mui/icons-material';
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
    getFocusedLabelSx,
} from './styles';

const API_BASE_URL = '/api/v1';

const OPERATORS = ['>', '>=', '<', '<=', '=', '!='];
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
            const response = await fetch(`${API_BASE_URL}/alert-rules`, {
                credentials: 'include',
            });
            if (!response.ok) throw new Error('Failed to fetch alert rules');
            const data = await response.json();
            setRules(data.alert_rules || data || []);
        } catch (err: any) {
            setError(err.message);
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
        if (!editRule) return;
        const thresholdNum = parseFloat(editThreshold);
        if (isNaN(thresholdNum)) {
            setError('Threshold must be a valid number.');
            return;
        }
        try {
            setSaving(true);
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/alert-rules/${editRule.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        default_enabled: editEnabled,
                        default_operator: editOperator,
                        default_threshold: thresholdNum,
                        default_severity: editSeverity,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update alert rule');
            }
            setEditOpen(false);
            setSuccess(`Alert rule "${editRule.name}" updated successfully.`);
            fetchRules();
        } catch (err: any) {
            setError(err.message);
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const focusedLabelSx = getFocusedLabelSx(theme);

    // Group rules by category
    const categories = Array.from(new Set(rules.map((r) => r.category))).sort();

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Alert Defaults
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
                                                <TableCell>{rule.name}</TableCell>
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
                    Edit Alert Rule: {editRule?.name}
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
                        onClick={handleSaveRule}
                        variant="contained"
                        disabled={saving}
                        sx={containedButtonSx}
                    >
                        {saving ? <CircularProgress size={20} color="inherit" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminAlertRules;
