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
    CircularProgress,
    Alert,
    Switch,
    Chip,
    Tooltip,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import { Delete as DeleteIcon } from '@mui/icons-material';
import DeleteConfirmationDialog from '../DeleteConfirmationDialog';
import { apiGet, apiDelete, apiPatch } from '../../utils/apiClient';
import {
    tableHeaderCellSx,
    pageHeadingSx,
    loadingContainerSx,
    getDeleteIconSx,
    getTableContainerSx,
} from './styles';

interface Memory {
    id: number;
    username: string;
    scope: string;
    category: string;
    content: string;
    pinned: boolean;
    model_name: string;
    created_at: string;
    updated_at: string;
}

/** Maximum number of characters shown in the Content column. */
const CONTENT_TRUNCATE_LENGTH = 120;

/**
 * Truncate a string to the given length and append an ellipsis when
 * the original text exceeds the limit.
 */
function truncateContent(text: string, maxLength: number): string {
    if (text.length <= maxLength) {
        return text;
    }
    return `${text.slice(0, maxLength)}\u2026`;
}

/**
 * Format an ISO date string into a human-readable locale string.
 */
function formatDate(isoDate: string): string {
    try {
        const d = new Date(isoDate);
        return d.toLocaleString(undefined, {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
        });
    } catch {
        return isoDate;
    }
}

const AdminMemories: React.FC = () => {
    const theme = useTheme();
    const [memories, setMemories] = useState<Memory[]>([]);
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);

    // Delete confirmation state
    const [deleteOpen, setDeleteOpen] = useState<boolean>(false);
    const [deleteMemory, setDeleteMemory] = useState<Memory | null>(null);
    const [deleteLoading, setDeleteLoading] = useState<boolean>(false);

    const fetchMemories = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const data = await apiGet<{ memories: Memory[] }>(
                '/api/v1/memories?limit=1000',
            );
            setMemories(data.memories || []);
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchMemories();
    }, [fetchMemories]);

    // Optimistic pin toggle via PATCH
    const handleTogglePin = async (mem: Memory) => {
        const newPinned = !mem.pinned;

        // Optimistic update
        setMemories((prev) =>
            prev.map((m) =>
                m.id === mem.id ? { ...m, pinned: newPinned } : m,
            ),
        );

        try {
            await apiPatch(`/api/v1/memories/${mem.id}`, {
                pinned: newPinned,
            });
        } catch (err: unknown) {
            // Revert on failure
            setMemories((prev) =>
                prev.map((m) =>
                    m.id === mem.id ? { ...m, pinned: mem.pinned } : m,
                ),
            );
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        }
    };

    // Delete handlers
    const handleOpenDelete = (mem: Memory) => {
        setDeleteMemory(mem);
        setDeleteOpen(true);
    };

    const handleConfirmDelete = async () => {
        if (!deleteMemory) {
            return;
        }
        try {
            setDeleteLoading(true);
            await apiDelete(`/api/v1/memories/${deleteMemory.id}`);
            setDeleteOpen(false);
            setDeleteMemory(null);
            fetchMemories();
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            setError(message);
        } finally {
            setDeleteLoading(false);
        }
    };

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label="Loading memories" />
            </Box>
        );
    }

    if (error) {
        return (
            <Alert severity="error" sx={{ borderRadius: 1 }}>
                {error}
            </Alert>
        );
    }

    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    Memories
                </Typography>
            </Box>

            <TableContainer
                component={Paper}
                elevation={0}
                sx={tableContainerSx}
            >
                <Table>
                    <TableHead>
                        <TableRow>
                            <TableCell sx={tableHeaderCellSx}>
                                Content
                            </TableCell>
                            <TableCell sx={tableHeaderCellSx}>
                                Category
                            </TableCell>
                            <TableCell sx={tableHeaderCellSx}>
                                Scope
                            </TableCell>
                            <TableCell
                                sx={tableHeaderCellSx}
                                align="center"
                            >
                                Pinned
                            </TableCell>
                            <TableCell sx={tableHeaderCellSx}>
                                Created
                            </TableCell>
                            <TableCell
                                sx={tableHeaderCellSx}
                                align="right"
                            >
                                Actions
                            </TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {memories.map((mem) => (
                            <TableRow key={mem.id} hover>
                                <TableCell
                                    sx={{ maxWidth: 400 }}
                                >
                                    <Tooltip
                                        title={
                                            mem.content.length >
                                            CONTENT_TRUNCATE_LENGTH
                                                ? mem.content
                                                : ''
                                        }
                                        placement="bottom-start"
                                    >
                                        <Typography
                                            variant="body2"
                                            sx={{
                                                whiteSpace: 'pre-wrap',
                                                wordBreak: 'break-word',
                                            }}
                                        >
                                            {truncateContent(
                                                mem.content,
                                                CONTENT_TRUNCATE_LENGTH,
                                            )}
                                        </Typography>
                                    </Tooltip>
                                </TableCell>
                                <TableCell>
                                    <Typography variant="body2">
                                        {mem.category || '-'}
                                    </Typography>
                                </TableCell>
                                <TableCell>
                                    <Chip
                                        label={
                                            mem.scope === 'system'
                                                ? 'System'
                                                : 'User'
                                        }
                                        size="small"
                                        sx={{
                                            bgcolor:
                                                mem.scope === 'system'
                                                    ? alpha(
                                                        theme.palette.info
                                                            .main,
                                                        0.15,
                                                    )
                                                    : alpha(
                                                        theme.palette
                                                            .success.main,
                                                        0.15,
                                                    ),
                                            color:
                                                mem.scope === 'system'
                                                    ? theme.palette.info
                                                        .main
                                                    : theme.palette.success
                                                        .main,
                                            fontSize: '0.875rem',
                                        }}
                                    />
                                </TableCell>
                                <TableCell align="center">
                                    <Switch
                                        checked={mem.pinned}
                                        size="small"
                                        onChange={() =>
                                            handleTogglePin(mem)
                                        }
                                        inputProps={{
                                            'aria-label': 'Toggle pinned',
                                        }}
                                    />
                                </TableCell>
                                <TableCell>
                                    <Typography
                                        variant="body2"
                                        color="text.secondary"
                                    >
                                        {formatDate(mem.created_at)}
                                    </Typography>
                                </TableCell>
                                <TableCell align="right">
                                    <IconButton
                                        size="small"
                                        onClick={() =>
                                            handleOpenDelete(mem)
                                        }
                                        aria-label="delete memory"
                                        sx={deleteIconSx}
                                    >
                                        <DeleteIcon fontSize="small" />
                                    </IconButton>
                                </TableCell>
                            </TableRow>
                        ))}
                        {memories.length === 0 && (
                            <TableRow>
                                <TableCell
                                    colSpan={6}
                                    align="center"
                                    sx={{ py: 4 }}
                                >
                                    <Typography color="text.secondary">
                                        No memories found.
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>

            <DeleteConfirmationDialog
                open={deleteOpen}
                onClose={() => {
                    setDeleteOpen(false);
                    setDeleteMemory(null);
                }}
                onConfirm={handleConfirmDelete}
                title="Delete Memory"
                message="Are you sure you want to delete this memory?"
                itemName={
                    deleteMemory
                        ? `"${truncateContent(deleteMemory.content, 60)}"`
                        : undefined
                }
                loading={deleteLoading}
            />
        </Box>
    );
};

export default AdminMemories;
