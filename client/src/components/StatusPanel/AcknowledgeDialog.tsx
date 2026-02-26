/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo } from 'react';
import {
    Alert,
    Box,
    Typography,
    Button,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    CheckCircle as HealthyIcon,
    CheckCircleOutline as AckIcon,
} from '@mui/icons-material';
import {
    getFriendlyTitle,
    ACK_DIALOG_TITLE_SX,
    ACK_DIALOG_ACTIONS_SX,
    ACK_FALSE_POSITIVE_TITLE_SX,
    ACK_FALSE_POSITIVE_DESC_SX,
} from './styles';

/**
 * AcknowledgeDialog - Dialog for entering ack reason and false positive flag
 */
const AcknowledgeDialog = ({ open, alert, alerts, onClose, onConfirm, onConfirmMultiple }) => {
    const theme = useTheme();
    const [message, setMessage] = useState('');
    const [falsePositive, setFalsePositive] = useState(false);

    const isGroupAck = alerts && alerts.length > 1;

    const handleConfirm = () => {
        if (isGroupAck && onConfirmMultiple) {
            onConfirmMultiple(alerts.map(a => a.id), message, falsePositive);
        } else {
            onConfirm(alert?.id, message, falsePositive);
        }
        setMessage('');
        setFalsePositive(false);
    };

    const handleClose = () => {
        setMessage('');
        setFalsePositive(false);
        onClose();
    };

    const dialogPaperSx = useMemo(() => ({
        bgcolor: theme.palette.background.paper,
        backgroundImage: 'none',
    }), [theme.palette.background.paper]);

    const falsePositiveBoxSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        p: 1.5,
        borderRadius: 1,
        bgcolor: alpha(theme.palette.error.main, 0.10),
        border: '1px solid',
        borderColor: alpha(theme.palette.error.main, 0.18),
        cursor: 'pointer',
    }), [theme.palette.error]);

    const checkboxSx = useMemo(() => ({
        width: 18,
        height: 18,
        borderRadius: 0.5,
        border: '2px solid',
        borderColor: falsePositive ? theme.palette.error.main : theme.palette.grey[400],
        bgcolor: falsePositive ? theme.palette.error.main : 'transparent',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
        transition: 'all 0.15s ease',
    }), [falsePositive, theme]);

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{ sx: dialogPaperSx }}
        >
            <DialogTitle sx={ACK_DIALOG_TITLE_SX}>
                {isGroupAck ? 'Acknowledge alerts' : 'Acknowledge alert'}
            </DialogTitle>
            <DialogContent>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                    {isGroupAck
                        ? getFriendlyTitle(alerts[0].title)
                        : (alert ? getFriendlyTitle(alert.title) : 'Alert')}
                </Typography>
                {isGroupAck && (
                    <Alert severity="warning" sx={{ mb: 2 }}>
                        This will acknowledge all {alerts.length} alerts in this group.
                    </Alert>
                )}
                <TextField
                    autoFocus
                    label="Reason"
                    placeholder="e.g., Investigating, Known issue, Scheduled maintenance"
                    fullWidth
                    multiline
                    rows={2}
                    value={message}
                    onChange={(e) => setMessage(e.target.value)}
                    variant="outlined"
                    size="small"
                    InputLabelProps={{ shrink: true }}
                    sx={{ mb: 2 }}
                />
                <Box
                    sx={falsePositiveBoxSx}
                    onClick={() => setFalsePositive(!falsePositive)}
                >
                    <Box sx={checkboxSx}>
                        {falsePositive && (
                            <HealthyIcon sx={{ fontSize: 14, color: 'common.white' }} />
                        )}
                    </Box>
                    <Box>
                        <Typography sx={ACK_FALSE_POSITIVE_TITLE_SX}>
                            Mark as false positive
                        </Typography>
                        <Typography sx={ACK_FALSE_POSITIVE_DESC_SX}>
                            This helps improve alert accuracy over time
                        </Typography>
                    </Box>
                </Box>
            </DialogContent>
            <DialogActions sx={ACK_DIALOG_ACTIONS_SX}>
                <Button onClick={handleClose} color="inherit" size="small">
                    Cancel
                </Button>
                <Button
                    onClick={handleConfirm}
                    variant="contained"
                    size="small"
                    startIcon={<AckIcon />}
                >
                    {isGroupAck ? 'Acknowledge all' : 'Acknowledge'}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default AcknowledgeDialog;
