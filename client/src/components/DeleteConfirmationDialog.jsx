/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Delete Confirmation Dialog
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import {
    Dialog,
    DialogContent,
    DialogActions,
    Typography,
    Button,
    Box,
    CircularProgress,
} from '@mui/material';
import { Warning as WarningIcon } from '@mui/icons-material';

/**
 * A confirmation dialog for delete operations with a warning icon.
 *
 * @param {Object} props
 * @param {boolean} props.open - Controls dialog visibility
 * @param {function} props.onClose - Called when dialog should close
 * @param {function} props.onConfirm - Called when delete is confirmed
 * @param {string} props.title - Dialog title (e.g., "Delete Server")
 * @param {string} props.message - Warning message to display
 * @param {string} props.itemName - Name of item being deleted (shown bold)
 * @param {boolean} props.loading - Shows loading spinner on delete button
 */
const DeleteConfirmationDialog = ({
    open,
    onClose,
    onConfirm,
    title,
    message,
    itemName,
    loading = false,
}) => {
    const handleConfirm = async () => {
        if (onConfirm) {
            await onConfirm();
        }
    };

    return (
        <Dialog
            open={open}
            onClose={loading ? undefined : onClose}
            maxWidth="xs"
            fullWidth
            aria-labelledby="delete-dialog-title"
            aria-describedby="delete-dialog-description"
        >
            <DialogContent sx={{ textAlign: 'center', pt: 4, pb: 2 }}>
                <Box
                    sx={{
                        display: 'flex',
                        justifyContent: 'center',
                        mb: 2,
                    }}
                >
                    <WarningIcon
                        sx={{
                            fontSize: 56,
                            color: '#F59E0B',
                        }}
                    />
                </Box>
                <Typography
                    id="delete-dialog-title"
                    variant="h6"
                    component="h2"
                    sx={{
                        fontWeight: 600,
                        mb: 1.5,
                    }}
                >
                    {title}
                </Typography>
                <Typography
                    id="delete-dialog-description"
                    variant="body2"
                    color="text.secondary"
                >
                    {message}{' '}
                    {itemName && (
                        <Box
                            component="span"
                            sx={{ fontWeight: 600, color: 'text.primary' }}
                        >
                            {itemName}
                        </Box>
                    )}
                </Typography>
            </DialogContent>
            <DialogActions
                sx={{
                    justifyContent: 'center',
                    pb: 3,
                    px: 3,
                    gap: 1,
                }}
            >
                <Button
                    onClick={onClose}
                    disabled={loading}
                    sx={{ minWidth: 100 }}
                >
                    Cancel
                </Button>
                <Button
                    onClick={handleConfirm}
                    variant="contained"
                    color="error"
                    disabled={loading}
                    sx={{ minWidth: 100 }}
                    startIcon={
                        loading ? (
                            <CircularProgress size={16} color="inherit" />
                        ) : null
                    }
                >
                    {loading ? 'Deleting...' : 'Delete'}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default DeleteConfirmationDialog;
