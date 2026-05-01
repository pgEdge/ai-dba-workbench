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
import { Box, TextField, MenuItem } from '@mui/material';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import { AUTH_TYPES } from './webhookHelpers';

export interface WebhookAuthTabProps {
    authType: string;
    authFields: Record<string, string>;
    onAuthTypeChange: (newType: string) => void;
    onAuthFieldChange: (field: string, value: string) => void;
    saving: boolean;
    visible: boolean;
    /**
     * True when the channel already has credentials stored on the
     * server. The server redacts the actual values, so we surface a
     * placeholder hint instead of leaving secret fields looking like
     * plain "empty" inputs (which would be misleading).
     */
    credentialsConfigured?: boolean;
}

/**
 * Authentication tab for the webhook channel dialog.
 * Allows selecting auth type and configuring credentials.
 */
const WebhookAuthTab: React.FC<WebhookAuthTabProps> = ({
    authType,
    authFields,
    onAuthTypeChange,
    onAuthFieldChange,
    saving,
    visible,
    credentialsConfigured = false,
}) => {
    // Hint shown only when editing an existing channel that has
    // credentials configured server-side. The server will preserve the
    // stored value if the user submits the form with these fields blank.
    const placeholder = credentialsConfigured
        ? 'Leave blank to keep existing'
        : '';
    const helperText = credentialsConfigured
        ? 'Existing credentials are configured. Leave blank to keep them unchanged.'
        : undefined;

    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            <TextField
                fullWidth
                select
                label="Auth Type"
                value={authType}
                onChange={(e) => {
                    onAuthTypeChange(e.target.value);
                }}
                disabled={saving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
                sx={SELECT_FIELD_SX}
            >
                {AUTH_TYPES.map((type) => (
                    <MenuItem key={type.value} value={type.value}>
                        {type.label}
                    </MenuItem>
                ))}
            </TextField>

            {/* Basic auth fields */}
            {authType === 'basic' && (
                <>
                    <TextField
                        fullWidth
                        label="Username"
                        value={authFields.username || ''}
                        onChange={(e) => {
                            onAuthFieldChange('username', e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        placeholder={placeholder}
                        helperText={helperText}
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Password"
                        type="password"
                        value={authFields.password || ''}
                        onChange={(e) => {
                            onAuthFieldChange('password', e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        placeholder={placeholder}
                        InputLabelProps={{ shrink: true }}
                    />
                </>
            )}

            {/* Bearer token field */}
            {authType === 'bearer' && (
                <TextField
                    fullWidth
                    label="Token"
                    type="password"
                    autoComplete="off"
                    value={authFields.token || ''}
                    onChange={(e) => {
                        onAuthFieldChange('token', e.target.value);
                    }}
                    disabled={saving}
                    margin="dense"
                    placeholder={placeholder}
                    helperText={helperText}
                    InputLabelProps={{ shrink: true }}
                />
            )}

            {/* API Key fields */}
            {authType === 'api_key' && (
                <>
                    <TextField
                        fullWidth
                        label="Header Name"
                        value={authFields.headerName || ''}
                        onChange={(e) => {
                            onAuthFieldChange('headerName', e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        placeholder={placeholder}
                        helperText={helperText}
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="API Key Value"
                        type="password"
                        autoComplete="off"
                        value={authFields.apiKeyValue || ''}
                        onChange={(e) => {
                            onAuthFieldChange('apiKeyValue', e.target.value);
                        }}
                        disabled={saving}
                        margin="dense"
                        placeholder={placeholder}
                        InputLabelProps={{ shrink: true }}
                    />
                </>
            )}
        </Box>
    );
};

export default WebhookAuthTab;
