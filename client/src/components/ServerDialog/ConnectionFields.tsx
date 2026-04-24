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
import { TextField, Box, Typography } from '@mui/material';
import type { ServerFormData, FormErrors, FieldChangeHandler } from './ServerDialog.types';
import { sectionLabelSx } from './ServerDialog.styles';
import { getSelectFieldSx } from '../shared/formStyles';

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
    const selectFieldSx = getSelectFieldSx(isEditMode ? 'background.default' : 'background.paper');

    return (
        <>
            {/* Name field - above section heading */}
            <TextField
                autoFocus
                fullWidth
                label="Name"
                value={formData.name}
                onChange={(e) => { onFieldChange('name', e.target.value); }}
                error={!!errors.name}
                helperText={errors.name}
                required
                disabled={isSaving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
                sx={selectFieldSx}
            />

            {/* Description field - optional */}
            <TextField
                fullWidth
                multiline
                minRows={2}
                label="Description"
                value={formData.description}
                onChange={(e) => { onFieldChange('description', e.target.value); }}
                disabled={isSaving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
                sx={selectFieldSx}
            />

            {/* Connection Details Section Label */}
            <Typography variant="subtitle2" sx={sectionLabelSx}>
                Connection Details
            </Typography>

            {/* Host and Port side by side */}
            <Box sx={{ display: 'flex', gap: 2, mt: 1 }}>
                <TextField
                    fullWidth
                    label="Host"
                    value={formData.host}
                    onChange={(e) => { onFieldChange('host', e.target.value); }}
                    error={!!errors.host}
                    helperText={errors.host}
                    required
                    disabled={isSaving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={{ flex: 2, ...selectFieldSx }}
                />
                <TextField
                    fullWidth
                    label="Port"
                    type="number"
                    value={formData.port}
                    onChange={(e) => { onFieldChange('port', e.target.value); }}
                    error={!!errors.port}
                    helperText={errors.port}
                    required
                    disabled={isSaving}
                    margin="dense"
                    inputProps={{ min: 1, max: 65535 }}
                    InputLabelProps={{ shrink: true }}
                    sx={{ flex: 1, ...selectFieldSx }}
                />
            </Box>

            {/* Maintenance Database */}
            <TextField
                fullWidth
                label="Maintenance Database"
                value={formData.database}
                onChange={(e) => { onFieldChange('database', e.target.value); }}
                error={!!errors.database}
                helperText={errors.database}
                required
                disabled={isSaving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
                sx={{ mt: 1, ...selectFieldSx }}
            />

            {/* Username and Password side by side */}
            <Box sx={{ display: 'flex', gap: 2, mt: 1 }}>
                <TextField
                    fullWidth
                    label="Username"
                    value={formData.username}
                    onChange={(e) => { onFieldChange('username', e.target.value); }}
                    error={!!errors.username}
                    helperText={errors.username}
                    required
                    disabled={isSaving}
                    margin="dense"
                    autoComplete="off"
                    InputLabelProps={{ shrink: true }}
                    sx={{ flex: 1, ...selectFieldSx }}
                />
                <TextField
                    fullWidth
                    label="Password"
                    type="password"
                    value={formData.password}
                    onChange={(e) => { onFieldChange('password', e.target.value); }}
                    error={!!errors.password}
                    helperText={
                        errors.password ||
                        (isEditMode ? 'Leave blank to keep unchanged' : '')
                    }
                    required={!isEditMode}
                    disabled={isSaving}
                    margin="dense"
                    autoComplete="new-password"
                    InputLabelProps={{ shrink: true }}
                    sx={{ flex: 1, ...selectFieldSx }}
                />
            </Box>
        </>
    );
};

export default ConnectionFields;
