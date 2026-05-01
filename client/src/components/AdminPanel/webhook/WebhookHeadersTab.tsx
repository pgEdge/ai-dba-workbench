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
    /**
     * Names of headers already stored on the server for this channel.
     * Surfaced to the user when editing a channel that has stored
     * headers but no header VALUES are available (the server redacts
     * them; see issue #187). When non-empty, a helper line lists the
     * names and explains the merge semantics: re-enter all to
     * replace, leave blank to keep. The component does NOT pre-fill
     * the form fields with these names — they're informational only.
     */
    configuredHeaderNames?: string[];
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
    configuredHeaderNames = [],
}) => {
    const theme = useTheme();
    const containedButtonSx = getContainedButtonSx(theme);
    const deleteIconSx = getDeleteIconSx(theme);

    const hasConfiguredNames = configuredHeaderNames.length > 0;
    // Helper line shown only when editing a channel that already has
    // stored headers and the user has not yet touched the headers
    // tab. Pluralise "header" for readability when there is more than
    // one configured name.
    const configuredHelperText = hasConfiguredNames
        ? `${configuredHeaderNames.length} custom `
            + `header${configuredHeaderNames.length === 1 ? '' : 's'} `
            + `configured: ${configuredHeaderNames.join(', ')} — `
            + 're-enter all to replace, leave blank to keep'
        : null;

    return (
        <Box sx={{ display: visible ? 'block' : 'none' }}>
            {configuredHelperText && (
                <Typography
                    color="text.secondary"
                    sx={{ fontSize: '0.875rem', mb: 2, mt: 1 }}
                >
                    {configuredHelperText}
                </Typography>
            )}
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
                // The "No custom headers configured." message would
                // mislead the user when the server actually has
                // headers stored but redacted (issue #187); the
                // configured-names helper above already conveys that
                // state, so suppress this empty-state line in that case.
                !hasConfiguredNames && (
                    <Typography
                        color="text.secondary"
                        sx={{ fontSize: '1rem', mb: 2, mt: 1 }}
                    >
                        No custom headers configured.
                    </Typography>
                )
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
