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
} from './ServerDialog.types';
import {
    dialogPaperSx,
    dialogTitleSx,
    cancelButtonSx,
    getSaveButtonSx,
    dialogActionsSx,
} from './ServerDialog.styles';
import { validateServerForm, prepareSaveData } from './ServerDialog.validation';
import ConnectionFields from './ConnectionFields';
import SSLSettings from './SSLSettings';
import OptionsSection from './OptionsSection';
import AlertOverridesPanel from '../AlertOverridesPanel';

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
}) => {
    const [formData, setFormData] = useState<ServerFormData>(getDefaultFormData());
    const [errors, setErrors] = useState<FormErrors>({});
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [isSaving, setIsSaving] = useState(false);
    const [sslExpanded, setSslExpanded] = useState(false);
    const [activeTab, setActiveTab] = useState(0);

    const isEditMode = mode === 'edit';

    // Reset form when dialog opens or server changes
    useEffect(() => {
        if (open) {
            setActiveTab(0);
            if (isEditMode && server) {
                setFormData({
                    name: server.name || '',
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
            setErrors({});
            setSubmitError(null);
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

        const validationErrors = validateServerForm(formData, isEditMode);
        if (Object.keys(validationErrors).length > 0) {
            setErrors(validationErrors);
            return;
        }

        setIsSaving(true);

        try {
            const saveData = prepareSaveData(formData);
            await onSave(saveData);
            onClose();
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
                            Edit Server
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
                    <Tab label="Connection" />
                    <Tab label="Alert Overrides" />
                </Tabs>
                <Box sx={{ flex: 1, overflow: 'auto', p: 3 }}>
                    {activeTab === 0 ? (
                        <Box sx={{ maxWidth: 600 }}>
                            <form onSubmit={handleSubmit} noValidate>
                                {submitError && (
                                    <Alert
                                        severity="error"
                                        sx={{ mb: 2, borderRadius: 1 }}
                                        onClose={() => setSubmitError(null)}
                                    >
                                        {submitError}
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

                                <Box sx={{
                                    mt: 3,
                                    display: 'flex',
                                    gap: 1,
                                    justifyContent: 'flex-end',
                                }}>
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
                                            />
                                        ) : (
                                            'Save'
                                        )}
                                    </Button>
                                </Box>
                            </form>
                        </Box>
                    ) : (
                        <AlertOverridesPanel
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

                    <ConnectionFields
                        formData={formData}
                        errors={errors}
                        isEditMode={isEditMode}
                        isSaving={isSaving}
                        onFieldChange={handleFieldChange}
                    />

                    <SSLSettings
                        formData={formData}
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
