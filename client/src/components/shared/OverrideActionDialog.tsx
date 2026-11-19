/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Override dialog for a proposed action that
 * the supervisor vetoed. Shows the danger rating and reasons, and asks
 * the user to type the connection name before the action may run.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useEffect, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import type { Action } from '../../types/actions';
import { MONO_CAPTION_SX } from '../../theme/tokens';

export interface OverrideActionDialogProps {
    open: boolean;
    action: Action;
    /** The connection's name; the user must type it exactly. */
    connectionName: string;
    onClose: () => void;
    /** Called with the confirmation text once it matches. */
    onConfirm: (confirm: string) => void;
}

/** Map a danger rating to a theme colour for its chip. */
export const dangerColour = (
    danger: string | undefined,
): 'success' | 'warning' | 'error' | 'default' => {
    switch (danger) {
        case 'low':
            return 'success';
        case 'moderate':
            return 'warning';
        case 'high':
        case 'critical':
            return 'error';
        default:
            return 'default';
    }
};

const OverrideActionDialog: React.FC<OverrideActionDialogProps> = ({
    open,
    action,
    connectionName,
    onClose,
    onConfirm,
}) => {
    const [typed, setTyped] = useState('');

    // Start every opening with an empty box so an earlier entry cannot
    // pre-authorise the next override.
    useEffect(() => {
        if (open) {
            setTyped('');
        }
    }, [open]);

    const danger = action.review?.danger;
    const reasons = action.review?.reasons ?? [];
    const matches = typed === connectionName;

    return (
        <Dialog
            open={open}
            onClose={onClose}
            maxWidth="sm"
            fullWidth
            aria-labelledby="override-action-title"
        >
            <DialogTitle id="override-action-title">
                Override the supervisor&apos;s veto
            </DialogTitle>
            <DialogContent>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                    <Typography variant="subtitle1">
                        Ellie&apos;s supervisor vetoed this change.
                    </Typography>
                    {danger && (
                        <Chip
                            size="small"
                            label={`Danger: ${danger}`}
                            color={dangerColour(danger)}
                            data-testid="override-danger-chip"
                        />
                    )}
                </Box>
                {reasons.length > 0 && (
                    <Box
                        component="ul"
                        data-testid="override-reasons"
                        sx={{ mt: 0, pl: 3 }}
                    >
                        {reasons.map((reason, idx) => (
                            <li key={idx}>
                                <Typography variant="body2">{reason}</Typography>
                            </li>
                        ))}
                    </Box>
                )}
                <Typography variant="body2" sx={{ mb: 1 }}>
                    Running it anyway is your decision and is recorded
                    against your account. To continue, type the connection
                    name{' '}
                    <Box component="span" sx={MONO_CAPTION_SX}>
                        {connectionName}
                    </Box>
                    {' '}below.
                </Typography>
                <TextField
                    autoFocus
                    fullWidth
                    size="small"
                    value={typed}
                    onChange={(e) => { setTyped(e.target.value); }}
                    inputProps={{ 'aria-label': 'Connection name' }}
                />
            </DialogContent>
            <DialogActions>
                <Button onClick={onClose} sx={{ textTransform: 'none' }}>
                    Cancel
                </Button>
                <Button
                    variant="contained"
                    color="error"
                    disabled={!matches}
                    onClick={() => { onConfirm(typed); }}
                    sx={{ textTransform: 'none' }}
                >
                    Run anyway
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default OverrideActionDialog;
