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
    Typography,
    FormControlLabel,
} from '@mui/material';
import { EmailFormState } from './emailTypes';

export interface EmailSettingsTabProps {
    form: EmailFormState;
    onChange: (field: keyof EmailFormState, value: string | boolean) => void;
    saving: boolean;
    isEditing: boolean;
    visible: boolean;
}

/**
 * Settings tab content for email channel create/edit dialog.
 * Renders SMTP configuration form fields.
 */
export const EmailSettingsTab: React.FC<EmailSettingsTabProps> = ({
    form,
    onChange,
    saving,
    isEditing,
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
                label="SMTP Host"
                value={form.smtp_host}
                onChange={(e) => onChange('smtp_host', e.target.value)}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="SMTP Port"
                type="number"
                value={form.smtp_port}
                onChange={(e) => onChange('smtp_port', e.target.value)}
                disabled={saving}
                margin="dense"
                inputProps={{ min: 1, max: 65535 }}
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="SMTP Username"
                value={form.smtp_username}
                onChange={(e) => onChange('smtp_username', e.target.value)}
                disabled={saving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="SMTP Password"
                type="password"
                value={form.smtp_password}
                onChange={(e) => onChange('smtp_password', e.target.value)}
                disabled={saving}
                margin="dense"
                placeholder={isEditing ? '(unchanged)' : ''}
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="From Address"
                value={form.from_address}
                onChange={(e) => onChange('from_address', e.target.value)}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="From Name"
                value={form.from_name}
                onChange={(e) => onChange('from_name', e.target.value)}
                disabled={saving}
                margin="dense"
                InputLabelProps={{ shrink: true }}
            />
            <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
                <FormControlLabel
                    sx={{ ml: 0, gap: 1 }}
                    control={
                        <Switch
                            checked={form.use_tls}
                            onChange={(e) => onChange('use_tls', e.target.checked)}
                            disabled={saving}
                            inputProps={{ 'aria-label': 'Toggle use TLS' }}
                        />
                    }
                    label="Use TLS"
                />
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

export default EmailSettingsTab;
