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
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogContentText,
    DialogActions,
    Button,
    Box,
} from '@mui/material';
import LinkOffIcon from '@mui/icons-material/LinkOff';
import { useConnectionStatus } from '../contexts/useConnectionStatus';

const reasonMessages: Record<string, string> = {
    auth: 'Your session has expired.',
    server: 'The server is not responding.',
    network: 'The server cannot be reached.',
};

const ConnectionLostOverlay: React.FC = () => {
    const { disconnected, reason, reconnect } = useConnectionStatus();

    if (!disconnected) {
        return null;
    }

    const message = reasonMessages[reason] || 'Connection lost.';

    return (
        <Dialog
            open={disconnected}
            disableEscapeKeyDown
            slotProps={{
                backdrop: {
                    sx: {
                        backdropFilter: 'blur(4px)',
                    },
                },
            }}
        >
            <DialogTitle>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <LinkOffIcon color="error" />
                    Disconnected
                </Box>
            </DialogTitle>
            <DialogContent>
                <DialogContentText>
                    {message} Please log in again to continue.
                </DialogContentText>
            </DialogContent>
            <DialogActions>
                <Button variant="contained" onClick={reconnect}>
                    Log In
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default ConnectionLostOverlay;
