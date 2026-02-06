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
import { TextField, Box, Typography } from '@mui/material';
import { ServerFormData, FormErrors, FieldChangeHandler } from './ServerDialog.types';
import {
    textFieldSx,
    sectionLabelSx,
} from './ServerDialog.styles';

interface ConnectionFieldsProps {
    formData: ServerFormData;
    errors: FormErrors;
    isEditMode: boolean;
    isSaving: boolean;
    onFieldChange: FieldChangeHandler;
}

/**
 * ConnectionFields renders the connection details section of the server form.
 * Includes fields for name, host, port, database, username, and password.
 */
const ConnectionFields: React.FC<ConnectionFieldsProps> = ({
    formData,
    errors,
    isEditMode,
    isSaving,
    onFieldChange,
}) => {
    const fields = (
        <>
            {/* Name field */}
            <TextField
                autoFocus
                fullWidth
                label="Name"
                value={formData.name}
                onChange={(e) => onFieldChange('name', e.target.value)}
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
                    onChange={(e) => onFieldChange('host', e.target.value)}
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
                    onChange={(e) => onFieldChange('port', e.target.value)}
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
                onChange={(e) => onFieldChange('database', e.target.value)}
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
                    onChange={(e) => onFieldChange('username', e.target.value)}
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
                    onChange={(e) => onFieldChange('password', e.target.value)}
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
        </>
    );

    return (
        <>
            {/* Connection Details Section Label */}
            <Typography variant="subtitle2" sx={sectionLabelSx}>
                Connection Details
            </Typography>
            {fields}
        </>
    );
};

export default ConnectionFields;
