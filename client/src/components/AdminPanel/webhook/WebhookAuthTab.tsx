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
}) => {
    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            <TextField
                fullWidth
                select
                label="Auth Type"
                value={authType}
                onChange={(e) => onAuthTypeChange(e.target.value)}
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
                        onChange={(e) => onAuthFieldChange('username', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="Password"
                        type="password"
                        value={authFields.password || ''}
                        onChange={(e) => onAuthFieldChange('password', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                </>
            )}

            {/* Bearer token field */}
            {authType === 'bearer' && (
                <TextField
                    fullWidth
                    label="Token"
                    value={authFields.token || ''}
                    onChange={(e) => onAuthFieldChange('token', e.target.value)}
                    disabled={saving}
                    margin="dense"
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
                        onChange={(e) => onAuthFieldChange('headerName', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                    <TextField
                        fullWidth
                        label="API Key Value"
                        value={authFields.apiKeyValue || ''}
                        onChange={(e) => onAuthFieldChange('apiKeyValue', e.target.value)}
                        disabled={saving}
                        margin="dense"
                        InputLabelProps={{ shrink: true }}
                    />
                </>
            )}
        </Box>
    );
};

export default WebhookAuthTab;
