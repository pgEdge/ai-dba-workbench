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
    MenuItem,
} from '@mui/material';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';
import AlertOverridesPanel from './AlertOverridesPanel';
import ProbeOverridesPanel from './ProbeOverridesPanel';
import ChannelOverridesPanel from './ChannelOverridesPanel';
import TopologyPanel from './TopologyPanel';
import { SELECT_FIELD_DEFAULT_BG_SX } from './shared/formStyles';

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

/**
 * Replication type options displayed in the select dropdown.
 */
const REPLICATION_TYPES = [
    { value: 'binary', label: 'Binary (Physical)' },
    { value: 'spock', label: 'Spock' },
    { value: 'logical', label: 'Logical' },
    { value: 'other', label: 'Other' },
] as const;

interface ClusterConfigDialogProps {
    open: boolean;
    onClose: () => void;
    mode: 'create' | 'edit';
    clusterId?: string;
    numericClusterId?: number;
    clusterName?: string;
    clusterDescription?: string;
    replicationType?: string | null;
    autoClusterKey?: string | null;
    onSave?: (data: { name: string; description: string; replication_type?: string }) => Promise<void>;
    onCreate?: (data: { name: string; description: string; replication_type: string }) => Promise<{ id: number }>;
    onMembershipChange?: () => void;
}

const ClusterConfigDialog: React.FC<ClusterConfigDialogProps> = ({
    open,
    onClose,
    mode,
    clusterId: _clusterId,
    numericClusterId,
    clusterName = '',
    clusterDescription,
    replicationType,
    autoClusterKey,
    onSave,
    onCreate,
    onMembershipChange,
}) => {
    const isCreateMode = mode === 'create';
    const [activeTab, setActiveTab] = useState(0);

    // Details form state
    const [name, setName] = useState(clusterName);
    const [description, setDescription] = useState(clusterDescription || '');
    const [selectedReplicationType, setSelectedReplicationType] = useState(
        replicationType || '',
    );
    const [nameError, setNameError] = useState('');
    const [replicationTypeError, setReplicationTypeError] = useState('');
    const [isSaving, setIsSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [saveSuccess, setSaveSuccess] = useState(false);

    // Track the numeric ID for create mode (set after cluster is created)
    const [createdClusterId, setCreatedClusterId] = useState<number | undefined>(
        undefined,
    );

    // The effective numeric ID is either the one passed in (edit mode)
    // or the one returned after creation (create mode)
    const effectiveNumericId = numericClusterId ?? createdClusterId;

    // The effective replication type for the topology panel
    const effectiveReplicationType = selectedReplicationType || replicationType || null;

    // Reset form state when the dialog opens
    useEffect(() => {
        if (open) {
            setName(clusterName || '');
            setDescription(clusterDescription || '');
            setSelectedReplicationType(replicationType || '');
            setNameError('');
            setReplicationTypeError('');
            setSaveError('');
            setSaveSuccess(false);
            setActiveTab(0);
            setCreatedClusterId(undefined);
        }
    }, [open, clusterName, clusterDescription, replicationType]);

    const handleSave = async () => {
        const trimmed = name.trim();
        if (!trimmed) {
            setNameError('Name is required');
            return;
        }
        setNameError('');
        setReplicationTypeError('');
        setSaveError('');
        setSaveSuccess(false);

        if (isCreateMode) {
            if (!selectedReplicationType) {
                setReplicationTypeError('Replication type is required');
                return;
            }

            setIsSaving(true);
            try {
                if (onCreate) {
                    const result = await onCreate({
                        name: trimmed,
                        description: description.trim(),
                        replication_type: selectedReplicationType,
                    });
                    setCreatedClusterId(result.id);
                }
                setSaveSuccess(true);
            } catch (err) {
                setSaveError(
                    err instanceof Error ? err.message : 'Failed to create cluster',
                );
            } finally {
                setIsSaving(false);
            }
        } else {
            setIsSaving(true);
            try {
                if (onSave) {
                    await onSave({
                        name: trimmed,
                        description: description.trim(),
                        replication_type: selectedReplicationType || undefined,
                    });
                }
                setSaveSuccess(true);
            } catch (err) {
                setSaveError(
                    err instanceof Error ? err.message : 'Failed to save',
                );
            } finally {
                setIsSaving(false);
            }
        }
    };

    // In create mode, the Topology tab is only available after the
    // cluster has been created (i.e., effectiveNumericId is set)
    const topologyAvailable = !isCreateMode || effectiveNumericId != null;

    // Title text
    const dialogTitle = isCreateMode
        ? createdClusterId
            ? `New Cluster: ${name}`
            : 'Create Cluster'
        : `Cluster Settings: ${clusterName}`;

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
                        {dialogTitle}
                    </Typography>
                </Toolbar>
            </AppBar>
            <Tabs
                value={activeTab}
                onChange={(_, v) => setActiveTab(v)}
                sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
            >
                <Tab label="Details" />
                <Tab
                    label="Topology"
                    disabled={!topologyAvailable}
                />
                <Tab
                    label="Alert overrides"
                    disabled={effectiveNumericId == null}
                />
                <Tab
                    label="Probe configuration"
                    disabled={effectiveNumericId == null}
                />
                <Tab
                    label="Notification channels"
                    disabled={effectiveNumericId == null}
                />
            </Tabs>
            <Box
                sx={{
                    flex: 1,
                    overflow: 'auto',
                    p: 3,
                    bgcolor: 'background.default',
                }}
            >
                {activeTab === 0 && (
                    <Box
                        sx={{
                            maxWidth: 600,
                            mx: 'auto',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 2.5,
                        }}
                    >
                        {saveError && (
                            <Alert
                                severity="error"
                                onClose={() => setSaveError('')}
                                sx={{ borderRadius: 1 }}
                            >
                                {saveError}
                            </Alert>
                        )}
                        {saveSuccess && (
                            <Alert
                                severity="success"
                                onClose={() => setSaveSuccess(false)}
                                sx={{ borderRadius: 1 }}
                            >
                                {isCreateMode
                                    ? 'Cluster created successfully. You can now add servers on the Topology tab.'
                                    : 'Cluster settings saved successfully.'}
                            </Alert>
                        )}
                        <TextField
                            autoFocus
                            fullWidth
                            label="Name"
                            value={name}
                            onChange={(e) => {
                                setName(e.target.value);
                                setNameError('');
                            }}
                            error={!!nameError}
                            helperText={nameError}
                            required
                            disabled={isSaving || (isCreateMode && createdClusterId != null)}
                            margin="dense"
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_DEFAULT_BG_SX}
                        />
                        <TextField
                            fullWidth
                            label="Description"
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            disabled={isSaving || (isCreateMode && createdClusterId != null)}
                            margin="dense"
                            multiline
                            minRows={3}
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_DEFAULT_BG_SX}
                        />
                        <TextField
                            select
                            fullWidth
                            margin="dense"
                            disabled={isSaving || (isCreateMode && createdClusterId != null)}
                            error={!!replicationTypeError}
                            helperText={replicationTypeError || undefined}
                            label="Replication Type"
                            value={selectedReplicationType}
                            onChange={(e) => {
                                setSelectedReplicationType(e.target.value);
                                setReplicationTypeError('');
                            }}
                            InputLabelProps={{ shrink: true }}
                            sx={SELECT_FIELD_DEFAULT_BG_SX}
                        >
                            {REPLICATION_TYPES.map((rt) => (
                                <MenuItem key={rt.value} value={rt.value}>
                                    {rt.label}
                                </MenuItem>
                            ))}
                        </TextField>
                        {/* Hide Save button in create mode after cluster is already created */}
                        {!(isCreateMode && createdClusterId != null) && (
                            <Box
                                sx={{
                                    display: 'flex',
                                    gap: 1,
                                    justifyContent: 'flex-end',
                                    borderTop: '1px solid',
                                    borderColor: 'divider',
                                    pt: 2,
                                    mt: 1,
                                }}
                            >
                                <Button onClick={onClose} disabled={isSaving}>
                                    Cancel
                                </Button>
                                <Button
                                    variant="contained"
                                    onClick={handleSave}
                                    disabled={isSaving}
                                >
                                    {isSaving ? (
                                        <CircularProgress
                                            size={20}
                                            sx={{ color: 'inherit' }}
                                            aria-label="Saving"
                                        />
                                    ) : isCreateMode ? (
                                        'Create'
                                    ) : (
                                        'Save'
                                    )}
                                </Button>
                            </Box>
                        )}
                    </Box>
                )}
                {activeTab === 1 &&
                    (effectiveNumericId != null ? (
                        <TopologyPanel
                            clusterId={effectiveNumericId}
                            clusterName={name || clusterName || ''}
                            replicationType={effectiveReplicationType}
                            autoClusterKey={autoClusterKey}
                            onMembershipChange={onMembershipChange}
                        />
                    ) : (
                        <Typography
                            color="text.secondary"
                            sx={{ py: 4, textAlign: 'center' }}
                        >
                            Save the cluster details first to manage
                            topology.
                        </Typography>
                    ))}
                {activeTab === 2 &&
                    (effectiveNumericId != null ? (
                        <AlertOverridesPanel
                            scope="cluster"
                            scopeId={effectiveNumericId}
                        />
                    ) : (
                        <Typography
                            color="text.secondary"
                            sx={{ py: 4, textAlign: 'center' }}
                        >
                            Save the cluster details first to configure
                            alert overrides.
                        </Typography>
                    ))}
                {activeTab === 3 &&
                    (effectiveNumericId != null ? (
                        <ProbeOverridesPanel
                            scope="cluster"
                            scopeId={effectiveNumericId}
                        />
                    ) : (
                        <Typography
                            color="text.secondary"
                            sx={{ py: 4, textAlign: 'center' }}
                        >
                            Save the cluster details first to configure
                            probe overrides.
                        </Typography>
                    ))}
                {activeTab === 4 &&
                    (effectiveNumericId != null ? (
                        <ChannelOverridesPanel
                            scope="cluster"
                            scopeId={effectiveNumericId}
                        />
                    ) : (
                        <Typography
                            color="text.secondary"
                            sx={{ py: 4, textAlign: 'center' }}
                        >
                            Save the cluster details first to configure
                            notification channels.
                        </Typography>
                    ))}
            </Box>
        </Dialog>
    );
};

export default ClusterConfigDialog;
