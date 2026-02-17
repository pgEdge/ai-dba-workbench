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
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
} from '@mui/icons-material';
import { apiGet, apiPut } from '../../utils/apiClient';
import {
    tableHeaderCellSx,
    dialogTitleSx,
    dialogActionsSx,
    pageHeadingSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getTableContainerSx,
} from './styles';

interface ProbeConfig {
    id: number;
    name: string;
    description: string;
    is_enabled: boolean;
    collection_interval_seconds: number;
    retention_days: number;
    connection_id: number | null;
}

const AdminProbes: React.FC = () => {
    const theme = useTheme();

    const [probes, setProbes] = useState<ProbeConfig[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Edit dialog state
    const [editOpen, setEditOpen] = useState(false);
    const [editProbe, setEditProbe] = useState<ProbeConfig | null>(null);
    const [editEnabled, setEditEnabled] = useState(false);
    const [editInterval, setEditInterval] = useState('');
    const [editRetention, setEditRetention] = useState('');
    const [saving, setSaving] = useState(false);

    const fetchProbes = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<Record<string, unknown>>('/api/v1/probe-configs');
            setProbes((data.probe_configs || data || []) as ProbeConfig[]);
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
        fetchProbes();
    }, [fetchProbes]);

    const handleEditProbe = (probe: ProbeConfig) => {
        setEditProbe(probe);
        setEditEnabled(probe.is_enabled);
        setEditInterval(String(probe.collection_interval_seconds));
        setEditRetention(String(probe.retention_days));
        setError(null);
        setEditOpen(true);
    };

    const handleSave = async () => {
        if (!editProbe) {return;}
        try {
            setSaving(true);
            setError(null);
            await apiPut(`/api/v1/probe-configs/${editProbe.id}`, {
                is_enabled: editEnabled,
                collection_interval_seconds: parseInt(editInterval, 10),
                retention_days: parseInt(editRetention, 10),
            });
            setEditOpen(false);
            setSuccess(`Probe "${editProbe.name}" updated successfully.`);
            fetchProbes();
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
                <CircularProgress />
            </Box>
        );
    }

    const containedButtonSx = getContainedButtonSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Probe defaults
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
                            <TableCell sx={tableHeaderCellSx}>Description</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Interval</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Retention (days)</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {probes.length > 0 ? (
                            probes.map((probe) => (
                                <TableRow
                                    key={probe.id}
                                    hover
                                >
                                    <TableCell>{probe.name}</TableCell>
                                    <TableCell>{probe.description}</TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={probe.is_enabled}
                                            size="small"
                                            disabled
                                        />
                                    </TableCell>
                                    <TableCell>{probe.collection_interval_seconds}s</TableCell>
                                    <TableCell>{probe.retention_days}</TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={() => handleEditProbe(probe)}
                                            aria-label="edit probe"
                                        >
                                            <EditIcon fontSize="small" />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                            ))
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

            {/* Edit Probe Dialog */}
            <Dialog
                open={editOpen}
                onClose={() => !saving && setEditOpen(false)}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    Edit probe: {editProbe?.name}
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
                        onClick={handleSave}
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

export default AdminProbes;
