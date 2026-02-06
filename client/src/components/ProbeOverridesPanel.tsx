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
    getContainedButtonSx,
    getTableContainerSx,
} from './AdminPanel/styles';

const API_BASE_URL = '/api/v1';

interface ProbeOverride {
    name: string;
    description: string;
    default_enabled: boolean;
    default_interval_seconds: number;
    default_retention_days: number;
    has_override: boolean;
    override_enabled: boolean | null;
    override_interval_seconds: number | null;
    override_retention_days: number | null;
}

interface ProbeOverridesPanelProps {
    scope: 'server' | 'cluster' | 'group';
    scopeId: number;
}

const ProbeOverridesPanel: React.FC<ProbeOverridesPanelProps> = ({ scope, scopeId }) => {
    const theme = useTheme();

    const [overrides, setOverrides] = useState<ProbeOverride[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Edit override dialog
    const [editOpen, setEditOpen] = useState(false);
    const [editOverride, setEditOverride] = useState<ProbeOverride | null>(null);
    const [editEnabled, setEditEnabled] = useState(false);
    const [editInterval, setEditInterval] = useState('');
    const [editRetention, setEditRetention] = useState('');
    const [saving, setSaving] = useState(false);

    const fetchOverrides = useCallback(async () => {
        try {
            setLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/probe-overrides/${scope}/${scopeId}`,
                { credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to fetch probe overrides');
            }
            const data = await response.json();
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

    const handleEditOverride = (override: ProbeOverride, e: React.MouseEvent) => {
        e.stopPropagation();
        const display = getDisplayValues(override);
        setEditOverride(override);
        setEditEnabled(display.enabled);
        setEditInterval(String(display.interval));
        setEditRetention(String(display.retention));
        setError(null);
        setEditOpen(true);
    };

    const handleSaveOverride = async () => {
        if (!editOverride) {
            return;
        }
        const intervalNum = parseInt(editInterval, 10);
        if (isNaN(intervalNum) || intervalNum < 1) {
            setError('Collection interval must be a positive integer.');
            return;
        }
        const retentionNum = parseInt(editRetention, 10);
        if (isNaN(retentionNum) || retentionNum < 1) {
            setError('Retention days must be a positive integer.');
            return;
        }
        try {
            setSaving(true);
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/probe-overrides/${scope}/${scopeId}/${editOverride.name}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        is_enabled: editEnabled,
                        collection_interval_seconds: intervalNum,
                        retention_days: retentionNum,
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to save override');
            }
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

    const handleResetOverride = async (override: ProbeOverride, e: React.MouseEvent) => {
        e.stopPropagation();
        try {
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/probe-overrides/${scope}/${scopeId}/${override.name}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to reset override');
            }
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
                <CircularProgress />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    /** Return the effective display values for a probe row. */
    const getDisplayValues = (item: ProbeOverride) => {
        if (item.has_override) {
            return {
                enabled: item.override_enabled ?? item.default_enabled,
                interval: item.override_interval_seconds ?? item.default_interval_seconds,
                retention: item.override_retention_days ?? item.default_retention_days,
            };
        }
        return {
            enabled: item.default_enabled,
            interval: item.default_interval_seconds,
            retention: item.default_retention_days,
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
                            <TableCell sx={tableHeaderCellSx}>Description</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Interval</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Retention</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {overrides.length > 0 ? (
                            overrides.map((item) => {
                                const display = getDisplayValues(item);
                                const cellSx = item.has_override ? {} : inheritedCellSx;

                                return (
                                    <TableRow
                                        key={item.name}
                                        hover
                                    >
                                        <TableCell sx={cellSx}>{item.name}</TableCell>
                                        <TableCell sx={cellSx}>{item.description}</TableCell>
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
                                        <TableCell sx={cellSx}>
                                            {display.interval}s
                                        </TableCell>
                                        <TableCell sx={cellSx}>
                                            {display.retention} days
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
                            })
                        ) : (
                            <TableRow>
                                <TableCell colSpan={6} align="center" sx={emptyRowSx}>
                                    <Typography color="text.secondary" sx={emptyRowTextSx}>
                                        No probe configurations found.
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
                    Edit Override: {editOverride?.name}
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
                    <TextField
                        label="Collection Interval (seconds)"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editInterval}
                        onChange={(e) => setEditInterval(e.target.value)}
                        disabled={saving}
                        inputProps={{ min: 1 }}
                        sx={(sxTheme) => ({
                            '& input[type=number]': {
                                colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                            },
                        })}
                    />
                    <TextField
                        label="Retention Days"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editRetention}
                        onChange={(e) => setEditRetention(e.target.value)}
                        disabled={saving}
                        inputProps={{ min: 1 }}
                        sx={(sxTheme) => ({
                            '& input[type=number]': {
                                colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                            },
                        })}
                    />
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
                        {saving ? <CircularProgress size={20} color="inherit" /> : 'Save'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default ProbeOverridesPanel;
