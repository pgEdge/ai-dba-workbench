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
import {
    Box,
    TextField,
    Switch,
    Typography,
    FormControlLabel,
} from '@mui/material';
import type { EmailFormState } from './emailTypes';

export interface EmailSettingsTabProps {
    form: EmailFormState;
    onChange: (field: keyof EmailFormState, value: string | boolean) => void;
    saving: boolean;
    isEditing: boolean;
    visible: boolean;
    /**
     * True when the channel being edited has an SMTP username
     * configured server-side. Used to show a "leave blank to keep
     * existing" hint, since the server redacts the actual value.
     */
    smtpUsernameConfigured?: boolean;
    /**
     * True when the channel being edited has an SMTP password
     * configured server-side.
     */
    smtpPasswordConfigured?: boolean;
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
    smtpUsernameConfigured = false,
    smtpPasswordConfigured = false,
}) => {
    // The username and password are redacted by the server. When
    // editing a channel that already has a value stored, surface a
    // placeholder so users know submitting an empty form value will
    // preserve it. When no value is configured (e.g. an edit dialog
    // for a channel that was created without SMTP credentials), the
    // placeholder must stay empty so we do not imply a stored secret.
    const usernamePlaceholder = isEditing && smtpUsernameConfigured
        ? 'Leave blank to keep existing'
        : '';
    const passwordPlaceholder = isEditing && smtpPasswordConfigured
        ? 'Leave blank to keep existing'
        : '';

    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            <TextField
                autoFocus
                fullWidth
                label="Name"
                value={form.name}
                onChange={(e) => { onChange('name', e.target.value); }}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="Description"
                value={form.description}
                onChange={(e) => { onChange('description', e.target.value); }}
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
                onChange={(e) => { onChange('smtp_host', e.target.value); }}
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
                onChange={(e) => { onChange('smtp_port', e.target.value); }}
                disabled={saving}
                margin="dense"
                inputProps={{ min: 1, max: 65535 }}
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="SMTP Username"
                value={form.smtp_username}
                onChange={(e) => { onChange('smtp_username', e.target.value); }}
                disabled={saving}
                margin="dense"
                placeholder={usernamePlaceholder}
                helperText={
                    isEditing && smtpUsernameConfigured
                        ? 'A username is configured. Leave blank to keep it unchanged.'
                        : undefined
                }
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="SMTP Password"
                type="password"
                value={form.smtp_password}
                onChange={(e) => { onChange('smtp_password', e.target.value); }}
                disabled={saving}
                margin="dense"
                placeholder={passwordPlaceholder}
                helperText={
                    isEditing && smtpPasswordConfigured
                        ? 'A password is configured. Leave blank to keep it unchanged.'
                        : undefined
                }
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="From Address"
                value={form.from_address}
                onChange={(e) => { onChange('from_address', e.target.value); }}
                disabled={saving}
                margin="dense"
                required
                InputLabelProps={{ shrink: true }}
            />
            <TextField
                fullWidth
                label="From Name"
                value={form.from_name}
                onChange={(e) => { onChange('from_name', e.target.value); }}
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
                            onChange={(e) => { onChange('use_tls', e.target.checked); }}
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
                            onChange={(e) => { onChange('enabled', e.target.checked); }}
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
                            onChange={(e) => { onChange('is_estate_default', e.target.checked); }}
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
