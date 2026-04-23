/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { forwardRef } from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    Box,
    IconButton,
    Alert,
    Tooltip,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import {
    ContentCopy as CopyIcon,
    Check as CheckIcon,
} from '@mui/icons-material';
import { dialogTitleSx, dialogActionsSx } from '../styles';

export interface CreatedTokenDialogProps {
    /** Whether the dialog is open. */
    open: boolean;
    /** Handler to close the dialog. */
    onClose: () => void;
    /** The created token string. */
    token: string | null;
    /** Handler for the copy button click. */
    onCopy: () => void;
    /** Whether the token was recently copied. */
    copied: boolean;
}

/**
 * Dialog showing the newly created token with a copy-to-clipboard button.
 * Uses forwardRef to allow passing a ref for the content element (used
 * for clipboard fallback in non-secure contexts).
 */
const CreatedTokenDialog = forwardRef<HTMLDivElement, CreatedTokenDialogProps>(
    ({ open, onClose, token, onCopy, copied }, ref) => {
        const theme = useTheme();

        return (
            <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
                <DialogTitle sx={dialogTitleSx}>Token created</DialogTitle>
                <DialogContent ref={ref}>
                    <Alert severity="warning" sx={{ mb: 2, borderRadius: 1 }}>
                        Save this token securely. It will not be shown again.
                    </Alert>
                    <Box
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 1,
                            fontFamily: 'monospace',
                            fontSize: '1rem',
                            bgcolor: alpha(theme.palette.text.primary, 0.08),
                            border: '1px solid',
                            borderColor: theme.palette.divider,
                            borderRadius: 1,
                            p: 2,
                            wordBreak: 'break-all',
                        }}
                    >
                        <Box sx={{ flex: 1 }}>{token}</Box>
                        <Tooltip
                            title={copied ? 'Copied!' : 'Copy to clipboard'}
                            placement="top"
                        >
                            <IconButton
                                onClick={onCopy}
                                size="small"
                                aria-label="copy token"
                            >
                                {copied ? (
                                    <CheckIcon fontSize="small" color="success" />
                                ) : (
                                    <CopyIcon fontSize="small" />
                                )}
                            </IconButton>
                        </Tooltip>
                    </Box>
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button onClick={onClose}>Close</Button>
                </DialogActions>
            </Dialog>
        );
    }
);

CreatedTokenDialog.displayName = 'CreatedTokenDialog';

export default CreatedTokenDialog;
