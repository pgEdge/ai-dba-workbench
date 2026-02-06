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
    Dialog,
    AppBar,
    Toolbar,
    IconButton,
    Typography,
    Box,
    Slide,
} from '@mui/material';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';
import AlertOverridesPanel from './AlertOverridesPanel';

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

interface ClusterConfigDialogProps {
    open: boolean;
    onClose: () => void;
    clusterId: number;
    clusterName: string;
}

const ClusterConfigDialog: React.FC<ClusterConfigDialogProps> = ({
    open,
    onClose,
    clusterId,
    clusterName,
}) => {
    return (
        <Dialog
            fullScreen
            open={open}
            onClose={onClose}
            TransitionComponent={Transition}
        >
            <AppBar
                position="static"
                elevation={0}
                sx={{
                    bgcolor: 'background.paper',
                    borderBottom: '1px solid',
                    borderColor: 'divider',
                }}
            >
                <Toolbar>
                    <IconButton
                        edge="start"
                        onClick={onClose}
                        aria-label="close cluster settings"
                        sx={{ color: 'text.secondary', mr: 2 }}
                    >
                        <CloseIcon />
                    </IconButton>
                    <Typography
                        variant="h6"
                        component="div"
                        sx={{
                            flexGrow: 1,
                            fontWeight: 600,
                            color: 'text.primary',
                        }}
                    >
                        Cluster Settings: {clusterName}
                    </Typography>
                </Toolbar>
            </AppBar>
            <Box sx={{ flex: 1, overflow: 'auto', p: 3 }}>
                <AlertOverridesPanel scope="cluster" scopeId={clusterId} />
            </Box>
        </Dialog>
    );
};

export default ClusterConfigDialog;
