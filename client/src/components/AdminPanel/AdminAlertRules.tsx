/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
    Add as AddIcon,
    Delete as DeleteIcon,
    Edit as EditIcon,
} from '@mui/icons-material';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    sectionHeaderSx,
    sectionTitleSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    categoryLabelSx,
    getContainedButtonSx,
    getTextButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
    getFocusedLabelSx,
} from './styles';

const API_BASE_URL = '/api/v1';

const OPERATORS = ['>', '>=', '<', '<=', '==', '!='];
const SEVERITIES = ['info', 'warning', 'critical'];

interface AlertRule {
    id: number;
    name: string;
    category: string;
    metric: string;
    default_operator: string;
    default_threshold: number;
    default_severity: string;
    default_enabled: boolean;
}

interface ThresholdOverride {
    id: number;
    alert_rule_id: number;
    connection_id: number;
    connection_name?: string;
    operator: string;
    threshold: number;
    severity: string;
    enabled: boolean;
}

interface AdminAlertRulesProps {
    mode?: string;
}

const severityColor = (severity: string): 'default' | 'info' | 'warning' | 'error' => {
    switch (severity) {
        case 'critical': return 'error';
        case 'warning': return 'warning';
        case 'info': return 'info';
        default: return 'default';
    }
};

const AdminAlertRules: React.FC<AdminAlertRulesProps> = () => {
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

    // Selected rule for threshold overrides
    const [selectedRule, setSelectedRule] = useState<AlertRule | null>(null);
    const [thresholds, setThresholds] = useState<ThresholdOverride[]>([]);
    const [thresholdsLoading, setThresholdsLoading] = useState(false);

    // Threshold edit/create dialog
    const [thresholdDialogOpen, setThresholdDialogOpen] = useState(false);
    const [editingThreshold, setEditingThreshold] = useState<ThresholdOverride | null>(null);
    const [thresholdConnectionId, setThresholdConnectionId] = useState('');
    const [thresholdOperator, setThresholdOperator] = useState('>');
    const [thresholdValue, setThresholdValue] = useState('');
    const [thresholdSeverity, setThresholdSeverity] = useState('warning');
    const [thresholdEnabled, setThresholdEnabled] = useState(true);
    const [thresholdSaving, setThresholdSaving] = useState(false);

    // Delete confirmation
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [deletingThreshold, setDeletingThreshold] = useState<ThresholdOverride | null>(null);
    const [deleting, setDeleting] = useState(false);

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

    const fetchThresholds = useCallback(async (ruleId: number) => {
        try {
            setThresholdsLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/alert-rules/${ruleId}/thresholds`,
                { credentials: 'include' }
            );
            if (!response.ok) throw new Error('Failed to fetch thresholds');
            const data = await response.json();
            setThresholds(data.thresholds || data || []);
        } catch (err: any) {
            setError(err.message);
        } finally {
            setThresholdsLoading(false);
        }
    }, []);

    const handleRowClick = (rule: AlertRule) => {
        setSelectedRule(rule);
        fetchThresholds(rule.id);
    };

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
                        default_threshold: parseFloat(editThreshold),
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

    // Threshold CRUD
    const handleAddThreshold = () => {
        setEditingThreshold(null);
        setThresholdConnectionId('');
        setThresholdOperator(selectedRule?.default_operator || '>');
        setThresholdValue(String(selectedRule?.default_threshold || ''));
        setThresholdSeverity(selectedRule?.default_severity || 'warning');
        setThresholdEnabled(true);
        setThresholdDialogOpen(true);
    };

    const handleEditThreshold = (t: ThresholdOverride) => {
        setEditingThreshold(t);
        setThresholdConnectionId(String(t.connection_id));
        setThresholdOperator(t.operator);
        setThresholdValue(String(t.threshold));
        setThresholdSeverity(t.severity);
        setThresholdEnabled(t.enabled);
        setThresholdDialogOpen(true);
    };

    const handleSaveThreshold = async () => {
        if (!selectedRule) return;
        try {
            setThresholdSaving(true);
            setError(null);
            const body = {
                connection_id: parseInt(thresholdConnectionId, 10),
                operator: thresholdOperator,
                threshold: parseFloat(thresholdValue),
                severity: thresholdSeverity,
                enabled: thresholdEnabled,
            };
            const url = editingThreshold
                ? `${API_BASE_URL}/alert-rules/${selectedRule.id}/thresholds/${editingThreshold.id}`
                : `${API_BASE_URL}/alert-rules/${selectedRule.id}/thresholds`;
            const method = editingThreshold ? 'PUT' : 'POST';
            const response = await fetch(url, {
                method,
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to save threshold override');
            }
            setThresholdDialogOpen(false);
            setSuccess('Threshold override saved successfully.');
            fetchThresholds(selectedRule.id);
        } catch (err: any) {
            setError(err.message);
        } finally {
            setThresholdSaving(false);
        }
    };

    const handleConfirmDelete = (t: ThresholdOverride) => {
        setDeletingThreshold(t);
        setDeleteDialogOpen(true);
    };

    const handleDeleteThreshold = async () => {
        if (!selectedRule || !deletingThreshold) return;
        try {
            setDeleting(true);
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/alert-rules/${selectedRule.id}/thresholds/${deletingThreshold.id}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) throw new Error('Failed to delete threshold override');
            setDeleteDialogOpen(false);
            setSuccess('Threshold override deleted.');
            fetchThresholds(selectedRule.id);
        } catch (err: any) {
            setError(err.message);
        } finally {
            setDeleting(false);
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
    const textButtonSx = getTextButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);
    const focusedLabelSx = getFocusedLabelSx(theme);

    // Group rules by category
    const categories = Array.from(new Set(rules.map((r) => r.category))).sort();

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Alert Rules
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
                            <TableCell sx={tableHeaderCellSx}>Category</TableCell>
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
                                            colSpan={7}
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
                                                selected={selectedRule?.id === rule.id}
                                                onClick={() => handleRowClick(rule)}
                                                sx={{ cursor: 'pointer' }}
                                            >
                                                <TableCell>{rule.name}</TableCell>
                                                <TableCell>
                                                    <Chip
                                                        label={rule.category}
                                                        size="small"
                                                        variant="outlined"
                                                    />
                                                </TableCell>
                                                <TableCell>{rule.metric}</TableCell>
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
                                                        onClick={(e) => e.stopPropagation()}
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
                                <TableCell colSpan={7} align="center" sx={emptyRowSx}>
                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                        No alert rules found.
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            {/* Threshold Overrides Section */}
            {selectedRule && (
                <Box sx={{ mt: 4 }}>
                    <Box sx={sectionHeaderSx}>
                        <Typography variant="subtitle1" sx={sectionTitleSx}>
                            Threshold Overrides: {selectedRule.name}
                        </Typography>
                        <Button
                            size="small"
                            startIcon={<AddIcon />}
                            onClick={handleAddThreshold}
                            sx={textButtonSx}
                        >
                            Add Override
                        </Button>
                    </Box>
                    <TableContainer
                        component={Paper}
                        elevation={0}
                        sx={tableContainerSx}
                    >
                        <Table size="small">
                            <TableHead>
                                <TableRow>
                                    <TableCell sx={tableHeaderCellSx}>Connection</TableCell>
                                    <TableCell sx={tableHeaderCellSx}>Condition</TableCell>
                                    <TableCell sx={tableHeaderCellSx}>Severity</TableCell>
                                    <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                                    <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                                </TableRow>
                            </TableHead>
                            <TableBody>
                                {thresholdsLoading ? (
                                    <TableRow>
                                        <TableCell colSpan={5} align="center" sx={{ py: 3 }}>
                                            <CircularProgress size={24} />
                                        </TableCell>
                                    </TableRow>
                                ) : thresholds.length > 0 ? (
                                    thresholds.map((t) => (
                                        <TableRow key={t.id}>
                                            <TableCell>
                                                {t.connection_name || `Connection ${t.connection_id}`}
                                            </TableCell>
                                            <TableCell>
                                                {t.operator} {t.threshold}
                                            </TableCell>
                                            <TableCell>
                                                <Chip
                                                    label={t.severity}
                                                    size="small"
                                                    color={severityColor(t.severity)}
                                                />
                                            </TableCell>
                                            <TableCell>
                                                <Switch
                                                    checked={t.enabled}
                                                    size="small"
                                                    disabled
                                                />
                                            </TableCell>
                                            <TableCell align="right">
                                                <IconButton
                                                    size="small"
                                                    onClick={() => handleEditThreshold(t)}
                                                    aria-label="edit threshold"
                                                >
                                                    <EditIcon fontSize="small" />
                                                </IconButton>
                                                <IconButton
                                                    size="small"
                                                    onClick={() => handleConfirmDelete(t)}
                                                    sx={deleteIconSx}
                                                    aria-label="delete threshold"
                                                >
                                                    <DeleteIcon fontSize="small" />
                                                </IconButton>
                                            </TableCell>
                                        </TableRow>
                                    ))
                                ) : (
                                    <TableRow>
                                        <TableCell colSpan={5} align="center" sx={emptyRowSx}>
                                            <Typography color="text.secondary" sx={emptyRowTextSx}>
                                                No threshold overrides for this rule.
                                            </Typography>
                                        </TableCell>
                                    </TableRow>
                                )}
                            </TableBody>
                        </Table>
                    </TableContainer>
                </Box>
            )}

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

            {/* Threshold Override Dialog */}
            <Dialog
                open={thresholdDialogOpen}
                onClose={() => !thresholdSaving && setThresholdDialogOpen(false)}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    {editingThreshold ? 'Edit Threshold Override' : 'Add Threshold Override'}
                </DialogTitle>
                <DialogContent>
                    <TextField
                        label="Connection ID"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={thresholdConnectionId}
                        onChange={(e) => setThresholdConnectionId(e.target.value)}
                        disabled={thresholdSaving || !!editingThreshold}
                        inputProps={{ min: 1 }}
                    />
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>Operator</InputLabel>
                        <Select
                            value={thresholdOperator}
                            label="Operator"
                            onChange={(e) => setThresholdOperator(e.target.value)}
                            disabled={thresholdSaving}
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
                        value={thresholdValue}
                        onChange={(e) => setThresholdValue(e.target.value)}
                        disabled={thresholdSaving}
                    />
                    <FormControl fullWidth margin="dense">
                        <InputLabel sx={focusedLabelSx}>Severity</InputLabel>
                        <Select
                            value={thresholdSeverity}
                            label="Severity"
                            onChange={(e) => setThresholdSeverity(e.target.value)}
                            disabled={thresholdSaving}
                        >
                            {SEVERITIES.map((s) => (
                                <MenuItem key={s} value={s}>{s}</MenuItem>
                            ))}
                        </Select>
                    </FormControl>
                    <Box sx={{ display: 'flex', alignItems: 'center', mt: 1 }}>
                        <Typography sx={{ flex: 1 }}>Enabled</Typography>
                        <Switch
                            checked={thresholdEnabled}
                            onChange={(e) => setThresholdEnabled(e.target.checked)}
                            disabled={thresholdSaving}
                        />
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={() => setThresholdDialogOpen(false)}
                        disabled={thresholdSaving}
                    >
                        Cancel
                    </Button>
                    <Button
                        onClick={handleSaveThreshold}
                        variant="contained"
                        disabled={thresholdSaving || !thresholdConnectionId}
                        sx={containedButtonSx}
                    >
                        {thresholdSaving ? <CircularProgress size={20} color="inherit" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete Confirmation Dialog */}
            <Dialog
                open={deleteDialogOpen}
                onClose={() => !deleting && setDeleteDialogOpen(false)}
                maxWidth="xs"
            >
                <DialogTitle sx={dialogTitleSx}>
                    Delete Threshold Override
                </DialogTitle>
                <DialogContent>
                    <Typography>
                        Are you sure you want to delete the threshold override for{' '}
                        {deletingThreshold?.connection_name ||
                            `Connection ${deletingThreshold?.connection_id}`}?
                    </Typography>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={() => setDeleteDialogOpen(false)}
                        disabled={deleting}
                    >
                        Cancel
                    </Button>
                    <Button
                        onClick={handleDeleteThreshold}
                        variant="contained"
                        color="error"
                        disabled={deleting}
                    >
                        {deleting ? <CircularProgress size={20} color="inherit" /> : 'Delete'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default AdminAlertRules;
