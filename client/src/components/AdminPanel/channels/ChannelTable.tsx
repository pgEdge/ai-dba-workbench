/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
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
    Tooltip,
    CircularProgress,
    Alert,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Edit as EditIcon,
    Delete as DeleteIcon,
    Send as SendIcon,
} from '@mui/icons-material';
import { truncateDescription } from '../../../utils/textHelpers';
import {
    tableHeaderCellSx,
    pageHeadingSx,
    loadingContainerSx,
    emptyRowSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
} from '../styles';
import { BaseChannel, ChannelColumnDef } from './channelTypes';

export interface ChannelTableProps<T extends BaseChannel> {
    channels: T[];
    loading: boolean;
    extraColumns?: ChannelColumnDef<T>[];
    testingChannelId: number | null;
    onEdit: (e: React.MouseEvent, channel: T) => void;
    onDelete: (e: React.MouseEvent, channel: T) => void;
    onToggleEnabled: (channel: T) => void;
    onTest: (e: React.MouseEvent, channel: T) => void;
    onAdd: () => void;
    emptyMessage: string;
    testTooltip: string;
    testAriaLabel: string;
    testingAriaLabel: string;
    title: string;
    error: string | null;
    success: string | null;
    onClearError: () => void;
    onClearSuccess: () => void;
}

export function ChannelTable<T extends BaseChannel>({
    channels,
    loading,
    extraColumns = [],
    testingChannelId,
    onEdit,
    onDelete,
    onToggleEnabled,
    onTest,
    onAdd,
    emptyMessage,
    testTooltip,
    testAriaLabel,
    testingAriaLabel,
    title,
    error,
    success,
    onClearError,
    onClearSuccess,
}: ChannelTableProps<T>): React.ReactElement {
    const theme = useTheme();

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    // Base columns (4) + extra columns + actions column (1)
    const totalColumnCount = 4 + extraColumns.length + 1;

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label={`Loading ${title.toLowerCase()}`} />
            </Box>
        );
    }

    return (
        <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" sx={pageHeadingSx}>
                    {title}
                </Typography>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={onAdd}
                    sx={containedButtonSx}
                >
                    Add Channel
                </Button>
            </Box>

            {error && (
                <Alert
                    severity="error"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={onClearError}
                >
                    {error}
                </Alert>
            )}
            {success && (
                <Alert
                    severity="success"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={onClearSuccess}
                >
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
                            {extraColumns.map((col, index) => (
                                <TableCell key={index} sx={tableHeaderCellSx}>
                                    {col.label}
                                </TableCell>
                            ))}
                            <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                            <TableCell sx={tableHeaderCellSx}>Estate Default</TableCell>
                            <TableCell sx={tableHeaderCellSx} align="right">
                                Actions
                            </TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>
                        {channels.length > 0 ? (
                            channels.map((channel) => (
                                <TableRow key={channel.id} hover>
                                    <TableCell>{channel.name}</TableCell>
                                    <TableCell>
                                        {truncateDescription(channel.description)}
                                    </TableCell>
                                    {extraColumns.map((col, index) => (
                                        <TableCell key={index}>
                                            {col.render(channel)}
                                        </TableCell>
                                    ))}
                                    <TableCell>
                                        <Switch
                                            checked={channel.enabled}
                                            size="small"
                                            onChange={() => onToggleEnabled(channel)}
                                            inputProps={{
                                                'aria-label': 'Toggle channel enabled',
                                            }}
                                        />
                                    </TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={channel.is_estate_default}
                                            size="small"
                                            disabled
                                            inputProps={{
                                                'aria-label': 'Toggle estate default',
                                            }}
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <Tooltip title={testTooltip}>
                                            <span>
                                                <IconButton
                                                    size="small"
                                                    onClick={(e) => onTest(e, channel)}
                                                    aria-label={testAriaLabel}
                                                    disabled={
                                                        testingChannelId === channel.id
                                                    }
                                                >
                                                    {testingChannelId === channel.id ? (
                                                        <CircularProgress
                                                            size={18}
                                                            aria-label={testingAriaLabel}
                                                        />
                                                    ) : (
                                                        <SendIcon fontSize="small" />
                                                    )}
                                                </IconButton>
                                            </span>
                                        </Tooltip>
                                        <Tooltip title="Edit channel">
                                            <IconButton
                                                size="small"
                                                onClick={(e) => onEdit(e, channel)}
                                                aria-label="edit channel"
                                            >
                                                <EditIcon fontSize="small" />
                                            </IconButton>
                                        </Tooltip>
                                        <Tooltip title="Delete channel">
                                            <IconButton
                                                size="small"
                                                onClick={(e) => onDelete(e, channel)}
                                                aria-label="delete channel"
                                                sx={deleteIconSx}
                                            >
                                                <DeleteIcon fontSize="small" />
                                            </IconButton>
                                        </Tooltip>
                                    </TableCell>
                                </TableRow>
                            ))
                        ) : (
                            <TableRow>
                                <TableCell
                                    colSpan={totalColumnCount}
                                    align="center"
                                    sx={emptyRowSx}
                                >
                                    <Typography
                                        color="text.secondary"
                                        sx={emptyRowTextSx}
                                    >
                                        {emptyMessage}
                                    </Typography>
                                </TableCell>
                            </TableRow>
                        )}
                    </TableBody>
                </Table>
            </TableContainer>
        </Box>
    );
}
