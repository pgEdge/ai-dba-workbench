/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState } from 'react';
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
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import {
    tableHeaderCellSx,
    emptyRowSx,
    emptyRowTextSx,
    getContainedButtonSx,
    getDeleteIconSx,
    getTableContainerSx,
} from '../styles';
import { EmailRecipient } from './emailTypes';

export interface EmailRecipientsTabProps {
    visible: boolean;
    isEditing: boolean;
    recipients: EmailRecipient[];
    recipientsLoading: boolean;
    recipientSaving: boolean;
    onToggleRecipientEnabled: (recipient: EmailRecipient) => void;
    onDeleteRecipient: (recipient: EmailRecipient) => void;
    onAddRecipient: (email: string, name: string) => void;
    pendingRecipients: Array<{ email: string; name: string }>;
    onRemovePending: (index: number) => void;
}

/**
 * Recipients tab content for email channel create/edit dialog.
 * Manages existing recipients (edit mode) or pending recipients (create mode).
 */
export const EmailRecipientsTab: React.FC<EmailRecipientsTabProps> = ({
    visible,
    isEditing,
    recipients,
    recipientsLoading,
    recipientSaving,
    onToggleRecipientEnabled,
    onDeleteRecipient,
    onAddRecipient,
    pendingRecipients,
    onRemovePending,
}) => {
    const theme = useTheme();
    const [newRecipientEmail, setNewRecipientEmail] = useState('');
    const [newRecipientName, setNewRecipientName] = useState('');

    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);
    const tableContainerSx = getTableContainerSx(theme);

    const handleAddRecipient = () => {
        if (!newRecipientEmail.trim()) { return; }
        onAddRecipient(newRecipientEmail.trim(), newRecipientName.trim());
        setNewRecipientEmail('');
        setNewRecipientName('');
    };

    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            {isEditing && recipientsLoading ? (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} aria-label="Loading recipients" />
                </Box>
            ) : (
                <TableContainer
                    component={Paper}
                    elevation={0}
                    sx={tableContainerSx}
                >
                    <Table size="small">
                        <TableHead>
                            <TableRow>
                                <TableCell sx={tableHeaderCellSx}>Email</TableCell>
                                <TableCell sx={tableHeaderCellSx}>Display Name</TableCell>
                                {isEditing && (
                                    <TableCell sx={tableHeaderCellSx}>Enabled</TableCell>
                                )}
                                <TableCell sx={tableHeaderCellSx} align="right">Actions</TableCell>
                            </TableRow>
                        </TableHead>
                        <TableBody>
                            {isEditing && recipients.map((recipient) => (
                                <TableRow key={recipient.id}>
                                    <TableCell>{recipient.email}</TableCell>
                                    <TableCell>{recipient.display_name || '-'}</TableCell>
                                    <TableCell>
                                        <Switch
                                            checked={recipient.enabled}
                                            size="small"
                                            onChange={() => onToggleRecipientEnabled(recipient)}
                                            disabled={recipientSaving}
                                            inputProps={{ 'aria-label': 'Toggle recipient enabled' }}
                                        />
                                    </TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={() => onDeleteRecipient(recipient)}
                                            aria-label="delete recipient"
                                            sx={deleteIconSx}
                                            disabled={recipientSaving}
                                        >
                                            <DeleteIcon fontSize="small" />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                            ))}
                            {!isEditing && pendingRecipients.map((pending, index) => (
                                <TableRow key={`pending-${index}`}>
                                    <TableCell>{pending.email}</TableCell>
                                    <TableCell>{pending.name || '-'}</TableCell>
                                    <TableCell align="right">
                                        <IconButton
                                            size="small"
                                            onClick={() => onRemovePending(index)}
                                            aria-label="remove pending recipient"
                                            sx={deleteIconSx}
                                        >
                                            <DeleteIcon fontSize="small" />
                                        </IconButton>
                                    </TableCell>
                                </TableRow>
                            ))}
                            {((isEditing && recipients.length === 0) ||
                              (!isEditing && pendingRecipients.length === 0)) && (
                                <TableRow>
                                    <TableCell
                                        colSpan={isEditing ? 4 : 3}
                                        align="center"
                                        sx={emptyRowSx}
                                    >
                                        <Typography color="text.secondary" sx={emptyRowTextSx}>
                                            No recipients configured.
                                        </Typography>
                                    </TableCell>
                                </TableRow>
                            )}
                            {/* Add recipient row */}
                            <TableRow>
                                <TableCell>
                                    <TextField
                                        size="small"
                                        placeholder="Email address"
                                        value={newRecipientEmail}
                                        onChange={(e) => setNewRecipientEmail(e.target.value)}
                                        disabled={recipientSaving}
                                        fullWidth
                                        variant="standard"
                                    />
                                </TableCell>
                                <TableCell>
                                    <TextField
                                        size="small"
                                        placeholder="Display name"
                                        value={newRecipientName}
                                        onChange={(e) => setNewRecipientName(e.target.value)}
                                        disabled={recipientSaving}
                                        fullWidth
                                        variant="standard"
                                    />
                                </TableCell>
                                <TableCell
                                    colSpan={isEditing ? 2 : 1}
                                    align="right"
                                >
                                    <Button
                                        size="small"
                                        variant="contained"
                                        startIcon={recipientSaving
                                            ? <CircularProgress size={14} color="inherit" aria-label="Adding recipient" />
                                            : <AddIcon />
                                        }
                                        onClick={handleAddRecipient}
                                        disabled={recipientSaving || !newRecipientEmail.trim()}
                                        sx={containedButtonSx}
                                    >
                                        Add
                                    </Button>
                                </TableCell>
                            </TableRow>
                        </TableBody>
                    </Table>
                </TableContainer>
            )}
        </Box>
    );
};

export default EmailRecipientsTab;
