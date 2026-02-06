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
    Dialog,
    AppBar,
    Toolbar,
    IconButton,
    Typography,
    Box,
    Slide,
    Tabs,
    Tab,
} from '@mui/material';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';
import AlertOverridesPanel from './AlertOverridesPanel';
import ProbeOverridesPanel from './ProbeOverridesPanel';
import ChannelOverridesPanel from './ChannelOverridesPanel';

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
    const [activeTab, setActiveTab] = useState(0);

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
            <Tabs
                value={activeTab}
                onChange={(_, v) => setActiveTab(v)}
                sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
            >
                <Tab label="Alert Overrides" />
                <Tab label="Probe Configuration" />
                <Tab label="Notification Channels" />
            </Tabs>
            <Box sx={{ flex: 1, overflow: 'auto', p: 3, bgcolor: 'background.default' }}>
                {activeTab === 0 && (
                    <AlertOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
                {activeTab === 1 && (
                    <ProbeOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
                {activeTab === 2 && (
                    <ChannelOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
            </Box>
        </Dialog>
    );
};

export default ClusterConfigDialog;
