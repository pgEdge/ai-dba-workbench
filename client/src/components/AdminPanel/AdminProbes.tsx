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

const API_BASE_URL = '/api/v1';

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
            const response = await fetch(`${API_BASE_URL}/probe-configs`, {
                credentials: 'include',
            });
            if (!response.ok) throw new Error('Failed to fetch probe configurations');
            const data = await response.json();
            setProbes(data.probe_configs || data || []);
        } catch (err: any) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchProbes();
    }, [fetchProbes]);

    const handleRowClick = (probe: ProbeConfig) => {
        setEditProbe(probe);
        setEditEnabled(probe.is_enabled);
        setEditInterval(String(probe.collection_interval_seconds));
        setEditRetention(String(probe.retention_days));
        setError(null);
        setEditOpen(true);
    };

    const handleSave = async () => {
        if (!editProbe) return;
        try {
            setSaving(true);
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/probe-configs/${editProbe.id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({
                        is_enabled: editEnabled,
                        collection_interval_seconds: parseInt(editInterval, 10),
                        retention_days: parseInt(editRetention, 10),
                    }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to update probe configuration');
            }
            setEditOpen(false);
            setSuccess(`Probe "${editProbe.name}" updated successfully.`);
            fetchProbes();
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

    return (
        <Box>
            <Typography variant="h6" sx={{ ...pageHeadingSx, mb: 2 }}>
                Probe Configuration
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
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {probes.length > 0 ? (
                            probes.map((probe) => (
                                <TableRow
                                    key={probe.id}
                                    hover
                                    onClick={() => handleRowClick(probe)}
                                    sx={{ cursor: 'pointer' }}
                                >
                                    <TableCell>{probe.name}</TableCell>
                                    <TableCell>{probe.description}</TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={probe.is_enabled}
                                            size="small"
                                            disabled
                                            onClick={(e) => e.stopPropagation()}
                                        />
                                    </TableCell>
                                    <TableCell>{probe.collection_interval_seconds}s</TableCell>
                                    <TableCell>{probe.retention_days}</TableCell>
                                </TableRow>
                            ))
                        ) : (
                            <TableRow>
                                <TableCell colSpan={5} align="center" sx={emptyRowSx}>
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
                    Edit Probe: {editProbe?.name}
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
