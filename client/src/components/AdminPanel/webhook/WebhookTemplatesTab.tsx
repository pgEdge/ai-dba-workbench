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
import { Box, TextField, Typography } from '@mui/material';
import { WebhookFormState } from './webhookTypes';
import {
    DEFAULT_ALERT_FIRE_TEMPLATE,
    DEFAULT_ALERT_CLEAR_TEMPLATE,
    DEFAULT_REMINDER_TEMPLATE,
} from './webhookHelpers';

export interface WebhookTemplatesTabProps {
    form: WebhookFormState;
    onChange: (field: keyof WebhookFormState, value: string) => void;
    saving: boolean;
    visible: boolean;
}

/**
 * Templates tab for the webhook channel dialog.
 * Allows configuring Go templates for different notification types.
 */
const WebhookTemplatesTab: React.FC<WebhookTemplatesTabProps> = ({
    form,
    onChange,
    saving,
    visible,
}) => {
    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            <Typography
                variant="body2"
                color="text.secondary"
                sx={{ mb: 2, mt: 1 }}
            >
                Templates use{' '}
                <a
                    href="https://pkg.go.dev/text/template"
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: 'inherit' }}
                >
                    Go template syntax
                </a>
                . Leave blank to use the
                defaults shown as placeholders. Available variables: AlertID,
                AlertTitle, AlertDescription, Severity, SeverityColor,
                SeverityEmoji, ServerName, ServerHost, ServerPort, DatabaseName,
                MetricName, MetricValue, ThresholdValue, Operator, TriggeredAt,
                ClearedAt, Duration, ReminderCount, Timestamp.
            </Typography>
            <TextField
                fullWidth
                label="Alert Fire Template"
                value={form.template_alert_fire}
                onChange={(e) => onChange('template_alert_fire', e.target.value)}
                disabled={saving}
                margin="dense"
                multiline
                rows={12}
                placeholder={DEFAULT_ALERT_FIRE_TEMPLATE}
                InputLabelProps={{ shrink: true }}
                InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
            />
            <TextField
                fullWidth
                label="Alert Clear Template"
                value={form.template_alert_clear}
                onChange={(e) => onChange('template_alert_clear', e.target.value)}
                disabled={saving}
                margin="dense"
                multiline
                rows={10}
                placeholder={DEFAULT_ALERT_CLEAR_TEMPLATE}
                InputLabelProps={{ shrink: true }}
                InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
            />
            <TextField
                fullWidth
                label="Alert Reminder Template"
                value={form.template_reminder}
                onChange={(e) => onChange('template_reminder', e.target.value)}
                disabled={saving}
                margin="dense"
                multiline
                rows={10}
                placeholder={DEFAULT_REMINDER_TEMPLATE}
                InputLabelProps={{ shrink: true }}
                InputProps={{ sx: { fontFamily: 'monospace', fontSize: '0.8rem' } }}
            />
        </Box>
    );
};

export default WebhookTemplatesTab;
