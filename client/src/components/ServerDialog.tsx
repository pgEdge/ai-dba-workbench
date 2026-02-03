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
    TextField,
    Button,
    Box,
    FormControl,
    InputLabel,
    Select,
    MenuItem,
    FormControlLabel,
    Checkbox,
    Alert,
    Accordion,
    AccordionSummary,
    AccordionDetails,
    Typography,
    CircularProgress,
    SelectChangeEvent,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';

// SSL mode options for PostgreSQL connections
const SSL_MODES = [
    { value: 'disable', label: 'Disable' },
    { value: 'allow', label: 'Allow' },
    { value: 'prefer', label: 'Prefer' },
    { value: 'require', label: 'Require' },
    { value: 'verify-ca', label: 'Verify CA' },
    { value: 'verify-full', label: 'Verify Full' },
];

// Default form values for a new server
const getDefaultFormData = () => ({
    name: '',
    host: '',
    port: 5432,
    database: 'postgres',
    username: 'postgres',
    password: '',
    ssl_mode: 'prefer',
    ssl_cert_path: '',
    ssl_key_path: '',
    ssl_root_cert_path: '',
    is_monitored: true,
    is_shared: false,
});

// --- Style constants (Issue 23) ---

const textFieldSx = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&:hover .MuiOutlinedInput-notchedOutline': {
            borderColor: 'grey.400',
        },
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: 'primary.main',
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: 'primary.main',
    },
};

const dialogPaperSx = {
    borderRadius: 2,
};

const dialogTitleSx = {
    fontWeight: 600,
    color: 'text.primary',
    pb: 1,
};

const sectionLabelSx = {
    color: 'text.secondary',
    mb: 1,
    mt: 1,
    textTransform: 'uppercase',
    fontSize: '0.75rem',
    letterSpacing: '0.05em',
};

const optionsSectionLabelSx = {
    ...sectionLabelSx,
    mt: 2,
};

const sslAccordionSx = {
    mt: 2,
    '&:before': { display: 'none' },
    border: '1px solid',
    borderColor: 'grey.200',
    borderRadius: '8px !important',
};

const accordionSummarySx = {
    minHeight: 48,
    '&.Mui-expanded': { minHeight: 48 },
};

const sslLabelSx = {
    color: 'text.secondary',
    textTransform: 'uppercase',
    fontSize: '0.75rem',
    letterSpacing: '0.05em',
};

const sslModeLabelSx = {
    '&.Mui-focused': { color: 'primary.main' },
};

const checkboxSx = {
    '&.Mui-checked': {
        color: 'primary.main',
    },
};

const formControlLabelSx = {
    '& .MuiFormControlLabel-label': {
        fontSize: '0.875rem',
        color: 'text.primary',
    },
};

const cancelButtonSx = {
    color: 'text.secondary',
    textTransform: 'none',
    fontWeight: 500,
};

const getSaveButtonSx = (theme: Theme) => ({
    textTransform: 'none',
    fontWeight: 600,
    minWidth: 80,
    background: theme.palette.primary.main,
    boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
    '&:hover': {
        background: theme.palette.primary.dark,
        boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
    },
    '&.Mui-disabled': {
        background: theme.palette.grey[200],
        color: theme.palette.grey[400],
    },
});

const dialogActionsSx = {
    px: 3,
    pb: 2,
};

// --- Component ---

interface ServerFormData {
    name: string;
    host: string;
    port: number | string;
    database: string;
    username: string;
    password: string;
    ssl_mode: string;
    ssl_cert_path: string;
    ssl_key_path: string;
    ssl_root_cert_path: string;
    is_monitored: boolean;
    is_shared: boolean;
}

interface ServerEditData {
    name?: string;
    host?: string;
    port?: number;
    database_name?: string;
    username?: string;
    ssl_mode?: string;
    ssl_cert_path?: string;
    ssl_key_path?: string;
    ssl_root_cert_path?: string;
    is_monitored?: boolean;
    is_shared?: boolean;
    [key: string]: unknown;
}

interface ServerDialogProps {
    open: boolean;
    onClose: () => void;
    onSave: (data: Record<string, unknown>) => Promise<void>;
    mode?: 'create' | 'edit';
    server?: ServerEditData | null;
    isSuperuser?: boolean;
}

/**
 * ServerDialog - Dialog component for adding and editing server connections
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
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [isSaving, setIsSaving] = useState(false);
    const [sslExpanded, setSslExpanded] = useState(false);

    // Reset form when dialog opens or server changes
    useEffect(() => {
        if (open) {
            if (mode === 'edit' && server) {
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
                const hasSSLPaths = server.ssl_cert_path ||
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
    }, [open, mode, server]);

    // Update a single form field and clear its error
    const handleFieldChange = (field: keyof ServerFormData, value: string | number | boolean) => {
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

    // Validate all form fields
    const validate = () => {
        const newErrors: Record<string, string> = {};

        // Name validation
        const trimmedName = formData.name.trim();
        if (!trimmedName) {
            newErrors.name = 'Name is required';
        }

        // Host validation
        const trimmedHost = formData.host.trim();
        if (!trimmedHost) {
            newErrors.host = 'Host is required';
        }

        // Port validation
        const port = parseInt(formData.port, 10);
        if (isNaN(port) || port < 1 || port > 65535) {
            newErrors.port = 'Port must be between 1 and 65535';
        }

        // Database validation
        if (!formData.database.trim()) {
            newErrors.database = 'Maintenance database is required';
        }

        // Username validation
        if (!formData.username.trim()) {
            newErrors.username = 'Username is required';
        }

        // Password validation - required only in create mode
        if (mode === 'create' && !formData.password) {
            newErrors.password = 'Password is required';
        }

        setErrors(newErrors);
        return Object.keys(newErrors).length === 0;
    };

    // Handle form submission
    const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        setSubmitError(null);

        if (!validate()) {
            return;
        }

        setIsSaving(true);

        try {
            // Prepare data for save, trimming string values
            const saveData = {
                name: formData.name.trim(),
                host: formData.host.trim(),
                port: parseInt(formData.port, 10),
                database_name: formData.database.trim(),
                username: formData.username.trim(),
                ssl_mode: formData.ssl_mode,
                ssl_cert_path: formData.ssl_cert_path.trim(),
                ssl_key_path: formData.ssl_key_path.trim(),
                ssl_root_cert_path: formData.ssl_root_cert_path.trim(),
                is_monitored: formData.is_monitored,
                is_shared: formData.is_shared,
            };

            // Only include password if provided
            if (formData.password) {
                saveData.password = formData.password;
            }

            await onSave(saveData);
            onClose();
        } catch (err: unknown) {
            setSubmitError(err instanceof Error ? err.message : 'Failed to save server');
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

    const isEditMode = mode === 'edit';
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
                <DialogTitle sx={dialogTitleSx}>
                    {dialogTitle}
                </DialogTitle>

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

                    {/* Connection Details Section */}
                    <Typography
                        variant="subtitle2"
                        sx={sectionLabelSx}
                    >
                        Connection Details
                    </Typography>

                    {/* Name field */}
                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={formData.name}
                        onChange={(e) => handleFieldChange('name', e.target.value)}
                        error={!!errors.name}
                        helperText={errors.name}
                        required
                        disabled={isSaving}
                        margin="dense"
                        sx={textFieldSx}
                    />

                    {/* Host and Port side by side */}
                    <Box sx={{ display: 'flex', gap: 2, mt: 1 }}>
                        <TextField
                            fullWidth
                            label="Host"
                            value={formData.host}
                            onChange={(e) => handleFieldChange('host', e.target.value)}
                            error={!!errors.host}
                            helperText={errors.host}
                            required
                            disabled={isSaving}
                            margin="dense"
                            sx={{ flex: 2, ...textFieldSx }}
                        />
                        <TextField
                            fullWidth
                            label="Port"
                            type="number"
                            value={formData.port}
                            onChange={(e) => handleFieldChange('port', e.target.value)}
                            error={!!errors.port}
                            helperText={errors.port}
                            required
                            disabled={isSaving}
                            margin="dense"
                            inputProps={{ min: 1, max: 65535 }}
                            sx={{ flex: 1, ...textFieldSx }}
                        />
                    </Box>

                    {/* Maintenance Database */}
                    <TextField
                        fullWidth
                        label="Maintenance Database"
                        value={formData.database}
                        onChange={(e) => handleFieldChange('database', e.target.value)}
                        error={!!errors.database}
                        helperText={errors.database}
                        required
                        disabled={isSaving}
                        margin="dense"
                        sx={{ mt: 1, ...textFieldSx }}
                    />

                    {/* Username and Password side by side */}
                    <Box sx={{ display: 'flex', gap: 2, mt: 1 }}>
                        <TextField
                            fullWidth
                            label="Username"
                            value={formData.username}
                            onChange={(e) => handleFieldChange('username', e.target.value)}
                            error={!!errors.username}
                            helperText={errors.username}
                            required
                            disabled={isSaving}
                            margin="dense"
                            autoComplete="off"
                            sx={{ flex: 1, ...textFieldSx }}
                        />
                        <TextField
                            fullWidth
                            label="Password"
                            type="password"
                            value={formData.password}
                            onChange={(e) => handleFieldChange('password', e.target.value)}
                            error={!!errors.password}
                            helperText={
                                errors.password ||
                                (isEditMode ? 'Leave blank to keep unchanged' : '')
                            }
                            required={!isEditMode}
                            disabled={isSaving}
                            margin="dense"
                            autoComplete="new-password"
                            sx={{ flex: 1, ...textFieldSx }}
                        />
                    </Box>

                    {/* SSL Settings Accordion */}
                    <Accordion
                        expanded={sslExpanded}
                        onChange={(e, expanded) => setSslExpanded(expanded)}
                        elevation={0}
                        sx={sslAccordionSx}
                    >
                        <AccordionSummary
                            expandIcon={<ExpandMoreIcon />}
                            sx={accordionSummarySx}
                        >
                            <Typography
                                variant="subtitle2"
                                sx={sslLabelSx}
                            >
                                SSL Settings
                            </Typography>
                        </AccordionSummary>
                        <AccordionDetails sx={{ pt: 0 }}>
                            {/* SSL Mode */}
                            <FormControl fullWidth margin="dense" sx={textFieldSx}>
                                <InputLabel sx={sslModeLabelSx}>
                                    SSL Mode
                                </InputLabel>
                                <Select
                                    value={formData.ssl_mode}
                                    label="SSL Mode"
                                    onChange={(e) => handleFieldChange('ssl_mode', e.target.value)}
                                    disabled={isSaving}
                                >
                                    {SSL_MODES.map((mode) => (
                                        <MenuItem key={mode.value} value={mode.value}>
                                            {mode.label}
                                        </MenuItem>
                                    ))}
                                </Select>
                            </FormControl>

                            {/* SSL Certificate Path */}
                            <TextField
                                fullWidth
                                label="SSL Certificate Path"
                                value={formData.ssl_cert_path}
                                onChange={(e) => handleFieldChange('ssl_cert_path', e.target.value)}
                                disabled={isSaving}
                                margin="dense"
                                sx={textFieldSx}
                            />

                            {/* SSL Key Path */}
                            <TextField
                                fullWidth
                                label="SSL Key Path"
                                value={formData.ssl_key_path}
                                onChange={(e) => handleFieldChange('ssl_key_path', e.target.value)}
                                disabled={isSaving}
                                margin="dense"
                                sx={textFieldSx}
                            />

                            {/* SSL Root Certificate Path */}
                            <TextField
                                fullWidth
                                label="SSL Root Certificate Path"
                                value={formData.ssl_root_cert_path}
                                onChange={(e) => handleFieldChange('ssl_root_cert_path', e.target.value)}
                                disabled={isSaving}
                                margin="dense"
                                sx={textFieldSx}
                            />
                        </AccordionDetails>
                    </Accordion>

                    {/* Options Section */}
                    <Typography
                        variant="subtitle2"
                        sx={optionsSectionLabelSx}
                    >
                        Options
                    </Typography>

                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
                        <FormControlLabel
                            control={
                                <Checkbox
                                    checked={formData.is_monitored}
                                    onChange={(e) => handleFieldChange('is_monitored', e.target.checked)}
                                    disabled={isSaving}
                                    sx={checkboxSx}
                                />
                            }
                            label="Monitor this server"
                            sx={formControlLabelSx}
                        />

                        {isSuperuser && (
                            <FormControlLabel
                                control={
                                    <Checkbox
                                        checked={formData.is_shared}
                                        onChange={(e) => handleFieldChange('is_shared', e.target.checked)}
                                        disabled={isSaving}
                                        sx={checkboxSx}
                                    />
                                }
                                label="Share with all users"
                                sx={formControlLabelSx}
                            />
                        )}
                    </Box>
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
