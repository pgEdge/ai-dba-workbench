/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Delete Confirmation Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import Dialog from '@mui/material/Dialog';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import WarningIcon from '@mui/icons-material/Warning';

// --- Style constants (Issue 23) ---

const dialogContentSx = {
    textAlign: 'center',
    pt: 4,
    pb: 2,
};

const iconContainerSx = {
    display: 'flex',
    justifyContent: 'center',
    mb: 2,
};

const warningIconSx = {
    fontSize: 56,
    color: 'warning.main',
};

const titleSx = {
    fontWeight: 600,
    mb: 1.5,
};

const dialogActionsSx = {
    justifyContent: 'center',
    pb: 3,
    px: 3,
    gap: 1,
};

const buttonSx = {
    minWidth: 100,
};

// --- Component ---

interface DeleteConfirmationDialogProps {
    open: boolean;
    onClose: () => void;
    onConfirm?: () => Promise<void> | void;
    title: string;
    message: string;
    itemName?: string;
    loading?: boolean;
}

/**
 * A confirmation dialog for delete operations with a warning icon.
 */
const DeleteConfirmationDialog: React.FC<DeleteConfirmationDialogProps> = ({
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
            <DialogContent sx={dialogContentSx}>
                <Box sx={iconContainerSx}>
                    <WarningIcon sx={warningIconSx} />
                </Box>
                <Typography
                    id="delete-dialog-title"
                    variant="h6"
                    component="h2"
                    sx={titleSx}
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
            <DialogActions sx={dialogActionsSx}>
                <Button
                    onClick={onClose}
                    disabled={loading}
                    sx={buttonSx}
                >
                    Cancel
                </Button>
                <Button
                    onClick={handleConfirm}
                    variant="contained"
                    color="error"
                    disabled={loading}
                    sx={buttonSx}
                    startIcon={
                        loading ? (
                            <CircularProgress size={16} color="inherit" aria-label="Deleting" />
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
