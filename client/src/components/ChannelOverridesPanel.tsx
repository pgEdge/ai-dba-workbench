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
    IconButton,
    Switch,
    Chip,
    CircularProgress,
    Alert,
    Tooltip,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    RestartAlt as ResetIcon,
} from '@mui/icons-material';
import {
    tableHeaderCellSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getTableContainerSx,
} from './AdminPanel/styles';

const API_BASE_URL = '/api/v1';

interface ChannelOverride {
    channel_id: number;
    channel_name: string;
    channel_type: string;
    description: string | null;
    is_estate_default: boolean;
    has_override: boolean;
    override_enabled: boolean | null;
}

interface ChannelOverridesPanelProps {
    scope: 'server' | 'cluster' | 'group';
    scopeId: number;
}

const ChannelOverridesPanel: React.FC<ChannelOverridesPanelProps> = ({ scope, scopeId }) => {
    const theme = useTheme();

    const [overrides, setOverrides] = useState<ChannelOverride[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    const fetchOverrides = useCallback(async () => {
        try {
            setLoading(true);
            const response = await fetch(
                `${API_BASE_URL}/channel-overrides/${scope}/${scopeId}`,
                { credentials: 'include' }
            );
            if (!response.ok) {
                throw new Error('Failed to fetch channel overrides');
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

    const handleToggleEnabled = async (item: ChannelOverride) => {
        const currentEnabled = item.has_override
            ? item.override_enabled ?? item.is_estate_default
            : item.is_estate_default;
        const newEnabled = !currentEnabled;

        try {
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/channel-overrides/${scope}/${scopeId}/${item.channel_id}`,
                {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    credentials: 'include',
                    body: JSON.stringify({ enabled: newEnabled }),
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to save channel override');
            }
            setSuccess(`Override for "${item.channel_name}" saved successfully.`);
            fetchOverrides();
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        }
    };

    const handleResetOverride = async (item: ChannelOverride, e: React.MouseEvent) => {
        e.stopPropagation();
        try {
            setError(null);
            const response = await fetch(
                `${API_BASE_URL}/channel-overrides/${scope}/${scopeId}/${item.channel_id}`,
                {
                    method: 'DELETE',
                    credentials: 'include',
                }
            );
            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || 'Failed to reset channel override');
            }
            setSuccess(`Override for "${item.channel_name}" reset to default.`);
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

    const tableContainerSx = getTableContainerSx(theme);

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
                <Table size="small">
                    <TableHead>
                        <TableRow>
                            <TableCell sx={tableHeaderCellSx}>Name</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Type</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Description</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Estate Default</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {overrides.length > 0 ? (
                            overrides.map((item) => {
                                const effectiveEnabled = item.has_override
                                    ? item.override_enabled ?? item.is_estate_default
                                    : item.is_estate_default;
                                const cellSx = item.has_override ? {} : inheritedCellSx;

                                return (
                                    <TableRow
                                        key={item.channel_id}
                                        hover
                                        sx={
                                            item.has_override
                                                ? {
                                                    borderLeft: '2px solid',
                                                    borderLeftColor: theme.palette.primary.main,
                                                }
                                                : undefined
                                        }
                                    >
                                        <TableCell sx={cellSx}>{item.channel_name}</TableCell>
                                        <TableCell>
                                            <Chip
                                                label={item.channel_type}
                                                size="small"
                                                sx={
                                                    !item.has_override
                                                        ? { opacity: 0.6 }
                                                        : undefined
                                                }
                                            />
                                        </TableCell>
                                        <TableCell sx={cellSx}>
                                            {item.description || '\u2014'}
                                        </TableCell>
                                        <TableCell>
                                            <Typography
                                                variant="body2"
                                                sx={
                                                    !item.has_override
                                                        ? { opacity: 0.6 }
                                                        : undefined
                                                }
                                            >
                                                {item.is_estate_default ? '\u2713' : '\u2014'}
                                            </Typography>
                                        </TableCell>
                                        <TableCell>
                                            <Switch
                                                checked={effectiveEnabled}
                                                size="small"
                                                onChange={() => handleToggleEnabled(item)}
                                                sx={
                                                    !item.has_override
                                                        ? {
                                                            opacity: 0.6,
                                                            fontStyle: 'italic',
                                                        }
                                                        : undefined
                                                }
                                                aria-label={`toggle ${item.channel_name} enabled`}
                                            />
                                        </TableCell>
                                        <TableCell align="right">
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
                                        No notification channels found.
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>
        </Box>
    );
};

export default ChannelOverridesPanel;
