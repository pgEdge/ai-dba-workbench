/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect } from 'react';
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
    TextField,
    Button,
    CircularProgress,
    Alert,
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
    clusterDescription?: string;
    onSave?: (data: { name: string; description: string }) => Promise<void>;
}

const ClusterConfigDialog: React.FC<ClusterConfigDialogProps> = ({
    open,
    onClose,
    clusterId,
    clusterName,
    clusterDescription,
    onSave,
}) => {
    const [activeTab, setActiveTab] = useState(0);

    // Details form state
    const [name, setName] = useState(clusterName);
    const [description, setDescription] = useState(clusterDescription || '');
    const [nameError, setNameError] = useState('');
    const [isSaving, setIsSaving] = useState(false);
    const [saveError, setSaveError] = useState('');

    // Reset form state when the dialog opens
    useEffect(() => {
        if (open) {
            setName(clusterName);
            setDescription(clusterDescription || '');
            setNameError('');
            setSaveError('');
            setActiveTab(0);
        }
    }, [open, clusterName, clusterDescription]);

    const handleSave = async () => {
        const trimmed = name.trim();
        if (!trimmed) {
            setNameError('Name is required');
            return;
        }
        setNameError('');
        setSaveError('');
        setIsSaving(true);
        try {
            if (onSave) {
                await onSave({ name: trimmed, description: description.trim() });
            }
        } catch (err) {
            setSaveError(err instanceof Error ? err.message : 'Failed to save');
        } finally {
            setIsSaving(false);
        }
    };

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
                <Tab label="Details" />
                <Tab label="Alert overrides" />
                <Tab label="Probe configuration" />
                <Tab label="Notification channels" />
            </Tabs>
            <Box sx={{ flex: 1, overflow: 'auto', p: 3, bgcolor: 'background.default' }}>
                {activeTab === 0 && (
                    <Box sx={{ maxWidth: 600, mx: 'auto', display: 'flex', flexDirection: 'column', gap: 2.5 }}>
                        {saveError && (
                            <Alert severity="error" onClose={() => setSaveError('')} sx={{ borderRadius: 1 }}>
                                {saveError}
                            </Alert>
                        )}
                        <TextField
                            autoFocus
                            fullWidth
                            label="Name"
                            value={name}
                            onChange={(e) => { setName(e.target.value); setNameError(''); }}
                            error={!!nameError}
                            helperText={nameError}
                            required
                            disabled={isSaving}
                            margin="dense"
                        />
                        <TextField
                            fullWidth
                            label="Description"
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            disabled={isSaving}
                            margin="dense"
                            multiline
                            minRows={3}
                        />
                        <Box sx={{ display: 'flex', gap: 1, justifyContent: 'flex-end', borderTop: '1px solid', borderColor: 'divider', pt: 2, mt: 1 }}>
                            <Button onClick={onClose} disabled={isSaving}>
                                Cancel
                            </Button>
                            <Button
                                variant="contained"
                                onClick={handleSave}
                                disabled={isSaving}
                            >
                                {isSaving ? <CircularProgress size={20} sx={{ color: 'inherit' }} /> : 'Save'}
                            </Button>
                        </Box>
                    </Box>
                )}
                {activeTab === 1 && (
                    <AlertOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
                {activeTab === 2 && (
                    <ProbeOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
                {activeTab === 3 && (
                    <ChannelOverridesPanel scope="cluster" scopeId={clusterId} />
                )}
            </Box>
        </Dialog>
    );
};

export default ClusterConfigDialog;
