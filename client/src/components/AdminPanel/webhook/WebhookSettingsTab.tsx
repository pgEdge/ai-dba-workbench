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
import {
    Box,
    TextField,
    Switch,
    FormControlLabel,
    MenuItem,
    Typography,
} from '@mui/material';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import { WebhookFormState } from './webhookTypes';
import { HTTP_METHODS } from './webhookHelpers';

export interface WebhookSettingsTabProps {
    form: WebhookFormState;
    onChange: (field: keyof WebhookFormState, value: string | boolean) => void;
    saving: boolean;
    visible: boolean;
}

/**
 * Settings tab for the webhook channel dialog.
 * Contains name, description, endpoint URL, HTTP method, and toggle switches.
 */
const WebhookSettingsTab: React.FC<WebhookSettingsTabProps> = ({
    form,
    onChange,
    saving,
    visible,
}) => {
    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            <TextField
                autoFocus
                fullWidth
                label="Name"
                value={form.name}
                onChange={(e) => onChange('name', e.target.value)}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="Description"
                value={form.description}
                onChange={(e) => onChange('description', e.target.value)}
                disabled={saving}
                margin="dense"
                multiline
                rows={2}
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="Endpoint URL"
                value={form.endpoint_url}
                onChange={(e) => onChange('endpoint_url', e.target.value)}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                select
                label="HTTP Method"
                value={form.http_method}
                onChange={(e) => onChange('http_method', e.target.value)}
                disabled={saving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
                sx={SELECT_FIELD_SX}
            >
                {HTTP_METHODS.map((method) => (
                    <MenuItem key={method} value={method}>
                        {method}
                    </MenuItem>
                ))}
            </TextField>
            <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                <FormControlLabel
                    sx={{ ml: 0, gap: 1 }}
                    control={
                        <Switch
                            checked={form.enabled}
                            onChange={(e) => onChange('enabled', e.target.checked)}
                            disabled={saving}
                            inputProps={{ 'aria-label': 'Toggle channel enabled' }}
                        />
                    }
                    label="Enabled"
                />
                <FormControlLabel
                    sx={{ ml: 0, gap: 1 }}
                    control={
                        <Switch
                            checked={form.is_estate_default}
                            onChange={(e) => onChange('is_estate_default', e.target.checked)}
                            disabled={saving}
                            inputProps={{ 'aria-label': 'Toggle estate default' }}
                        />
                    }
                    label={
                        <Box>
                            <Typography variant="body1">Estate Default</Typography>
                            <Typography variant="caption" color="text.secondary">
                                When enabled, this channel is active for all servers by default
                            </Typography>
                        </Box>
                    }
                />
            </Box>
        </Box>
    );
};

export default WebhookSettingsTab;
