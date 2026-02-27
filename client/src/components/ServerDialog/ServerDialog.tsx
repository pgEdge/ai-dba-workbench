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
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    Alert,
    CircularProgress,
    Tabs,
    Tab,
    AppBar,
    Toolbar,
    IconButton,
    Typography,
    Box,
    Slide,
} from '@mui/material';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';

import {
    ServerFormData,
    ServerDialogProps,
    getDefaultFormData,
    FormErrors,
    ClusterFieldsValue,
} from './ServerDialog.types';
import {
    dialogPaperSx,
    dialogTitleSx,
    cancelButtonSx,
    getSaveButtonSx,
    dialogActionsSx,
    editFormContainerSx,
    stickyFooterSx,
} from './ServerDialog.styles';
import { validateServerForm, prepareSaveData } from './ServerDialog.validation';
import ConnectionFields from './ConnectionFields';
import SSLSettings from './SSLSettings';
import OptionsSection from './OptionsSection';
import ClusterFields from './ClusterFields';
import AlertOverridesPanel from '../AlertOverridesPanel';
import ProbeOverridesPanel from '../ProbeOverridesPanel';
import ChannelOverridesPanel from '../ChannelOverridesPanel';
import { apiPost, apiPut } from '../../utils/apiClient';

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

/**
 * ServerDialog - Dialog component for adding and editing server connections.
 */
const ServerDialog: React.FC<ServerDialogProps> = ({
    open,
    onClose,
    onSave,
    mode = 'create',
    server = null,
    isSuperuser = false,
    onOpenClusterConfig,
}) => {
    const [formData, setFormData] = useState<ServerFormData>(getDefaultFormData());
    const [errors, setErrors] = useState<FormErrors>({});
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [isSaving, setIsSaving] = useState(false);
    const [saveSuccess, setSaveSuccess] = useState(false);
    const [sslExpanded, setSslExpanded] = useState(false);
    const [activeTab, setActiveTab] = useState(0);
    const [clusterValue, setClusterValue] = useState<ClusterFieldsValue>({
        clusterId: null,
        role: null,
        membershipSource: 'auto',
    });

    const isEditMode = mode === 'edit';

    // Reset form when dialog opens or server changes
    useEffect(() => {
        if (open) {
            setActiveTab(0);
            if (isEditMode && server) {
                setFormData({
                    name: server.name || '',
                    description: server.description || '',
                    host: server.host || '',
                    port: server.port || 5432,
                    database: server.database_name || '',
                    username: server.username || '',
                    password: '', // Never pre-populate password
                    ssl_mode: server.ssl_mode || 'prefer',
                    ssl_cert_path: server.ssl_cert_path || '',
                    ssl_key_path: server.ssl_key_path || '',
                    ssl_root_cert_path: server.ssl_root_cert_path || '',
                    is_monitored: server.is_monitored !== false,
                    is_shared: server.is_shared || false,
                });
                // Expand SSL section if any SSL paths are set
                const hasSSLPaths =
                    server.ssl_cert_path ||
                    server.ssl_key_path ||
                    server.ssl_root_cert_path;
                setSslExpanded(!!hasSSLPaths);
            } else {
                setFormData(getDefaultFormData());
                setSslExpanded(false);
            }
            setClusterValue({
                clusterId: null,
                role: null,
                membershipSource: 'auto',
            });
            setErrors({});
            setSubmitError(null);
            setSaveSuccess(false);
        }
    }, [open, isEditMode, server]);

    // Update a single form field and clear its error
    const handleFieldChange = (
        field: keyof ServerFormData,
        value: string | number | boolean
    ) => {
        setFormData((prev) => ({ ...prev, [field]: value }));
        if (errors[field]) {
            setErrors((prev) => {
                const newErrors = { ...prev };
                delete newErrors[field];
                return newErrors;
            });
        }
        // Clear submit error when user makes changes
        if (submitError) {
            setSubmitError(null);
        }
    };

    // Handle form submission
    const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        setSubmitError(null);
        setSaveSuccess(false);

        const validationErrors = validateServerForm(formData, isEditMode);
        if (Object.keys(validationErrors).length > 0) {
            setErrors(validationErrors);
            return;
        }

        setIsSaving(true);

        try {
            const saveData = prepareSaveData(formData);
            const result = await onSave(saveData);

            // In create mode, apply cluster assignment if set
            if (
                !isEditMode &&
                (clusterValue.clusterId !== null ||
                    clusterValue.newCluster)
            ) {
                // Attempt to get the new connection ID from the result
                const newId = (result as Record<string, unknown>)?.id as
                    | number
                    | undefined;
                if (newId) {
                    let clusterId = clusterValue.clusterId;

                    // Create a new cluster first if needed
                    if (clusterValue.newCluster) {
                        const created = await apiPost<{
                            id: number;
                            name: string;
                        }>('/api/v1/clusters', {
                            name: clusterValue.newCluster.name.trim(),
                            replication_type:
                                clusterValue.newCluster.replication_type,
                        });
                        clusterId = created.id;
                    }

                    await apiPut(
                        `/api/v1/connections/${newId}/cluster`,
                        {
                            cluster_id: clusterId,
                            role: clusterValue.role,
                            membership_source: 'manual',
                        },
                    );
                }
            }

            setSaveSuccess(true);
        } catch (err: unknown) {
            setSubmitError(
                err instanceof Error ? err.message : 'Failed to save server'
            );
        } finally {
            setIsSaving(false);
        }
    };

    // Handle cancel/close
    const handleClose = () => {
        if (!isSaving) {
            onClose();
        }
    };

    if (isEditMode) {
        return (
            <Dialog
                fullScreen
                open={open}
                onClose={handleClose}
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
                            onClick={handleClose}
                            aria-label="close edit server"
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
                            Server Settings: {server?.name}
                        </Typography>
                    </Toolbar>
                </AppBar>
                <Tabs
                    value={activeTab}
                    onChange={(_, v) => setActiveTab(v)}
                    sx={{
                        px: 3,
                        borderBottom: 1,
                        borderColor: 'divider',
                    }}
                >
                    <Tab label="Details" />
                    <Tab label="Cluster" />
                    <Tab label="Alert overrides" />
                    <Tab label="Probe configuration" />
                    <Tab label="Notification channels" />
                </Tabs>
                <Box sx={{ flex: 1, overflow: 'auto', p: 3, bgcolor: 'background.default' }}>
                    {activeTab === 0 && (
                        <Box sx={editFormContainerSx}>
                            <form onSubmit={handleSubmit} noValidate>
                                {submitError && (
                                    <Alert
                                        severity="error"
                                        sx={{ mb: 0.5, borderRadius: 1 }}
                                        onClose={() => setSubmitError(null)}
                                    >
                                        {submitError}
                                    </Alert>
                                )}
                                {saveSuccess && (
                                    <Alert
                                        severity="success"
                                        sx={{ mb: 0.5, borderRadius: 1 }}
                                        onClose={() => setSaveSuccess(false)}
                                    >
                                        Server settings saved successfully.
                                    </Alert>
                                )}

                                <ConnectionFields
                                    formData={formData}
                                    errors={errors}
                                    isEditMode={isEditMode}
                                    isSaving={isSaving}
                                    onFieldChange={handleFieldChange}
                                />

                                <SSLSettings
                                    formData={formData}
                                    isEditMode={isEditMode}
                                    isSaving={isSaving}
                                    expanded={sslExpanded}
                                    onExpandedChange={setSslExpanded}
                                    onFieldChange={handleFieldChange}
                                />

                                <OptionsSection
                                    formData={formData}
                                    isSaving={isSaving}
                                    isSuperuser={isSuperuser}
                                    onFieldChange={handleFieldChange}
                                />

                                <Box sx={stickyFooterSx}>
                                    <Button
                                        onClick={handleClose}
                                        disabled={isSaving}
                                        sx={cancelButtonSx}
                                    >
                                        Cancel
                                    </Button>
                                    <Button
                                        type="submit"
                                        variant="contained"
                                        disabled={isSaving}
                                        sx={getSaveButtonSx}
                                    >
                                        {isSaving ? (
                                            <CircularProgress
                                                size={20}
                                                sx={{ color: 'inherit' }}
                                                aria-label="Saving"
                                            />
                                        ) : (
                                            'Save'
                                        )}
                                    </Button>
                                </Box>
                            </form>
                        </Box>
                    )}
                    <Box
                        sx={{
                            ...editFormContainerSx,
                            display: activeTab === 1 ? 'flex' : 'none',
                        }}
                    >
                        <ClusterFields
                            mode="edit"
                            serverId={server?.id as number}
                            onOpenClusterConfig={(clusterId, clusterName) => {
                                onClose();
                                onOpenClusterConfig?.(clusterId, clusterName);
                            }}
                        />
                    </Box>
                    {activeTab === 2 && (
                        <AlertOverridesPanel
                            scope="server"
                            scopeId={server?.id as number}
                        />
                    )}
                    {activeTab === 3 && (
                        <ProbeOverridesPanel
                            scope="server"
                            scopeId={server?.id as number}
                        />
                    )}
                    {activeTab === 4 && (
                        <ChannelOverridesPanel
                            scope="server"
                            scopeId={server?.id as number}
                        />
                    )}
                </Box>
            </Dialog>
        );
    }

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{
                sx: dialogPaperSx,
            }}
        >
            <DialogTitle sx={dialogTitleSx}>Add Server</DialogTitle>
            <form onSubmit={handleSubmit} noValidate>
                <DialogContent>
                    {submitError && (
                        <Alert
                            severity="error"
                            sx={{ mb: 2, borderRadius: 1 }}
                            onClose={() => setSubmitError(null)}
                        >
                            {submitError}
                        </Alert>
                    )}
                    {saveSuccess && (
                        <Alert
                            severity="success"
                            sx={{ mb: 2, borderRadius: 1 }}
                            onClose={() => setSaveSuccess(false)}
                        >
                            Server settings saved successfully.
                        </Alert>
                    )}

                    <ConnectionFields
                        formData={formData}
                        errors={errors}
                        isEditMode={isEditMode}
                        isSaving={isSaving}
                        onFieldChange={handleFieldChange}
                    />

                    <SSLSettings
                        formData={formData}
                        isEditMode={isEditMode}
                        isSaving={isSaving}
                        expanded={sslExpanded}
                        onExpandedChange={setSslExpanded}
                        onFieldChange={handleFieldChange}
                    />

                    <OptionsSection
                        formData={formData}
                        isSaving={isSaving}
                        isSuperuser={isSuperuser}
                        onFieldChange={handleFieldChange}
                    />

                    <ClusterFields
                        mode="create"
                        value={clusterValue}
                        onChange={setClusterValue}
                    />
                </DialogContent>

                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={handleClose}
                        disabled={isSaving}
                        sx={cancelButtonSx}
                    >
                        Cancel
                    </Button>
                    <Button
                        type="submit"
                        variant="contained"
                        disabled={isSaving}
                        sx={getSaveButtonSx}
                    >
                        {isSaving ? (
                            <CircularProgress
                                size={20}
                                sx={{ color: 'inherit' }}
                                aria-label="Saving"
                            />
                        ) : (
                            'Save'
                        )}
                    </Button>
                </DialogActions>
            </form>
        </Dialog>
    );
};

export default ServerDialog;
