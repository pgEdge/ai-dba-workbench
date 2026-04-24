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
import { Box, TextField, IconButton, Button, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Add as AddIcon, Delete as DeleteIcon } from '@mui/icons-material';
import type { HeaderEntry } from './webhookTypes';
import { getContainedButtonSx, getDeleteIconSx } from '../styles';

export interface WebhookHeadersTabProps {
    headers: HeaderEntry[];
    onAddHeader: () => void;
    onChangeHeader: (id: string, field: 'key' | 'value', value: string) => void;
    onRemoveHeader: (id: string) => void;
    saving: boolean;
    visible: boolean;
}

/**
 * Headers tab for the webhook channel dialog.
 * Allows adding, editing, and removing custom HTTP headers.
 */
const WebhookHeadersTab: React.FC<WebhookHeadersTabProps> = ({
    headers,
    onAddHeader,
    onChangeHeader,
    onRemoveHeader,
    saving,
    visible,
}) => {
    const theme = useTheme();
    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);

    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            {headers.length > 0 ? (
                headers.map((header) => (
                    <Box
                        key={header.id}
                        sx={{ display: 'flex', gap: 1, alignItems: 'center', mb: 1 }}
                    >
                        <TextField
                            label="Key"
                            value={header.key}
                            onChange={(e) => { onChangeHeader(header.id, 'key', e.target.value); }}
                            disabled={saving}
                            size="small"
                            sx={{ flex: 1 }}
                            InputLabelProps={{ shrink: true }}
                        />
                        <TextField
                            label="Value"
                            value={header.value}
                            onChange={(e) => { onChangeHeader(header.id, 'value', e.target.value); }}
                            disabled={saving}
                            size="small"
                            sx={{ flex: 1 }}
                            InputLabelProps={{ shrink: true }}
                        />
                        <IconButton
                            size="small"
                            onClick={() => { onRemoveHeader(header.id); }}
                            aria-label="remove header"
                            sx={deleteIconSx}
                            disabled={saving}
                        >
                            <DeleteIcon fontSize="small" />
                        </IconButton>
                    </Box>
                ))
            ) : (
                <Typography
                    color="text.secondary"
                    sx={{ fontSize: '1rem', mb: 2, mt: 1 }}
                >
                    No custom headers configured.
                </Typography>
            )}
            <Button
                size="small"
                variant="contained"
                startIcon={<AddIcon />}
                onClick={onAddHeader}
                disabled={saving}
                sx={containedButtonSx}
            >
                Add Header
            </Button>
        </Box>
    );
};

export default WebhookHeadersTab;
