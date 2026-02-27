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
    Accordion,
    AccordionSummary,
    AccordionDetails,
    Typography,
    TextField,
    MenuItem,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import {
    ServerFormData,
    FieldChangeHandler,
    SSL_MODES,
} from './ServerDialog.types';
import {
    sslAccordionSx,
    accordionSummarySx,
    sslLabelSx,
} from './ServerDialog.styles';
import { getSelectFieldSx } from '../shared/formStyles';

interface SSLSettingsProps {
    formData: ServerFormData;
    isEditMode: boolean;
    isSaving: boolean;
    expanded: boolean;
    onExpandedChange: (expanded: boolean) => void;
    onFieldChange: FieldChangeHandler;
}

/**
 * SSLSettings renders the SSL configuration accordion section.
 * Includes SSL mode selection and certificate path fields.
 */
const SSLSettings: React.FC<SSLSettingsProps> = ({
    formData,
    isEditMode,
    isSaving,
    expanded,
    onExpandedChange,
    onFieldChange,
}) => {
    const selectFieldSx = getSelectFieldSx(isEditMode ? 'background.default' : 'background.paper');

    return (
        <Accordion
            expanded={expanded}
            onChange={(_, isExpanded) => onExpandedChange(isExpanded)}
            elevation={0}
            sx={sslAccordionSx}
        >
            <AccordionSummary
                expandIcon={<ExpandMoreIcon />}
                sx={accordionSummarySx}
            >
                <Typography variant="subtitle2" sx={sslLabelSx}>
                    SSL Settings
                </Typography>
            </AccordionSummary>
            <AccordionDetails sx={{ pt: 0 }}>
                {/* SSL Mode */}
                <TextField
                    select
                    fullWidth
                    label="SSL Mode"
                    value={formData.ssl_mode}
                    onChange={(e) => onFieldChange('ssl_mode', e.target.value)}
                    disabled={isSaving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={selectFieldSx}
                >
                    {SSL_MODES.map((mode) => (
                        <MenuItem key={mode.value} value={mode.value}>
                            {mode.label}
                        </MenuItem>
                    ))}
                </TextField>

                {/* SSL Certificate Path */}
                <TextField
                    fullWidth
                    label="SSL Certificate Path"
                    value={formData.ssl_cert_path}
                    onChange={(e) => onFieldChange('ssl_cert_path', e.target.value)}
                    disabled={isSaving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={selectFieldSx}
                />

                {/* SSL Key Path */}
                <TextField
                    fullWidth
                    label="SSL Key Path"
                    value={formData.ssl_key_path}
                    onChange={(e) => onFieldChange('ssl_key_path', e.target.value)}
                    disabled={isSaving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={selectFieldSx}
                />

                {/* SSL Root Certificate Path */}
                <TextField
                    fullWidth
                    label="SSL Root Certificate Path"
                    value={formData.ssl_root_cert_path}
                    onChange={(e) => onFieldChange('ssl_root_cert_path', e.target.value)}
                    disabled={isSaving}
                    margin="dense"
                    InputLabelProps={{ shrink: true }}
                    sx={selectFieldSx}
                />
            </AccordionDetails>
        </Accordion>
    );
};

export default SSLSettings;
