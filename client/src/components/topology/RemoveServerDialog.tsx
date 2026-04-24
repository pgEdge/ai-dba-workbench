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
    CircularProgress,
} from '@mui/material';
import type { ClusterServerInfo } from '../ServerDialog/ServerDialog.types';

export interface RemoveServerDialogProps {
    server: ClusterServerInfo | null;
    clusterName: string;
    removing: boolean;
    onConfirm: () => void;
    onCancel: () => void;
}

/**
 * Confirmation dialog for removing a server from a cluster.
 */
const RemoveServerDialog: React.FC<RemoveServerDialogProps> = ({
    server,
    clusterName,
    removing,
    onConfirm,
    onCancel,
}) => {
    return (
        <Dialog
            open={server !== null}
            onClose={() => void (!removing && onCancel())}
        >
            <DialogTitle>Remove server from cluster</DialogTitle>
            <DialogContent>
                <DialogContentText>
                    Remove <strong>{server?.name}</strong>{' '}
                    from <strong>{clusterName}</strong>? The
                    server will become standalone. All
                    relationships involving this server within the
                    cluster will be deleted.
                </DialogContentText>
            </DialogContent>
            <DialogActions>
                <Button
                    onClick={onCancel}
                    disabled={removing}
                >
                    Cancel
                </Button>
                <Button
                    onClick={onConfirm}
                    variant="contained"
                    color="error"
                    disabled={removing}
                    aria-label="Remove"
                >
                    {removing ? (
                        <CircularProgress
                            size={18}
                            sx={{ color: 'inherit' }}
                        />
                    ) : (
                        'Remove'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default RemoveServerDialog;
