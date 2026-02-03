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
} from '@mui/material';

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

    const isEditMode = mode === 'edit';

    // Reset form when dialog opens or server changes
    useEffect(() => {
        if (open) {
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

    const dialogTitle = isEditMode ? 'Edit Server' : 'Add Server';

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
            <form onSubmit={handleSubmit} noValidate>
                <DialogTitle sx={dialogTitleSx}>{dialogTitle}</DialogTitle>

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
                            <CircularProgress size={20} sx={{ color: 'inherit' }} />
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
